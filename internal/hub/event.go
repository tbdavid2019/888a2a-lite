package hub

import "time"

type Event struct {
	ID            uint64         `json:"id"`
	HubID         string         `json:"hubId"`
	Type          string         `json:"type"`
	ActorAgentID  string         `json:"actorAgentId,omitempty"`
	TargetAgentID string         `json:"targetAgentId,omitempty"`
	TaskID        string         `json:"taskId,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
}

const (
	EventAgentRegistered             = "agent.registered"
	EventAgentRegistrationRetry      = "agent.registration_retry"
	EventAgentHeartbeat              = "agent.heartbeat"
	EventAgentDisconnected           = "agent.disconnected"
	EventAgentRevoked                = "agent.revoked"
	EventTaskQueued                  = "task.queued"
	EventTaskDuplicate               = "task.duplicate"
	EventInboxPolled                 = "inbox.polled"
	EventInboxAcknowledged           = "inbox.acknowledged"
	EventTaskCanceled                = "task.canceled"
	EventRegistrationChanged         = "registration.changed"
	EventHubStarted                  = "hub.started"
	EventHubStopped                  = "hub.stopped"
	EventAnnouncementPublished       = "announcement.published"
	EventAnnouncementDraftCreated    = "announcement.draft_created"
	EventAnnouncementDraftUpdated    = "announcement.draft_updated"
	EventAnnouncementRevisionCreated = "announcement.revision_created"
	EventGroupCreated                = "group.created"
	EventGroupInvitationCreated      = "group.invitation_created"
	EventGroupInvitationAccepted     = "group.invitation_accepted"
	EventGroupMemberLeft             = "group.member_left"
	EventGroupMemberRemoved          = "group.member_removed"
	EventGroupOwnershipTransferred   = "group.ownership_transferred"
	EventGroupArchived               = "group.archived"
	EventGroupMessageQueued          = "group.message_queued"
	EventGroupMessageDuplicate       = "group.message_duplicate"
	EventGroupRosterViewed           = "group.roster_viewed"
	EventGroupHistoryViewed          = "group.history_viewed"
	EventGroupDeliveryPolled         = "group.delivery_polled"
	EventGroupAuthorizationDenied    = "group.authorization_denied"
	EventAgentDeleted                = "agent.deleted"
	EventAgentsPruned                = "agents.pruned"
)

