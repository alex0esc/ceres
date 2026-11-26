package stream

import (
	"errors"

	"github.com/alex0esc/ceres/internal/history"
)

const DefaultBuffer = 4096

// ErrOverflow is reported by Stream.Err when the producer dropped the
// subscriber because its buffer was full. The consumer must resync by taking a
// fresh snapshot instead of trying to catch up on the old channel.
var ErrOverflow = errors.New("stream: subscriber buffer overflow")

// Stream is a live token stream owned by one consumer. Tokens are immutable and
// are the same values delivered to every other subscriber. The consumer reads
// from Tokens and must call Close when it is done.
type Stream interface {
	Tokens() <-chan history.Token
	Close()
	Err() error
}

// Snapshot is a consistent view of an agent's state at one point in time.
// History and Partial together contain everything a new consumer needs to
// render the current conversation; Partial holds the tokens of a response that
// is still being streamed.
type Snapshot struct {
	History     history.History
	Partial     []history.Token
	Compressing bool
}
