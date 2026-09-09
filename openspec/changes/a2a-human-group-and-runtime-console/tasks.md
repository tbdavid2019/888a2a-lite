## 1. Dependency and contract gate

- [ ] 1.1 Confirm `a2a-group-coordination-extension` is complete in CI, including group discovery, Parent/Member Task aggregation, all eligible member delivery for `MENTIONED_ONLY`, result updates, cancellation races, and stream replay; record the evidence before starting Phase 3.
- [ ] 1.2 Define the Human Agent identity, membership, sender type, standard Group Gateway request envelope, extension metadata namespace, `replyPolicy`, `mentions`, Parent Task reference, and local API error schema; verify the contract is documented independently from legacy `/hub/v1/groups`.
- [ ] 1.3 Define local UI security, runtime config schema, service lifecycle states, group history scope, event cursor, and embedded/distributed asset parity; verify all mutation and restart behavior has an acceptance case.

## 2. Local runtime read-only foundation

- [ ] 2.1 Implement `detect_runtimes()` in both bridge assets for OpenClaw, Claude Code, Goose, Hermes, Codex, and OpenCode; verify PATH/known-directory scanning returns bounded executable paths and does not claim provider login health.
- [ ] 2.2 Implement `GET /api/runtimes` with `ready`, `cli_needed`, and `unavailable` states, version probe timeout, active process backend, and desired backend; verify response contains no environment values, Token, API key, or secret command text.
- [ ] 2.3 Add the Runtime read-only panel and status cards; verify missing, executable, probe failure, active, and pending states render correctly without invoking a runtime.
- [ ] 2.4 Add source/embedded bridge parity verification; verify downloaded `/a2a_bridge.py` and the source asset expose the same runtime API and security behavior.

## 3. Local UI security boundary

- [ ] 3.1 Generate a high-entropy per-process local UI session/CSRF token and inject it through a safe same-origin bootstrap mechanism; verify tokens are absent from URLs and logs and invalid after server restart.
- [ ] 3.2 Enforce loopback Host validation, Origin/Referer checks, strict JSON Content-Type, bounded bodies, unknown-field rejection, no-store/security headers, and constant-time token comparison; verify cross-origin and missing-token mutations are rejected.
- [ ] 3.3 Apply the local security boundary to group sends, custom Runtime mutation, service install, service restart, and active Runtime changes; verify GET history/events never trigger an OS mutation.

## 4. Runtime configuration and service control

- [ ] 4.1 Implement `POST /api/runtimes/custom` using bounded name/ID, absolute executable, argv array, and environment name references; verify shell operators, arbitrary paths, environment values, duplicate IDs, and oversized input are rejected.
- [ ] 4.2 Persist Runtime configuration with schema version, 0600 permissions, atomic replace, and no secret values; verify restart recovery, malformed-file handling, and configuration migration.
- [ ] 4.3 Implement Runtime selection using persisted desired config plus immutable current process state; verify UI reports requested/pending/active accurately and does not claim an un-restarted process changed backend.
- [ ] 4.4 Implement fixed managed LaunchAgent/systemd user service status/install/restart endpoints with idempotency and bounded OS command execution; verify browser cannot provide arbitrary unit/plist names or arguments.
- [ ] 4.5 Implement failed-start rollback and service status reporting; verify a new Runtime failure preserves old config, credential file, queue, and recoverable service state.
- [ ] 4.6 Add the Runtime panel controls for add/select/install/restart; verify ready-only execution, confirmation/error state, no duplicate service units, and accessible status feedback.

## 5. Human Agent and group data layer

