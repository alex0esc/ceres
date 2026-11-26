package inference

import (
	"github.com/alex0esc/ceres/internal/history"
	"github.com/alex0esc/ceres/internal/stream"
)

// Compile-time check that Subscription fulfils the neutral stream interface.
var _ stream.Stream = (*Subscription)(nil)

// Subscription is a live token stream owned by one consumer. The consumer reads
// from Tokens and must call Close when it is done. Tokens are immutable and are
// the same values delivered to every other subscriber.
type Subscription struct {
	ch chan history.Token

	client *Client

	// closed and reason are guarded by client.mutex.
	closed bool
	reason error
}

// Tokens returns the channel on which streamed tokens are delivered. It is
// closed when the subscription ends; check Err afterwards to distinguish an
// overflow from a normal Close.
func (s *Subscription) Tokens() <-chan history.Token {
	if s == nil {
		return nil
	}
	return s.ch
}

// Close unsubscribes the consumer. It is safe to call multiple times and from
// any goroutine. Tokens that are already buffered can still be drained from
// Tokens after Close returns.
func (s *Subscription) Close() {
	if s == nil || s.client == nil {
		return
	}

	c := s.client
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if s.closed {
		return
	}
	s.closed = true
	delete(c.subscriptions, s)
	close(s.ch)
}

// Err returns why the subscription was closed by the client, or nil if it was
// closed by the consumer. Check it after Tokens is drained to detect an
// overflow (errors.Is(err, stream.ErrOverflow)).
func (s *Subscription) Err() error {
	if s == nil || s.client == nil {
		return nil
	}

	c := s.client
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return s.reason
}

// Subscribe registers a new live subscriber without taking a snapshot. New
// consumers should normally use SnapshotAndSubscribe so they cannot miss state.
func (c *Client) Subscribe(buffer int) stream.Stream {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.subscribeLocked(buffer)
}

// SnapshotAndSubscribe atomically captures the current state and registers the
// subscription. Every token is therefore either part of the snapshot or
// delivered through the subscription, never both and never neither.
func (c *Client) SnapshotAndSubscribe(buffer int) (stream.Snapshot, stream.Stream) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	snap := c.snapshotLocked()
	return snap, c.subscribeLocked(buffer)
}

// Snapshot returns a consistent copy of the current history and streamed
// partial answer.
func (c *Client) Snapshot() stream.Snapshot {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.snapshotLocked()
}


func (c *Client) snapshotLocked() stream.Snapshot {
	return stream.Snapshot{
		History:     c.history.Copy(),
		Partial:     append([]history.Token(nil), c.partialAnswer...),
		Compressing: c.isCompressing,
	}
}

func (c *Client) subscribeLocked(buffer int) *Subscription {
	if buffer <= 0 {
		buffer = stream.DefaultBuffer
	}
	if c.subscriptions == nil {
		c.subscriptions = make(map[*Subscription]struct{})
	}

	ch := make(chan history.Token, buffer)
	sub := &Subscription{
		ch:     ch,
		client: c,
	}
	c.subscriptions[sub] = struct{}{}
	return sub
}

// publishLocked fans a token out to every subscriber. It never blocks on a
// subscriber: a subscriber whose buffer is full is removed and closed instead,
// so one slow consumer cannot stall the agent or the other consumers.
func (c *Client) publishLocked(token history.Token) {
	for sub := range c.subscriptions {
		select {
		case sub.ch <- token:
		default:
			sub.closed = true
			sub.reason = stream.ErrOverflow
			delete(c.subscriptions, sub)
			close(sub.ch)
		}
	}
}

// emitDelta appends a streamed delta to the partial answer and publishes it in
// the same critical section, so snapshots and live tokens stay consistent.
func (c *Client) emitDelta(token history.Token) {
	c.mutex.Lock()
	c.partialAnswer = append(c.partialAnswer, token)
	c.publishLocked(token)
	c.mutex.Unlock()
}

// publish sends a token that is not part of the partial answer (e.g. a stored
// entry, an end-of-sequence marker or a reset).
func (c *Client) publish(token history.Token) {
	c.mutex.Lock()
	c.publishLocked(token)
	c.mutex.Unlock()
}
