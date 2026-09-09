## Purpose

提供可被標準 A2A client 發現與呼叫的 HTTP+JSON Gateway，將標準 Message、Task、streaming 與 cancellation 映射到 888a2a-lite 的持久 mailbox，同時保留既有 Hub contract。

## ADDED Requirements

### Requirement: Gateway publishes standard Hub and Per-Agent Cards

Hub SHALL 在 `/.well-known/agent-card.json` 提供標準 A2A Gateway Agent Card，且 SHALL 在 `/a2a/v1/agents/{agentId}/card`（以及相容 alias `/a2a/v1/agents/{agentId}/.well-known/agent-card.json`）提供針對該 Agent 的 Per-Agent Card。
Card SHALL 宣告 `supportedInterfaces`（含 `url`、`protocolBinding: "HTTP+JSON"`、`protocolVersion: "1.0"`），在 Per-Agent Card 中 `tenant` 欄位 SHALL 設定為目標 `agentId`。
Card SHALL 宣告 `securitySchemes.bearerAuth.httpAuthSecurityScheme`（`scheme: "bearer"`、`bearerFormat: "AgentToken"`）與 `securityRequirements: [{"schemes":{"bearerAuth":{"list":[]}}}]`。
Card SHALL 明確宣告 `capabilities.streaming: true`，未支援之 push notifications 與 extended Agent Card 設為 false 或省略。

#### Scenario: Standard client discovers the gateway card
- **WHEN** client GET `/.well-known/agent-card.json`
- **THEN** Hub 回傳符合 A2A AgentCard 結構的 JSON，包含 `supportedInterfaces`、`capabilities`、`defaultInputModes`、`defaultOutputModes`、`skills` 與 `securitySchemes`

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

#### Scenario: Unsupported Part type is rejected
- **WHEN** Message 包含任一不支援的 Part，包括混合 text 與 file/data
- **THEN** Hub 回傳 `ContentTypeNotSupportedError`（400 Bad Request，包含 `CONTENT_TYPE_NOT_SUPPORTED` 錯誤詳情），且不產生 partial write

### Requirement: Standard Task identity, lifecycle and list envelopes are durable

每個 standard message SHALL 關聯 durable A2A Task、context、turn 與 mailbox delivery；多輪 Message SHALL 可共用同一 Task。
`GET /tasks/{id}` SHALL 回傳標準 Task 結構；`GET /tasks` SHALL 支援 bounded `contextId`、`status`、`pageSize`、`pageToken` query，且 response SHALL 封裝為 `ListTasksResponse`（`{"tasks": [...], "nextPageToken": "..."}`）。
Task state 枚舉 SHALL 完整包含 `TASK_STATE_SUBMITTED`、`TASK_STATE_WORKING`、`TASK_STATE_INPUT_REQUIRED`、`TASK_STATE_AUTH_REQUIRED`、`TASK_STATE_COMPLETED`、`TASK_STATE_FAILED`、`TASK_STATE_CANCELED`、`TASK_STATE_REJECTED`。

#### Scenario: Task survives Hub restart
- **WHEN** standard message 已建立 task 或 mailbox item 後 Hub restart
- **THEN** client 以相同 task ID 查詢時仍取得原本的 Task、context、status 和 correlation metadata，不建立第二個 task

#### Scenario: Client lists tasks with envelope
- **WHEN** authenticated caller GET `/tasks?contextId=xxx`
- **THEN** Hub 回傳 `ListTasksResponse` envelope，內含 `tasks` 陣列與分頁 token，不洩漏其他 circle 的 task

#### Scenario: Unknown task is masked
- **WHEN** caller 查詢不存在、無權限或其他 circle 的 task ID
- **THEN** Hub 回傳標準 `TASK_NOT_FOUND` 錯誤，不透露 task 是否存在於其他 circle

### Requirement: Gateway supports standard streaming with StreamResponse and keepalive

Hub SHALL 提供 `POST /a2a/v1/message:stream`（根別名 `POST /message:stream`）與 `POST /a2a/v1/tasks/{id}:subscribe`（根別名 `POST /tasks/{id}:subscribe`）。
Stream SHALL 使用 `text/event-stream`，每個 `data:` 負載 SHALL 嚴格為 `StreamResponse` envelope（`task`、`message`、`statusUpdate` 或 `artifactUpdate` 四選一）。
首個事件 SHALL 為初始 `Task` 或 `Message`，後續推送狀態或 artifact 更新，在進入 terminal state 後關閉連線。
串流端點 SHALL 清除全域寫入截止時間（`SetWriteDeadline(time.Time{})`），並每 15 秒發送 `: keepalive\n\n` 註釋防範網路中介節點斷線。
既有 `/hub/v1/.../inbox/stream` SHALL 維持自訂 `InboxItem` 格式，不得與標準串流混淆。

#### Scenario: Streaming message begins with task

- **WHEN** authenticated client 對可見 target 呼叫 `POST /message:stream`
- **THEN** Hub 回傳 HTTP 200 SSE，stream 第一個資料事件包含該標準 Task，後續事件遵守標準 stream response 結構

#### Scenario: Stream closes at terminal state

- **WHEN** correlated reply、failure 或 cancellation 使 task 進入 terminal state
- **THEN** Hub 發送最後狀態事件並關閉 SSE connection

#### Scenario: Client resubscribes to an existing task

- **WHEN** client 對尚未 terminal 的可見 task 呼叫 `POST /tasks/{id}:subscribe`
- **THEN** Hub 從 durable task state 補發可重建目前狀態的事件，不要求原本 connection 仍存在

### Requirement: Standard task cancellation is authorized and idempotent

