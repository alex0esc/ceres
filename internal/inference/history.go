package inference

import (
	"github.com/alex0esc/ceres/internal/history"
)

// GetHistory returns a snapshot copy of the client's lossless chat history.
func (client *Client) GetHistory() *history.History {
	snapshot := &history.History{}
	for _, entry := range client.chatHistory.Entries {
		snapshot.Push(entry.Copy())
	}
	return snapshot
}
