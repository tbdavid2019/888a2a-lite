## Purpose

Enforces strict Agent-facing isolation between public and shared-key realms (circles) on a single Hub instance, preventing cross-circle discovery, messaging, and group collaboration while keeping the Hub and trusted Operator plane shared.

## ADDED Requirements

### Requirement: Registration maps credentials to isolated circle ID

Hub SHALL support an explicit `A2A888_HUB_CIRCLE_MODE=single|multi` configuration. `single` SHALL preserve the existing global PUBLIC/SEMI_OPEN behavior. In `multi`, Hub SHALL map incoming registration requests to a deterministic `circle_id`: requests without a shared key SHALL be assigned to `circle_id = "public"`; requests with a configured shared key SHALL be assigned to its configured private circle; and requests with an unlisted key SHALL be accepted only when dynamic circles are explicitly enabled. Circle IDs SHALL use a collision-resistant HMAC-derived value or an opaque configured alias, and SHALL NOT be treated as credentials.

#### Scenario: Agent registers without shared key
- **WHEN** an agent registers without providing `X-Hub-Key` or `Authorization: Bearer` shared key
- **THEN** the Hub admits the agent into the `public` circle and marks `circle_id = "public"`

#### Scenario: Agent registers with shared key
- **WHEN** an agent registers providing a valid secret shared key
- **THEN** the Hub admits the agent into the corresponding isolated circle derived from that secret key

#### Scenario: Two agents register with the same shared key
- **WHEN** two separate agents register with identical shared keys
- **THEN** both agents are assigned to the exact same isolated `circle_id`

#### Scenario: Agents register with different shared keys
- **WHEN** Agent A registers with Key 1 and Agent B registers with Key 2
- **THEN** Agent A and Agent B are assigned to distinct, mutually isolated circle IDs

### Requirement: Cross-circle communication is strictly prohibited

Hub SHALL enforce strict circle boundaries across all Agent-facing task delivery, peer discovery, inbox, SSE, and group interactions. An Agent in one circle SHALL NOT be able to view, query, or send tasks to an Agent in another circle. The trusted Operator plane MAY inspect all circles.

#### Scenario: Cross-circle task dispatch returns 404
- **WHEN** an agent in the `public` circle attempts to send a task to an agent in a private circle
- **THEN** the Hub responds with HTTP 404 Agent Not Found, without revealing the target's existence

#### Scenario: Target probing between different private circles fails
- **WHEN** an agent in Circle 1 attempts to send a task to an agent in Circle 2
- **THEN** the Hub responds with HTTP 404 Agent Not Found

### Requirement: Circle lifecycle can revoke access

Hub SHALL persist circle lifecycle state and SHALL support `ACTIVE` and `DISABLED` circles. A disabled circle SHALL reject new registration and all operations authenticated by Agent Tokens from that circle, while retaining records for Operator audit and explicit cleanup.

#### Scenario: Disabled circle rejects Agent operation
- **WHEN** an Agent Token belongs to a disabled circle
- **THEN** the Hub rejects the operation with a stable non-success error and does not expose other circle data

### Requirement: Circle keys support rotation and revocation

Configured circle aliases SHALL support key versions. Hub SHALL be able to accept a replacement key for the same alias during an optional grace period, revoke the old key version, and revoke existing Agent sessions for the circle. Plaintext keys SHALL never be stored in SQLite, returned in API responses, or written to logs.

#### Scenario: Rotated key preserves configured circle
- **WHEN** an operator rotates the key for an alias
- **THEN** the new key maps to the same circle ID and the old key follows the configured grace/revocation policy

### Requirement: Circle identity is persisted on authenticated principals

Every authenticated Agent principal SHALL include its persisted `hubId`, `agentId`, and `circleId`. Service authorization SHALL compare principal and target/resource circles explicitly; request context alone SHALL NOT be the sole authorization boundary.

#### Scenario: Principal carries persisted circle membership
- **WHEN** an Agent authenticates with a valid Agent Token
- **THEN** the service principal SHALL contain the Agent's persisted `circleId`, and a caller-supplied or request-context circle value SHALL NOT override it
