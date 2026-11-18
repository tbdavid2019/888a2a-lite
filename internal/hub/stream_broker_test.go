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

func TestInboxEventBrokerCircleSkillEventsOptInIsolation(t *testing.T) {
	broker := NewInboxEventBroker()
	legacySub := broker.Subscribe("agent-legacy", 4)
	optInSub := broker.SubscribeWithCircleEvents("agent-c1", "circle-1", 4)
	otherCircleSub := broker.SubscribeWithCircleEvents("agent-c2", "circle-2", 4)
	defer broker.Unsubscribe(legacySub)
	defer broker.Unsubscribe(optInSub)
	defer broker.Unsubscribe(otherCircleSub)

	broker.PublishCircleSkillEvent(CircleSkillEvent{
		Action:            SkillEventActionUpdated,
		CircleID:          "circle-1",
		Slug:              "deploy-playbook",
		Version:           2,
		CollectionVersion: 5,
		ContentHash:       "abc123",
		UpdatedAt:         "2026-11-18T00:00:00Z",
	})

	if legacySub.CircleEvents != nil {
		t.Fatal("legacy Subscribe must not allocate CircleEvents channel")
	}
	select {
	case ev := <-optInSub.CircleEvents:
		if ev.Slug != "deploy-playbook" || ev.CollectionVersion != 5 {
			t.Fatalf("unexpected circle skill event: %+v", ev)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("optInSub did not receive circle skill event")
	}
	select {
	case ev := <-otherCircleSub.CircleEvents:
		t.Fatalf("otherCircleSub received cross-circle event: %+v", ev)
	default:
	}
}
