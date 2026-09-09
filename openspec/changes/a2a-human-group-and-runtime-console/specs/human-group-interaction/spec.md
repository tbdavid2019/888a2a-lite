## Purpose

Enables human users to participate in multi-agent group chats through an interactive web interface with @mention autocomplete, selective dispatch policies, and silence guarantees for unmentioned agents.

## ADDED Requirements

### Requirement: Human group workstation with @mention autocomplete
The Local Web UI SHALL render a dedicated Groups section allowing human users to view group conversations, inspect member rosters, and participate by sending text messages. The input composer SHALL provide an @mention autocomplete dropdown populated from the active group roster.

#### Scenario: Typing @ triggers member autocomplete
- **WHEN** user types `@` into the group chat input field
- **THEN** composer renders an inline suggestion menu listing group member display names and agent IDs

#### Scenario: Human message without mentions defaults to ACK_ONLY
- **WHEN** user submits a group message without any `@` mentions
- **THEN** client sets `replyPolicy` to `ACK_ONLY` and empty `mentions`, notifying all bot members without soliciting LLM replies

#### Scenario: Human message with @ mentions injects MENTIONED_ONLY
- **WHEN** user submits a group message containing one or more `@<DisplayName>` tags
- **THEN** client parses mentioned tags into corresponding target Agent IDs, sets `replyPolicy` to `MENTIONED_ONLY`, and populates `mentions` with the target IDs

### Requirement: Group member bots silence non-mentioned human input
When a group message carries `replyPolicy: MENTIONED_ONLY`, group member bridges receiving the delivery SHALL immediately acknowledge with Instant ACK. If the receiving agent ID is not in `mentions`, the bridge SHALL complete the delivery without invoking the local LLM engine.

#### Scenario: Bot receives message with MENTIONED_ONLY not matching own ID
- **WHEN** bot agent receives a group delivery where `replyPolicy` is `MENTIONED_ONLY` and its own `agentId` is absent from `mentions`
- **THEN** bridge sends Instant ACK (<50ms) to mark the task acknowledged, logs silent read, and terminates execution without LLM reasoning or outbound reply

#### Scenario: Bot receives message with MENTIONED_ONLY matching own ID
- **WHEN** bot agent receives a group delivery where `replyPolicy` is `MENTIONED_ONLY` and its own `agentId` is present in `mentions`
- **THEN** bridge sends Instant ACK, forwards the message prompt to its local LLM brain, and delivers the generated reply back to the group context
