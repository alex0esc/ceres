package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)


type Client struct {
	endpoint        *Endpoint
	modelName       string
	ExtraBody       map[string]any
	ReasoningEffort openai.ReasoningEffort
	SystemPrompt    string
	chatHistory     []responses.ResponseInputItemUnionParam
	Tools           map[string]tool.Tool

	// prevents endless tool-call loops if the model gets "stuck"
	MaxToolIterations int

	// Thread-sichere Verwaltung des aktiven Contexts für Interrupts
    mutex           sync.Mutex
    cancelActiveRun context.CancelFunc

    //dynamically changeable onEvent function
    onEvent func(Token)

    //usage
    TotalTokens int64

    //compression
    CompressionThreshold int64
	NumMessagesToKeep int
    CompressionPromt string
}


func NewClient(endpoint *Endpoint, modelName string) *Client {
	return &Client{
		modelName:         modelName,
		ReasoningEffort:   responses.ReasoningEffortNone,
		SystemPrompt:      "Your are Ceres a helpful AI assistent.",
		MaxToolIterations: 30,
		endpoint: endpoint,
		CompressionThreshold: 200000,
		CompressionPromt: "Summerize your current chat history. Make it precise and short.",
		NumMessagesToKeep: 8,
	}
}


// executes all items found in a response output in their original order:
// message-text items are appended to chatHistory as assistant turns,
// function-call items are executed and their call+result appended.
// Returns the concatenated assistant text found in this output, whether
// any tool calls were found, and an error if one occurred.
func (client *Client) handleToolCalls(ctx context.Context, output []responses.ResponseOutputItemUnion, handle handles.AgentHandle) (bool, error) {
	foundCall := false

	for _, item := range output {
		switch v := item.AsAny().(type) {

		//put reasoning in the chat history for the bot to have more context
		case responses.ResponseReasoningItem:
    		var converted responses.ResponseInputItemUnion
    		if err := json.Unmarshal([]byte(v.RawJSON()), &converted); err != nil {
    		    return false, fmt.Errorf("failed to convert reasoning item: %w", err)
    		}
    		client.chatHistory = append(client.chatHistory, converted.ToParam())

			
		case responses.ResponseOutputMessage:
			var msgText strings.Builder
			for _, part := range v.Content {
				if t, ok := part.AsAny().(responses.ResponseOutputText); ok {
					msgText.WriteString(t.Text)
				}
			}
			if msgText.Len() > 0 {
				client.appendAssistentMessage(msgText.String())
			}

		case responses.ResponseFunctionToolCall:
			foundCall = true

			client.chatHistory = append(client.chatHistory,
				responses.ResponseInputItemParamOfFunctionCall(v.Arguments, v.CallID, v.Name),
			)

			tool, ok := client.Tools[v.Name]
			var result string
			if !ok {
				result = fmt.Sprintf(`{"error": "unknown tool %q"}`, v.Name)
			} else {
				out, err := tool.Handler()(ctx, v.Arguments, handle)
				if err != nil {
					result = fmt.Sprintf(`{"error": %q}`, err.Error())
				} else {
					result = out
				}
			}

			client.chatHistory = append(client.chatHistory,
				responses.ResponseInputItemParamOfFunctionCallOutput(v.CallID, result),
			)
		}
	}
	return foundCall, nil
}



// get answer streamed; onEvent is called for every text chunk and every
// tool-call lifecycle event that occurs while generating the response
// HINT do not execute this at the same time if another AskStream call or CompressHistory call is running
func (client *Client) AskStream(ctx context.Context, userPrompt string, roleSystem bool, handle handles.AgentHandle) (string, error) {
	if roleSystem {
		client.appendSystemMessage(userPrompt)
	} else {
		client.appendUserMessage(userPrompt)
	}

	//allow cancable context with thread safety
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	client.mutex.Lock()
	client.cancelActiveRun = cancel
	client.mutex.Unlock()

	defer func() {
		client.mutex.Lock()
		client.cancelActiveRun = nil
		client.mutex.Unlock()
	}()
		

	// tool names we've already announced as "started" in the current round,
	// keyed by output index, so we don't emit StreamEventToolCallStarted twice
	// for the same item if the SDK emits multiple related events for it
	var fullAnswer strings.Builder
	for i := 0; i < client.MaxToolIterations; i++ {
		if client.TotalTokens > client.CompressionThreshold {
			client.CompressHistory(runCtx)
		} 

		stream := client.endpoint.client.Responses.NewStreaming(runCtx, responses.ResponseNewParams{
			Model:        client.modelName,
			Instructions: openai.String(client.SystemPrompt),
			Input: responses.ResponseNewParamsInputUnion{
				OfInputItemList: client.chatHistory,
			},
			Reasoning: responses.ReasoningParam{
				Effort: client.ReasoningEffort,
			},
			Tools: client.toolParams(),
		},
		client.requestOpts()...
		)

		var tempAnswer strings.Builder
		var finalOutput []responses.ResponseOutputItemUnion
		var lastFcName string
		for stream.Next() {
			event := stream.Current()
			switch e := event.AsAny().(type) {

			case responses.ResponseTextDeltaEvent:
				client.triggerOnEvent(NewToken(TokenTypeAssistent, e.Delta))
				tempAnswer.WriteString(e.Delta)

			case responses.ResponseOutputItemAddedEvent:
			    if fc, ok := e.Item.AsAny().(responses.ResponseFunctionToolCall); ok {
			        lastFcName = fc.Name
				} 
				//skip reasoning items
				if _, ok := e.Item.AsAny().(responses.ResponseReasoningItem); ok {
					continue
				}
				//mark the end of the current msg generation
				client.triggerOnEvent(NewToken(TokenEndOfSequence, ""))
				tempAnswer.WriteString("\n\n")

			case responses.ResponseFunctionCallArgumentsDoneEvent:
				// fires as soon as a new output item starts; used here to
				call_text := fmt.Sprintf("Calling tool [%s] with arguments %s...", lastFcName, e.Arguments)

				tempAnswer.WriteString(call_text)
				client.triggerOnEvent(NewToken(TokenTypeSystemInfo, call_text))

			case responses.ResponseCompletedEvent:
				// contains the final, complete output including finished function calls
				finalOutput = e.Response.Output
				client.TotalTokens = e.Response.Usage.TotalTokens
			}
		}

		fullAnswer.WriteString(tempAnswer.String())
		
		if errors.Is(stream.Err(), context.Canceled) {
			client.appendAssistentMessage(tempAnswer.String())
			return fullAnswer.String(), nil
		}
		
		if err := stream.Err(); err != nil {
			return "", err
		}

		hadToolCalls, err := client.handleToolCalls(runCtx, finalOutput, handle)
		if err != nil {
			return "", fmt.Errorf("Error handling tool calls: %w", err)
		}

		if !hadToolCalls {
			return fullAnswer.String(), nil
		}
		// otherwise: history contains call + result -> next streaming round
	}

	return "", fmt.Errorf("max tool iterations (%d) exceeded", client.MaxToolIterations)
}
