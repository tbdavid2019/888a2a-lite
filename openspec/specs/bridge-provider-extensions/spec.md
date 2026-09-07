# bridge-provider-extensions Specification

## Purpose
Extends the Universal Agent Bridge with additional cognitive execution backends, specifically Claude Code CLI, Codex CLI, and arbitrary shell command runners.

## Requirements

### Requirement: Claude Code CLI cognitive backend
The bridge SHALL support `--backend claudecode`, invoking Anthropic's Claude Code CLI in print/non-interactive mode (`claude -p "<prompt>"`).

#### Scenario: Ingested task executed with Claude Code
- **WHEN** the bridge receives a task while configured with `--backend claudecode`
- **THEN** it executes `claude -p` passing the task instruction and anti-echo guard, capturing stdout as the reasoned response

### Requirement: Codex CLI cognitive backend
The bridge SHALL support `--backend codex`, invoking the Codex CLI execution interface (`codex exec "<prompt>"`).

#### Scenario: Ingested task executed with Codex
- **WHEN** the bridge receives a task while configured with `--backend codex`
- **THEN** it executes the `codex` command line, capturing stdout as the reasoned response

### Requirement: Generic Command Runner backend
The bridge SHALL support `--backend command` with `--backend-cmd "<command-template>"`, allowing arbitrary shell commands or custom scripts to act as cognitive agents.

#### Scenario: Ingested task executed with generic command
- **WHEN** the bridge receives a task while configured with `--backend command --backend-cmd "my_agent.sh"`
- **THEN** it executes the specified command, providing task payload via stdin or arguments, and captures stdout as the reasoned response
