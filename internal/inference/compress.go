package inference

import (
	"context"
	"fmt"
	"strings"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// CompressHistory compresses older messages in the history when total token count exceeds limits.
// HINT do not execute this at the same time if another AskStream call or CompressHistory call is running
func (client *Client) CompressHistory(ctx context.Context) error {
	totalMessages := len(client.History.Entries)

	// Return early if there are not enough messages to trigger compression
	if totalMessages <= client.NumMessagesToKeep {
		return nil
	}

	client.mutex.Lock()
	if client.cancelActiveRun == nil {

		runCtx, cancel := context.WithCancel(ctx)
		ctx = runCtx
		defer cancel()

		client.cancelActiveRun = cancel

		defer func() {
			client.mutex.Lock()
			client.cancelActiveRun = nil
			client.mutex.Unlock()
		}()		
	}

	client.isCompressing = true
	defer func() {
		client.mutex.Lock()
		client.isCompressing = false
		client.mutex.Unlock()
	}()
	client.mutex.Unlock()

	

	cutoff := totalMessages - client.NumMessagesToKeep
	toCompress := client.History.Entries[:cutoff]
	toKeep := client.History.Entries[cutoff:]

	if client.CompressionPrompt == "" {
		return fmt.Errorf("There is no compression promt given for the client with model %s", client.modelName)
	}

	// 1. Only the older entries are summarized; the recent ones are kept verbatim
	// and re-appended afterwards, so they are not sent to the model here.
	inputItems := entriesToItems(toCompress)
	promptMsg := responses.ResponseInputItemParamOfMessage(client.CompressionPrompt, responses.EasyInputMessageRoleUser)
	promptMsg.OfMessage.Type = "message"
	inputItems = append(inputItems, promptMsg)

	// 2. Loop with tools left enabled. If the model tries to call a tool, echo the
	// call back and reject it via a function_call_output error so it retries with
	// plain text. The first turn that contains no tool call is the summary.
	var summary string
	for i := 0; i < client.MaxToolIterations; i++ {
		resp, err := client.endpoint.client.Responses.New(ctx, responses.ResponseNewParams{
			Model:        client.modelName,
			Instructions: openai.String(client.SystemPrompt),
			Input: responses.ResponseNewParamsInputUnion{
				OfInputItemList: inputItems,
			},
			Reasoning: responses.ReasoningParam{
				Effort: client.ReasoningEffort,
			},
			Tools: client.toolParams,
		},
		client.requestOpts()...
		)
		if err != nil {
			return fmt.Errorf("failed to compress history: %w", err)
		}

		var summaryBuilder strings.Builder
		toolCallFound := false
		for _, item := range resp.Output {
			switch v := item.AsAny().(type) {
			case responses.ResponseFunctionToolCall:
				toolCallFound = true
				inputItems = append(inputItems,
					responses.ResponseInputItemParamOfFunctionCall(v.Arguments, v.CallID, v.Name),
					responses.ResponseInputItemParamOfFunctionCallOutput(v.CallID,
						`{"error":"tool calls are disabled during compression; output the summary text now"}`),
				)
			case responses.ResponseOutputMessage:
				for _, part := range v.Content {
					if t, ok := part.AsAny().(responses.ResponseOutputText); ok {
						summaryBuilder.WriteString(t.Text)
					}
				}
			}
		}

		if !toolCallFound {
			summary = summaryBuilder.String()
			break
		}
	}

	if summary == "" {
		return fmt.Errorf("compression yielded an empty summary")
	}

	// 4. Rebuild chat history: [Summary turn] + [unmodified recent messages]
	newHistory := make([]history.Entry, 0, 1+len(toKeep))

	summaryStr := "[Summary of previous conversation]\n\n" + summary + "\n\n[End of summary]"
	newHistory = append(newHistory, history.Entry{Type: history.EntryTypeUser, Text: summaryStr})
	newHistory = append(newHistory, toKeep...)

	// Replace the history; TotalTokens (now part of the history) resets to 0 and
	// is set again by the next response's usage.
	client.mutex.Lock()
	client.History = history.History{Entries: newHistory}
	client.mutex.Unlock()
	// tell the consumer (TUI) to reload the whole view from the new history
	client.triggerOnEvent(history.Token{Type: history.TokenTypeResetChat})
	return nil
}



func (client *Client) IsCompressing() bool {
	client.mutex.Lock()
	compressing := client.isCompressing
	client.mutex.Unlock()
	return compressing
}
