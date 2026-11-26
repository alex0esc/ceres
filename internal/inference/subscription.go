package inference

import (
	"errors"

	"github.com/alex0esc/ceres/internal/history"
)

// DefaultSubscriptionBuffer is the channel capacity used when a caller does not
// request a specific buffer size. At typical streaming rates this is several
// minutes worth of tokens, so reaching it means the consumer is really stuck.
const DefaultSubscriptionBuffer = 4096

// ErrSubscriptionOverflow is returned by Subscription.Err when the consumer was
// too slow and the client dropped its subscription. The consumer must resync by
// taking a fresh snapshot instead of trying to catch up on the old channel.
var ErrSubscriptionOverflow = errors.New("inference: subscription buffer overflow")

// Snapshot is a consistent view of a client's state at one point in time.
// History and Partial together contain everything a new consumer needs to
// render the current conversation; Partial holds the tokens of a response that
// is still being streamed.
type Snapshot struct {
	History     history.History
	Partial     []history.Token
	Compressing bool
}

// Subscription is a live token stream owned by one consumer. The consumer reads
// from C and must call Close when it is done. Tokens in C are immutable and are
// the same values delivered to every other subscriber.
type Subscription struct {
	C  <-chan history.Token
	ch chan history.Token

	client *Client

	// closed and reason are guarded by client.mutex.
	closed bool
	reason error
}

// Close unsubscribes the consumer. It is safe to call multiple times and from
// any goroutine. Tokens that are already buffered can still be drained from C
// after Close returns.
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
// closed by the consumer. Check it after C is drained to detect an overflow.
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
func (c *Client) Subscribe(buffer int) *Subscription {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.subscribeLocked(buffer)
}

// SnapshotAndSubscribe atomically captures the current state and registers the
// subscription. Every token is therefore either part of the snapshot or
// delivered through the subscription, never both and never neither.
func (c *Client) SnapshotAndSubscribe(buffer int) (Snapshot, *Subscription) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	snap := c.snapshotLocked()
	return snap, c.subscribeLocked(buffer)
}

// Snapshot returns a consistent copy of the current history and streamed
// partial answer.
func (c *Client) Snapshot() Snapshot {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.snapshotLocked()
}


func (c *Client) snapshotLocked() Snapshot {
	return Snapshot{
		History:     c.history.Copy(),
		Partial:     append([]history.Token(nil), c.partialAnswer...),
		Compressing: c.isCompressing,
	}
}

func (c *Client) subscribeLocked(buffer int) *Subscription {
	if buffer <= 0 {
		buffer = DefaultSubscriptionBuffer
	}
	if c.subscriptions == nil {
		c.subscriptions = make(map[*Subscription]struct{})
	}

	ch := make(chan history.Token, buffer)
	sub := &Subscription{
		C:      ch,
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
			sub.reason = ErrSubscriptionOverflow
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
