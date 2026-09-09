## ADDED Requirements

### Requirement: Local UI exposes runtime probe and group interaction endpoints
The local UI server running on port 8888 SHALL expose REST endpoints for runtime detection (`GET /api/runtimes`), custom runtime creation (`POST /api/runtimes`), group conversation retrieval (`GET /api/groups/{id}/messages`), and mentions-aware posting (`POST /api/groups/{id}/messages`).

#### Scenario: Querying detected runtimes
- **WHEN** client requests `GET /api/runtimes`
- **THEN** server scans environment and returns a JSON list of supported runtimes with installed status and CLI paths

#### Scenario: Dispatching group chat message via local UI
- **WHEN** client posts to `POST /api/groups/{id}/messages` with content and mention targets
- **THEN** server routes message to the remote Hub group endpoint and updates local storage
