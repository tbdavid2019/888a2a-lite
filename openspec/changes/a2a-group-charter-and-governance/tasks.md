## 1. Dependency and governance boundary

- [ ] 1.1 Confirm Phases 1–3 and `a2a-group-coordination-extension` pass their CI/remote gates; record the fixed A2A protocol/SDK versions and Group Task/result/cancel behavior before enabling governance.
- [ ] 1.2 Define governance precedence: provider/system safety > local policy > Human-approved Charter > untrusted group message; verify Charter cannot grant tools, credentials, membership, circle, operator or remote execution authority.
- [ ] 1.3 Define 4A/4B/4C feature flags and rollout gates; verify 4B cannot activate without 4A Charter gate and 4C cannot activate without 4B approval/provenance gate.

## 2. 4A — Charter Core in the Hub

- [ ] 2.1 Extend the existing `agent_group` model and SQLite migration with `charter_version`, `has_charter`, `charter_content_hash`, and `charter_updated_at`; verify legacy groups use version 0/hasCharter false and retain old behavior.
- [ ] 2.2 Add `group_charter_revision` history storage scoped by hub/circle/group/version, including content hash, updatedBy, timestamps, and superseded relationship; verify restart migration preserves all existing groups.
- [ ] 2.3 Implement bounded UTF-8 Markdown subset validation, raw HTML/script/event-handler rejection, credential-like content rejection, and no uncontrolled external resource policy; verify invalid content causes no write.
- [ ] 2.4 Implement `GET /hub/v1/groups/{groupId}/charter` with active-member authorization, camelCase response fields, no-store behavior, and no cross-circle leakage; verify non-member/disabled/archived access is masked.
- [ ] 2.5 Implement `PUT /hub/v1/groups/{groupId}/charter` with Owner/Admin authorization, `expectedVersion`, idempotency key, atomic CAS, content hash, and audit event; verify concurrent updates return one success and one 409.
- [ ] 2.6 Implement explicit charter rollback as a new revision using CAS; verify old revisions remain immutable and rollback cannot be used to overwrite a newer concurrent version.
- [ ] 2.7 Define and implement durable `CHARTER_UPDATED` event/replay envelope containing group/circle/version/hash/updatedBy/revision without full Charter content; verify old Bridge clients safely ignore unknown governance events.

## 3. 4A — Bridge cache and safe prompt context

- [ ] 3.1 Implement scoped Charter cache under Hub/Circle/Group directories with metadata, 0600 permissions, atomic replace, symlink protection, size limit, version and hash verification; verify caches cannot cross Hub or Circle.
- [ ] 3.2 Refresh Charter on group accept, Bridge startup, group stream connection, version event, or ETag mismatch; verify missed events are recovered and duplicate events do not cause duplicate writes.
- [ ] 3.3 Define stale/offline behavior for optional versus required Charter; verify stale optional cache is labeled and required governance work pauses without silently using unverified content.
- [ ] 3.4 Implement safe prompt assembly with delimiters and fixed policy precedence; verify Charter prompt injection cannot authorize shell, credential access, ACL changes, or data export.
- [ ] 3.5 Add 4A Go/Python integration tests for Charter CRUD, CAS, history, event replay, cache recovery, unsafe content, scope isolation, and prompt boundary; verify all run in CI.

## 4. 4B — Secretary appointment and meeting sessions

- [ ] 4.1 Add `group_secretary` persistence for group/circle/Agent, epoch, state, lease expiry, appointedBy, and updatedAt; verify only one active secretary lease exists per group/epoch.
- [ ] 4.2 Implement Owner/Admin secretary appointment, replacement, revoke, lease renewal, and failover with CAS/epoch; verify old secretary jobs and approvals are rejected after epoch changes.
- [ ] 4.3 Add `--role=secretary`, `--auto-minutes`, and charter cache options to the Bridge; verify flags do not grant role authority without Hub appointment and valid lease.
- [ ] 4.4 Add `meeting_session` and idempotent synthesis job records with immutable start/cutoff revisions, trigger identity, Charter version, state, and job ID; verify repeated `/minutes`/`/wrapup` does not create duplicate jobs.
- [ ] 4.5 Implement authorized `/minutes`, `/wrapup`, and `/summary` command handling; verify only Human/Owner or explicitly charter-authorized members can trigger synthesis and untrusted message text cannot invoke commands.
- [ ] 4.6 Implement secretary full-stream observation with durable revision replay and bounded context windows; verify missed events, reconnect, and secretary failover do not mix sessions or duplicate turns.

