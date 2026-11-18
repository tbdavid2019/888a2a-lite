## MODIFIED Requirements

### Requirement: Gateway publishes standard Hub and Per-Agent Cards

Hub SHALL 在 `/.well-known/agent-card.json` 提供標準 A2A Gateway Agent Card，且 SHALL 在 `/a2a/v1/agents/{agentId}/card`（以及相容 alias `/a2a/v1/agents/{agentId}/.well-known/agent-card.json`）提供針對該 Agent 的 Per-Agent Card。
Card SHALL 宣告 `supportedInterfaces`（含 `url`、`protocolBinding: "HTTP+JSON"`、`protocolVersion: "1.0"`），在 Per-Agent Card 中 `tenant` 欄位 SHALL 設定為目標 `agentId`。
Card SHALL 宣告 `securitySchemes.bearerAuth.httpAuthSecurityScheme`（`scheme: "bearer"`、`bearerFormat: "AgentToken"`）與 `securityRequirements: [{"schemes":{"bearerAuth":{"list":[]}}}]`。
Card SHALL 明確宣告 `capabilities.streaming: true`，未支援之 push notifications 與 extended Agent Card 設為 false 或省略。
Card 的 `defaultInputModes`、`defaultOutputModes` 與相關 Skill modes SHALL 宣告 `text/plain` 以及 URL reference 可承載的實際 MIME profile；此宣告代表 Gateway 可保存與轉送該類型的 URL reference，不代表 Hub 會下載或解析檔案。

#### Scenario: Standard client discovers the gateway card
- **WHEN** client GET `/.well-known/agent-card.json`
- **THEN** Hub 回傳符合 A2A AgentCard 結構的 JSON，包含 `supportedInterfaces`、`capabilities`、`defaultInputModes`、`defaultOutputModes`、`skills` 與 `securitySchemes`，且 modes 包含已支援的 URL reference MIME profile

#### Scenario: Standard client fetches a specific agent card
- **WHEN** client GET `/a2a/v1/agents/{agentId}/card`
- **THEN** Hub 回傳該 Agent 的標準 Agent Card，其 `supportedInterfaces[0].tenant` 包含該 `agentId`，客戶端 SDK 能自動取得正確的 routing tenant

#### Scenario: Card does not expose circle secrets
- **WHEN** anonymous client 讀取標準 Agent Card
- **THEN** response 不包含 shared key、key digest、Agent Token、Operator Token、private circle list 或本機執行細節

### Requirement: Gateway accepts standard HTTP+JSON message requests with returnImmediately and envelope

Hub SHALL 提供 `POST /a2a/v1/message:send`（並在根目錄掛載 `POST /message:send` alias）。
Request SHALL 支援 `Content-Type: application/a2a+json` 與 `application/json`，並解析 `A2A-Version` 標頭；若請求不支援之版本，SHALL 回傳 `VERSION_NOT_SUPPORTED` 錯誤。
Request SHALL 接受標準 `SendMessageRequest` 結構，驗證 `message.messageId`、`message.role` 和 `parts`。Gateway 使用 request 的 `tenant` 欄位作為目標 Agent ID，authenticated caller 作為 requester。
Success response SHALL 封裝於 `SendMessageResponse` envelope（`{"task": {...}}` 或 `{"message": {...}}`），不得回傳未封裝之裸物件。
Message Parts SHALL 允許 `text` 與符合 URL attachment capability 的 HTTPS `url` reference 混合；`raw`、`data`、不合法 URL、缺少 URL MIME type 或未宣告 MIME profile 的 Part SHALL 被拒絕。

**`returnImmediately` 語意支援：**
- 若 `configuration.returnImmediately: false`（省略時預設）：Hub SHALL 等待終態或 INPUT_REQUIRED／AUTH_REQUIRED 才成功返回。HTTP 等待逾時 SHALL 回傳 504 deadline error 並保留 Task；不得以 200 WORKING 返回。client 斷線 SHALL 只終止等待，不取消工作。
- 若 `returnImmediately: true`：Hub 寫入 mailbox 後 SHALL 立即返回 `TASK_STATE_SUBMITTED` 或 `TASK_STATE_WORKING`。

#### Scenario: Client sends message with default blocking return
- **WHEN** client 發送未設定 `returnImmediately`（預設 false）的 SendMessageRequest
- **THEN** Hub 建立 task 並等待，目標 Agent 回傳 correlated reply 後，Hub 在同一個 HTTP 連線中直接回傳 `SendMessageResponse` 包含 `TASK_STATE_COMPLETED`

#### Scenario: Client sends message with returnImmediately true
- **WHEN** client 發送 `returnImmediately: true` 的 SendMessageRequest
- **THEN** Hub 建立 task 後立即回傳 `SendMessageResponse` 包含 `TASK_STATE_SUBMITTED`，由 client 自行後續輪詢或訂閱

#### Scenario: Missing target tenant is rejected
- **WHEN** request 沒有可解析的 target tenant
- **THEN** Hub 回傳 400 INVALID_ARGUMENT，且不建立 mailbox 或 A2A task

#### Scenario: Mixed text and URL Parts are accepted
- **WHEN** Message contains text Parts and valid HTTPS URL Parts whose media types are advertised by the target Card
- **THEN** Hub creates one Task and preserves all original Parts in the Task history and mailbox correlation

#### Scenario: Unsupported Part type is rejected
- **WHEN** Message 包含 `raw`、`data`、不合法 URL、缺少 URL media type，或未宣告的 media type
- **THEN** Hub 回傳 `ContentTypeNotSupportedError` 或 `INVALID_ARGUMENT`（400 Bad Request，包含 machine-readable error 詳情），且不產生 partial write
