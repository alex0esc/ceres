package inference

import (
	"fmt"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/pkg/handles"
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


// managing the event callback
func (client *Client) SetOnEvent(fn func(history.Token)) {
    client.mutex.Lock()
    defer client.mutex.Unlock()
    client.onEvent = fn
}


// streams the tokens currently in the pipeline instantly
func (client *Client) CatchUpOnEvent() {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.onEvent == nil {
		return
	}
	tokens := make([]history.Token, len(client.partialAnswer))
	copy(tokens, client.partialAnswer)
	onEvent := client.onEvent

	go func() {
		for _, token := range tokens {
			onEvent(token)
		}
	}()
}


func (client *Client) triggerOnEvent(token history.Token) {
    client.mutex.Lock()
    defer client.mutex.Unlock()
    if client.onEvent == nil {
    	return
    }
	client.onEvent(token)
}

func (client *Client) ClearOnEvent() {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.onEvent = nil
}


func (client *Client) appendAssistentMessage(promt string) {
	msg := responses.ResponseInputItemParamOfMessage(promt, responses.EasyInputMessageRoleAssistant)
	msg.OfMessage.Type = "message"
	client.chatHistory = append(client.chatHistory, msg)
}





// appends a list of images with a single prompt after them in the history
func (client *Client) AppendUserPrompt(prompt handles.Prompt) {
	content := responses.ResponseInputMessageContentListParam{}

	dataURLs := make([]string, 0, len(prompt.Images))

	hasImage := false
	for _, img := range prompt.Images {
		mimeType := img.MimeType
		if mimeType == "" {
			mimeType = "image/png"
		}
		dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, img.Base64Image)
		dataURLs = append(dataURLs, dataURL)

		content = append(content, responses.ResponseInputContentUnionParam{
			OfInputImage: &responses.ResponseInputImageParam{
				Detail:   responses.ResponseInputImageDetailAuto,
				ImageURL: openai.String(dataURL),
			},
		})
		hasImage = true
	}


	if prompt.Text != "" {
		content = append(content, responses.ResponseInputContentParamOfInputText(prompt.Text))
	}

	msg := responses.ResponseInputItemParamOfMessage(content, responses.EasyInputMessageRoleUser)
	msg.OfMessage.Type = "message"
	client.chatHistory = append(client.chatHistory, msg)

	if hasImage {
		client.triggerOnEvent(history.Token{Type: history.TokenTypeImage, Content: dataURLs})
	}
	if prompt.Text != "" {
		client.triggerOnEvent(history.Token{Type: history.TokenTypeUser, Content: []string{prompt.Text}})
	}
	client.triggerOnEvent(history.Token{Type: history.TokenEndOfSequence})
}

// returns request opts for ask stream and compress
func (client *Client) requestOpts() []option.RequestOption {
	opts := make([]option.RequestOption, 0, len(client.ExtraBody))
	for k, v := range client.ExtraBody {
		opts = append(opts, option.WithJSONSet(k, v))
	}
	return opts
}

