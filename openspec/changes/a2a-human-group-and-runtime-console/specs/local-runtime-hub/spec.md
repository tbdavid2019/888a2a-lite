## Purpose

Provides local system scanning, status probing, runtime registration, and daemon lifecycle management for AI CLI runtimes (OpenClaw, Claude Code, Goose, Hermes, Codex, OpenCode).

## ADDED Requirements

### Requirement: Local CLI runtime discovery and status probing
The local UI server SHALL probe system PATH and standard binary directories for known AI CLI runtimes, classifying each as Ready (executable detected) or CLI Needed (not installed or not executable).

#### Scenario: Runtime detected in system PATH
- **WHEN** user opens the Agent Runtimes tab and `openclaw` or `claude` exists in PATH
- **THEN** the UI displays the runtime with a green Ready status indicator and version metadata

#### Scenario: Runtime missing in system PATH
- **WHEN** user views an uninstalled engine such as `goose`
- **THEN** the UI displays an amber `CLI needed` badge with installation guidance link

### Requirement: Custom runtime configuration and service installation
The local UI server SHALL support registering user-defined runtimes with custom command-lines and environment variables, and provide one-click daemon installation for systemd (Linux) or launchd (macOS).

#### Scenario: User registers custom runtime
- **WHEN** user submits a new runtime configuration via the UI
- **THEN** the runtime configuration is persisted in `~/.a2a/runtimes.json` and immediately displayed in the dashboard

#### Scenario: User triggers daemon install
- **WHEN** user clicks "Install Service" for an active runtime
- **THEN** the local bridge generates and loads the user service configuration, returning active service status
