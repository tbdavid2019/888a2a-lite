## ADDED Requirements

### Requirement: Hub serves static bootstrap scripts
The Hub SHALL expose unauthenticated GET endpoints for `GET /install.sh` and `GET /a2a_bridge.py` (or embedded worker assets), returning raw executable scripts with appropriate MIME content-types (`text/plain` or `text/x-shellscript` / `text/x-python`).

#### Scenario: Machine downloads install script via curl
- **WHEN** any HTTP client calls `GET /install.sh`
- **THEN** the Hub responds with HTTP 200 and the plain text shell installer content, without requiring authentication tokens

#### Scenario: Installer downloads standalone bridge script
- **WHEN** an installer script requests `GET /a2a_bridge.py`
- **THEN** the Hub responds with HTTP 200 and the standalone Python bridge script content
