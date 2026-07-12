package tracing

import "sync"

// subBufferSize is each subscriber channel's capacity. A slow SSE client that
// fills its buffer has events DROPPED (not blocked) — a dropped live event is
// recoverable: the client reconnects with Last-Event-ID and replays from the
// ring buffer / DB. Bounding here keeps one slow client from stalling Emit.
const subBufferSize = 64

// Broker fans one run's events out to any number of live SSE subscribers. It is
// independent of the store, so live streaming works even with no DB.
type Broker struct {
	mu   sync.Mutex
	subs map[string][]*subscriber
}

// subscriber is one live connection. done (never ch) is closed to signal "no
// more events" — so Publish never sends on a closed channel and cannot panic.
type subscriber struct {
	ch   chan Event
	done chan struct{}
	once sync.Once
}

func (s *subscriber) close() { s.once.Do(func() { close(s.done) }) }

// Subscription is a live listener handle. The SSE handler reads Events, and ends
// its loop on: a terminal Event, Done (broker Close — safety net if the terminal
// event was dropped), or its own request context. It MUST defer Cancel to avoid
// leaking the subscriber.
type Subscription struct {
	Events <-chan Event
	Done   <-chan struct{}
	cancel func()
}

// Cancel unregisters the subscription. Safe to call multiple times.
func (s *Subscription) Cancel() { s.cancel() }

// NewBroker builds an empty broker.
func NewBroker() *Broker {
	return &Broker{subs: make(map[string][]*subscriber)}
}

// Subscribe registers a live listener for runID.
func (b *Broker) Subscribe(runID string) *Subscription {
	sub := &subscriber{ch: make(chan Event, subBufferSize), done: make(chan struct{})}
	b.mu.Lock()
	b.subs[runID] = append(b.subs[runID], sub)
	b.mu.Unlock()
	return &Subscription{Events: sub.ch, Done: sub.done, cancel: func() { b.remove(runID, sub) }}
}

// Publish delivers evt to every current subscriber of runID, non-blocking. It
// snapshots the subscriber slice under the lock, then sends outside the lock so
// a slow receiver never holds up Emit or other subscribers.
func (b *Broker) Publish(runID string, evt Event) {
	b.mu.Lock()
	subs := append([]*subscriber(nil), b.subs[runID]...)
	b.mu.Unlock()
	for _, sub := range subs {
		select {
		case sub.ch <- evt:
		case <-sub.done: // subscriber gone — skip
		default: // buffer full — drop (recoverable via reconnect/replay)
		}
	}
}

// Close ends the run's live streaming: every subscriber's done is closed (its
// SSE read loop exits) and the run's entry is removed. Called on a terminal
// event. Idempotent.
func (b *Broker) Close(runID string) {
	b.mu.Lock()
	subs := b.subs[runID]
	delete(b.subs, runID)
	b.mu.Unlock()
	for _, sub := range subs {
		sub.close()
	}
}

// remove drops one subscriber (the Subscribe cancel func) and signals its done
// so the read loop exits even if the run has not terminated.
func (b *Broker) remove(runID string, target *subscriber) {
	b.mu.Lock()
	subs := b.subs[runID]
	kept := subs[:0]
	for _, s := range subs {
		if s != target {
			kept = append(kept, s)
		}
	}
	if len(kept) == 0 {
		delete(b.subs, runID)
	} else {
		b.subs[runID] = kept
	}
	b.mu.Unlock()
	target.close()
}
