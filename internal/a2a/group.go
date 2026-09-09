package a2a

import "time"

const (
	GroupReplyPolicyAll           = "ALL"
	GroupReplyPolicyMentionedOnly = "MENTIONED_ONLY"
	GroupReplyPolicyAckOnly       = "ACK_ONLY"
)

type GroupTaskMember struct {
	ParentTaskID  string
	MemberTaskID  string
	GroupID       string
	CircleID      string
	TargetAgentID string
	ReplyPolicy   string
	Mentions      []string
	Ordinal       int
	CreatedAt     time.Time
}
