package hub

import (
	"testing"
	"time"
)

func TestInboxEventBroker(t *testing.T) {
	broker := NewInboxEventBroker()

	subA1 := broker.Subscribe("agent-A", 10)
	subA2 := broker.Subscribe("agent-A", 10)
	subB := broker.Subscribe("agent-B", 10)

	if count := broker.ActiveSubscribers("agent-A"); count != 2 {
		t.Fatalf("agent-A subscribers = %d, want 2", count)
	}
	if count := broker.ActiveSubscribers("agent-B"); count != 1 {
		t.Fatalf("agent-B subscribers = %d, want 1", count)
	}

	// Publish item to agent-A
	itemA := InboxItem{
		Sequence:      1,
		TargetAgentID: "agent-A",
		TaskID:        "task-1",
		Message:       "hello A",
	}
	broker.Publish(itemA)

	select {
	case received := <-subA1.C:
		if received.TaskID != "task-1" {
			t.Fatalf("subA1 got task %q, want task-1", received.TaskID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subA1 did not receive itemA")
	}

	select {
	case received := <-subA2.C:
		if received.TaskID != "task-1" {
			t.Fatalf("subA2 got task %q, want task-1", received.TaskID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subA2 did not receive itemA")
	}

	// subB should NOT receive itemA
	select {
	case received := <-subB.C:
		t.Fatalf("subB received unexpected item: %+v", received)
	default:
	}

	// Unsubscribe subA1
	broker.Unsubscribe(subA1)
	if count := broker.ActiveSubscribers("agent-A"); count != 1 {
		t.Fatalf("agent-A subscribers after unsubscribe = %d, want 1", count)
	}

	itemA2 := InboxItem{
		Sequence:      2,
		TargetAgentID: "agent-A",
		TaskID:        "task-2",
	}
	broker.Publish(itemA2)

	select {
	case <-subA1.C:
		t.Fatal("subA1 received item after unsubscribe")
	default:
	}

	select {
	case received := <-subA2.C:
		if received.TaskID != "task-2" {
			t.Fatalf("subA2 got task %q, want task-2", received.TaskID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subA2 did not receive itemA2")
	}

	broker.Unsubscribe(subA2)
	broker.Unsubscribe(subB)
	if count := broker.ActiveSubscribers("agent-A"); count != 0 {
		t.Fatalf("agent-A subscribers after all unsubscribe = %d, want 0", count)
	}
}