## 5. 4B — Structured minutes, memory, and approval

- [ ] 5.1 Define structured JSON schema and Markdown renderer for Decisions, Action Items, and Artifact References; verify malformed LLM output is rejected or held as draft without side effects.
- [ ] 5.2 Persist source event/message IDs, revision range, session ID, Charter version, proposer, model/schema version, content hash, needsReview, and status for every extracted item; verify each item is traceable to source events.
- [ ] 5.3 Add local `work.db` tables `group_minutes`, `group_decisions`, `group_action_items`, and migrations with hub/circle/group/session scope, WAL, 0600 permissions, retention, and writer locking; verify restart and multi-scope isolation.
- [ ] 5.4 Implement Decision states `DRAFT/CONFIRMED/REJECTED` and Action states `DRAFT/APPROVED/DISPATCHED/COMPLETED/CANCELED`; verify synthesis never auto-confirms a decision or dispatches an action.
- [ ] 5.5 Implement Human/Owner approval UI/API with Agent ID assignee resolution, timezone-aware deadlines, safe content validation, approval audit, and idempotency; verify unauthorized approval and unknown assignee are rejected.
- [ ] 5.6 Implement `charter_amendment` proposal, diff, baseVersion CAS, Owner approval, apply/reject states, and audit; verify Secretary cannot directly mutate Charter and stale amendments return 409.

## 6. 4C — Export outbox and external integrations

- [ ] 6.1 Implement Markdown export with bounded content, secret redaction, scoped filename, temporary file, atomic rename, and idempotency; verify export failure leaves approved minutes intact.
- [ ] 6.2 Add `export_outbox` with provider, payload hash, idempotency key, attempts, retry time, state, last error, remote ID, and dead-letter handling; verify restart/timeout retries do not duplicate remote outputs.
- [ ] 6.3 Implement optional Wiki/GitHub/Webhook adapters behind explicit configuration; verify credentials come only from process secret sources and never enter DB, logs, UI, or exported content.
- [ ] 6.4 Enforce HTTPS, host allowlist, timeout, response bounds, webhook signing, and SSRF-safe URL validation; verify disallowed hosts and malformed responses are rejected.
- [ ] 6.5 Add approval-controlled Action Item dispatch through standard Hub task routing; verify dispatch requires APPROVED state, idempotency, authorized assignee, and does not execute code through the Hub.

## 7. Documentation and rollout verification

- [ ] 7.1 Document actual implementation paths (`internal/hub/group.go`, `internal/service/groups.go`, `internal/service/http.go`, `internal/store/sqlite/sqlite.go`, `internal/store/sqlite/group_repository.go`, and both Bridge assets); verify tasks no longer reference nonexistent `internal/hub/storage.go` or `groups` table.
- [ ] 7.2 Document Charter precedence, no-charter compatibility, cache scope, secretary appointment, session commands, draft/approval lifecycle, export opt-in, retention, and failure recovery in Chinese and English docs.
- [ ] 7.3 Add tests for prompt injection, XSS-safe Markdown rendering, credential redaction, command authorization, source provenance, stale cache, epoch split-brain, CAS conflict, outbox retry, and cross-circle isolation; verify CI coverage is behavior-based.
- [ ] 7.4 Run GitHub Actions Go/Python/browser tests and the required remote smoke test on `david@10.9.0.11`; enable 4A, 4B, and 4C independently only after each gate passes.
