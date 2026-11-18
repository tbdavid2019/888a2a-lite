## Purpose

提供安全且可持久化的外部檔案引用能力，讓 Agent 可先將圖片、音訊、影片或文件放入 888box 等物件儲存，再透過 A2A Part URL 傳遞引用，而不讓 Hub 下載或保存 binary。

## ADDED Requirements

### Requirement: URL attachment parts are bounded and structurally valid

Hub SHALL 接受 URL reference Part 與 text Part 的混合 Message 或 Artifact。每個 Part SHALL 恰好包含一種內容；本 capability 只允許 `text` 或 `url`，`raw` 與 `data` SHALL 被拒絕。URL Part SHALL 是絕對 HTTPS URL，不得包含 userinfo、fragment，且 SHALL 受 URL、MIME type、filename、Part 數量與 Artifact 數量上限限制。URL Part SHALL 提供合法的 `mediaType`。

#### Scenario: Agent sends a mixed text and file reference message
- **WHEN** authenticated client sends a Message containing one text Part and one HTTPS URL Part with `mediaType` and optional `filename`
- **THEN** Hub creates one durable Task and preserves both Parts in their original order

#### Scenario: Invalid URL reference is rejected
- **WHEN** client sends a URL Part with HTTP scheme, userinfo, fragment, missing media type, or a value beyond the configured URL limit
- **THEN** Hub returns `CONTENT_TYPE_NOT_SUPPORTED` or `INVALID_ARGUMENT` before creating a Task or mailbox delivery

#### Scenario: Inline and structured content remain disabled
- **WHEN** client sends a Message or Artifact containing `raw`, `data`, or more than one content field in a Part
- **THEN** Hub rejects the complete request atomically and creates no partial Task, delivery, or update

### Requirement: Hub treats URL references as opaque external content

Hub SHALL 保存與轉送合法 URL reference 的 metadata，但 SHALL NOT fetch、proxy、redirect、解析或執行 URL。Hub SHALL NOT require 888box credentials for ordinary A2A requests. URL upload、存取授權、下載、內容掃描與交給多模態模型 SHALL 由 Agent adapter 或外部 storage workflow 負責。

#### Scenario: URL reference is relayed without server-side fetch
- **WHEN** a valid URL Part is accepted in a standard Message or Artifact update
- **THEN** Hub stores and returns the URL Part without making an outbound request to the URL host

#### Scenario: URL is absent from audit payloads
- **WHEN** Hub records an audit event for a Task or Artifact containing a URL reference
- **THEN** the event contains bounded attachment metadata or a redacted URL indicator, and does not contain URL query credentials or the complete signed URL

### Requirement: Standard Part references reach the target adapter

For standard A2A deliveries, Hub SHALL preserve the validated Message Parts in the durable inbox projection in addition to the legacy flattened message string. The target adapter SHALL be able to recover URL, media type, filename, content type, and Part order from the inbox item. Legacy non-standard deliveries SHALL continue to omit the optional Parts field.

#### Scenario: Standard inbox preserves URL Part metadata
- **WHEN** a standard Message containing text and URL Parts is delivered to a target Agent
- **THEN** the target inbox item contains the same validated Parts in the same order, and the flattened message remains available for backward-compatible adapters

#### Scenario: Inbox Part projection survives restart
- **WHEN** a standard inbox item with URL Parts is persisted, the Hub restarts, and the target polls or opens the inbox stream
- **THEN** the Parts projection is returned without downloading the referenced URL or losing media type and filename metadata

### Requirement: URL attachment limits are independently configurable and observable

Hub SHALL retain the existing total HTTP body limit and SHALL additionally enforce bounded limits for URL length, filename length, MIME type length, Parts per Message, Parts per Artifact, Artifacts per Task, and total attachment metadata per update. Rejected requests SHALL use the existing machine-readable error envelope and SHALL not write durable state.

#### Scenario: Attachment collection exceeds a limit
- **WHEN** a Message or update contains more URL Parts, Artifacts, or attachment metadata than the configured bound
- **THEN** Hub returns a bounded 4xx error before persistence and emits no delivery event

#### Scenario: Valid attachment metadata survives restart
- **WHEN** a Task with URL Parts is persisted, the Hub restarts, and the requester reads the Task again
- **THEN** the same bounded URL Parts, media types, filenames, and ordering are returned
