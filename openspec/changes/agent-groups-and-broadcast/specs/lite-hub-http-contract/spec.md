## MODIFIED Requirements

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

## ADDED Requirements

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

