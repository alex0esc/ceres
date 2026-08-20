package history

import (
	"slices"
	"iter"
	"strings"
)

type History struct {
	entries []Entry
}

func (history *History) Push(entry Entry) {
	history.entries = append(history.entries, entry)
}

func (history *History) Append(other *History) {
	for _, entry := range other.entries {
		history.entries = append(history.entries, entry)
	}
}

func (history *History) All() iter.Seq[Entry] {
	return func(yield func(Entry) bool) {
		for _, entry := range history.entries {
			if !yield(entry) {
				return
			}
		}
	}
}

func (history *History) String() string {
	var builder strings.Builder
	for _, entry := range history.entries {
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString(entry.String())
	}
	return builder.String()
}

func (history *History) LastEntry() *Entry {
	if len(history.entries) <= 0 {
		return nil
	}
	return &history.entries[len(history.entries) - 1]
}


func (history *History) Filter(types ...EntryType) *History {
	filtered := &History{}

	for _, entry := range history.entries {
		keep := slices.Contains(types, entry.Type)

		if keep {
			filtered.Push(entry)
		}
	}

	return filtered
}
