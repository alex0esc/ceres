package inference

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/task"
	"github.com/alex0esc/ceres/pkg/tool"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared/constant"
)

// RegisterTool adds a callable tool to the client.
func (client *Client) RegisterTool(t tool.Tool) {
	client.tools[t.Name()] = t
	tp := responses.ToolParamOfFunction(t.Name(), t.Parameters(), true) // strict mode
	tp.OfFunction.Description = openai.String(t.Description())
	client.toolParams = append(client.toolParams, tp)

	// Keep toolParams sorted alphabetically by name
	sort.Slice(client.toolParams, func(i, j int) bool {
		return client.toolParams[i].OfFunction.Name < client.toolParams[j].OfFunction.Name
	})
}

// resets the chat history of the client
func (client *Client) ClearHistory() {
	client.chatHistory = history.History{}
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



func (client *Client) appendAssistantMessage(promt string) {
	client.chatHistory.Push(history.Entry{Type: history.EntryTypeAssistant, Text: promt})
}


// appendReasoningItem stores a reasoning item losslessly (id + encrypted_content
// + summary/content) so it can be replayed to reasoning-mode backends.
func (client *Client) appendReasoningItem(item responses.ResponseReasoningItem) {
	r := &history.Reasoning{
		ID:               item.ID,
		EncryptedContent: item.EncryptedContent,
	}
	for _, si := range item.Summary {
		r.Summary = append(r.Summary, si.Text)
	}
	for _, ci := range item.Content {
		r.Content = append(r.Content, ci.Text)
	}
	client.chatHistory.Push(history.Entry{Type: history.EntryTypeReasoning, Reasoning: r})
}


func generateReasoningID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "rs_fallback"
	}
	return "rs_" + hex.EncodeToString(b)
}


func (client *Client) appendReasoningText(text string) {
	if text == "" {
		return
	}

	r := &history.Reasoning{ID: generateReasoningID()}
	if client.UseReasoningSummary {
		r.Summary = []string{text}
	} else {
		r.Content = []string{text}
	}
	client.chatHistory.Push(history.Entry{Type: history.EntryTypeReasoning, Reasoning: r})
}


// appends a user prompt (text and/or images) to the history as a single entry
func (client *Client) AppendUserPrompt(prompt task.Prompt) {
	entry := history.Entry{Type: history.EntryTypeUser, Text: prompt.Text}

	hasImage := false
	for _, img := range prompt.Images {
		mimeType := img.MimeType
		if mimeType == "" {
			mimeType = "image/png"
		}
		entry.Images = append(entry.Images, history.Image{
			Base64:   img.Base64Image,
			MimeType: mimeType,
			Detail:   string(responses.ResponseInputImageDetailAuto),
		})
		hasImage = true
	}

	client.chatHistory.Push(entry)

	if hasImage {
		client.triggerOnEvent(history.Token{Type: history.EntryTypeImage, Images: append([]history.Image(nil), entry.Images...)})
		// separate the image from the text so the TUI renders them as two messages
		client.triggerOnEvent(history.Token{Type: history.EntryEndOfSequence})
	}
	if prompt.Text != "" {
		client.triggerOnEvent(history.Token{Type: history.EntryTypeUser, Text: prompt.Text})
	}
	client.triggerOnEvent(history.Token{Type: history.EntryEndOfSequence})
}

// buildRequestInput reconstructs the OpenAI Responses input items from the
// stored, lossless history. Called right before every request.
func (client *Client) buildRequestInput() []responses.ResponseInputItemUnionParam {
	return entriesToItems(client.chatHistory.Entries)
}

// entriesToItems converts history entries back into OpenAI input items,
// preserving order and every field required to replay the conversation.
func entriesToItems(entries []history.Entry) []responses.ResponseInputItemUnionParam {
	items := make([]responses.ResponseInputItemUnionParam, 0, len(entries))

	for _, e := range entries {
		switch e.Type {
		case history.EntryTypeUser:
			items = append(items, userEntryToItem(e))

		case history.EntryTypeAssistant:
			msg := responses.ResponseInputItemParamOfMessage(e.Text, responses.EasyInputMessageRoleAssistant)
			msg.OfMessage.Type = "message"
			items = append(items, msg)

		case history.EntryTypeReasoning:
			if e.Reasoning == nil {
				continue
			}
			items = append(items, reasoningEntryToItem(e.Reasoning))

		case history.EntryTypeToolCall:
			if e.ToolCall == nil {
				continue
			}
			items = append(items, responses.ResponseInputItemParamOfFunctionCall(
				e.ToolCall.Arguments, e.ToolCall.CallID, e.ToolCall.Name))

		case history.EntryTypeToolResult:
			if e.ToolResult == nil {
				continue
			}
			items = append(items, responses.ResponseInputItemParamOfFunctionCallOutput(
				e.ToolResult.CallID, e.ToolResult.Output))
		}
	}

	return items
}

func userEntryToItem(e history.Entry) responses.ResponseInputItemUnionParam {
	content := responses.ResponseInputMessageContentListParam{}

	for _, img := range e.Images {
		dataURL := fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Base64)
		detail := responses.ResponseInputImageDetailAuto
		if img.Detail != "" {
			detail = responses.ResponseInputImageDetail(img.Detail)
		}
		content = append(content, responses.ResponseInputContentUnionParam{
			OfInputImage: &responses.ResponseInputImageParam{
				Detail:   detail,
				ImageURL: openai.String(dataURL),
			},
		})
	}

	if e.Text != "" {
		content = append(content, responses.ResponseInputContentParamOfInputText(e.Text))
	}

	msg := responses.ResponseInputItemParamOfMessage(content, responses.EasyInputMessageRoleUser)
	msg.OfMessage.Type = "message"
	return msg
}

func reasoningEntryToItem(r *history.Reasoning) responses.ResponseInputItemUnionParam {
	p := responses.ResponseReasoningItemParam{
		ID:   r.ID,
		Type: constant.ValueOf[constant.Reasoning](),
	}
	if r.EncryptedContent != "" {
		p.EncryptedContent = param.NewOpt(r.EncryptedContent)
	}
	for _, s := range r.Summary {
		p.Summary = append(p.Summary, responses.ResponseReasoningItemSummaryParam{Text: s})
	}
	for _, c := range r.Content {
		p.Content = append(p.Content, responses.ResponseReasoningItemContentParam{Text: c})
	}
	return responses.ResponseInputItemUnionParam{OfReasoning: &p}
}

// returns request opts for ask stream and compress
func (client *Client) requestOpts() []option.RequestOption {
	opts := make([]option.RequestOption, 0, len(client.ExtraBody))
	for k, v := range client.ExtraBody {
		opts = append(opts, option.WithJSONSet(k, v))
	}
	return opts
}
