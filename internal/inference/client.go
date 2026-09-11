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
	UseReasoningSummary      bool

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


	for _, item := range output {
		switch v := item.AsAny().(type) {

		//put reasoning in the chat history for the bot to have more context
		case responses.ResponseReasoningItem:    		
			if client.UseReasoningSummary {
				for _, part := range v.Summary {
					fullAnswer.Push(history.Entry{ Type: history.EntryTypeReasoning, Content: []string{ part.Text }})
				}
			} else {
				for _, part := range v.Content {
					fullAnswer.Push(history.Entry{ Type: history.EntryTypeReasoning, Content: []string{ part.Text }})
				}
			}
			client.appendReasoningItem(v)

			
		case responses.ResponseOutputMessage:
			for _, part := range v.Content {
				if t, ok := part.AsAny().(responses.ResponseOutputText); ok {
					client.appendAssistentMessage(t.Text)
					fullAnswer.Push(history.Entry{ Type: history.EntryTypeAssistent, Content: []string{ t.Text }})
				}
			}


		case responses.ResponseFunctionToolCall:
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
	return foundCall
}



// get answer streamed; onEvent is called for every text chunk and every
// tool-call lifecycle event that occurs while generating the response
// HINT do not execute this at the same time if another AskStream call or CompressHistory call is running
func (client *Client) AskStream(ctx context.Context, prompt handles.Prompt, handle handles.AgentHandle) (*history.History, error, bool) {
	client.AppendUserPrompt(prompt)

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


		var finalOutput []responses.ResponseOutputItemUnion
		for stream.Next() {
			event := stream.Current()
			switch e := event.AsAny().(type) {
			case responses.ResponseReasoningSummaryTextDeltaEvent:
				if !client.UseReasoningSummary {
					continue
				}
				token := history.Token {Type: history.TokenTypeReasoning, Content: []string { event.Delta } }
				client.partialAnswer = append(client.partialAnswer, token)
				client.triggerOnEvent(token)				

			case responses.ResponseReasoningTextDeltaEvent: 
				if client.UseReasoningSummary {
					continue
				}
				token := history.Token {Type: history.TokenTypeReasoning, Content: []string { event.Delta } }
				client.partialAnswer = append(client.partialAnswer, token)
				client.triggerOnEvent(token)				

			case responses.ResponseTextDeltaEvent:
				token := history.Token {Type: history.TokenTypeAssistent, Content: []string { event.Delta } }
				client.partialAnswer = append(client.partialAnswer, token)
				client.triggerOnEvent(token)				

			case responses.ResponseOutputItemAddedEvent: 
				if _, ok := e.Item.AsAny().(responses.ResponseOutputMessage); ok || e.Item.Type == "message" {
					token := history.Token { Type: history.TokenEndOfSequence }
					client.partialAnswer = append(client.partialAnswer, token)
					client.triggerOnEvent(token)
				}

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
				client.appendReasoningText(reason.String())
			}
			if normal.Len() > 0 {
				client.appendAssistentMessage(normal.String())
			}
			return &fullAnswer, nil, true
		}

		
		if err := stream.Err(); err != nil {
			return nil, err, false
		}


		client.partialAnswer = nil
		if !client.handleToolCalls(runCtx, finalOutput, handle, &fullAnswer) {
			return &fullAnswer, nil, false
		}
	}

	return nil, fmt.Errorf("max tool iterations (%d) exceeded", client.MaxToolIterations), false
}
