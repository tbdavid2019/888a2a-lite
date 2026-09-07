## 1. Compatibility Contract & Circle Lifecycle

- [x] 1.1 Define `A2A888_HUB_CIRCLE_MODE=single|multi`; preserve current global PUBLIC/SEMI_OPEN behavior in `single` mode and enable circle coexistence only in `multi` mode.
- [x] 1.2 Define and validate `A2A888_HUB_SHARED_KEYS`, `A2A888_HUB_ALLOW_DYNAMIC_CIRCLES`, and persistent `A2A888_HUB_CIRCLE_DERIVATION_SECRET`; reject unsafe or ambiguous configuration without logging secrets.
- [x] 1.3 Add `hub_circle` and `hub_circle_key` storage for ACTIVE/DISABLED state, aliases, key versions, rotation grace periods, and key digests; never store plaintext shared keys.
- [x] 1.4 Add circle disable, key-version rotation, and circle Agent-session revoke operator operations.

## 2. SQLite Schema Migration & Store Scoping

- [x] 2.1 Add `circle_id TEXT NOT NULL DEFAULT 'public'` to `agent`, `inbox_item`, `agent_group`, `group_invitation`, `group_message`, `group_delivery`, and `event_log` in `internal/store/sqlite/sqlite.go` / repository migrations.
- [x] 2.2 Update Go data structures (`RegisteredAgent`, `InboxItem`, `Group`, `GroupMember`, invitations, messages, events) with `CircleID string` where the resource is circle-scoped.
- [x] 2.3 Add composite indices for `(hub_id, circle_id, state)` and circle-aware uniqueness/FK constraints where applicable.
- [x] 2.4 Migrate all existing rows to `public`; do not infer private membership for historical rows.
- [x] 2.5 Update repository methods to require or derive circle scope for Agent, mailbox, group, invitation, delivery, message, and event queries.

## 3. Key-to-Circle Derivation & Registration

- [x] 3.1 Implement HMAC-based deterministic circle derivation with a collision-resistant identifier; support configured aliases and explicitly gated dynamic circles.
- [x] 3.2 Resolve circle before registration idempotency lookup; scope uniqueness as `(hub_id, circle_id, registration_key_hash)` and define same-key/different-circle behavior without identity leakage.
- [x] 3.3 Update `POST /hub/v1/agents/register` to assign no-key Agents to `public` and key-bearing Agents to the corresponding active private circle.
- [x] 3.4 Store an explicit typed authenticated Agent principal containing `agentId`, `hubId`, and `circleId`; request context is supplementary only.
- [x] 3.5 Ensure shared keys are accepted only during registration; ordinary Agent requests use Agent Token plus persisted circle membership.

## 4. Strict Air-Gapped Routing & 404 Error Masking

- [x] 4.1 Restrict `GET /hub/v1/agents` peer directory strictly to caller's circle.
- [x] 4.2 Enforce 404 Not Found in Agent lookup and Agent Card lookup when target belongs to another circle.
- [x] 4.3 Enforce 404 Not Found in direct task dispatch when target belongs to another circle, before any mailbox write.
- [x] 4.4 Apply circle checks to inbox poll, ACK, SSE stream, disconnect, and all task cancellation paths.
- [x] 4.5 Restrict group creation, list, lookup, roster, history, invitation, accept, leave, remove, ownership, archive, and message delivery to one circle.
- [x] 4.6 Reject disabled-circle Agent operations and new registrations with stable bounded errors.

## 5. Operator Admin & Audit Inspection

- [x] 5.1 Update operator admin endpoints (`/hub/v1/admin/agents`, `/hub/v1/admin/events`, messages, groups, status) to include `circleId` and support safe filtering.
- [x] 5.2 Add Circle filtering selector and Circle badge rendering in the Web Admin Console (`admin.html`).
- [x] 5.3 Keep Operator global visibility while preventing Operator credentials, key digests, and plaintext secrets from responses or logs.
- [x] 5.4 Make anonymous status/system card responses non-sensitive; authenticated Agent summaries are restricted to the caller's circle.

## 6. End-to-End Testing & Verification

- [ ] 6.1 Write tests for deterministic HMAC circle derivation, configured aliases, dynamic-circle gating, disabled circles, and key versions.
- [ ] 6.2 Write integration tests for public vs private circle isolation (registration, peer listing, lookup, Agent Card, task sending, inbox, SSE).
- [ ] 6.3 Write integration tests verifying cross-circle task dispatch, Agent lookup, Agent Card, group lookup, and group invitations return HTTP 404.
- [ ] 6.4 Verify registration idempotency with same key/same circle and same key/different circle.
- [ ] 6.5 Verify anonymous status does not expose private counts and Operator filters expose all circles without secrets.
- [ ] 6.6 Verify key rotation, old-token revocation, Hub restart persistence, and existing public-row migration.
- [ ] 6.7 Verify backward compatibility with existing client bridge (`a2a_bridge.py`) in `single` mode and explicit shared-key behavior in `multi` mode.

## 7. Documentation & Sync

- [x] 7.1 Update `README.md`, `llms.txt`, client skill, and architecture documentation with multi-circle registration, credential, status, rotation, and isolation rules.
- [x] 7.2 Record changes in `CHANGELOG.md` under today's date.
