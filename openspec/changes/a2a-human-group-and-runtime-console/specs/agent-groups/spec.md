## ADDED Requirements

### Requirement: Group message dispatch validates human client mentions and reply policy
Hub group message dispatch endpoints SHALL accept client-specified `replyPolicy` and `mentions` from authenticated human client sessions, validating that all mentioned IDs belong to active group members.

#### Scenario: Group message dispatch preserves human mentions list and reply policy
- **WHEN** human client session posts a group message with `replyPolicy: MENTIONED_ONLY` and a list of member agent IDs
- **THEN** Hub records `reply_policy` and `mentions_json` on the generated inbox items and delivers the metadata to member SSE stream listeners
