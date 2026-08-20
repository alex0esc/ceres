package inference

import (
	"fmt"

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


// apppending messages to history
func (client *Client) appendUserMessage(prompt string) {
	msg := responses.ResponseInputItemParamOfMessage(prompt, responses.EasyInputMessageRoleUser)
	msg.OfMessage.Type = "message"
	client.chatHistory = append(client.chatHistory, msg)	
	client.triggerOnEvent(history.Token {Type: history.TokenTypeUser, Content: []string{ prompt }})
	client.triggerOnEvent(history.Token { Type: history.TokenEndOfSequence })
}


func (client *Client) appendAssistentMessage(promt string) {
	msg := responses.ResponseInputItemParamOfMessage(promt, responses.EasyInputMessageRoleAssistant)
	msg.OfMessage.Type = "message"
	client.chatHistory = append(client.chatHistory, msg)
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
		client.triggerOnEvent(history.Token {Type: history.TokenTypeUser, Content: []string{ prompt }})
		client.triggerOnEvent(history.Token { Type: history.TokenEndOfSequence })
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
	client.triggerOnEvent(history.Token {Type: history.TokenTypeImage, Content: []string{ dataURL }})
}


// returns request opts for ask stream and compress
func (client *Client) requestOpts() []option.RequestOption {
	opts := make([]option.RequestOption, 0, len(client.ExtraBody))
	for k, v := range client.ExtraBody {
		opts = append(opts, option.WithJSONSet(k, v))
	}
	return opts
}

