# Spec Delta

## ADDED Requirements

### Requirement: Executor reply messages carry required identity and task correlation

When an executor update includes a standard Agent `Message`, the Gateway SHALL require a non-empty `messageId`, `contextId`, and `taskId`. The `taskId` and `contextId` SHALL match the persisted task before the update is committed. An invalid or mismatched message SHALL be rejected without changing task state, history, revision, or events.

#### Scenario: Executor reply has complete matching identity
- **WHEN** an assigned target reports a Message with messageId, matching taskId, and matching contextId
- **THEN** the Hub stores the Message in task history and applies the requested state update

#### Scenario: Executor reply is missing messageId
- **WHEN** an assigned target reports a Message without messageId
- **THEN** the Hub returns `INVALID_ARGUMENT` and leaves the task unchanged

#### Scenario: Executor reply targets a different task context
- **WHEN** an assigned target reports a Message whose taskId or contextId differs from the persisted task
- **THEN** the Hub returns `INVALID_ARGUMENT` and leaves the task unchanged

### Requirement: Agent Cards advertise the accepted URL attachment MIME profile

The Gateway and Per-Agent Agent Cards SHALL advertise every MIME family and exact MIME type accepted by URL attachment validation, including supported text, image, audio, video, PDF, archive, and office document types. The Gateway SHALL reject URL Part media types that are outside the advertised profile.

#### Scenario: Card includes every accepted exact MIME type
- **WHEN** a client reads the Gateway or Per-Agent Agent Card
- **THEN** its input and output modes include the full set of exact URL attachment MIME types accepted by the Gateway

#### Scenario: Wildcard media type is represented consistently
- **WHEN** the Gateway accepts an image, audio, or video subtype
- **THEN** the corresponding Agent Card advertises that media family using its wildcard mode
