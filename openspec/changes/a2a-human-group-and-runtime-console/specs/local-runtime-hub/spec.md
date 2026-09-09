## Purpose

Provides a visual runtime management hub and local diagnostic interface for AI CLI engines, enabling real-time detection, health inspection, custom command integration, and background service control.

## ADDED Requirements

### Requirement: Local Web UI discovers installed AI CLI backends
The Local UI server SHALL inspect the local environment PATH and known installation directories for available AI CLI backends (including OpenClaw, Claude Code, Goose, Hermes, Codex, and OpenCode). The inspection result SHALL be exposed via `GET /api/runtimes` with status flags indicating whether each engine is ready or requires installation (`CLI needed`).

#### Scenario: Local CLI discovery returns detected runtimes and missing statuses
- **WHEN** user opens the Agent Runtimes panel or client queries `GET /api/runtimes`
- **THEN** server inspects system PATH and returns a JSON list of supported engines with their binary location, detection status (`ready` or `cli_needed`), and active backend indicator

#### Scenario: Custom runtime command registration
- **WHEN** user adds a custom runtime via `POST /api/runtimes/custom` with display name and CLI command string
- **THEN** server verifies command executable syntax, stores configuration locally, and makes the runtime selectable for bridge dispatch

### Requirement: Background service management via UI
The Local UI server SHALL provide endpoints to inspect and control the host OS background service manager (macOS LaunchAgent or Linux systemd user unit) for the active Agent bridge.

#### Scenario: User installs or restarts background service from UI
- **WHEN** user clicks "Install Service" or "Restart Service" on the Web UI for a ready runtime
- **THEN** server triggers OS service configuration, starts the background service, and returns updated service status
