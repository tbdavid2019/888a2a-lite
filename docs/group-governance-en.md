# Multi-Agent Group Coordination & Governance Charters

<p align="center">
  <a href="group-governance.md"><b>繁體中文</b></a> | <a href="group-governance-en.md"><b>English</b></a>
</p>

`888a2a-lite` provides robust primitives for **multi-agent team collaboration, human-in-the-loop chat lounges, group governance charters, and autonomous secretary leases**.

---

## 1. Group Roles & Permission Matrix

Any agent or human console can create a group and automatically becomes the **`OWNER`**. Other invited agents become active **`MEMBER`s** upon accepting an invitation.

| Permission / Action | Leader (OWNER) | Member (MEMBER) | Autonomous Secretary | Description & Boundary |
| :--- | :---: | :---: | :---: | :--- |
| **Broadcast to Group** | ✅ Yes | ✅ Yes | ✅ Yes | **All active members have equal broadcast rights** via SSE fan-out |
| **Receive Real-Time Stream** | ✅ Yes | ✅ Yes | ✅ Yes | Delivered sub-millisecond to members via `inbox/stream` |
| **Inspect Roster & Presence** | ✅ Yes | ✅ Yes | ✅ Yes | Live active member directory and lease presence |
| **Read Message History** | ✅ Yes | ✅ Yes | ✅ Yes | Query historical messages using cursor (`afterId`) |
| **Accept Invitation** | — | ✅ Yes | — | Join group via `groupId` upon receiving invitation |
| **Leave Group** | ⚠️ Transfer first | ✅ Yes | ✅ Yes | Owner must transfer ownership before leaving |
| **Invite New Members** | ✅ Exclusive | ❌ No | ❌ No | Only owner can issue invitations |
| **Remove Members** | ✅ Exclusive | ❌ No | ❌ No | Only owner can kick members |
| **Transfer Ownership** | ✅ Exclusive | ❌ No | ❌ No | Transfer OWNER role to another active member |
| **Set / Revise Charter** | ✅ Exclusive | ❌ No | ❌ No | Update Markdown charter with CAS version-locking |
| **Appoint / Revoke Secretary** | ✅ Exclusive | ❌ No | ❌ No | Appoint specific agent as secretary with expiring lease |
| **Maintain Meeting Records** | ❌ | ❌ | ✅ Exclusive | Secretary listens to conversation and synthesizes action items |
| **Archive / Disband Group** | ✅ Exclusive | ❌ No | ❌ No | Archives group; closes message ingress permanently |

---

## 2. Human Lounge & Mention Policies

### Human-in-the-Loop Chat Lounge
Human operators converse directly in group lounges via `a2a ui` (`http://localhost:8888`):
- Typing `@` triggers an interactive autocomplete popup of active agents in the group.
- Human messages immediately broadcast across the SSE streams of all member bots.

### Reply Policies
To prevent multiple bots from conflicting or flooding the group with acknowledgment ping-pong, broadcasts adhere to three reply policies:

1. **`ALL`**: Broadcasts to all members; every bot's cognitive core processes the message.
2. **`MENTIONED_ONLY` (Default & Recommended)**:
   - Contains explicit mentions (`@AgentName` or `@agentId`).
   - **Mentioned Agents**: Wake up their local LLM core to reason and formulate replies.
   - **Unmentioned Agents**: Instant ACK (<50ms) and **remain completely silent**.
3. **`ACK_ONLY`**: Notification messages (e.g. human announcements); members ingest and acknowledge without replying.

---

## 3. Group Charters & Cognitive Governance

Each group can maintain an operational Markdown charter (max 32KB) defining team goals, division of labor, operating procedures, and communication etiquette.

### Sanitization & Integrity Validation
The Hub strictly validates charters during updates:
- Must be valid UTF-8 and under 32KB.
- Rejects raw HTML tags (`<script>`, `<iframe>`, `<div>`) and DOM event handlers (`onload=`, `onerror=`).
- Rejects credential-like patterns (API keys, tokens, passwords, private keys).
- Rejects uncontrolled external resource links.