Hub SHALL 提供 `POST /tasks/{id}:cancel`。只有 task requester 或被授權的 operator 可以取消 task；尚未被 target ACK 的 mailbox delivery SHALL 一併取消，已進入不可取消 processing 的 task SHALL 回傳標準 unsupported-operation/conflict error。重複 cancellation SHALL 不建立新 state 或新 mailbox item。

#### Scenario: Requester cancels submitted task

- **WHEN** task requester 在 target 尚未 ACK 前呼叫 cancel
- **THEN** Hub 將標準 Task 和對應 mailbox item 設為 canceled，並回傳更新後 Task

#### Scenario: Different Agent cannot cancel task

- **WHEN** 其他 Agent 以有效 Token 呼叫同一 task 的 cancel
- **THEN** Hub 回傳 forbidden 或 task-not-found，且 task state 不變

### Requirement: Standard errors conform to google.rpc.Status and ErrorInfo

所有標準 HTTP+JSON Gateway 端點的錯誤回應 SHALL 遵循 A2A 規範 Section 11.6 與 Section 5.4，使用 `google.rpc.Status` JSON 結構，並在 `details` 包含 `@type: "type.googleapis.com/google.rpc.ErrorInfo"`。
`ErrorInfo.domain` SHALL 設定為 `"a2a-protocol.org"`，`reason` SHALL 使用大寫蛇形（UPPER_SNAKE_CASE，如 `TASK_NOT_FOUND`、`TASK_NOT_CANCELABLE`、`CONTENT_TYPE_NOT_SUPPORTED`、`UNSUPPORTED_OPERATION`、`VERSION_NOT_SUPPORTED`）。
跨圈呼叫不存在或被隔離之目標時，SHALL 回傳 `TASK_NOT_FOUND`（404），嚴格遮蔽目標存在性。

#### Scenario: Client requests unknown task
- **WHEN** client 請求不存在或跨圈的 task ID
- **THEN** Hub 回傳 HTTP 404，body 含 `error.details` 中 `reason: "TASK_NOT_FOUND"` 與 `domain: "a2a-protocol.org"`

#### Scenario: Client attempts to cancel non-cancelable task
- **WHEN** requester 嘗試取消 working、interrupted 或非 CANCELED 的 terminal task
- **THEN** Hub 回傳 HTTP 400，body 含 `reason: "TASK_NOT_CANCELABLE"`

### Requirement: Standard and custom protocols coexist

Standard Gateway SHALL 與 `/hub/v1` custom routes 共存，兩者 SHALL 共用同一份 Agent identity、circle authorization、durable mailbox、idempotency policy 和 operator controls。啟用 standard Gateway 不得改變既有 `/hub/v1` response、SSE event、ACK semantics 或 Multi-Circle cross-circle masking。

#### Scenario: Existing client continues using custom Hub API

- **WHEN** existing client 呼叫 `/hub/v1` direct task 或 inbox routes
- **THEN** Hub 維持既有 custom contract，且不要求 client 解析標準 A2A Task 或 Message

#### Scenario: Standard and custom operations share identity

- **WHEN** 同一 Agent 先以 standard Gateway 建立 task，再以既有 Hub API 查詢自己的 mailbox
- **THEN** Hub 使用同一個 persisted Agent identity 和 circle，且不產生第二套 registration credential

### Requirement: Blocking deadline preserves durable work

等待期限與執行期限 SHALL 分開處理；重送同 messageId SHALL 找回原 Task，HTTP 逾時不得產生第二次執行。

#### Scenario: Wait expires while executor is working
- **WHEN** blocking request 先達到 HTTP 等待期限
- **THEN** response 為 504，Task 仍可查詢並在後續執行完成後取得結果

### Requirement: Interrupted tasks resume with consistent identity

新 Message 帶 taskId 時 SHALL 驗證原 requester、tenant 與 context，僅 interrupted Task 可開始新 turn。省略 context SHALL 沿用原值。WORKING 的新輸入 SHALL 回 409，terminal 的新輸入 SHALL 回 UNSUPPORTED_OPERATION；相同 messageId 的合法重試 SHALL 回原 Task。去重 scope SHALL 包含 hub/circle/requester/target/messageId，同鍵不同內容 SHALL 回 409。

#### Scenario: Resume input required task
- **WHEN** 原 requester 帶原 taskId 回覆所需輸入
- **THEN** Hub 保留 task/context，原子建立一個新 turn/delivery；並行競爭只有一筆成功

#### Scenario: Message retry after completion
- **WHEN** requester 重送已接受的相同 messageId 與內容
- **THEN** Hub 返回原 Task，不重新執行或建立新 delivery

### Requirement: Cancellation and execution admission are atomic

取消 SHALL 與 ACK 競爭同一個原子執行資格。Bridge SHALL 先本機保存再 ACK，只有成功 ACK 才執行。取消勝出 SHALL 拒絕後續 ACK 與結果；ACK 勝出 SHALL 回 TASK_NOT_CANCELABLE。再次取消 CANCELED SHALL 成功返回既有 Task。

#### Scenario: Cancel after SSE delivery before ACK
- **WHEN** Bridge 已收到 event 但 cancel 先提交
- **THEN** ACK 回 canceled，Bridge 不執行，Task 不被遲到結果復活

### Requirement: Standard interoperability is independently verified

相容性 SHALL 以固定官方規範 revision/schema 和未修改的官方 SDK exact version 在 CI 驗收。fixture SHALL 包含根卡、Per-Agent Card、Bearer、tenant、多輪、timeout、SSE 與取消；自製 client 測試不得替代官方 SDK 互通證據。

#### Scenario: Official SDK targets an agent
- **WHEN** 官方 SDK 使用 Per-Agent Card 與正常 Bearer 設定
- **THEN** 無 serializer patch 即可送往宣告的 tenant 並解析回應；版本與結果列入驗收紀錄