- [ ] 5.1 Load Human Agent ID/Token and expose only safe local identity metadata; verify group APIs use the existing Hub Agent principal and cannot bypass membership with a local session.
- [ ] 5.2 Add scoped local group tables/migrations for hub/circle/group/parent/member task/message/sequence/revision/sender type/policy/mentions/state/idempotency; verify P2P history and group history use separate cursors and old data remains readable.
- [ ] 5.3 Implement bounded `GET /api/groups` and `GET /api/groups/{id}/messages` using same-circle Hub discovery/Card and durable local hydration; verify archived, removed, expired, revoked, and cross-circle groups are hidden.
- [ ] 5.4 Add group timeline models for Human and Agent senders, Parent/Member Task progress, empty completion, failure metadata, and ordered results; verify duplicate SSE events do not duplicate bubbles.

## 6. Human group workspace and mentions

- [ ] 6.1 Add Groups navigation, active group switching, roster display, bounded history loading, and reconnect state to `CLIENT_HTML`; verify group state does not leak into P2P conversations.
- [ ] 6.2 Implement mention autocomplete from the active group roster with unique Agent ID binding, duplicate display-name disambiguation, HTML escaping, and bounded mention count; verify display names never become routing or authorization keys.
- [ ] 6.3 Implement client policy mapping: no mention → `ACK_ONLY` with empty mentions; one or more mentions → `MENTIONED_ONLY` with exact Agent IDs; verify local validation and Hub validation reject non-members.
- [ ] 6.4 Implement `POST /api/groups/{id}/messages` as a standard Group Gateway facade using `tenant: group:<groupId>`, `A2A-Extensions`, extension metadata, `returnImmediately`, and idempotency; verify it never sends policy-bearing messages through legacy `/hub/v1/groups/{id}/messages`.
- [ ] 6.5 Persist the local pending message before dispatch and reconcile Parent Task ID/status/result; verify HTTP timeout remains pending/failed according to contract and is never displayed as completed without a confirmed result.

## 7. Bridge delivery and Task stream integration

- [ ] 7.1 Update both Bridge assets to receive all eligible Human group deliveries, commit locally before ACK, and pass Parent/Member correlation to the work queue; verify unmentioned members still receive and acknowledge delivery.
- [ ] 7.2 Enforce Bridge behavior for `ACK_ONLY` and unmentioned `MENTIONED_ONLY`: durable ACK, no Runtime invocation, empty COMPLETED update, no reciprocal group message; verify no echo task is created.
- [ ] 7.3 Enforce mentioned-bot behavior: durable ACK, execution lease, selected Runtime invocation, correlated Member update, retry/outbox, and Parent aggregation; verify response is attached to the originating group task.
- [ ] 7.4 Subscribe the Local UI to standard Parent Task stream/subscribe and merge revisions into group history; verify P2P `/api/events` and standard group events use separate serializers and cursors.
- [ ] 7.5 Add reconnect, restart, late result, cancellation, membership removal, and circle disable handling; verify UI stops mutation and hides stale group state after authorization loss.

## 8. Verification and rollout

- [ ] 8.1 Add Python unit tests for runtime detection, custom argv validation, local token/Origin checks, mention parsing, policy mapping, durable group history, duplicate event hydration, and silent ACK.
- [ ] 8.2 Add Go/HTTP integration fixtures for Human Agent membership, standard Group Gateway payloads, all-member `MENTIONED_ONLY` delivery, Parent Task result aggregation, cross-circle masking, and legacy group compatibility.
- [ ] 8.3 Add browser tests for Groups, autocomplete, human/bot timeline, Runtime panel, pending/active/error states, CSRF rejection, and service-control confirmation; capture console/network failures.
- [ ] 8.4 Add macOS LaunchAgent and Linux systemd user service matrix tests for idempotent install, restart, failed start rollback, fixed unit scope, and no privileged command execution.
- [ ] 8.5 Verify README, `llms.txt`, client Skill, and embedded install output describe Human Agent membership, standard Group Gateway, local security, Runtime limits, and Buzz scope without unsupported claims.
- [ ] 8.6 Run GitHub Actions Go/Python/browser/service tests, then the required remote smoke test on `david@10.9.0.11`; enable privileged Runtime controls only after standard group, legacy P2P/group, restart, and Multi-Circle checks pass.
