package history

import (
	"iter"
	"slices"
	"strings"
)

type History struct {
	Entries []Entry
}

func (history *History) Push(entry Entry) {
	history.Entries = append(history.Entries, entry)
}

func (history *History) Append(other *History) {
	for _, entry := range other.Entries {
		history.Entries = append(history.Entries, entry)
	}
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


func (history *History) Filter(types ...EntryType) *History {
	filtered := &History{}

	for _, entry := range history.Entries {
		keep := slices.Contains(types, entry.Type)

		if keep {
			filtered.Push(entry)
		}
	}

	return filtered
}

func (history *History) LastEntry() *Entry {
	return &history.Entries[len(history.Entries) - 1]
}
