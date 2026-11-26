package inference

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/task"
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
	// History, partialAnswer, isCompressing and subscriptions are guarded by
	// mutex so that snapshots and live token delivery are always consistent.
	history         history.History
	partialAnswer   []history.Token
	subscriptions   map[*Subscription]struct{}

	// tools
	tools           map[string]tool.Tool
	toolParams      []responses.ToolUnionParam
	MaxToolIterations int
	MaxToolResultChars int

	// sync
	// mutex guards History, partialAnswer, isCompressing, subscriptions and
	// cancelActiveRun.
	mutex           sync.Mutex
	cancelActiveRun context.CancelFunc


    // compression
    CompressionThreshold int64
	NumMessagesToKeep int
    CompressionPrompt string
    isCompressing bool
}


// NewClient creates a client for the given endpoint and model. Only the endpoint
// and the model name are required; everything else gets a sensible default.
func NewClient(endpoint *Endpoint, modelName string) *Client {
	return &Client{
		endpoint:             endpoint,
		modelName:            modelName,
		ReasoningEffort:      responses.ReasoningEffortNone,
		SystemPrompt:         "Your are Ceres a helpful AI assistant.",
		MaxToolIterations:    100,
		MaxToolResultChars:   100000,
		CompressionThreshold: 200000,
		NumMessagesToKeep:    8,
		CompressionPrompt: "This is a message injected by the system, your task is to compress the current chat. " +
			"Dont ask any question, do the summary in this turn and keep everything that is relevant.",
		tools: make(map[string]tool.Tool),
	}
}

// executes all items found in a response output in their original order:
// message-text items are appended to the history as assistant turns,
// function-call items are executed and their call+result appended.
// Returns the concatenated assistant text found in this output, whether
// any tool calls were found, and an error if one occurred.
func (client *Client) handleToolCalls(ctx context.Context, output []responses.ResponseOutputItemUnion, handle handles.AgentHandle, fullAnswer *history.History) bool {
	foundCall := false
	var calls []responses.ResponseFunctionToolCall

	// Finalize the completed response in one critical section: clear the
	// streamed partial answer and append the authoritative entries from the
	// response. This way a snapshot can never observe a gap between the
	// streamed deltas and the stored history.
	client.mutex.Lock()
	client.partialAnswer = nil
	for _, item := range output {
		switch v := item.AsAny().(type) {

		//put reasoning in the chat history for the bot to have more context
		case responses.ResponseReasoningItem:
			if client.UseReasoningSummary {
				for _, part := range v.Summary {
					fullAnswer.Push(history.Entry{Type: history.EntryTypeReasoning, Text: part.Text})
				}
			} else {
				for _, part := range v.Content {
					fullAnswer.Push(history.Entry{Type: history.EntryTypeReasoning, Text: part.Text})
				}
			}

			r := &history.Reasoning{
				ID:               v.ID,
				EncryptedContent: v.EncryptedContent,
			}
			for _, si := range v.Summary {
				r.Summary = append(r.Summary, si.Text)
			}
			for _, ci := range v.Content {
				r.Content = append(r.Content, ci.Text)
			}
			client.history.Push(history.Entry{Type: history.EntryTypeReasoning, Reasoning: r})

		case responses.ResponseOutputMessage:
			for _, part := range v.Content {
				if t, ok := part.AsAny().(responses.ResponseOutputText); ok {
					client.history.Push(history.Entry{Type: history.EntryTypeAssistant, Text: t.Text})
					fullAnswer.Push(history.Entry{Type: history.EntryTypeAssistant, Text: t.Text})
				}
			}

		case responses.ResponseFunctionToolCall:
			foundCall = true

			call := history.Entry{Type: history.EntryTypeToolCall, ToolCall: &history.ToolCall{
				Name:      v.Name,
				Arguments: v.Arguments,
				CallID:    v.CallID,
			}}
			client.history.Push(call)
			fullAnswer.Push(call.Copy())
			client.publishLocked(call.Copy())
			client.publishLocked(history.Token{Type: history.TokenTypeEndOfSequence})
			calls = append(calls, v)
		}
	}
	client.mutex.Unlock()

	// Execute the calls outside the lock; tool handlers can be slow.
	for _, v := range calls {
		t, ok := client.tools[v.Name]

		var result string
		if !ok {
			result = fmt.Sprintf(`{"error": "unknown tool %q"}`, v.Name)
		} else {
			out, err := t.Handler()(ctx, v.Arguments, handle)
			if err != nil {
				result = fmt.Sprintf(`{"error": %q}`, err.Error())
			} else if len_out := len(out); len_out > client.MaxToolResultChars {
				result = fmt.Sprintf(`{"error": tool result is too large, max is %d, but was %d}`, client.MaxToolResultChars, len_out)
			} else {
				result = out
			}
		}

		resultEntry := history.Entry{Type: history.EntryTypeToolResult, ToolResult: &history.ToolResult{
			CallID: v.CallID,
			Output: result,
		}}

		client.mutex.Lock()
		client.history.Push(resultEntry)
		client.publishLocked(resultEntry.Copy())
		client.publishLocked(history.Token{Type: history.TokenTypeEndOfSequence})
		client.mutex.Unlock()

		fullAnswer.Push(resultEntry.Copy())
	}

	return foundCall
}



