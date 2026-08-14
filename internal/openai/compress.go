package openai

import (
	"context"
	"fmt"
	"strings"

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

	cmp_msg := fmt.Sprintf("Compressing current chat history {MessagesToKeep: %v}...", client.NumMessagesToKeep)
	client.onEvent(NewToken(TokenTypeSystemInfo, cmp_msg))

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
		return fmt.Errorf("There is no compression promt given for the client with model %s", client.modelName)
	}

	// 1. Prepare payload for the non-streaming compression call
	inputItems := make([]responses.ResponseInputItemUnionParam, 0, len(toCompress)+1)
	inputItems = append(inputItems, toCompress...)

	promptMsg := responses.ResponseInputItemParamOfMessage(prompt, responses.EasyInputMessageRoleSystem)
	promptMsg.OfMessage.Type = "message"

	inputItems = append(inputItems, promptMsg)

	// 2. Execute synchronous (non-streaming) API request WITHOUT tools
	resp, err := client.endpoint.client.Responses.New(ctx, responses.ResponseNewParams{
		Model:        client.modelName,
		Instructions: openai.String("You are an assistent whose task is to summerize the current chat. Do not ask questions, execute your task in one turn."),
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
	summaryBuilder.WriteString("[Summary of previous conversation]\n\n")
	for _, item := range resp.Output {
		if msg, ok := item.AsAny().(responses.ResponseOutputMessage); ok {
			for _, part := range msg.Content {
				if t, ok := part.AsAny().(responses.ResponseOutputText); ok {
					summaryBuilder.WriteString(t.Text)
				}
			}
		}
	}

	if summaryBuilder.String() == "[Summary of previous conversation]\n\n" {
		return fmt.Errorf("compression yielded an empty summary")
	}

	summaryBuilder.WriteString("\n\n[End of summary]")
	

	// 4. Rebuild chat history: [Summary turn] + [unmodified recent messages]
	newHistory := make([]responses.ResponseInputItemUnionParam, 0, 1+len(toKeep))


	summaryItem := responses.ResponseInputItemParamOfMessage(summaryBuilder.String(), responses.EasyInputMessageRoleSystem)
	summaryItem.OfMessage.Type = "message"

	newHistory = append(newHistory, summaryItem)
	newHistory = append(newHistory, toKeep...)

	// Update active history and reset TotalTokens to the latest usage baseline
	client.mutex.Lock()
	client.chatHistory = newHistory
	client.TotalTokens = resp.Usage.TotalTokens
	client.mutex.Unlock()
	return nil
}
