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

	client.mutex.Unlock()

	totalMessages := len(client.History.Entries)

	// Return early if there are not enough messages to trigger compression
	if totalMessages <= client.NumMessagesToKeep {
		return nil
	}

	cutoff := totalMessages - client.NumMessagesToKeep
	toCompress := client.History.Entries[:cutoff]
	toKeep := client.History.Entries[cutoff:]

	prompt := client.CompressionPrompt
	if prompt == "" {
		return fmt.Errorf("There is no compression promt given for the client with model %s", client.modelName)
	}

	// 1. Prepare payload for the non-streaming compression call
	inputItems := entriesToItems(toCompress)
	prompt = "[System] " + prompt
	promptMsg := responses.ResponseInputItemParamOfMessage(prompt, responses.EasyInputMessageRoleUser)
	promptMsg.OfMessage.Type = "message"
	inputItems = append(inputItems, promptMsg)
	client.triggerOnEvent(history.Token{ Type: history.EntryTypeUser, Text: prompt })
	client.triggerOnEvent(history.Token{ Type: history.EntryEndOfSequence })

	// 2. Execute synchronous (non-streaming) API request WITHOUT tools
	resp, err := client.endpoint.client.Responses.New(ctx, responses.ResponseNewParams{
		Model:        client.modelName,
		Instructions: openai.String("You are an assistant whose task is to summarize the current chat. Do not ask questions, execute your task in one turn."),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: inputItems,
		},
		Tools: nil, // Pass nil to disable function/tool calls completely
	},
	client.requestOpts()...
	)
	if err != nil {
		return fmt.Errorf("failed to compress history: %w", err)
	}

	// 3. Extract generated summary text from response output
	var summaryBuilder strings.Builder
	for _, item := range resp.Output {
		if msg, ok := item.AsAny().(responses.ResponseOutputMessage); ok {
			for _, part := range msg.Content {
				if t, ok := part.AsAny().(responses.ResponseOutputText); ok {
					summaryBuilder.WriteString(t.Text)
				}
			}
		}
	}

	if summaryBuilder.String() == "" {
		return fmt.Errorf("compression yielded an empty summary")
	}

	

	// 4. Rebuild chat history: [Summary turn] + [unmodified recent messages]
	newHistory := make([]history.Entry, 0, 1+len(toKeep))

	summaryStr := "[Summary of previous conversation]\n\n" + summaryBuilder.String() + "\n\n[End of summary]"
	newHistory = append(newHistory, history.Entry{Type: history.EntryTypeUser, Text: summaryStr})
	newHistory = append(newHistory, toKeep...)

	// Replace the history; TotalTokens (now part of the history) resets to 0 and
	// is set again by the next response's usage.
	client.mutex.Lock()
	client.History = history.History{Entries: newHistory}
	client.mutex.Unlock()
	client.triggerOnEvent(history.Token{ Type: history.EntryTypeUser, Text: summaryStr })
	client.triggerOnEvent(history.Token{ Type: history.EntryEndOfSequence })
	return nil
}
