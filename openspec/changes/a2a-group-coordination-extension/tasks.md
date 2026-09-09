## 1. Dependency gate and extension contract

- [x] 1.1 Confirm `a2a-standard-compatibility` is complete and its fixed A2A specification revision, schema checksum, official SDK version, Bearer principal, result update endpoint and Task lifecycle fixtures pass in CI; record the evidence before enabling this change.
- [x] 1.2 Define the Group Extension URI, version, `AgentExtension` object schema, `A2A-Extensions` opt-in rule, `group:` tenant syntax, metadata namespace, `replyPolicy`, `mentions` and unsupported-client behavior; verify contract fixtures reject missing or malformed extension declarations.
- [x] 1.3 Define Parent Task, Member Delivery, member outcome aggregation, execution deadline, retry budget, retention and terminal-state rules; verify the state matrix covers all-success, empty-result, failure, rejection, timeout, cancellation and no-recipient cases.

## 2. Discovery and virtual Group Cards

- [x] 2.1 Implement bounded `GET /a2a/v1/groups` extension discovery with same-circle active group references, display name, member summary and Card URLs; verify archived, disabled and cross-circle groups are hidden.
- [x] 2.2 Implement `GET /a2a/v1/groups/{groupId}/card` and its documented alias with standard AgentCard fields, HTTP+JSON interface, `tenant: "group:<groupId>"`, Group Extension object, reply policies and safe skills; verify no member IDs or credentials are exposed.
- [x] 2.3 Add `private, no-store` behavior and lifecycle invalidation for Group Cards; verify archived/disabled cards and stale discovery references return masked 404.
- [x] 2.4 Advertise Group Extension correctly in root Agent Card and system card; verify `capabilities.extensions` contains AgentExtension objects and P2P clients can ignore the optional extension.

## 3. Parent Task and atomic fan-out

- [x] 3.1 Add durable Parent Task and Member Delivery models with parent/member/group/circle/requester/target/message/turn/revision fields; verify migration preserves all existing group and mailbox rows.
- [x] 3.2 Implement `group:<groupId>` tenant parsing and active accepted member authorization; verify tenant is routing only, requester Bearer principal is the authorization source, and non-members receive the documented masked error.
- [x] 3.3 Implement one-transaction eligible member snapshot and all-or-nothing preflight for circle, membership, capability, group size, fan-out and per-target pending capacity; verify a failed preflight leaves no parent, delivery, mailbox or audit event.
- [x] 3.4 Implement deterministic idempotency for `circle/requester/group/messageId/contentDigest`; verify identical retry returns the existing Parent Task and different content with the same key returns conflict.
- [x] 3.5 Preserve existing `/hub/v1/groups` sender exclusion and legacy delivery behavior; verify standard fan-out does not deliver to the sender and legacy tests remain unchanged.

## 4. Member execution and result aggregation

- [x] 4.1 Extend standard delivery payloads with parent/member/turn correlation while keeping fields optional for legacy clients; verify old `/hub/v1` group clients continue to work.
- [x] 4.2 Gate standard fan-out on a versioned executor capability and implement member update authorization; verify only the designated target can submit an update and an old bridge is not treated as a standard executor.
- [x] 4.3 Implement ACK-to-WORKING and correlated outcome transitions with CAS/revision and updateId idempotency; verify concurrent members update independently and duplicate/modified/late updates are handled safely.
- [x] 4.4 Implement Parent aggregation: all terminal successes/empty results complete, any failed/rejected/deadline child fails the parent with per-member metadata/artifacts, and no custom PARTIAL Task state is emitted; verify deterministic result ordering.
- [x] 4.5 Implement bounded execution deadline, retry budget and retention; verify timeout marks the member and parent terminal, restart preserves results, and late results cannot revive a terminal task.

## 5. Standard send, stream and cancellation integration

- [x] 5.1 Route standard `message:send` and `message:stream` group tenants through the Parent Task fan-out while preserving standard response envelopes and `returnImmediately` semantics; verify blocking requests wait for parent terminal/interrupted state or return a durable 504 deadline error.
- [x] 5.2 Emit ordered Parent and Member status/artifact updates as standard StreamResponse envelopes; verify multiple subscriptions receive the same revision order and reconnect replays from durable state without refan-out.
- [x] 5.3 Implement Parent cancellation only before any Member ACK, atomically canceling unaccepted deliveries; verify ACK wins returns `TASK_NOT_CANCELABLE`, cancel wins blocks executor admission, duplicate cancel of CANCELED returns the existing task, and late updates are rejected.
- [x] 5.4 Recheck requester token, circle, group state and task ACL before each stream event and keepalive; verify revoke, circle disable, group archive and requester disconnect stop or detach streams without holding a database transaction during network writes.

## 6. Bridge Anti-Echo and local durability

- [x] 6.1 Update the Bridge to persist group input before ACK and obtain an execution lease only after successful ACK; verify the 50ms value is an observability target and durable ordering remains correct under retry.
- [x] 6.2 Implement extension reply policies `ALL`, `MENTIONED_ONLY` and `ACK_ONLY` using validated metadata/mentions; verify Hub does not infer policy from natural language and non-addressed members do not send reciprocal messages.
- [x] 6.3 Make `[[A2A_NO_REPLY]]` and ACK-only handling submit a durable COMPLETED empty result to the Parent correlation; verify no group echo task is created and crash/retry does not duplicate the result.
- [x] 6.4 Preserve existing anti-echo behavior for `/hub/v1/groups`; verify embedded and distributed Bridge scripts use the same correlation and policy rules.

## 7. Security and compatibility verification

- [ ] 7.1 Add public/private/dynamic Multi-Circle tests for group discovery, Group Card, send, stream, subscribe, cancel and member updates; verify cross-circle responses mask group/task existence and create no state.
- [ ] 7.2 Add authorization matrix tests for requester, same-circle non-requester, group member, removed member, operator and expired/revoked Agent; verify Group Card, Parent Task, Member Delivery and result visibility are separately enforced.
- [ ] 7.3 Add race tests for membership snapshot, fan-out capacity, ACK/cancel, duplicate Message, concurrent Member updates, group archive and circle disable; verify SQLite transactions are all-or-nothing and terminal states are monotonic.
- [ ] 7.4 Add standard content/error tests for extension negotiation, `application/a2a+json`, `A2A-Version`, malformed metadata, unsupported Part, bounded pagination, invalid tenant and standard error reasons; verify P2P behavior remains unchanged.

## 8. Official interoperability and rollout

- [x] 8.1 Add an extension-aware client fixture that discovers `/a2a/v1/groups`, reads a Group Card, sends `A2A-Extensions: ...` to `group:<groupId>`, subscribes and parses Parent/Member progress; verify the fixture does not patch serializers.
- [ ] 8.2 Run the unmodified official A2A SDK against the standard core flow and the extension-aware fixture against group flow; verify the version, schema and result are recorded in CI and no unsupported-client behavior is claimed as core compatibility.
- [x] 8.3 Document Group Extension discovery, tenant routing, reply policies, member outcomes, cancellation, Multi-Circle rules and the explicit Buzz scope boundary in README, `llms.txt` and the client Skill; verify examples use valid extension headers and no secrets.
- [ ] 8.4 Update Docker/CI/deployment feature flags and run the required remote smoke test on `david@10.9.0.11`; verify standard P2P, legacy `/hub/v1/groups`, and Group Extension flows before production enablement.
