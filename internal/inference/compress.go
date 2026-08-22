package inference

import (
	"context"
	"fmt"
	"strings"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

// CompressHistory compresses older messages in chatHistory when total token count exceeds limits.
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

	totalMessages := len(client.chatHistory)

	// Return early if there are not enough messages to trigger compression
	if totalMessages <= client.NumMessagesToKeep {
		return nil
	}

	cutoff := totalMessages - client.NumMessagesToKeep
	toCompress := client.chatHistory[:cutoff]
	toKeep := client.chatHistory[cutoff:]

	prompt := client.CompressionPromt
	if prompt == "" {
		return fmt.Errorf("There is no compression promt given for the client with model %s.", client.modelName)
	}

	// 1. Prepare payload for the non-streaming compression call
	inputItems := make([]responses.ResponseInputItemUnionParam, 0, len(toCompress)+1)
	inputItems = append(inputItems, toCompress...)

	prompt = "[System]: " + prompt
	promptMsg := responses.ResponseInputItemParamOfMessage(prompt, responses.EasyInputMessageRoleUser)
	promptMsg.OfMessage.Type = "message"
	inputItems = append(inputItems, promptMsg)

	client.triggerOnEvent(history.Token{ Type: history.TokenTypeSystem, Content: []string{ "[System]: " + prompt } })
	client.triggerOnEvent(history.Token{ Type: history.TokenEndOfSequence })

	// 2. Execute synchronous (non-streaming) API request WITHOUT tools
	// leave everything as is for better caching efficency
	resp, err := client.endpoint.client.Responses.New(ctx, responses.ResponseNewParams{
		Model:        client.modelName,
		Instructions: openai.String(client.SystemPrompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: inputItems,
		},
		Tools: client.toolParams, 
		Reasoning: responses.ReasoningParam{
			Effort: client.ReasoningEffort,
		},
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
	newHistory := make([]responses.ResponseInputItemUnionParam, 0, 1+len(toKeep))


	summaryItem := responses.ResponseInputItemParamOfMessage(
		"[Summary of previous conversation]\n\n" + summaryBuilder.String() + "\n\n[End of summary]",
		responses.EasyInputMessageRoleSystem)
	summaryItem.OfMessage.Type = "message"

	newHistory = append(newHistory, summaryItem)
	newHistory = append(newHistory, toKeep...)

	// Update active history and reset TotalTokens to the latest usage baseline
	client.mutex.Lock()
	client.chatHistory = newHistory
	client.TotalTokens = resp.Usage.TotalTokens
	client.mutex.Unlock()
	client.triggerOnEvent(history.Token{ Type: history.TokenTypeSystem, Content: []string{ summaryBuilder.String() } })
	client.triggerOnEvent(history.Token{ Type: history.TokenEndOfSequence })
	return nil
}
