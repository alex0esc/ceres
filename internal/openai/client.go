package openai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/alex0esc/ceres/pkg/config"
	"github.com/alex0esc/ceres/pkg/tools"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)


type Client struct {
	endpoint        *Endpoint
	modelName       string
	ReasoningEffort openai.ReasoningEffort
	SystemPrompt    string
	chatHistory     []responses.ResponseInputItemUnionParam
	Tools           map[string]tools.Tool

	// prevents endless tool-call loops if the model gets "stuck"
	MaxToolIterations int

	// Thread-sichere Verwaltung des aktiven Contexts für Interrupts
    mutex           sync.Mutex
    cancelActiveRun context.CancelFunc

    //dynamically changeable onEvent function
    onEvent func(Token)

    //usage
    TotalTokens int64
}


func NewClient(endpoint *Endpoint, modelName string) *Client {
	return &Client{
		modelName:         modelName,
		ReasoningEffort:   responses.ReasoningEffortNone,
		SystemPrompt:      "Your are Ceres a helpful AI assistent!",
		MaxToolIterations: config.ReadEntry(tools.GetToolConfig(), "max_tool_iterations", 30),
		endpoint: endpoint,
	}
}




// builds the tool list for the request payload from client.Tools
func (client *Client) toolParams() []responses.ToolUnionParam {
	if len(client.Tools) == 0 {
		return nil
	}
	params := make([]responses.ToolUnionParam, 0, len(client.Tools))
	for _, t := range client.Tools {
		tp := responses.ToolParamOfFunction(t.Name(), t.Parameters(), true) // strict mode
		tp.OfFunction.Description = openai.String(t.Description())
		params = append(params, tp)
	}
	return params
}


// executes all items found in a response output in their original order:
// message-text items are appended to chatHistory as assistant turns,
// function-call items are executed and their call+result appended.
// Returns the concatenated assistant text found in this output, whether
// any tool calls were found, and an error if one occurred.
func (client *Client) handleToolCalls(ctx context.Context, output []responses.ResponseOutputItemUnion) (bool, error) {
	foundCall := false

	for _, item := range output {
		switch v := item.AsAny().(type) {

		//put reasoning in the chat history for the bot to have more context
		case responses.ResponseReasoningItem:
    		client.appendReasoningSummary(v)

			
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
				out, err := tool.Handler()(ctx, v.Arguments)
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
func (client *Client) AskStream(ctx context.Context, userPrompt string) (string, error) {
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
		})

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
				if tempAnswer.Len() > 0 {
					//mark the end of the current msg generation
					client.triggerOnEvent(NewToken(TokenEndOfSequence, ""))
					tempAnswer.WriteString("\n\n")
				}

			case responses.ResponseFunctionCallArgumentsDoneEvent:
				// fires as soon as a new output item starts; used here to
				call_text := fmt.Sprintf("Calling tool [%s] with arguments %s...", lastFcName, e.Arguments)

				tempAnswer.WriteString(call_text)
				client.triggerOnEvent(NewToken(TokenTypeToolCall, call_text))

			case responses.ResponseCompletedEvent:
				// contains the final, complete output including finished function calls
				finalOutput = e.Response.Output
				client.TotalTokens = e.Response.Usage.TotalTokens
			}
		}
		//mark the end of the current msg generation
		if fullAnswer.Len() > 0 {
			client.triggerOnEvent(NewToken(TokenEndOfSequence, ""))
			fullAnswer.WriteString("\n\n")
		}		

		fullAnswer.WriteString(tempAnswer.String())
		
		if errors.Is(stream.Err(), context.Canceled) {
			client.appendAssistentMessage(tempAnswer.String())
			return fullAnswer.String(), nil
		}
		
		if err := stream.Err(); err != nil {
			return "", err
		}

		hadToolCalls, err := client.handleToolCalls(runCtx, finalOutput)
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



// interrupts the client while streaming and tells the modell
func (client *Client) Interrupt() {
    client.mutex.Lock()
    defer client.mutex.Unlock()
    if client.cancelActiveRun != nil {
        client.cancelActiveRun()
        client.cancelActiveRun = nil 
    }
}

