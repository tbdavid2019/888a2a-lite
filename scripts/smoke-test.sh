#!/bin/sh
set -eu

HUB_URL=${HUB_URL:-http://127.0.0.1:8080}
HUB_SERVICE=${HUB_SERVICE:-hub}
OPERATOR_TOKEN=${A2A888_HUB_OPERATOR_TOKEN:?A2A888_HUB_OPERATOR_TOKEN is required}

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

register_agent smoke-codex codex smoke-codex-installation "$workdir/agent-a.json"
register_agent smoke-openclaw openclaw smoke-openclaw-installation "$workdir/agent-b.json"
register_agent smoke-hermes hermes smoke-hermes-installation "$workdir/agent-c.json"

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
