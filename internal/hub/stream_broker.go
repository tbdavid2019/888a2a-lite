package hub

import (
	"sync"
)

// InboxSubscription represents an active streaming subscriber.
type InboxSubscription struct {
	AgentID      string
	CircleID     string
	C            chan InboxItem
	CircleEvents chan CircleSkillEvent
	done         chan struct{}
	closeOnce    sync.Once
}

// Close closes the subscription channel safely.
func (s *InboxSubscription) Close() {
	s.closeOnce.Do(func() {
		close(s.done)
	})
}

// InboxEventBroker manages in-memory pub-sub for agent inbox and optional circle skill event streams.
type InboxEventBroker struct {
	mu                sync.RWMutex
	subscribers       map[string]map[*InboxSubscription]struct{}
	circleSubscribers map[string]map[*InboxSubscription]struct{}
}

// NewInboxEventBroker creates a new event broker.
func NewInboxEventBroker() *InboxEventBroker {
	return &InboxEventBroker{
		subscribers:       make(map[string]map[*InboxSubscription]struct{}),
		circleSubscribers: make(map[string]map[*InboxSubscription]struct{}),
	}
}

// Subscribe registers a legacy inbox-only subscriber for incoming inbox items of target agentID.
func (b *InboxEventBroker) Subscribe(agentID string, bufferSize int) *InboxSubscription {
	return b.subscribe(agentID, "", false, bufferSize)
}

// SubscribeWithCircleEvents registers a subscriber for inbox items and capability-gated Circle skill events.
func (b *InboxEventBroker) SubscribeWithCircleEvents(agentID, circleID string, bufferSize int) *InboxSubscription {
	return b.subscribe(agentID, circleID, true, bufferSize)
}

func (b *InboxEventBroker) subscribe(agentID, circleID string, withCircleEvents bool, bufferSize int) *InboxSubscription {
	if bufferSize <= 0 {
		bufferSize = 64
	}
	sub := &InboxSubscription{
		AgentID:  agentID,
		CircleID: circleID,
		C:        make(chan InboxItem, bufferSize),
		done:     make(chan struct{}),
	}
	if withCircleEvents && circleID != "" {
		sub.CircleEvents = make(chan CircleSkillEvent, bufferSize)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	subs, ok := b.subscribers[agentID]
	if !ok {
		subs = make(map[*InboxSubscription]struct{})
		b.subscribers[agentID] = subs
	}
	subs[sub] = struct{}{}
	if sub.CircleEvents != nil {
		csubs, ok := b.circleSubscribers[circleID]
		if !ok {
			csubs = make(map[*InboxSubscription]struct{})
			b.circleSubscribers[circleID] = csubs
		}
		csubs[sub] = struct{}{}
	}
	return sub
}

// Unsubscribe removes an active subscription and closes its done channel.
func (b *InboxEventBroker) Unsubscribe(sub *InboxSubscription) {
	if sub == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if subs, ok := b.subscribers[sub.AgentID]; ok {
		delete(subs, sub)
		if len(subs) == 0 {
			delete(b.subscribers, sub.AgentID)
		}
	}
	if sub.CircleEvents != nil && sub.CircleID != "" {
		if csubs, ok := b.circleSubscribers[sub.CircleID]; ok {
			delete(csubs, sub)
			if len(csubs) == 0 {
				delete(b.circleSubscribers, sub.CircleID)
			}
		}
	}
	sub.Close()
}

// Publish dispatches an inbox item to all active subscribers of item.TargetAgentID.
func (b *InboxEventBroker) Publish(item InboxItem) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	subs, ok := b.subscribers[item.TargetAgentID]
	if !ok || len(subs) == 0 {
		return
	}
	for sub := range subs {
		select {
		case <-sub.done:
		case sub.C <- item:
		default:
			// Buffer full, drop without blocking publisher
		}
	}
}

// PublishCircleSkillEvent dispatches metadata-only Circle skill events strictly to opted-in subscribers in event.CircleID.
func (b *InboxEventBroker) PublishCircleSkillEvent(event CircleSkillEvent) {
	if event.CircleID == "" {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	subs, ok := b.circleSubscribers[event.CircleID]
	if !ok || len(subs) == 0 {
		return
	}
	for sub := range subs {
		if sub.CircleEvents == nil {
			continue
		}
		select {
		case <-sub.done:
		case sub.CircleEvents <- event:
		default:
			// Buffer full, drop without blocking publisher
		}
	}
}

// ActiveSubscribers returns the count of active subscriptions for an agent.
func (b *InboxEventBroker) ActiveSubscribers(agentID string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers[agentID])
}
