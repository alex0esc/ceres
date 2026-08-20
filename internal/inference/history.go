package inference

import (
	"regexp"
	"strings"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/openai/openai-go/v3/responses"
)

//extracts the history of the client and formats it correctly
var thinkTagRegex = regexp.MustCompile(`(?s)<think>(.*?)</think>`)

// extracts the first (and only) reasoning block from the agent message
func extractReasoning(rawContent string) (reasoningText string, cleanText string) {
	matches := thinkTagRegex.FindStringSubmatch(rawContent)
	if len(matches) > 1 {
		reasoningText = strings.TrimSpace(matches[1])
		cleanText = strings.TrimSpace(thinkTagRegex.ReplaceAllString(rawContent, ""))
		return reasoningText, cleanText
	}

	return "", strings.TrimSpace(rawContent)
}


// removes any base64 strings of images from the message,
// but keeps the total base64 size in bytes
func extractMessageContent(hist *history.History, content responses.EasyInputMessageContentUnionParam) {
	if s := content.OfString.String(); s != "" {
		hist.Push(history.Entry{ Type: history.EntryTypeUser, Content: []string{ s } })
		return
	}

	for _, part := range content.OfInputItemContentList {
		switch {
		case part.OfInputText != nil:
			hist.Push(history.Entry{ Type: history.EntryTypeUser, Content: []string{part.OfInputText.Text} })

		case part.OfInputImage != nil:
			data := part.OfInputImage.ImageURL.String()
			hist.Push(history.Entry{ Type: history.EntryTypeImage, Content: []string{ data } })
		}
	}
}

// returns the extracted history
func (client *Client) GetHistory() *history.History {
	var hist history.History
	for _, item := range client.chatHistory {
		switch {
		case item.OfMessage != nil:
			content := item.OfMessage.Content
			switch item.OfMessage.Role {
			case responses.EasyInputMessageRoleAssistant:
				reasoning, text := extractReasoning(content.OfString.String())

				if reasoning != "" {
					hist.Push(history.Entry { Type: history.EntryTypeReasoning, Content: []string {reasoning} })
				}

				if text != "" {
					hist.Push(history.Entry { Type: history.EntryTypeAssistent, Content: []string {text} })
				}

			case responses.EasyInputMessageRoleUser:
				extractMessageContent(&hist, content)

			case responses.EasyInputMessageRoleSystem:
				hist.Push(history.Entry { Type: history.EntryTypeSystem, Content: []string{ content.OfString.String() } })
			}

		case item.OfFunctionCall != nil:
			fc := item.OfFunctionCall
			hist.Push(history.Entry{ Type: history.EntryTypeToolCall, Content: []string{ fc.Name, fc.Arguments }})

		case item.OfFunctionCallOutput != nil:
			fco := item.OfFunctionCallOutput
			hist.Push(history.Entry{ Type: history.EntryTypeToolResult, Content: []string{ fco.Output.OfString.String() }})
		}
	}
	return &hist
}
