## Purpose

Defines autonomous multi-agent cognitive meeting governance, including secretary role activation, in-stream meeting minutes synthesis, durable decision extraction, and persistent local SQLite and external knowledge base export.

## ADDED Requirements

### Requirement: Secretary role declaration and activation
A group member SHALL be capable of declaring or being designated as the meeting secretary (`role: secretary`). A secretary agent SHALL monitor full-stream group conversation and act as the designated synthesizer of team conclusions.

#### Scenario: Agent configured as secretary
- **WHEN** an agent bridge is launched with `--role=secretary` and joins an active group
- **THEN** the bridge identifies its role as secretary and tracks all conversational turns for synthesis

#### Scenario: Human triggers explicit wrapup command
- **WHEN** a human or authorized participant posts `/minutes`, `/wrapup`, or `/summary` in the group chat
- **THEN** the designated secretary agent acknowledges receipt and initiates cognitive synthesis of the preceding session

### Requirement: In-stream meeting minutes and key decisions synthesis
The secretary agent SHALL filter out transient pleasantries, intermediate debugging logs, and chit-chat, synthesizing conversation into structured sections: Key Decisions (conclusions reached), Action Items (assignee, deliverable, deadline), and Artifact References (files, PRs, URLs).

#### Scenario: Secretary generates structured minutes
- **WHEN** the session concludes or a synthesis command is received
- **THEN** the secretary agent parses dialogue turns and produces a structured Markdown document containing Key Decisions and Action Items

#### Scenario: Non-essential messages are omitted from permanent record
- **WHEN** conversation contains small talk, greeting ping-pong, or transient error retries
- **THEN** the synthesized minutes exclude these items while preserving substantive technical deliberations and agreements

### Requirement: Durable group memory persistence in local SQLite work.db
Synthesized meeting minutes and distinct decision items SHALL be stored durably in the local SQLite WAL database (`~/.a2a/work.db`) under dedicated tables (`group_minutes` and `group_decisions`), decoupled from transient chat message logs.

#### Scenario: Persisting minutes record
- **WHEN** structured minutes are synthesized
- **THEN** the bridge writes a record to `group_minutes` with `group_id`, `session_id`, `charter_version`, `summary_md`, and `created_at` timestamp

#### Scenario: Querying past decisions across sessions
- **WHEN** a participant or agent queries past decisions for a group
- **THEN** the local bridge retrieves historical decision records from `group_decisions` without replaying raw chat messages

### Requirement: Markdown and Wiki export of structured meeting outcomes
The secretary bridge SHALL support automated export of synthesized minutes to local Markdown archives (`~/.a2a/minutes/<groupId>-<timestamp>.md`) and integration endpoints (such as Wiki REST APIs or GitHub Issues).

#### Scenario: Exporting minutes to local filesystem
- **WHEN** minutes are persisted
- **THEN** the bridge creates a standardized Markdown file in the configured local output directory for immediate inspection

#### Scenario: Automatic dispatch of action items
- **WHEN** action items are clearly assigned to specific member agents
- **THEN** the secretary optionally dispatches direct follow-up reminder tasks to assigned agents via standard Hub task routing
