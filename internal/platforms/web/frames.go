package web

import (
	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/stream"
)

// WebSocket frame types. The browser dispatches on "type".
const (
	frameSnapshot   = "snapshot"
	frameDelta      = "delta"
	frameMessageEnd = "message_end"
	frameReset      = "reset"
	frameStatus     = "status"
	frameError      = "error"
)

// frame is the wire format for every WebSocket message. Entries are serialized
// directly from history.Entry (including its JSON tags), so the browser gets
// the raw entry type and fields and does all HTML rendering itself.
type frame struct {
	Type        string          `json:"type"`
	Agent       string          `json:"agent,omitempty"`
	Entries     []history.Entry `json:"entries,omitempty"`
	Entry       *history.Entry  `json:"entry,omitempty"`
	Partial     []history.Entry `json:"partial,omitempty"`
	Compressing bool            `json:"compressing,omitempty"`
	Message     string          `json:"message,omitempty"`
}

// snapshotFrame converts a client snapshot into the initial frame. Reasoning
// entries are omitted when the web platform is configured to hide them.
func snapshotFrame(agentName string, snap stream.Snapshot, showReasoning bool) frame {
	entries := make([]history.Entry, 0, len(snap.History.Entries))
	for _, entry := range snap.History.Entries {
		if !showReasoning && entry.Type == history.EntryTypeReasoning {
			continue
		}
		entries = append(entries, entry)
	}

	partial := make([]history.Entry, 0, len(snap.Partial))
	for _, token := range snap.Partial {
		if token.Type == history.TokenTypeResetChat {
			continue
		}
		if !showReasoning && token.Type == history.EntryTypeReasoning {
			continue
		}
		partial = append(partial, token)
	}

	return frame{
		Type:        frameSnapshot,
		Agent:       agentName,
		Entries:     entries,
		Partial:     partial,
		Compressing: snap.Compressing,
	}
}

// tokenFrame converts one live token into a frame. Lifecycle tokens become
// their own frame types; everything else is a delta carrying the raw entry.
func tokenFrame(token history.Token, showReasoning bool) (frame, bool) {
	switch token.Type {
	case history.TokenTypeEndOfSequence:
		return frame{Type: frameMessageEnd}, true
	case history.TokenTypeResetChat:
		return frame{Type: frameReset}, true
	case history.EntryTypeReasoning:
		if !showReasoning {
			return frame{}, false
		}
	}

	entry := token.Copy()
	return frame{Type: frameDelta, Entry: &entry}, true
}
