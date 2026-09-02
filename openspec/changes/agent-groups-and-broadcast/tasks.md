## 1. Contract and policy foundation

- [x] 1.1 Define group, membership, invitation, roster, group message, history cursor, and per-recipient delivery types; verify JSON compatibility and package tests in GitHub Actions
- [x] 1.2 Define group roles, membership states, stable error codes, group size/fan-out/history limits, and optional extension identifiers; verify invalid transitions and limit values are rejected in CI

## 2. Durable SQLite storage

- [x] 2.1 Add additive SQLite WAL migration for groups, memberships, invitations, group messages, and group deliveries with foreign keys and indexes; verify migration preserves existing Agent, direct inbox, announcement, and event rows in CI
- [x] 2.2 Implement repository transactions for group lifecycle, invitations, membership changes, and ownership transfer; verify idempotent accept/leave/remove/archive behavior and rollback on invalid mutations
- [x] 2.3 Implement atomic group message fan-out and per-recipient delivery lookup/ACK/cancel persistence; verify no partial fan-out occurs when a limit, membership, or storage check fails

## 3. Group domain service

- [ ] 3.1 Implement authenticated group create/list/get/archive operations with owner assignment and bounded names; verify archived groups reject future mutation
- [ ] 3.2 Implement invitation creation, explicit accept, member leave, admin authorization, owner transfer, and member removal; verify revoked/expired Agents cannot join and repeated operations are idempotent
- [ ] 3.3 Build member-only roster responses from existing Agent safe cards and heartbeat lease state; verify ONLINE/OFFLINE/EXPIRED/REVOKED status and absence of tokens, paths, and secrets
- [ ] 3.4 Implement member-only group history with monotonic group message IDs, `afterId`, bounded `limit`, next cursor, and visibility rules; verify removed members cannot read post-removal content

## 4. Group mailbox behavior

- [ ] 4.1 Extend delivery idempotency and sender authorization for `(hubId, groupId, requesterAgentId, idempotencyKey)`; verify duplicate group requests return the original group message without new sequences
- [ ] 4.2 Implement transaction-atomic fan-out to the membership snapshot at send time, excluding the sender inbox while retaining sender-visible history; verify recipient summaries and independent ACK state
- [ ] 4.3 Reuse direct inbox polling for group deliveries while preventing cross-recipient state access; verify offline recipients recover the same message and sequence after reconnect and Hub restart
- [ ] 4.4 Cancel pending group deliveries when a member is removed, without changing other recipients or already-polled data; verify audit events capture safe group/message/member identities only

## 5. HTTP and system discovery

- [ ] 5.1 Add `/hub/v1/groups` lifecycle, membership, roster, history, and message routes with existing error envelopes and authentication; verify member/non-member/owner authorization through HTTP tests
- [ ] 5.2 Enforce body, page, group size, fan-out, rate, and concurrency limits before mutation; verify oversized or malformed requests leave no database changes
- [ ] 5.3 Extend the dynamic system card with optional versioned group extension links, limits, and trust semantics; verify alternate Host and configured public URL responses remain dynamic
- [ ] 5.4 Record group lifecycle, membership, message, delivery, and authorization events in the durable operator audit feed without secrets or full sensitive payloads; verify retrieval after restart

## 6. Client and adapter support

- [ ] 6.1 Extend the generic HTTP client with group create/list/invite/accept/leave/roster/history/send methods and cursor handling; verify unknown optional fields remain compatible and retries are idempotent
- [ ] 6.2 Add CLI commands or examples for group lifecycle, roster, history, send, and ACK workflows; verify help text documents Agent Token versus operator Token and safe data handling
- [ ] 6.3 Update Codex, OpenClaw, Hermes, and generic adapter guidance to treat group content and roster metadata as untrusted data; verify dangerous group text never becomes executable local instructions

## 7. Security and integration tests

- [ ] 7.1 Add unit and integration coverage for credential separation, invitation consent, role boundaries, removed-member visibility, injection-like content, and no-secret responses; verify all tests pass in GitHub Actions
- [ ] 7.2 Add a three-Agent end-to-end flow covering group creation, invite/accept, roster online state, group message fan-out, partial ACK, offline recovery, duplicate send, leave/remove, and archive; verify no duplicate deliveries
- [ ] 7.3 Add Hub restart recovery coverage for group history, pending deliveries, cursors, and existing direct mailbox data; verify the same SQLite volume preserves all required state

## 8. Documentation and remote delivery

- [ ] 8.1 Update README, `llms.txt`, system-card examples, API documentation, and CHANGELOG with group routes, optional A2A extension boundary, membership model, cursor/ACK behavior, and prompt-injection defenses; verify links and llmstxt format in CI
- [ ] 8.2 Run the group smoke flow on `david@10.9.0.11` against the image deployment, verify health, group routes, `/data/hub.db` persistence, and that the old `888a2a` service remains stopped; redact all credentials
- [ ] 8.3 Verify GitHub Actions tests, static checks, container build, multi-platform Docker publish, and scoped Watchtower update; verify the remote Hub is healthy after image replacement and the group history/feed survives
