package inference


import (
	"github.com/alex0esc/ceres/internal/history"
	"github.com/openai/openai-go/v3/responses"
)

// extracts the message content from an input message.
func extractMessageContent(hist *history.History, content responses.EasyInputMessageContentUnionParam) {
	if s := content.OfString.String(); s != "" {
		hist.Push(history.Entry{
			Type:    history.EntryTypeUser,
			Content: []string{s},
		})
		return
	}

	for _, part := range content.OfInputItemContentList {
		switch {
		case part.OfInputText != nil:
			hist.Push(history.Entry{
				Type:    history.EntryTypeUser,
				Content: []string{part.OfInputText.Text},
			})

		case part.OfInputImage != nil:
			data := part.OfInputImage.ImageURL.String()
			hist.Push(history.Entry{
				Type:    history.EntryTypeImage,
				Content: []string{data},
			})
		}
	}
}

// returns the extracted history
func (client *Client) GetHistory() *history.History {
	var hist history.History

	for _, item := range client.chatHistory {
		switch {
		case item.OfReasoning != nil:
			reasoning := item.OfReasoning

			// Llama.cpp may expose reasoning through Content,
			// while other providers use Summary. Keep both.
			for _, part := range reasoning.Summary {
				if part.Text != "" {
					hist.Push(history.Entry{
						Type:    history.EntryTypeReasoning,
						Content: []string{part.Text},
					})
				}
			}

			for _, part := range reasoning.Content {
				if part.Text != "" {
					hist.Push(history.Entry{
						Type:    history.EntryTypeReasoning,
						Content: []string{part.Text},
					})
				}
			}

		case item.OfMessage != nil:
			content := item.OfMessage.Content

			switch item.OfMessage.Role {
			case responses.EasyInputMessageRoleAssistant:
				// Assistant message contains only normal assistant text.
				if text := content.OfString.String(); text != "" {
					hist.Push(history.Entry{
						Type:    history.EntryTypeAssistent,
						Content: []string{text},
					})
				}

			case responses.EasyInputMessageRoleUser:
				extractMessageContent(&hist, content)

			}

		
		case item.OfFunctionCall != nil:
			fc := item.OfFunctionCall
			hist.Push(history.Entry{
				Type:    history.EntryTypeToolCall,
				Content: []string{fc.Name, fc.Arguments},
			})

		case item.OfFunctionCallOutput != nil:
			fco := item.OfFunctionCallOutput
			hist.Push(history.Entry{
				Type:    history.EntryTypeToolResult,
				Content: []string{fco.Output.OfString.String()},
			})
		}
	}

	return &hist
}
