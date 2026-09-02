package hub

import (
	"strings"
	"testing"
)

func TestGroupInputValidation(t *testing.T) {
	if err := ValidateCreateGroup(CreateGroupInput{Name: "team"}); err != nil {
		t.Fatalf("valid group rejected: %v", err)
	}
	if err := ValidateGroupMessage(GroupMessageInput{ContextID: "ctx", IdempotencyKey: "k", Message: "hello"}, 32); err != nil {
		t.Fatalf("valid group message rejected: %v", err)
	}
	for _, input := range []CreateGroupInput{{Name: ""}, {Name: strings.Repeat("x", MaxGroupNameLength+1)}} {
		if err := ValidateCreateGroup(input); err == nil {
			t.Fatalf("invalid group accepted: %+v", input)
		}
	}
	for _, input := range []GroupMessageInput{
		{ContextID: "", IdempotencyKey: "k", Message: "hello"},
		{ContextID: "ctx", IdempotencyKey: "", Message: "hello"},
		{ContextID: "ctx", IdempotencyKey: "k", Message: ""},
		{ContextID: "ctx", IdempotencyKey: "k", Message: strings.Repeat("x", 33)},
	} {
		if err := ValidateGroupMessage(input, 32); err == nil {
			t.Fatalf("invalid group message accepted: %+v", input)
		}
	}
}

func TestGroupRolesAndStates(t *testing.T) {
	owner := GroupMember{Role: GroupRoleOwner, State: MembershipActive}
	admin := GroupMember{Role: GroupRoleAdmin, State: MembershipActive}
	member := GroupMember{Role: GroupRoleMember, State: MembershipActive}
	if !owner.CanManageMembers() || !admin.CanManageMembers() || member.CanManageMembers() {
		t.Fatal("group role management rules are incorrect")
	}
	member.State = MembershipRemoved
	if member.IsActive() || member.CanManageMembers() {
		t.Fatal("inactive member retained active permissions")
	}
}
