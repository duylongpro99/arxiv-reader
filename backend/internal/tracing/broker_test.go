package tracing

import (
	"testing"
	"time"
)

func recv(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}

func TestBrokerDeliversLive(t *testing.T) {
	b := NewBroker()
	sub := b.Subscribe("run1")
	defer sub.Cancel()

	b.Publish("run1", Event{Seq: 0, Kind: KindDiscoveryStarted})
	if got := recv(t, sub.Events); got.Kind != KindDiscoveryStarted {
		t.Fatalf("got %q", got.Kind)
	}
	// Events for a different run must not arrive.
	b.Publish("run2", Event{Seq: 0, Kind: KindRunCompleted})
	select {
	case e := <-sub.Events:
		t.Fatalf("received cross-run event: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBrokerCloseSignalsDone(t *testing.T) {
	b := NewBroker()
	sub := b.Subscribe("run1")
	defer sub.Cancel()

	b.Close("run1")
	select {
	case <-sub.Done:
	case <-time.After(time.Second):
		t.Fatal("Done not signalled on Close")
	}
	// A publish after Close reaches nobody (run entry deleted) — must not panic.
	b.Publish("run1", Event{Seq: 1})
}

func TestBrokerCancelUnsubscribes(t *testing.T) {
	b := NewBroker()
	sub := b.Subscribe("run1")
	sub.Cancel()

	// Done fires on cancel; subsequent publish is not delivered.
	select {
	case <-sub.Done:
	case <-time.After(time.Second):
		t.Fatal("Done not signalled on Cancel")
	}
	b.Publish("run1", Event{Seq: 0})
	select {
	case e := <-sub.Events:
		t.Fatalf("received after cancel: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBrokerMultiSubscriberRemoveKeepsOthers(t *testing.T) {
	b := NewBroker()
	a := b.Subscribe("run1")
	c := b.Subscribe("run1")
	defer c.Cancel()

	a.Cancel() // remove one of two — the other must keep receiving
	b.Publish("run1", Event{Seq: 0, Kind: KindDiscoveryStarted})
	if got := recv(t, c.Events); got.Kind != KindDiscoveryStarted {
		t.Fatalf("surviving subscriber missed event: %+v", got)
	}
	select {
	case e := <-a.Events:
		t.Fatalf("cancelled subscriber received: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBrokerSlowSubscriberDoesNotBlock(t *testing.T) {
	b := NewBroker()
	sub := b.Subscribe("run1")
	defer sub.Cancel()
	// Publish more than the subscriber buffer without ever reading — must not
	// block (excess is dropped). If Publish blocked, this test would hang.
	done := make(chan struct{})
	go func() {
		for i := 0; i < subBufferSize*3; i++ {
			b.Publish("run1", Event{Seq: i})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
}
