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
billing 或 runtime execution API；group extension 不等同於完整 SaaS chat。

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
