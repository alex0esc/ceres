package inference

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/pkg/handles"
	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)


type Client struct {
	endpoint        *Endpoint
	modelName       string
	ExtraBody       map[string]any

	// other
	ReasoningEffort openai.ReasoningEffort
	SystemPrompt    string

	// history
	chatHistory     []responses.ResponseInputItemUnionParam
	partialAnswer   []history.Token
    onEvent func(history.Token)
	

	// tools
	tools           map[string]tool.Tool
	toolParams      []responses.ToolUnionParam
	MaxToolIterations int

	// sync
    mutex           sync.Mutex
    cancelActiveRun context.CancelFunc


    // usage
    TotalTokens int64

    // compression
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
func (client *Client) handleToolCalls(ctx context.Context, output []responses.ResponseOutputItemUnion, handle handles.AgentHandle, fullAnswer *history.History) bool {
	foundCall := false


	var message strings.Builder
	for _, item := range output {
		switch v := item.AsAny().(type) {

		//put reasoning in the chat history for the bot to have more context
		case responses.ResponseReasoningItem:    		
			var summary strings.Builder
			for _, part := range v.Content {
				summary.WriteString(part.Text)
			}
			for _, part := range v.Summary {
				summary.WriteString(part.Text)
			}

			if summary.String() != "" {
				fullAnswer.Push(history.Entry{ Type: history.EntryTypeReasoning, Content: []string{ summary.String() }})
				message.WriteString("<think>\n")
				message.WriteString(summary.String())
				message.WriteString("\n</think>\n\n")
			}

			
		case responses.ResponseOutputMessage:
			var complete strings.Builder
			for _, part := range v.Content {
				if t, ok := part.AsAny().(responses.ResponseOutputText); ok {
					complete.WriteString(t.Text)
				}
			}
			if len(complete.String()) > 0 {
				fullAnswer.Push(history.Entry{ Type: history.EntryTypeAssistent, Content: []string{ complete.String() }})
				message.WriteString(complete.String())
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

			// fires as soon as a new output item starts; used here to
			fullAnswer.Push(history.Entry{ Type: history.EntryTypeToolCall, Content: []string{ v.Name, v.Arguments }})
			client.triggerOnEvent(history.Token {Type: history.TokenTypeToolCall, Content: []string{ v.Name, v.Arguments }})
			client.triggerOnEvent(history.Token {Type: history.TokenEndOfSequence })

			tool, ok := client.tools[v.Name]

			

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

			// fires as soon as a tool call is finished
			fullAnswer.Push(history.Entry{ Type: history.EntryTypeToolResult, Content: []string{ result }})
			client.triggerOnEvent(history.Token {Type: history.TokenTypeToolResult, Content: []string{ result }})
			client.triggerOnEvent(history.Token {Type: history.TokenEndOfSequence })

		}
	}
	if message.Len() > 0 {
		client.appendAssistentMessage(message.String())
		message.Reset()
	}
	return foundCall
}



// get answer streamed; onEvent is called for every text chunk and every
// tool-call lifecycle event that occurs while generating the response
// HINT do not execute this at the same time if another AskStream call or CompressHistory call is running
func (client *Client) AskStream(ctx context.Context, userPrompt string, handle handles.AgentHandle) (*history.History, error) {
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
	var fullAnswer history.History
	defer func() { client.partialAnswer = nil }()
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


		client.partialAnswer = nil
		var finalOutput []responses.ResponseOutputItemUnion
		for stream.Next() {
			event := stream.Current()
			switch e := event.AsAny().(type) {
			case responses.ResponseReasoningTextDeltaEvent, responses.ResponseReasoningSummaryTextDeltaEvent:
				token := history.Token {Type: history.TokenTypeReasoning, Content: []string { event.Delta } }
				client.partialAnswer = append(client.partialAnswer, token)
				client.triggerOnEvent(token)				

			case responses.ResponseTextDeltaEvent:
				token := history.Token {Type: history.TokenTypeAssistent, Content: []string { event.Delta } }
				client.partialAnswer = append(client.partialAnswer, token)
				client.triggerOnEvent(token)				

			case responses.ResponseOutputItemAddedEvent: 
				if _, ok := e.Item.AsAny().(responses.ResponseReasoningItem); ok || e.Item.Type == "reasoningmessage" {
					continue
				}
				token := history.Token { Type: history.TokenEndOfSequence }
				client.partialAnswer = append(client.partialAnswer, token)
				client.triggerOnEvent(token)

			case responses.ResponseCompletedEvent:
				// contains the final, complete output including finished function calls
				finalOutput = e.Response.Output
				client.TotalTokens = e.Response.Usage.TotalTokens
			}
		}

		client.triggerOnEvent(history.Token { Type: history.TokenEndOfSequence })

		if errors.Is(stream.Err(), context.Canceled) {
			var reason strings.Builder
			var normal strings.Builder
			for _, token := range client.partialAnswer {
				switch token.Type {
				case history.TokenTypeReasoning:
					reason.WriteString(token.Content[0])
				case history.TokenTypeAssistent:
					normal.WriteString(token.Content[0])
				}
			}
			if reason.Len() > 0 {
				client.appendAssistentMessage("<think>\n" + reason.String() + "\n</think>\n\n" + normal.String())
			} else if normal.Len() > 0 {
				client.appendAssistentMessage(normal.String())
			}
			return &fullAnswer, nil
		}

		
		if err := stream.Err(); err != nil {
			return nil, err
		}

		if !client.handleToolCalls(runCtx, finalOutput, handle, &fullAnswer) {
			return &fullAnswer, nil
		}
	}

	return nil, fmt.Errorf("max tool iterations (%d) exceeded", client.MaxToolIterations)
}