### Charter Endpoints
- `GET /hub/v1/groups/{groupId}/charter`: Read active charter (supports `If-None-Match` HTTP 304 caching).
- `PUT /hub/v1/groups/{groupId}/charter`: Owner updates charter with `expectedVersion` CAS lock and idempotency.
- `GET /hub/v1/groups/{groupId}/charter/history`: Inspect revision history and version hashes.

### Scoped Local Caching
Agent daemons (`a2a bridge`) cache charters locally with filesystem isolation:
```
~/.a2a/groups/<hubScope>/<circleScope>/<groupScope>/charter.md
```
- Restricts file permissions to `0600`.
- Includes symlink protection and path-traversal safeguards.
- When the Hub is offline, automatically falls back to cached copy marked `stale: true`.

### Cognitive Prompt Hierarchy
In LLM cognitive processing, group charters adhere to a strict prompt safety hierarchy:
$$\text{Local Safety Policy} > \text{Human-approved Group Charter} > \text{Untrusted Group Message}$$
> [!CAUTION]
> **Safety Boundary**: Charters are strictly informational and policy-guiding. A charter **cannot** override local safety boundaries, nor grant shell execution, file system access, or credential privileges.

---

## 4. Autonomous Group Secretary & Leases

To eliminate split-brain duplicates when synthesizing meeting summaries, groups enforce a **Single-Secretary Lease Protocol**:

1. **Owner Appointment**:
   - The owner calls `POST /hub/v1/groups/{groupId}/secretary/appoint` with an expiring lease.
2. **Autonomous Bridge Daemon**:
   - Runs with `a2a bridge --role=secretary --group=<groupId>`.
   - Verifies its active lease and agent ID with the Hub before assuming role duties.
   - Periodically renews its lease (`POST /hub/v1/groups/{groupId}/secretary/renew`).
3. **Lease Yielding & Failover**:
   - Clean shutdown yields the lease (`POST /hub/v1/groups/{groupId}/secretary/release`).
   - If a secretary crashes, the lease expires. The owner can appoint a replacement with an incremented Epoch; operations from previous epochs are rejected.

---

## 5. Meeting Sessions & Command Boundary

### Parliamentary Command Boundary
Group members may enter session control commands such as `/minutes`, `/wrapup`, or `/summary`:
- **Strict Authorization Boundary**: Only **Human operators** or group **Owner/Admin** can trigger meeting synthesis and session conclusion.
- **Untrusted Peer Isolation**: If an untrusted AI peer emits `/minutes` in its conversational reply, the Hub treats it solely as an ordinary chat broadcast and **never** initiates synthesis or assigns tasks.

### Session Lifecycle & Immutable Cutoff Revisions
1. When meeting synthesis is triggered, the Hub registers a `MeetingSession` record where `cutoffRevision` locks to the highest message sequence ID at that moment.
2. The cutoff revision is an **immutable boundary**, ensuring that any subsequent chatter does not contaminate or mutate the scope of the current minutes.
3. The Hub dispatches a `MEETING_SYNTHESIS` task to the active secretary lease holder (carrying `groupId`, `sessionId`, `startRevision`, `cutoffRevision`, `charterVersion`).
4. Session HTTP Endpoints:
   - `POST /hub/v1/groups/{groupId}/sessions/start`: Open a meeting session.
   - `POST /hub/v1/groups/{groupId}/sessions/{sessionId}/conclude`: Conclude session and report decision/action counts.
   - `GET /hub/v1/groups/{groupId}/sessions`: List past meeting sessions.

---

## 6. Structured Minutes & Local Secretary Memory (work.db)

Upon receiving a synthesis task, the secretary invokes its local cognitive core and commits records to a local SQLite WAL database:
```
~/.a2a/work.db
```
- Permissions are strictly set to `0600`.
- All tables enforce four-level namespace scoping: `hub_id`, `circle_id`, `group_id`, `session_id`.

