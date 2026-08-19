package inference

import (
	"context"
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
	tools           map[string]tool.Tool
	toolParams      []responses.ToolUnionParam

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
		CompressionPromt: "Your task is to summerize the current chat. Make it precise and dont leave anything important out.",
		NumMessagesToKeep: 8,
		tools: make(map[string]tool.Tool),
	}
}


// executes all items found in a response output in their original order:
// message-text items are appended to chatHistory as assistant turns,
// function-call items are executed and their call+result appended.
// Returns the concatenated assistant text found in this output, whether
// any tool calls were found, and an error if one occurred.
func (client *Client) handleToolCalls(ctx context.Context, output []responses.ResponseOutputItemUnion, handle handles.AgentHandle, tempAnswer *strings.Builder) (bool, error) {
	foundCall := false


	var message strings.Builder
	for _, item := range output {
		switch v := item.AsAny().(type) {

		//put reasoning in the chat history for the bot to have more context
		case responses.ResponseReasoningItem:    		
			summary := ""
			for _, part := range v.Content {
				summary += part.Text
			}
			if summary == "" {
				for _, part := range v.Summary {
					summary += part.Text
				}
			}
			if summary != "" {
				message.WriteString("<think>\n")
				message.WriteString(summary)
				message.WriteString("\n</think>\n\n")
			}

			
		case responses.ResponseOutputMessage:
			for _, part := range v.Content {
				if t, ok := part.AsAny().(responses.ResponseOutputText); ok {
					message.WriteString(t.Text)
				}
			}

			if message.Len() > 0 {
				client.appendAssistentMessage(message.String())
				message.Reset()
			}

		case responses.ResponseFunctionToolCall:
			if message.Len() > 0 {
				client.appendAssistentMessage(message.String())
				message.Reset()
			}
			
			foundCall = true

			client.chatHistory = append(client.chatHistory,
				responses.ResponseInputItemParamOfFunctionCall(v.Arguments, v.CallID, v.Name),
			)

			tool, ok := client.tools[v.Name]

			// fires as soon as a new output item starts; used here to
			call_text := fmt.Sprintf("Calling tool [%s] with arguments %s...", v.Name, v.Arguments)
			tempAnswer.WriteString(call_text);
			client.triggerOnEvent(NewToken(TokenTypeSystemInfo, call_text))
			client.triggerOnEvent(NewToken(TokenEndOfSequence, ""))

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
	if message.Len() > 0 {
		client.appendAssistentMessage(message.String())
		message.Reset()
	}
	return foundCall, nil
}



// get answer streamed; onEvent is called for every text chunk and every
// tool-call lifecycle event that occurs while generating the response
// HINT do not execute this at the same time if another AskStream call or CompressHistory call is running
func (client *Client) AskStream(ctx context.Context, userPrompt string, handle handles.AgentHandle) (string, error) {
	client.appendUserMessage(userPrompt)

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
			Tools: client.toolParams,
		},
		client.requestOpts()...
		)

		var tempAnswer strings.Builder
		var finalOutput []responses.ResponseOutputItemUnion
		for stream.Next() {
			event := stream.Current()
			switch e := event.AsAny().(type) {
			case responses.ResponseReasoningTextDeltaEvent, responses.ResponseReasoningSummaryTextDeltaEvent:
				client.triggerOnEvent(NewToken(TokenTypeReasoning, event.Delta))				

			case responses.ResponseTextDeltaEvent:
				client.triggerOnEvent(NewToken(TokenTypeAssistent, e.Delta))
				tempAnswer.WriteString(e.Delta)

			case responses.ResponseOutputItemAddedEvent: 
				if _, ok := e.Item.AsAny().(responses.ResponseReasoningItem); ok || e.Item.Type == "reasoningmessage" {
					continue
				}

				if _, ok := e.Item.AsAny().(responses.ResponseFunctionToolCall); ok {
					client.triggerOnEvent(NewToken(TokenEndOfSequence, ""))
					continue
				}
				//mark the end of the current msg generation
				client.triggerOnEvent(NewToken(TokenEndOfSequence, ""))
				tempAnswer.WriteString("\n\n")

			case responses.ResponseCompletedEvent:
				// contains the final, complete output including finished function calls
				finalOutput = e.Response.Output
				client.TotalTokens = e.Response.Usage.TotalTokens
			}
		}

		if errors.Is(stream.Err(), context.Canceled) {
			client.appendAssistentMessage(tempAnswer.String())
			fullAnswer.WriteString(tempAnswer.String())
			return fullAnswer.String(), nil
		}
		
		if err := stream.Err(); err != nil {
			return "", err
		}

		hadToolCalls, err := client.handleToolCalls(runCtx, finalOutput, handle, &tempAnswer)
		if err != nil {
			return "", fmt.Errorf("Error handling tool calls: %w", err)
		}

		fullAnswer.WriteString(tempAnswer.String())

		if !hadToolCalls {
			return fullAnswer.String(), nil
		}
	}

	return "", fmt.Errorf("max tool iterations (%d) exceeded", client.MaxToolIterations)
}
