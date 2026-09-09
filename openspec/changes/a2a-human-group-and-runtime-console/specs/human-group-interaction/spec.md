## Purpose

Provides an interactive human-in-the-loop group chat workspace within the local Web UI, enabling humans to converse with multi-bot groups using @mentions and selective response policies.

## ADDED Requirements

### Requirement: Human participation in multi-agent group chats
The local Web UI SHALL provide a Group Chat interface allowing humans to read real-time group dialogue and inject messages into any group they have joined.

#### Scenario: Human posts message in group
- **WHEN** human types a message and submits it in the active group chat view
- **THEN** the local server dispatches the message to the Hub group endpoint and updates the local conversation view

### Requirement: @Mention parsing and selective reply policy injection
The group chat composer SHALL provide auto-completion for active group members when the `@` character is typed. When submitted, the client SHALL automatically set `replyPolicy: MENTIONED_ONLY` and populate `mentions` with mentioned agent IDs. If no `@` mention is present, the message SHALL default to `replyPolicy: ACK_ONLY`.

#### Scenario: Human mentions a specific bot
- **WHEN** human sends `@OpenClaw please summarize this architecture`
- **THEN** the message payload includes `mentions: ["agent_openclaw_id"]` and `replyPolicy: MENTIONED_ONLY`, prompting only the targeted agent to generate an LLM response while other members silently ACK

#### Scenario: Human sends general broadcast without mention
- **WHEN** human sends a general remark without `@` mentions
- **THEN** the payload includes `replyPolicy: ACK_ONLY`, causing all bots to sign an Instant ACK without generating duplicate conversational chatter
