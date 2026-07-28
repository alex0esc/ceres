package openai

import (
	"fmt"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/pkg/tools"
	"github.com/openai/openai-go/v3/responses"
)


// RegisterTool adds a callable tool to the client.
func (client *Client) RegisterTool(t tools.Tool) {
	client.Tools[t.Name()] = t
}


//extracts the history of the client and formats it correctly
func (client *Client) GetHistory() *history.History {
	var hist history.History
	for _, item := range client.chatHistory {
		switch {
		case item.OfMessage != nil:
			msg := item.OfMessage
			switch msg.Role {
			case responses.EasyInputMessageRoleAssistant:
				hist.Add(history.NewEntry(history.EntryTypeAssistent, msg.Content.OfString.String()))
			case responses.EasyInputMessageRoleUser:
				hist.Add(history.NewEntry(history.EntryTypeUser, msg.Content.OfString.String()))
			}

		case item.OfFunctionCall != nil:
			fc := item.OfFunctionCall
			hist.Add(history.NewEntry(history.EntryTypeToolCall, fmt.Sprintf("Calling tool [%s] with arguments %s...\n\n", fc.Name, fc.Arguments)))
		}
	}
	return &hist
}


// resets the chat history of the client
func (client *Client) ClearHistory() {
	client.chatHistory = nil
	client.TotalTokens = 0
}



type TokenType int

const (
	TokenTypeUser = iota
	TokenTypeAssistent
	TokenTypeToolCall
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


func (client *Client) appendReasoningSummary(item responses.ResponseReasoningItem) {
	summaryParams := make([]responses.ResponseReasoningItemSummaryParam, 0, len(item.Summary))
    for _, s := range item.Summary {
        summaryParams = append(summaryParams, responses.ResponseReasoningItemSummaryParam{
            Text: s.Text,
        })
    }
    msg := responses.ResponseInputItemParamOfReasoning(item.ID, summaryParams)
    if msg.OfMessage != nil {
    	msg.OfMessage.Type = "reasoning"
    }
    client.chatHistory = append(client.chatHistory, msg)
}
