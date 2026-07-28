package history

import "iter"

type History struct {
	entries []Entry
}

func (history *History) Add(entry Entry) {
	history.entries = append(history.entries, entry)
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