// get answer streamed; tokens are published to all subscribers, partialAnswer
// is kept in sync for snapshots. Tool-call lifecycle events are published too.
// HINT do not execute this at the same time if another AskStream call or CompressHistory call is running
func (client *Client) AskStream(ctx context.Context, prompt task.Prompt, handle handles.AgentHandle) (*history.History, error, bool) {
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
	// context size before this answer; the answer's token count is how much the
	// total grew during it (generated output plus any tool results added meanwhile)
	startTokens := client.TotalTokens()
	defer func() {
		client.mutex.Lock()
		client.partialAnswer = nil
		client.mutex.Unlock()
	}()
	for i := 0; i < client.MaxToolIterations; i++ {
		if client.TotalTokens() > client.CompressionThreshold {
			if err := client.CompressHistory(runCtx); err != nil {
				return nil, err, false
			}
		}

		stream := client.endpoint.client.Responses.NewStreaming(runCtx, responses.ResponseNewParams{
			Model:        client.modelName,
			Instructions: openai.String(client.SystemPrompt),
			Input: responses.ResponseNewParamsInputUnion{
				OfInputItemList: entriesToItems(client.history.Entries),
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
				client.emitDelta(history.Token{Type: history.EntryTypeReasoning, Text: event.Delta})

			case responses.ResponseReasoningTextDeltaEvent:
				if client.UseReasoningSummary {
					continue
				}
				client.emitDelta(history.Token{Type: history.EntryTypeReasoning, Text: event.Delta})

			case responses.ResponseTextDeltaEvent:
				client.emitDelta(history.Token{Type: history.EntryTypeAssistant, Text: event.Delta})

			case responses.ResponseOutputItemAddedEvent:
				if _, ok := e.Item.AsAny().(responses.ResponseOutputMessage); ok || e.Item.Type == "message" {
					client.emitDelta(history.Token{Type: history.TokenTypeEndOfSequence})
				}

			case responses.ResponseCompletedEvent:
				// contains the final, complete output including finished function calls
				finalOutput = e.Response.Output
				client.mutex.Lock()
				client.history.TotalTokens = e.Response.Usage.TotalTokens
				client.mutex.Unlock()
			}
		}

		client.publish(history.Token{Type: history.TokenTypeEndOfSequence})

		if errors.Is(stream.Err(), context.Canceled) {
			// Persist the interrupted answer and clear the partial state in one
			// critical section so snapshots do not show it twice or lose it.
			client.mutex.Lock()
			var reason strings.Builder
			var normal strings.Builder
			for _, token := range client.partialAnswer {
				switch token.Type {
				case history.EntryTypeReasoning:
					reason.WriteString(token.Text)
				case history.EntryTypeAssistant:
					normal.WriteString(token.Text)
				}
			}
			if reason.Len() > 0 {
				r := &history.Reasoning{ID: generateReasoningID()}
				if client.UseReasoningSummary {
					r.Summary = []string{reason.String()}
				} else {
					r.Content = []string{reason.String()}
				}
				client.history.Push(history.Entry{Type: history.EntryTypeReasoning, Reasoning: r})
			}
			if normal.Len() > 0 {
				client.history.Push(history.Entry{Type: history.EntryTypeAssistant, Text: normal.String()})
			}
			client.partialAnswer = nil
			client.mutex.Unlock()

			fullAnswer.TotalTokens = max(0, client.TotalTokens()-startTokens)
			return &fullAnswer, nil, true
		}

		
		if err := stream.Err(); err != nil {
			return nil, err, false
		}


		if !client.handleToolCalls(runCtx, finalOutput, handle, &fullAnswer) {
			fullAnswer.TotalTokens = max(0, client.TotalTokens()-startTokens)
			return &fullAnswer, nil, false
		}
	}

	return nil, fmt.Errorf("max tool iterations (%d) exceeded", client.MaxToolIterations), false
}
