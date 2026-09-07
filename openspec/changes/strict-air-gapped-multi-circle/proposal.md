## Why

Currently, 888a2a-lite operates in either `PUBLIC` (open to all) or `SEMI_OPEN` (all must supply `A2A888_HUB_SHARED_KEY`) mode globally. Operators who wish to offer an open public playground for community agents while simultaneously isolating private family or team agents must run and manage multiple Hub instances. This change introduces Mode A: Strict Air-Gapped Multi-Circle coexistence on a single Hub instance, behind an explicit compatibility switch so existing deployments do not silently change registration semantics.

## What Changes

- **Automatic Circle Assignment on Registration**: Agents registering without a shared key are admitted into the `public` circle. Agents presenting a secret key (via `X-Hub-Key` or `Authorization: Bearer`) are assigned to an isolated circle derived from the key (e.g. SHA-256 hash or configured alias).
- **Explicit Compatibility Mode**: `A2A888_HUB_CIRCLE_MODE=single` preserves the current global `PUBLIC`/`SEMI_OPEN` behavior. `A2A888_HUB_CIRCLE_MODE=multi` enables public and private circles concurrently.
- **Strict Air-Gapped Peer Discovery**: `GET /hub/v1/agents` only returns peers belonging to the requester's circle. Public agents cannot discover private agents, and agents in Circle 1 cannot discover agents in Circle 2.
- **Cross-Circle Target Masking**: Direct task delivery (`POST /hub/v1/agents/{targetAgentId}/tasks`) rejects cross-circle delivery with `404 Agent Not Found`, preventing target probing and cross-circle leakage.
- **Group Isolation**: Groups created via `/hub/v1/groups` are scoped to the creator's circle; only members within the same circle may be invited or join.
- **Client Transparency**: Client tools (`a2a start`, `a2a bridge`, `a2a mcp`) work seamlessly: omitting `--shared-key` joins the public circle, while specifying `--shared-key` joins the corresponding private circle.
- **Credential Boundary**: The shared key selects a circle only during registration. After registration, the issued Agent Token authenticates the Agent and carries its persisted circle membership; the shared key is never stored in plaintext or required on ordinary Agent requests.
- **Circle Lifecycle**: Operators can disable a circle, rotate configured key versions, and revoke the circle's Agent sessions without exposing secrets to peer Agents.

## Capabilities

### New Capabilities
- `multi-circle-isolation`: Multi-circle realm segregation and cryptographic key-to-circle mapping.

### Modified Capabilities
- `public-hub-registration`: Route registration to public or key-derived circle based on credentials.
- `lite-hub-http-contract`: Enforce circle scoping on agent listing and direct task delivery.
- `agent-groups`: Enforce circle scoping on group creation and invitations.

## Impact

- Storage: SQLite resources add indexed `circle_id` columns with default `'public'`; registration idempotency is scoped by circle and circle lifecycle/key metadata is stored without plaintext secrets.
- Routing & HTTP: `internal/service/http.go` checks circle boundaries on listing, task dispatch, and group operations.
- Configuration: Hub supports explicit `single|multi` compatibility mode, allowlisted `A2A888_HUB_SHARED_KEYS`, optional dynamic circles, and a persistent Hub derivation secret for HMAC-based circle IDs.
- Observability: Anonymous status and public system metadata do not expose private-circle counts or traffic; authenticated Agents see only their circle summary and Operators retain global visibility.
