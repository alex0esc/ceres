package history

import "fmt"

type EntryType int

const (
	EntryTypeUser = iota
	EntryTypeAssistent
	EntryTypeReasoning
	EntryTypeToolCall
	EntryTypeToolResult
	EntryTypeImage
)

type Entry struct {
	Type    EntryType
	Content []string
}

func (entry *Entry) String() string {
	switch entry.Type {
	case EntryTypeAssistent, EntryTypeReasoning, EntryTypeUser:
		return entry.Content[0]
	case EntryTypeToolCall:
		return fmt.Sprintf("Calling tool [%s] with arguments %s...", entry.Content[0], entry.Content[1])
	case EntryTypeToolResult:
		return fmt.Sprintf("Recieved tool result: %s", entry.Content[0])
	case EntryTypeImage: 
		return fmt.Sprintf("[Appended image, base64 size %.2fKB]", float64(len(entry.Content[0])) / 1024)
	}
	return ""
}
