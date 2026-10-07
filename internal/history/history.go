package history

import (
	"iter"
	"slices"
	"strings"
)

type History struct {
	Entries     []Entry `json:"entries"`
	TotalTokens int64   `json:"total_tokens"`
}

func (history *History) Push(entry Entry) {
	history.Entries = append(history.Entries, entry)
}


func (history *History) Append(other History) {
	history.Entries = append(history.Entries, other.Entries...)
}


func (history *History) All() iter.Seq[Entry] {
	return func(yield func(Entry) bool) {
		for _, entry := range history.Entries {
			if !yield(entry) {
				return
			}
		}
	}
}

func (history *History) String() string {
	var builder strings.Builder
	for _, entry := range history.Entries {
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString(entry.String())
	}
	return builder.String()
}


func (history *History) Filter(types ...EntryType) History {
	filtered := History{}

	for _, entry := range history.Entries {
		keep := slices.Contains(types, entry.Type)

		if keep {
			filtered.Push(entry)
		}
	}

	return filtered
}


func (history *History) LastEntry() (bool, Entry) {
	if len(history.Entries) == 0 {
		return false, Entry{} 
	}
	return true, history.Entries[len(history.Entries)-1]
}


// Copy returns a deep copy of the history, so it can be read or stored safely
// while the original keeps changing.
func (history *History) Copy() History {
	c := History{TotalTokens: history.TotalTokens}
	if history.Entries != nil {
		c.Entries = make([]Entry, 0, len(history.Entries))
		for _, entry := range history.Entries {
			c.Entries = append(c.Entries, entry.Copy())
		}
	}
	return c
}
