## ADDED Requirements

### Requirement: Embedded and distributed Bridge assets remain behaviorally identical

`examples/worker/a2a_bridge.py` 與 embedded `internal/service/a2a_bridge.py` SHALL 使用相同的 Local UI API、Group Gateway mapping、reply policy、runtime validation、local security token 與 durable history behavior。CI SHALL 比對來源/embedded asset parity 或等價產物。

#### Scenario: Downloaded bridge matches embedded bridge

- **WHEN** CI 取得 Hub 的 `/a2a_bridge.py` 與 repository distributed script
- **THEN** 版本、security behavior、group metadata mapping 和 standard update handling 一致

### Requirement: Local APIs provide scoped group history and standard dispatch

Local UI SHALL 提供 bounded `GET /api/groups`、`GET /api/groups/{id}/messages` 和 `POST /api/groups/{id}/messages`。POST facade SHALL 使用 existing Human Agent Token 經 standard Group Gateway 發送，帶 `tenant`、`A2A-Extensions`、reply policy、mentions、Parent Task reference 和 idempotency；local store SHALL 保存 hub/circle/group/task/revision scope。

#### Scenario: Local group send uses standard contract

- **WHEN** user 透過 local facade 發送帶 `text`、`replyPolicy` 和 `mentions` 的訊息
- **THEN** server 轉成 standard Group Message、保存本地 pending record、回傳 Parent Task reference，並可透過 stream/subscribe 更新狀態

#### Scenario: Local history pagination is bounded

- **WHEN** client 查詢 group history
- **THEN** server 驗證 group/circle scope、bounded cursor/limit，並以 idempotent event hydration 回傳結果

### Requirement: Local mutation APIs have a browser security boundary

Local UI SHALL 為 process session 產生高熵 token，所有 POST/PATCH/DELETE facade SHALL 驗證 token、Origin/Host、Content-Type、body limit 和 unknown fields。GET history/event APIs SHALL 不觸發 OS mutation；錯誤不得回傳 stack trace、command output secret 或 credentials。

#### Scenario: Missing local token is rejected

- **WHEN** browser 呼叫 local mutation API 但沒有有效 local token
- **THEN** server 回 401/403，不寫入 group/runtime config，也不啟動 service

### Requirement: Group events hydrate after local restart

Local UI SHALL 將 group Parent/Member Task stream 與 local P2P events 分開解析，使用 scoped revision cursor 補發 SSE 中斷期間事件；重複 event、跨圈 event、撤銷 Agent event SHALL 不重複或不落盤。

#### Scenario: Group stream reconnects

- **WHEN** browser 或 local bridge 重連 group Task stream
- **THEN** server 從 durable revision 補發缺少的 parent/member messages，timeline 順序穩定且不產生重複氣泡
