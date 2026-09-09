# lite-hub-http-contract Specification

## Purpose
定義 Lite Hub 可供各種 Agent adapter 使用的穩定 HTTP/JSON 邊界，讓註冊、發現、
heartbeat、訊息遞送和 operator controls 不需要依賴完整 888a2a 的 SaaS API。

## Requirements

### Requirement: Lite Hub exposes the versioned HTTP routes

Server SHALL 提供 `GET /healthz`，以及既有 `/hub/v1` status、Agent register、Agent list、
Agent lookup、Agent Card、heartbeat、disconnect、direct task delivery、inbox poll、inbox
ACK、registration control、Agent revoke、task cancel、announcement 和 operator-only event
routes。Lite v1 另 SHALL 提供 optional group extension 的 group lifecycle、membership、
roster、group message 和 history routes。Lite v1 SHALL 不提供完整 SaaS 的 organization、
billing 或 runtime execution API；group extension 不等同於完整 SaaS chat。在 `multi` mode，
對於跨 Agent 的 Task 派送與查詢，Hub SHALL 嚴格比對發起端與目標端的 `circle_id`；若目標端存在於不同圈，Hub SHALL 回傳 HTTP 404 Agent Not Found，不洩漏任何跨圈目標存在資訊。

#### Scenario: Health endpoint is available

- **WHEN** client 呼叫 `GET /healthz`
- **THEN** server 回傳可表示 process 與 database ready 狀態的成功或服務不可用結果，
  且不洩漏 credential

#### Scenario: Versioned route is stable

- **WHEN** client 呼叫 `/hub/v1` 中已定義的 endpoint
- **THEN** server 使用 JSON response 遵守本規格的 request、response、status code 和
  error envelope，不要求完整 Manager 的 session 或 organization context

#### Scenario: Unsupported group extension is optional

- **WHEN** client 不理解 group extension 而只呼叫既有 direct routes
- **THEN** Hub 維持既有 direct flow，且不要求 client 實作或執行 group operation

#### Scenario: Status metadata is scoped by credential

- **WHEN** an anonymous client, an authenticated Agent, or an Operator requests Hub status in `multi` mode
- **THEN** the anonymous response SHALL omit private-circle Agent and task counts, the Agent response SHALL contain only its own circle summary, and the Operator response MAY contain global totals with circle filters

#### Scenario: Cross-circle task dispatch yields 404

- **WHEN** 處於 public 圈的 Agent 嘗試對私有圈的目標 Agent 發送 Task
- **THEN** Hub 回傳 HTTP 404 Agent Not Found，拒絕排程且不建立任何 mailbox 項目

#### Scenario: Intra-circle task dispatch succeeds

- **WHEN** 處於同一圈的 Agent A 對 Agent B 發送 Task
- **THEN** Hub 接受 Task 並排入目標 Agent B 的 inbox，回傳 HTTP 200 或 202

### Requirement: Group HTTP routes enforce membership and bounded pagination

Hub SHALL 以 `/hub/v1/groups` 提供建立、列出可見群組、取得群組、邀請／接受／退出／移除、
封存、roster、group history 和 group message routes。每個 request SHALL 驗證 Agent 或
operator credential、group membership／role、path ID、body limit、group size、fan-out
limit 和 cursor／page limit，並使用既有 machine-readable error envelope。

#### Scenario: Member sends a group message

- **WHEN** authenticated group member POST group message with valid idempotency key
- **THEN** Hub 回傳 group message identity、recipient delivery summary 和下一個 history
  cursor，且不回傳其他 Agent 的 credential 或私有 inbox data

#### Scenario: Non-member sends a group message

- **WHEN** registered Agent 對不屬於自己的 group POST message
- **THEN** Hub 回傳 forbidden 或 not-found，且不建立任何 fan-out delivery

#### Scenario: Member reads group roster and history

- **WHEN** authenticated member 呼叫 roster 或 history，帶 bounded `afterId`／`limit`
- **THEN** Hub 只回傳該 member 有權限看見的 safe metadata，依 monotonic ID 排序並提供
  next cursor

### Requirement: HTTP authentication separates Agent and operator credentials

Lite v1 的 Public registration SHALL 使用匿名請求；後續 Agent request SHALL 使用
Agent ID 和 Agent bearer Token；admin request SHALL 使用獨立 operator bearer Token。
Lite v1 不得因相容完整 Hub 而重新引入 bootstrap/open registration。Server SHALL
拒絕將一種 credential 當作另一種使用。

#### Scenario: Public registration is anonymous

- **WHEN** Public mode 且 registration enabled 的 client 送出合法 declaration
- **THEN** server 接受 registration，不要求 credential，並只在首次成功回應
  明文 Agent Token

#### Scenario: Admin endpoint requires operator token

- **WHEN** client 沒有有效 operator bearer Token 呼叫 admin endpoint
- **THEN** server 回傳 unauthorized 或 forbidden，且不執行 admin action

### Requirement: Errors are bounded and machine-readable

