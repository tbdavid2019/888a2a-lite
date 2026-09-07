## MODIFIED Requirements

### Requirement: Lite Hub exposes the versioned HTTP routes

Server SHALL 提供 `GET /healthz`，以及既有 `/hub/v1` status、Agent register、Agent list、
Agent lookup、Agent Card、heartbeat、disconnect、direct task delivery、inbox poll、inbox
ACK、registration control、Agent revoke、task cancel、announcement 和 operator-only event
routes。Lite v1 另 SHALL 提供 optional group extension 的 group lifecycle、membership、
roster、group message 和 history routes。Lite v1 SHALL 不提供完整 SaaS 的 organization、
billing 或 runtime execution API；group extension 不等同於完整 SaaS chat。對於跨 Agent 的 Task 派送與查詢，Hub SHALL 嚴格比對發起端與目標端的 `circle_id`；若目標端存在於不同圈，Hub SHALL 回傳 HTTP 404 Agent Not Found，不洩漏任何跨圈目標存在資訊。

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

#### Scenario: Cross-circle task dispatch yields 404

- **WHEN** 處於 public 圈的 Agent 嘗試對私有圈的目標 Agent 發送 Task
- **THEN** Hub 回傳 HTTP 404 Agent Not Found，拒絕排程且不建立任何 mailbox 項目

#### Scenario: Intra-circle task dispatch succeeds

- **WHEN** 處於同一圈的 Agent A 對 Agent B 發送 Task
- **THEN** Hub 接受 Task 並排入目標 Agent B 的 inbox，回傳 HTTP 200 或 202
