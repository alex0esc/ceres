package history

import (
	"fmt"
	"strings"
)

type EntryType int

const (
	EntryTypeUser = iota
	EntryTypeAssistant
	EntryTypeReasoning
	EntryTypeToolCall
	EntryTypeToolResult
	EntryTypeImage
	EntryEndOfSequence
)

// Reasoning holds everything needed to replay a native reasoning item back to
// the provider (id + encrypted_content) as well as the human readable text.
type Reasoning struct {
	ID               string
	EncryptedContent string
	Summary          []string
	Content          []string
}

// ToolCall is a function call requested by the model. CallID links it to the
// matching ToolResult and is required to rebuild a valid request.
type ToolCall struct {
	Name      string
	Arguments string
	CallID    string
}

// ToolResult is the output for a ToolCall, linked by CallID.
type ToolResult struct {
	CallID string
	Output string
}

// Image is a base64 image with its mime type and detail level.
type Image struct {
	Base64   string
	MimeType string
	Detail   string
}

// Entry is a single, lossless unit of the chat history. Depending on Type the
// matching field is populated; Text also carries streamed deltas.
type Entry struct {
	Type       EntryType
	Text       string
	Reasoning  *Reasoning
	ToolCall   *ToolCall
	ToolResult *ToolResult
	Images     []Image
}

// Token is an Entry used as a streamed delta or lifecycle event (as opposed to
// an Entry stored in the chat history). It is the very same type; the alias
// only documents the intent so token and stored entry are easy to tell apart.
type Token = Entry

func (entry *Entry) String() string {
	switch entry.Type {
	case EntryTypeAssistant, EntryTypeUser:
		return entry.Text
	case EntryTypeReasoning:
		if entry.Reasoning == nil {
			return entry.Text
		}
		var b strings.Builder
		for _, s := range entry.Reasoning.Summary {
			b.WriteString(s)
		}
		for _, c := range entry.Reasoning.Content {
			b.WriteString(c)
		}
		return b.String()
	case EntryTypeToolCall:
		return fmt.Sprintf("Calling tool [%s] with arguments %s...", entry.ToolCall.Name, entry.ToolCall.Arguments)
	case EntryTypeToolResult:
		return fmt.Sprintf("Recieved tool result: %s", entry.ToolResult.Output)
	case EntryTypeImage:
		size := 0
		if len(entry.Images) > 0 {
			size = len(entry.Images[0].Base64)
		}
		return fmt.Sprintf("[Appended image, base64 size %.2fKB]", float64(size)/1024)
	}
	return ""
}

// Copy returns a deep copy so the value can be safely handed across goroutines.
func (entry Entry) Copy() Entry {
	c := entry
	if entry.Reasoning != nil {
		r := *entry.Reasoning
		r.Summary = append([]string(nil), entry.Reasoning.Summary...)
		r.Content = append([]string(nil), entry.Reasoning.Content...)
		c.Reasoning = &r
	}
	if entry.ToolCall != nil {
		tc := *entry.ToolCall
		c.ToolCall = &tc
	}
	if entry.ToolResult != nil {
		tr := *entry.ToolResult
		c.ToolResult = &tr
	}
	if entry.Images != nil {
		c.Images = append([]Image(nil), entry.Images...)
	}
	return c
}