每個失敗 HTTP response SHALL 使用一致 JSON error envelope，至少包含穩定 error code
和人類可讀 message；message 不得包含 stack trace、SQL、Token、request secret 或完整
payload。Server SHALL 對不合法 JSON、未知欄位、過大 body、無效 path parameter 和
不支援 method 回傳適當 4xx status。

#### Scenario: Invalid request has a stable error

- **WHEN** client 傳送缺少必要欄位或格式錯誤的 request
- **THEN** server 回傳 4xx、machine-readable error code 和不含秘密的 message，且不
  建立或修改 Hub state

#### Scenario: Method is unsupported

- **WHEN** client 對合法 path 使用未定義 HTTP method
- **THEN** server 回傳 method-not-allowed 類型的 4xx response，且 response 不洩漏
  server internals

### Requirement: HTTP contract enforces compatibility and limits

API SHALL 使用 Hub contract 定義的 camelCase JSON 欄位和 `/hub/v1` path naming，並
對 page size、inbox limit、wait time、payload bytes、capabilities 數量和字串長度套用
安全上限。新增欄位不得改變既有欄位的語義；不支援的 full-project API 不得被假裝
實作。

#### Scenario: Oversized query or payload is rejected

- **WHEN** page size、wait time、payload 或 collection 超過 policy 上限
- **THEN** server 在進入 domain mutation 前回傳 bounded 4xx error

#### Scenario: Full SaaS route is not part of Lite

- **WHEN** client 呼叫 organization、chat、billing 或 runtime execution route
- **THEN** server 回傳 not-found 或 not-supported，且不觸發任何本機 Agent 執行

### Requirement: Audit events are durable and operator-only

Hub SHALL 將註冊、heartbeat、disconnect、task delivery、poll、ACK、cancel、revoke、
registration policy change 和 Hub lifecycle 的安全摘要寫入 durable event log。只有
operator credential 可以查詢 event log；事件不得包含 Agent Token、Token hash、完整
message payload 或其他 credential。

#### Scenario: Operator reads events after restart

- **WHEN** operator 以合法 credential 呼叫 `GET /hub/v1/admin/events`，且 Hub 曾經重啟
- **THEN** Hub 依 `afterId` 遞增回傳持久事件，並保留 event type、actor/target/task
  identity、safe details 和 created-at

#### Scenario: Agent cannot read audit events

- **WHEN** Agent Token 或無效 credential 呼叫 admin event endpoint
- **THEN** Hub 回傳 unauthorized 或 forbidden，且不回傳任何 event detail

### Requirement: Hub serves static bootstrap scripts
The Hub SHALL expose unauthenticated GET endpoints for `GET /install.sh` and `GET /a2a_bridge.py` (or embedded worker assets), returning raw executable scripts with appropriate MIME content-types (`text/plain` or `text/x-shellscript` / `text/x-python`).

#### Scenario: Machine downloads install script via curl
- **WHEN** any HTTP client calls `GET /install.sh`
- **THEN** the Hub responds with HTTP 200 and the plain text shell installer content, without requiring authentication tokens

#### Scenario: Installer downloads standalone bridge script
- **WHEN** an installer script requests `GET /a2a_bridge.py`
- **THEN** the Hub responds with HTTP 200 and the standalone Python bridge script content

### Requirement: Circle authorization is explicit across Agent routes

Authenticated Agent operations SHALL carry a typed principal containing `hubId`, `agentId`, and
persisted `circleId`. Circle authorization SHALL apply to Agent list, lookup, Agent Card, heartbeat,
disconnect, task delivery, inbox poll, ACK, SSE stream, group lifecycle, invitations, roster,
history, and group messages. Request context MAY carry the principal for handler convenience but
SHALL NOT be the only authorization source.

#### Scenario: Agent route rejects a mismatched resource circle

- **WHEN** an authenticated Agent requests a target, inbox, or group resource whose persisted `circleId` differs from the principal's `circleId`
- **THEN** the Hub SHALL return the route's masked 404 response and SHALL perform no mutation

### Requirement: Lite Hub exposes a standard A2A compatibility surface

在既有 `/hub/v1` contract 之外，Lite Hub SHALL 提供由標準 Agent Card 宣告的 A2A HTTP+JSON Gateway routes（掛載於 `/a2a/v1` 並支援根目錄 alias），包括 `/.well-known/agent-card.json`、Per-Agent Card `/a2a/v1/agents/{agentId}/card`、`POST /message:send`、`POST /message:stream`、`GET /tasks/{id}`、`GET /tasks`、`POST /tasks/{id}:cancel` 和 `POST /tasks/{id}:subscribe`。Standard routes SHALL 有獨立且嚴格遵守 A2A 1.0.0 規範的 contract（包含 `SendMessageResponse` envelope 與 `google.rpc.Status` 錯誤結構），並 SHALL 不改寫或假裝是既有 `/hub/v1` endpoint。

#### Scenario: Standard route and custom route coexist

- **WHEN** client 分別呼叫 standard route 與 `/hub/v1` route
- **THEN** 兩者都依各自宣告的 schema 回應，且既有 `/hub/v1` 行為不被 standard adapter 改變
