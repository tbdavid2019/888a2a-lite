## MODIFIED Requirements

### Requirement: HTTP contract enforces compatibility and limits

API SHALL 使用 Hub contract 定義的 camelCase JSON 欄位和 `/hub/v1` path naming，並對 page size、inbox limit、wait time、payload bytes、capabilities 數量和字串長度套用安全上限。新增欄位不得改變既有欄位的語義；不支援的 full-project API 不得被假裝實作。
Standard A2A Message 與 Artifact URL references SHALL 額外套用 URL length、filename length、MIME type length、Part count、Artifact count 與 attachment metadata size 上限。超過上限或不符合 URL reference profile 的內容 SHALL 在任何 durable mutation 前被拒絕。

#### Scenario: Oversized query or payload is rejected
- **WHEN** page size、wait time、payload 或 collection 超過 policy 上限
- **THEN** server 在進入 domain mutation 前回傳 bounded 4xx error

#### Scenario: Invalid URL attachment is rejected before mutation
- **WHEN** standard Message 或 Executor update 含不合法 scheme、userinfo、fragment、MIME type、filename 或超過 attachment bound 的 URL Part
- **THEN** server 回傳 machine-readable 4xx error，且沒有新增 Task、mailbox、revision 或 audit delivery event

#### Scenario: Full SaaS route is not part of Lite
- **WHEN** client 呼叫 organization、chat、billing 或 runtime execution route
- **THEN** server 回傳 not-found 或 not-supported，且不觸發任何本機 Agent 執行

### Requirement: Audit events are durable and operator-only

Hub SHALL 將註冊、heartbeat、disconnect、task delivery、poll、ACK、cancel、revoke、registration policy change 和 Hub lifecycle 的安全摘要寫入 durable event log。只有 operator credential 可以查詢 event log；事件不得包含 Agent Token、Token hash、完整 message payload 或其他 credential。URL attachment audit data SHALL 只包含 bounded metadata、content hash 或 redacted URL indicator，不得包含完整預簽名 URL 或其 credential-like query values。

#### Scenario: Operator reads events after restart
- **WHEN** operator 以合法 credential 呼叫 `GET /hub/v1/admin/events`，且 Hub 曾經重啟
- **THEN** Hub 依 `afterId` 遞增回傳持久事件，並保留 event type、actor/target/task identity、safe details 和 created-at

#### Scenario: Signed URL is not written to audit event
- **WHEN** standard Task 或 Artifact 包含帶有 query credential 的 URL reference
- **THEN** audit event 不包含完整 URL、query token 或其他可直接重用的下載憑證

#### Scenario: Agent cannot read audit events
- **WHEN** Agent Token 或無效 credential 呼叫 admin event endpoint
- **THEN** Hub 回傳 unauthorized 或 forbidden，且不回傳任何 event detail
