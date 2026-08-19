package inference

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)


// RegisterTool adds a callable tool to the client.
func (client *Client) RegisterTool(t tool.Tool) {
	client.tools[t.Name()] = t
	tp := responses.ToolParamOfFunction(t.Name(), t.Parameters(), true) // strict mode
	tp.OfFunction.Description = openai.String(t.Description())
	client.toolParams = append(client.toolParams, tp)
}

// resets the chat history of the client
func (client *Client) ClearHistory() {
	client.chatHistory = nil
	client.TotalTokens = 0
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


type TokenType int

const (
	TokenTypeUser = iota
	TokenTypeAssistent
	TokenTypeReasoning
	TokenTypeSystemInfo
	TokenEndOfSequence
)

type Token struct {
	Type TokenType
	Content string
}

func NewToken(tp TokenType, content string) Token {
	return Token{ Type: tp, Content: content }
}


// SetOnEvent tauscht den Event-Callback thread-sicher aus.
func (client *Client) SetOnEvent(fn func(Token)) {
    client.mutex.Lock()
    defer client.mutex.Unlock()
    client.onEvent = fn
}


// triggerOnEvent liest den Callback geschützt aus und führt ihn aus, falls gesetzt.
func (client *Client) triggerOnEvent(token Token) {
    client.mutex.Lock()
    fn := client.onEvent
    client.mutex.Unlock()
    if fn != nil {
        fn(token)
    }
}

// ClearOnEvent entfernt den aktuell registrierten Event-Callback thread-sicher.
func (client *Client) ClearOnEvent() {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.onEvent = nil
}



func (client *Client) appendUserMessage(promt string) {
	msg := responses.ResponseInputItemParamOfMessage(promt, responses.EasyInputMessageRoleUser,)
	msg.OfMessage.Type = "message"
	client.chatHistory = append(client.chatHistory, msg)	
}


func (client *Client) appendAssistentMessage(promt string) {
	msg := responses.ResponseInputItemParamOfMessage(promt, responses.EasyInputMessageRoleAssistant)
	msg.OfMessage.Type = "message"
	client.chatHistory = append(client.chatHistory, msg)
}


// returns request opts for ask stream and compress
func (c *Client) requestOpts() []option.RequestOption {
	opts := make([]option.RequestOption, 0, len(c.ExtraBody))
	for k, v := range c.ExtraBody {
		opts = append(opts, option.WithJSONSet(k, v))
	}
	return opts
}


// appends an image with a promt before it in the history
func (client *Client) AppendImage(base64Image string, mimeType string, prompt string) {
	if mimeType == "" {
		mimeType = "image/png"
	}
	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64Image)

	content := responses.ResponseInputMessageContentListParam{}

	if prompt != "" {
		content = append(content, responses.ResponseInputContentParamOfInputText(prompt))
	}

	content = append(content, responses.ResponseInputContentUnionParam{
		OfInputImage: &responses.ResponseInputImageParam{
			Detail:   responses.ResponseInputImageDetailAuto,
			ImageURL: openai.String(dataURL),
		},
	})

	msg := responses.ResponseInputItemParamOfMessage(content, responses.EasyInputMessageRoleUser)
	msg.OfMessage.Type = "message"
	client.chatHistory = append(client.chatHistory, msg)
	client.triggerOnEvent(NewToken(TokenTypeUser, fmt.Sprintf("[Image Appended - %.2f KB Base64]", float64(len(base64Image))/(1024))))
	client.triggerOnEvent(NewToken(TokenEndOfSequence, ""))
}




//extracts the history of the client and formats it correctly
var thinkTagRegex = regexp.MustCompile(`(?s)<think>(.*?)</think>`)

// extractReasoning trennt <think>...</think> Blöcke vom eigentlichen Text.
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
func extractMessageContent(content responses.EasyInputMessageContentUnionParam) (text string, imageBase64Size int) {
	if s := content.OfString.String(); s != "" {
		return s, 0
	}

	var parts []string
	for _, part := range content.OfInputItemContentList {
		switch {
		case part.OfInputText != nil:
			parts = append(parts, part.OfInputText.Text)

		case part.OfInputImage != nil:
			image := part.OfInputImage
			data := image.ImageURL.String()

			// Remove "data:image/...;base64," prefix if present
			if idx := strings.Index(data, "base64,"); idx >= 0 {
				data = data[idx+len("base64,"):]
			}

			imageBase64Size += len(data)
		}
	}

	return strings.Join(parts, "\n"), imageBase64Size
}

func (client *Client) GetHistory() *history.History {
	var hist history.History

	for _, item := range client.chatHistory {
		switch {
		case item.OfMessage != nil:
			content, imageBase64Size := extractMessageContent(item.OfMessage.Content)

			switch item.OfMessage.Role {
			case responses.EasyInputMessageRoleAssistant:
				reasoning, text := extractReasoning(content)

				if reasoning != "" {
					hist.Add(history.NewEntry(history.EntryTypeReasoning, reasoning))
				}

				if text != "" {
					hist.Add(history.NewEntry(history.EntryTypeAssistent, text))
				}

			case responses.EasyInputMessageRoleUser:
				if content != "" {
					hist.Add(history.NewEntry(history.EntryTypeUser, content))
				}

				if imageBase64Size > 0 {
					hist.Add(history.NewEntry(
						history.EntryTypeUser,
						fmt.Sprintf("[Image Appended - %.2f KB Base64]", float64(imageBase64Size)/(1024)),
					))
				}

			case responses.EasyInputMessageRoleSystem:
				hist.Add(history.NewEntry(history.EntryTypeSystemInfo, content))
			}

		case item.OfFunctionCall != nil:
			fc := item.OfFunctionCall
			hist.Add(history.NewEntry(
				history.EntryTypeSystemInfo,
				fmt.Sprintf("Calling tool [%s] with arguments %s...\n\n", fc.Name, fc.Arguments),
			))
		}
	}
	return &hist
}
