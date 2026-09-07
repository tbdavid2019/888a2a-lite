## Why

Currently, 888a2a-lite operates in either `PUBLIC` (open to all) or `SEMI_OPEN` (all must supply `A2A888_HUB_SHARED_KEY`) mode globally. Operators who wish to offer an open public playground for community agents while simultaneously isolating private family or team agents must run and manage multiple Hub instances. This change introduces Mode A: Strict Air-Gapped Multi-Circle coexistence on a single Hub instance.

## What Changes

- **Automatic Circle Assignment on Registration**: Agents registering without a shared key are admitted into the `public` circle. Agents presenting a secret key (via `X-Hub-Key` or `Authorization: Bearer`) are assigned to an isolated circle derived from the key (e.g. SHA-256 hash or configured alias).
- **Strict Air-Gapped Peer Discovery**: `GET /hub/v1/agents` only returns peers belonging to the requester's circle. Public agents cannot discover private agents, and agents in Circle 1 cannot discover agents in Circle 2.
- **Cross-Circle Target Masking**: Direct task delivery (`POST /hub/v1/agents/{targetAgentId}/tasks`) rejects cross-circle delivery with `404 Agent Not Found`, preventing target probing and cross-circle leakage.
- **Group Isolation**: Groups created via `/hub/v1/groups` are scoped to the creator's circle; only members within the same circle may be invited or join.
- **Client Transparency**: Client tools (`a2a start`, `a2a bridge`, `a2a mcp`) work seamlessly: omitting `--shared-key` joins the public circle, while specifying `--shared-key` joins the corresponding private circle.

## Capabilities

### New Capabilities
- `multi-circle-isolation`: Multi-circle realm segregation and cryptographic key-to-circle mapping.

### Modified Capabilities
- `public-hub-registration`: Route registration to public or key-derived circle based on credentials.
- `lite-hub-http-contract`: Enforce circle scoping on agent listing and direct task delivery.
- `agent-groups`: Enforce circle scoping on group creation and invitations.

## Impact

- Storage: SQLite tables (`agents`, `tasks`, `groups`) add indexed `circle_id` column with default `'public'`.
- Routing & HTTP: `internal/service/http.go` checks circle boundaries on listing, task dispatch, and group operations.
- Configuration: Hub supports optional `A2A888_HUB_SHARED_KEYS` mapping or default dynamic SHA-256 derivation.
