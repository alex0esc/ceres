package history

type EntryType int

const (
	EntryTypeUser = iota
	EntryTypeAssistent
	EntryTypeToolCall
)

type Entry struct {
	Type EntryType
	Content string
}

func NewEntry(etype EntryType, content string) Entry {
	return Entry{ Type: etype, Content: content }
}

