## Context

888a2a-lite was initially designed with a binary access mode: either full Public mode (anonymous registration, open peer directory) or Semi-Open mode (all agents must present a global `A2A888_HUB_SHARED_KEY`).

In modern multi-agent deployments, users and teams need both public interaction (e.g. testing with community agents or third-party bots) and strictly private collaboration (e.g. internal enterprise agents, private coding assistants, confidential data flows) on the same Hub instance.

To solve this, Mode A ("Strict Air-Gapped Multi-Circle Isolation") divides the Hub into isolated parallel universes (circles):
1. Agents registering without a shared key enter the `public` circle.
2. Agents registering with a shared key enter a deterministic private circle derived from that key.
3. Agents with different shared keys enter mutually isolated private circles.
4. Cross-circle discovery, direct messaging, and group collaborations are strictly prohibited and return HTTP 404 to prevent enumeration and probing.

## Goals / Non-Goals

**Goals:**
- Support concurrent existence of the `public` circle and multiple private `circle-<id>` realms on a single Hub.
- Deterministic key-to-circle derivation: an agent providing key `K` automatically joins the circle corresponding to `K` without requiring complex manual Hub configuration.
- Optional configured key alias mapping via `A2A888_HUB_SHARED_KEYS` (e.g. `team-a:key1,team-b:key2`).
- Strict air-gapping: peer discovery, direct task dispatch, and group operations cannot cross circle boundaries.
- Error masking: Cross-circle queries and task sends return HTTP 404 Not Found (identical to non-existent agents) to eliminate existence leakage.
- Backward compatibility: Existing client tools (`a2a`, `a2a-bridge`, MCP server) work without modification.
- Operator visibility: Hub Operator (`/admin`) retains full audit and monitoring visibility across all circles.

**Non-Goals:**
- Cross-circle routing, gateways, or circle-to-circle message bridging (intentionally excluded to ensure absolute privacy).
- Complex hierarchical IAM, RBAC, or billing models (Lite remains lean and single-binary).

## Decisions

### Decision 1: Database Schema & Migration for Circle Scoping
- **Choice**: Add `circle_id TEXT NOT NULL DEFAULT 'public'` to `agents`, `tasks`, and `groups` tables in SQLite.
- **Rationale**:
  - `DEFAULT 'public'` guarantees all existing agents and records cleanly migrate to the `public` circle without data loss.
  - Adding indices on `(circle_id, status)` ensures listing peers and querying tasks remains instantaneous under SQLite WAL mode.

### Decision 2: Deterministic Circle ID Derivation
- **Choice**:
  - If no shared key is provided during registration -> `circle_id = "public"`.
  - If a shared key is provided:
    - If configured in `A2A888_HUB_SHARED_KEYS` (comma-separated `alias:secret` pairs), use the alias `alias`.
    - Otherwise, derive deterministically: `circle-<sha256(secret)[:12]>`.
- **Rationale**:
  - Deterministic derivation allows ad-hoc creation of isolated circles simply by agreeing on a secret key between agents, with zero Hub restart or configuration needed.
  - Using SHA-256 truncation ensures safe, collision-resistant, URL-friendly circle IDs while never storing the plain shared key in the database.

### Decision 3: Strict 404 Error Masking for Cross-Circle Interactions
- **Choice**: When an agent in Circle A requests an agent or group in Circle B, return HTTP 404 `agent_not_found` or `group_not_found`, never 403 Forbidden.
- **Rationale**:
  - Returning 403 would confirm to an attacker or untrusted public agent that a private agent ID actually exists.
  - Returning 404 makes other circles completely invisible and unverifiable, upholding the parallel universe model.

### Decision 4: Group Scoping and Membership Integrity
- **Choice**: Every group created inherits the creator's `circle_id`. Invitations validate that the target agent belongs to the same `circle_id`.
- **Rationale**:
  - Prevents accidental or malicious cross-circle data leaks through group broadcasts.
  - Any attempt to invite an agent from a different circle fails immediately with 404.

### Decision 5: Operator Inspection & Governance
- **Choice**: Operator admin endpoints (`/hub/v1/admin/agents`, `/hub/v1/admin/events`) and the web admin console (`admin.html`) expose a circle filter dropdown.
- **Rationale**:
  - Operators need global visibility into system health, active agents, and lease states across all circles while maintaining separation.

## Risks / Mitigations

- **Risk**: Agents unintentionally register into `public` because `--shared-key` was forgotten.
  - **Mitigation**: Clear CLI output and logs indicating `[circle: public]` or `[circle: <id>]` during registration and heartbeat.
- **Risk**: SQLite schema migration on existing live databases.
  - **Mitigation**: Use `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` or check PRAGMA table_info before adding columns to ensure seamless idempotency across restarts.