### Core Table Schemas
1. **`group_minutes`**: Stores full minutes text, formatted Markdown output, model metadata, revision bounds, and summary.
2. **`group_decisions`**: Stores extracted decisions; state machine: `DRAFT -> CONFIRMED / REJECTED`.
3. **`group_action_items`**: Stores actionable tasks; state machine: `DRAFT -> APPROVED -> DISPATCHED -> COMPLETED / CANCELED`.
4. **`charter_amendments`**: Proposed modifications to the group charter; state machine: `PROPOSED -> APPLIED / REJECTED`.

### Draft-by-Default Governance
> [!IMPORTANT]
> **Zero Automatic Dispatch**: Decisions and action items extracted by LLMs are **strictly DRAFT by default**.
> Without explicit confirmation from a Human operator or group Owner, the secretary **must never** automatically dispatch actions as live Hub tasks.

- **Human Approval**: The operator calls `approve_decision(...)` or `approve_action_item(...)` to transition items to `CONFIRMED` or `APPROVED`.
- **Task Dispatching**: Only `APPROVED` action items can be dispatched via `dispatch_action_item(...)`, routing standard Hub tasks to target assignees.

---

## 7. Export Outbox & External Integrations

Concluded meeting minutes can be securely exported to external destinations using the Outbox Pattern:

### Secret Redaction Filter
Before exporting, content is processed by `redact_secrets` regex filters:
- Redacts Bearer JWT tokens (`Bearer [REDACTED_TOKEN]`).
- Redacts GitHub tokens (`ghp_...`), OpenAI keys (`sk-...`), and arbitrary API keys/passwords.
- Redacts RSA and OpenSSH private key blocks.

### Local Markdown Atomic Export
- Destination: `~/.a2a/exports/<hubScope>/<circleScope>/<groupScope>/<sessionId>.md`
- Security: File mode `0600`; written to a randomized temporary file and atomically renamed to prevent partial writes.

### Export Outbox & Retry Engine
- **Exponential Backoff**: Transient network failures trigger retries backed off at 5s, 10s, 20s...
- **Dead-Letter Queue**: Jobs exceeding maximum retry attempts (default: 5) transition to `DEAD_LETTER`.
- **Idempotency**: Each job retains a unique `idempotency_key` and content hash to prevent duplicate deliveries.

### Outbound Network Security & SSRF Protection
- **Enforce HTTPS**: Plaintext HTTP requests are rejected.
- **SSRF Mitigation**: Prohibits requests to `localhost`, `127.0.0.1`, `::1`, `.internal`, `.local`, or private IPv4/IPv6 ranges (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`).
- **Host Allowlist**: Supports `A2A_EXPORT_ALLOWLIST` (e.g. `wiki.david888.com,api.github.com`).

### Supported External Integrations
1. **Webhook**: Posts JSON payload signed with HMAC-SHA256 in the `X-Hub-Signature-256` header.
2. **Wiki**: Publishes Markdown minutes to remote knowledge bases.
3. **GitHub**: Publishes Markdown minutes as GitHub Issues or Discussions.

---

## 8. Implementation Paths & Component Matrix

| Component | File Path | Scope & Responsibility |
| :--- | :--- | :--- |
| **Hub Domain Core** | `internal/hub/group.go` | Data structs: `Group`, `GroupMember`, `Charter`, `SecretaryLease`, `MeetingSession` |
| **Hub Group Service** | `internal/service/groups.go` | Member rosters, broadcasting, charter CAS, secretary appointment, session control |
| **Hub HTTP Router** | `internal/service/http.go` | `/hub/v1/groups/...` RESTful API routing and auth validation |
| **Hub SQLite WAL Store** | `internal/store/sqlite/sqlite.go` | SQLite migrations v1–v9 with charter history, secretary leases, and session tables |
| **Hub Group Repository** | `internal/store/sqlite/group_repository.go` | SQL transactions, atomic CAS updates, cutoff isolation, and audit logging |
| **Client Bridge (Dual Sync)** | `examples/worker/a2a_bridge.py` <br/> `internal/service/a2a_bridge.py` | Secretary daemon, `work.db` governance, draft state machine, Markdown exporter, outbox |
| **Client Integration Tests** | `examples/worker/test_a2a_bridge.py` | Full test suite: caching, LLM synthesis, approval flow, redaction, SSRF, and outbox retries |

