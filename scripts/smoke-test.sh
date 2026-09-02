#!/bin/sh
set -eu

HUB_URL=${HUB_URL:-http://127.0.0.1:8080}
HUB_SERVICE=${HUB_SERVICE:-hub}
OPERATOR_TOKEN=${A2A888_HUB_OPERATOR_TOKEN:?A2A888_HUB_OPERATOR_TOKEN is required}
RUN_ID=${SMOKE_RUN_ID:-$(date +%s)}

command -v curl >/dev/null 2>&1 || { echo "curl is required" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

fail() {
	echo "smoke test failed: $1" >&2
	exit 1
}

request() {
	method=$1
	path=$2
	output=$3
	shift 3
	curl -sS -X "$method" "$HUB_URL$path" "$@" -o "$output" -w '%{http_code}'
}

register_agent() {
	name=$1
	provider=$2
	key=$3
	output=$4
	status=$(request POST /hub/v1/agents/register "$output" \
		-H 'Content-Type: application/json' \
		--data "{\"displayName\":\"$name\",\"providerFamily\":\"$provider\",\"transportId\":\"http-json\",\"capabilities\":[\"text/plain\"],\"registrationIdempotencyKey\":\"$key\"}")
	[ "$status" = 201 ] || fail "registration returned HTTP $status"
}

health_file=$workdir/health.json
health_status=000
health_attempt=1
while [ "$health_attempt" -le 30 ]; do
	health_status=$(request GET /healthz "$health_file" 2>/dev/null || true)
	[ "$health_status" = 200 ] && break
	sleep 1
	health_attempt=$((health_attempt + 1))
done
[ "$health_status" = 200 ] || fail "health returned HTTP $health_status after readiness wait"

control_file=$workdir/control.json
control_status=$(request POST /hub/v1/admin/registration "$control_file" \
	-H "Authorization: Bearer $OPERATOR_TOKEN" \
	-H 'Content-Type: application/json' \
	--data '{"enabled":true}')
[ "$control_status" = 200 ] || fail "enabling registration returned HTTP $control_status"

register_agent smoke-codex codex "smoke-codex-installation-$RUN_ID" "$workdir/agent-a.json"
register_agent smoke-openclaw openclaw "smoke-openclaw-installation-$RUN_ID" "$workdir/agent-b.json"
register_agent smoke-hermes hermes "smoke-hermes-installation-$RUN_ID" "$workdir/agent-c.json"

agent_a=$(jq -r '.identity.agentId' "$workdir/agent-a.json")
token_a=$(jq -r '.identity.agentToken' "$workdir/agent-a.json")
agent_b=$(jq -r '.identity.agentId' "$workdir/agent-b.json")
token_b=$(jq -r '.identity.agentToken' "$workdir/agent-b.json")
agent_c=$(jq -r '.identity.agentId' "$workdir/agent-c.json")
token_c=$(jq -r '.identity.agentToken' "$workdir/agent-c.json")

peers_file=$workdir/peers.json
peers_status=$(request GET /hub/v1/agents "$peers_file" \
	-H "X-Agent-ID: $agent_a" -H "Authorization: Bearer $token_a")
[ "$peers_status" = 200 ] || fail "peer list returned HTTP $peers_status"
jq -e --arg a "$agent_a" --arg b "$agent_b" --arg c "$agent_c" \
	'[.agents[].agentId] | index($a) and index($b) and index($c)' "$peers_file" >/dev/null || fail "peer list missed a registered agent"

task_body='{"contextId":"smoke-context","idempotencyKey":"smoke-task-1","message":"smoke notification","taskId":"smoke-task-1"}'
task_file=$workdir/task.json
task_status=$(request POST "/hub/v1/agents/$agent_b/tasks" "$task_file" \
	-H "X-Agent-ID: $agent_a" -H "Authorization: Bearer $token_a" \
	-H 'Content-Type: application/json' --data "$task_body")
[ "$task_status" = 202 ] || fail "task delivery returned HTTP $task_status"

duplicate_file=$workdir/duplicate.json
duplicate_status=$(request POST "/hub/v1/agents/$agent_b/tasks" "$duplicate_file" \
	-H "X-Agent-ID: $agent_a" -H "Authorization: Bearer $token_a" \
	-H 'Content-Type: application/json' --data "$task_body")
[ "$duplicate_status" = 202 ] || fail "duplicate task returned HTTP $duplicate_status"
jq -e '.deliveryOutcome == "DUPLICATE"' "$duplicate_file" >/dev/null || fail "duplicate task was not idempotent"

inbox_file=$workdir/inbox.json
inbox_status=$(request GET "/hub/v1/agents/$agent_b/inbox?afterSequence=0" "$inbox_file" \
	-H "X-Agent-ID: $agent_b" -H "Authorization: Bearer $token_b")
[ "$inbox_status" = 200 ] || fail "inbox poll returned HTTP $inbox_status"
sequence=$(jq -r '.items[0].sequence' "$inbox_file")
[ "$sequence" != null ] && [ "$sequence" != 0 ] || fail "inbox item was not delivered"

ack_file=$workdir/ack.json
ack_status=$(request POST "/hub/v1/agents/$agent_b/inbox/$sequence/ack" "$ack_file" \
	-H "X-Agent-ID: $agent_b" -H "Authorization: Bearer $token_b")
[ "$ack_status" = 200 ] || fail "inbox ACK returned HTTP $ack_status"

group_file=$workdir/group.json
group_status=$(request POST /hub/v1/groups "$group_file" \
	-H "X-Agent-ID: $agent_a" -H "Authorization: Bearer $token_a" \
	-H 'Content-Type: application/json' --data '{"name":"smoke coordination"}')
[ "$group_status" = 201 ] || fail "group creation returned HTTP $group_status"
group_id=$(jq -r '.groupId' "$group_file")
[ "$group_id" != null ] && [ -n "$group_id" ] || fail "group ID was not returned"

invite_b_file=$workdir/invite-b.json
invite_b_status=$(request POST "/hub/v1/groups/$group_id/invitations" "$invite_b_file" \
	-H "X-Agent-ID: $agent_a" -H "Authorization: Bearer $token_a" \
	-H 'Content-Type: application/json' --data "{\"agentId\":\"$agent_b\"}")
[ "$invite_b_status" = 201 ] || fail "group invite for Agent B returned HTTP $invite_b_status"
invite_b=$(jq -r '.invitationId' "$invite_b_file")

invite_c_file=$workdir/invite-c.json
invite_c_status=$(request POST "/hub/v1/groups/$group_id/invitations" "$invite_c_file" \
	-H "X-Agent-ID: $agent_a" -H "Authorization: Bearer $token_a" \
	-H 'Content-Type: application/json' --data "{\"agentId\":\"$agent_c\"}")
[ "$invite_c_status" = 201 ] || fail "group invite for Agent C returned HTTP $invite_c_status"
invite_c=$(jq -r '.invitationId' "$invite_c_file")

accept_b_file=$workdir/accept-b.json
accept_b_status=$(request POST "/hub/v1/groups/invitations/$invite_b/accept" "$accept_b_file" \
	-H "X-Agent-ID: $agent_b" -H "Authorization: Bearer $token_b")
[ "$accept_b_status" = 200 ] || fail "group invite acceptance for Agent B returned HTTP $accept_b_status"
accept_c_file=$workdir/accept-c.json
accept_c_status=$(request POST "/hub/v1/groups/invitations/$invite_c/accept" "$accept_c_file" \
	-H "X-Agent-ID: $agent_c" -H "Authorization: Bearer $token_c")
[ "$accept_c_status" = 200 ] || fail "group invite acceptance for Agent C returned HTTP $accept_c_status"

roster_file=$workdir/roster.json
roster_status=$(request GET "/hub/v1/groups/$group_id/roster" "$roster_file" \
	-H "X-Agent-ID: $agent_a" -H "Authorization: Bearer $token_a")
[ "$roster_status" = 200 ] || fail "group roster returned HTTP $roster_status"
jq -e --arg a "$agent_a" --arg b "$agent_b" --arg c "$agent_c" \
	'[.members[].agentId] | index($a) and index($b) and index($c)' "$roster_file" >/dev/null || fail "group roster missed a member"

group_message_body='{"contextId":"smoke-group-context","idempotencyKey":"smoke-group-1","message":"smoke group message"}'
group_message_file=$workdir/group-message.json
group_message_status=$(request POST "/hub/v1/groups/$group_id/messages" "$group_message_file" \
	-H "X-Agent-ID: $agent_a" -H "Authorization: Bearer $token_a" \
	-H 'Content-Type: application/json' --data "$group_message_body")
[ "$group_message_status" = 202 ] || fail "group message returned HTTP $group_message_status"
jq -e '.message.trust == "UNTRUSTED_DATA" and (.message.deliveries | length) == 2' "$group_message_file" >/dev/null || fail "group message fan-out summary was incorrect"

group_duplicate_file=$workdir/group-duplicate.json
group_duplicate_status=$(request POST "/hub/v1/groups/$group_id/messages" "$group_duplicate_file" \
	-H "X-Agent-ID: $agent_a" -H "Authorization: Bearer $token_a" \
	-H 'Content-Type: application/json' --data "$group_message_body")
[ "$group_duplicate_status" = 202 ] || fail "duplicate group message returned HTTP $group_duplicate_status"
jq -e '.deliveryOutcome == "DUPLICATE"' "$group_duplicate_file" >/dev/null || fail "duplicate group message was not idempotent"

for target in b c; do
	if [ "$target" = b ]; then
		target_id=$agent_b
		target_token=$token_b
	else
		target_id=$agent_c
		target_token=$token_c
	fi
	target_file=$workdir/group-inbox-$target.json
	target_status=$(request GET "/hub/v1/agents/$target_id/inbox?afterSequence=0" "$target_file" \
		-H "X-Agent-ID: $target_id" -H "Authorization: Bearer $target_token")
	[ "$target_status" = 200 ] || fail "group inbox $target returned HTTP $target_status"
	group_sequence=$(jq -r '.items[] | select(.groupMessageId > 0) | .sequence' "$target_file" | head -n 1)
	[ -n "$group_sequence" ] && [ "$group_sequence" != null ] || fail "group delivery for Agent $target was not delivered"
	ack_status=$(request POST "/hub/v1/agents/$target_id/inbox/$group_sequence/ack" "$workdir/group-ack-$target.json" \
		-H "X-Agent-ID: $target_id" -H "Authorization: Bearer $target_token")
	[ "$ack_status" = 200 ] || fail "group ACK for Agent $target returned HTTP $ack_status"
done

group_recovery_file=$workdir/group-recovery.json
group_recovery_status=$(request POST "/hub/v1/groups/$group_id/messages" "$group_recovery_file" \
	-H "X-Agent-ID: $agent_a" -H "Authorization: Bearer $token_a" \
	-H 'Content-Type: application/json' --data '{"contextId":"smoke-group-recovery","idempotencyKey":"smoke-group-recovery","message":"recover group message"}')
[ "$group_recovery_status" = 202 ] || fail "group recovery message returned HTTP $group_recovery_status"

recovery_body='{"contextId":"smoke-recovery","idempotencyKey":"smoke-task-recovery","message":"recover me","taskId":"smoke-task-recovery"}'
recovery_file=$workdir/recovery.json
recovery_status=$(request POST "/hub/v1/agents/$agent_b/tasks" "$recovery_file" \
	-H "X-Agent-ID: $agent_a" -H "Authorization: Bearer $token_a" \
	-H 'Content-Type: application/json' --data "$recovery_body")
[ "$recovery_status" = 202 ] || fail "recovery task returned HTTP $recovery_status"

docker compose restart "$HUB_SERVICE" >/dev/null

recovered_file=$workdir/recovered.json
recovered_status=$(request GET "/hub/v1/agents/$agent_b/inbox?afterSequence=0" "$recovered_file" \
	-H "X-Agent-ID: $agent_b" -H "Authorization: Bearer $token_b")
[ "$recovered_status" = 200 ] || fail "recovered inbox poll returned HTTP $recovered_status"
jq -e '.items | any(.[]; .taskId == "smoke-task-recovery" and .state == "PENDING")' "$recovered_file" >/dev/null || fail "unacknowledged task did not recover"

group_recovered_file=$workdir/group-recovered.json
group_recovered_status=$(request GET "/hub/v1/agents/$agent_b/inbox?afterSequence=0" "$group_recovered_file" \
	-H "X-Agent-ID: $agent_b" -H "Authorization: Bearer $token_b")
[ "$group_recovered_status" = 200 ] || fail "recovered group inbox returned HTTP $group_recovered_status"
jq -e '.items | any(.[]; .message == "recover group message" and .state == "PENDING")' "$group_recovered_file" >/dev/null || fail "unacknowledged group message did not recover"

remove_member_file=$workdir/remove-member.json
remove_member_status=$(request POST "/hub/v1/groups/$group_id/members/$agent_c/remove" "$remove_member_file" \
	-H "X-Agent-ID: $agent_a" -H "Authorization: Bearer $token_a")
[ "$remove_member_status" = 200 ] || fail "group member removal returned HTTP $remove_member_status"
removed_history_status=$(request GET "/hub/v1/groups/$group_id/history?afterId=0" "$workdir/removed-history.json" \
	-H "X-Agent-ID: $agent_c" -H "Authorization: Bearer $token_c")
[ "$removed_history_status" = 403 ] || fail "removed member history returned HTTP $removed_history_status"
archive_group_status=$(request POST "/hub/v1/groups/$group_id/archive" "$workdir/archive-group.json" \
	-H "X-Agent-ID: $agent_a" -H "Authorization: Bearer $token_a")
[ "$archive_group_status" = 200 ] || fail "group archive returned HTTP $archive_group_status"

revoke_file=$workdir/revoke.json
revoke_status=$(request POST "/hub/v1/admin/agents/$agent_c/revoke" "$revoke_file" \
	-H "Authorization: Bearer $OPERATOR_TOKEN" -H 'Content-Type: application/json' \
	--data '{"reason":"smoke test"}')
[ "$revoke_status" = 200 ] || fail "revoke returned HTTP $revoke_status"

revoked_file=$workdir/revoked.json
revoked_status=$(request POST "/hub/v1/agents/$agent_c/heartbeat" "$revoked_file" \
	-H "X-Agent-ID: $agent_c" -H "Authorization: Bearer $token_c")
[ "$revoked_status" = 401 ] || fail "revoked heartbeat returned HTTP $revoked_status"

control_status=$(request POST /hub/v1/admin/registration "$control_file" \
	-H "Authorization: Bearer $OPERATOR_TOKEN" -H 'Content-Type: application/json' \
	--data '{"enabled":false}')
[ "$control_status" = 200 ] || fail "disabling registration returned HTTP $control_status"

blocked_file=$workdir/blocked.json
blocked_status=$(request POST /hub/v1/agents/register "$blocked_file" \
	-H 'Content-Type: application/json' \
	--data '{"displayName":"blocked","providerFamily":"test","transportId":"http-json","registrationIdempotencyKey":"smoke-blocked"}')
[ "$blocked_status" = 403 ] || fail "disabled registration returned HTTP $blocked_status"

echo "888a2a-lite smoke test passed"
