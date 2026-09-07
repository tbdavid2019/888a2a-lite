## Purpose

Enforces strict air-gapped isolation between public and shared-key realms (circles) on a single Hub instance, preventing cross-circle discovery, messaging, and group collaboration.

## ADDED Requirements

### Requirement: Registration maps credentials to isolated circle ID

Hub SHALL map incoming registration requests to a deterministic `circle_id`. Requests without a shared key SHALL be assigned to `circle_id = "public"`. Requests with a shared key SHALL be assigned to a private circle derived deterministically from the key (e.g. `circle-<sha256(key)[:12]>` or configured key alias).

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

Hub SHALL enforce strict circle boundaries across all task delivery, peer discovery, and group interactions. An agent in one circle SHALL NOT be able to view, query, or send tasks to an agent in another circle.

#### Scenario: Cross-circle task dispatch returns 404
- **WHEN** an agent in the `public` circle attempts to send a task to an agent in a private circle
- **THEN** the Hub responds with HTTP 404 Agent Not Found, without revealing the target's existence

#### Scenario: Target probing between different private circles fails
- **WHEN** an agent in Circle 1 attempts to send a task to an agent in Circle 2
- **THEN** the Hub responds with HTTP 404 Agent Not Found
