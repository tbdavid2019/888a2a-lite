## Purpose

Provides Hub-level Group Charter specification, versioning, and lifecycle management, allowing agents to automatically synchronize organizational SOPs and inject cognitive constraints during multi-agent group onboarding.

## ADDED Requirements

### Requirement: Group Charter schema and versioning
Hub SHALL maintain a Markdown-formatted `charter` string and an integer `charter_version` (starting at 1) for each Group. The charter SHALL contain standardized sections defining team RACI matrix, speaking policy (`[[A2A_NO_REPLY]]` trigger conditions), decision thresholds, and output formatting.

#### Scenario: Group is created with initial charter
- **WHEN** an authenticated agent creates a group and provides an optional initial Markdown charter
- **THEN** Hub persists the charter, initializes `charter_version` to 1, and associates it with the created group

#### Scenario: Default charter is assigned when omitted
- **WHEN** an authenticated agent creates a group without providing a charter
- **THEN** Hub populates a default minimal charter template establishing basic speaking and anti-echo rules with `charter_version: 1`

### Requirement: Group Charter read and write endpoints
Hub SHALL expose `GET /hub/v1/groups/{groupId}/charter` and `PUT /hub/v1/groups/{groupId}/charter`. Only the group owner or designated administrator agents SHALL be permitted to update the charter. Updates SHALL atomically increment `charter_version` by 1 and trigger a `CHARTER_UPDATED` notification broadcast to all current group members.

#### Scenario: Group member fetches charter
- **WHEN** an authenticated member of the group sends `GET /hub/v1/groups/{groupId}/charter`
- **THEN** Hub returns HTTP 200 containing `groupId`, `charter_version`, `updated_at`, and the full Markdown `charter` content

#### Scenario: Non-member cannot fetch charter
- **WHEN** an agent that is not a member of the group requests `GET /hub/v1/groups/{groupId}/charter`
- **THEN** Hub rejects the request with HTTP 403 Forbidden or 404 Not Found

#### Scenario: Group owner updates charter
- **WHEN** the group owner sends `PUT /hub/v1/groups/{groupId}/charter` with a revised Markdown charter
- **THEN** Hub saves the new content, increments `charter_version`, and broadcasts a `CHARTER_UPDATED` event through the group event stream

#### Scenario: Non-owner update is rejected
- **WHEN** a regular group member attempts to send `PUT /hub/v1/groups/{groupId}/charter`
- **THEN** Hub rejects the update with HTTP 403 Forbidden without modifying the existing charter

### Requirement: Automatic onboarding charter synchronization for joining agents
When an agent accepts a group invitation via `POST /hub/v1/groups/{groupId}/accept` or connects to the group event stream, the agent bridge SHALL automatically fetch and cache the latest `charter.md` locally under `~/.a2a/groups/{groupId}/charter.md`.

#### Scenario: Agent accepts invitation and caches charter
- **WHEN** an agent accepts an invitation to join a group
- **THEN** the agent bridge invokes the charter endpoint and saves `charter.md` along with its cached version number

#### Scenario: Agent refreshes cached charter on version bump
- **WHEN** the agent bridge receives a `CHARTER_UPDATED` event with a higher `charter_version`
- **THEN** the bridge invalidates its local cache and pulls the newest charter content

### Requirement: Charter prompt assembly and cognitive boundary injection
When dispatching a group message to an underlying LLM runtime, the bridge SHALL inject an abbreviated, authoritative snapshot of the group charter into the system prompt. The injected context SHALL explicitly command the model to conform to the group's speaking rules, role boundaries, and silent acknowledgment instructions.

#### Scenario: Ingestion of charter into reasoning context
- **WHEN** the bridge receives an inbound group task requiring LLM evaluation
- **THEN** the bridge prepends the cached group charter guidelines to the prompt context before invoking the backend LLM engine
