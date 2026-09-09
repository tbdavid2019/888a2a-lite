## ADDED Requirements

### Requirement: LocalUIServer exposes runtime inspection and group chat APIs
The `LocalUIServer` embedded in the bridge CLI SHALL expose local REST endpoints supporting visual runtime detection (`GET /api/runtimes`), group list fetching (`GET /api/groups`), group history reading (`GET /api/groups/{id}/messages`), and group message dispatch (`POST /api/groups/{id}/messages`).

#### Scenario: Querying runtime inspection endpoint
- **WHEN** client queries `GET /api/runtimes` on `http://localhost:8888`
- **THEN** server returns an array of supported backends with their installation path and availability flag

#### Scenario: Sending group message with replyPolicy and mentions from local UI
- **WHEN** user posts a message via `POST /api/groups/{id}/messages` with `text`, `replyPolicy`, and `mentions`
- **THEN** server persists message locally in `~/.a2a/chat.db`, relays the message to the Hub group endpoint with corresponding metadata, and returns the assigned task sequence
