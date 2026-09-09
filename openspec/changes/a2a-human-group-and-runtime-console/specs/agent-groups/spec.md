## ADDED Requirements

### Requirement: Human clients participate as first-class group participants
Hub SHALL accept group messages dispatched by human client sessions, correctly recording author identity, `replyPolicy`, and `mentions` arrays, and dispatching them through the standard group distribution pipeline.

#### Scenario: Human message routing in group
- **WHEN** a human client sends a message containing `replyPolicy: MENTIONED_ONLY` and `mentions`
- **THEN** Hub preserves these routing directives and delivers individual task items to member inboxes accordingly
