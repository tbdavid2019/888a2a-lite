# hub-announcements Specification

## Purpose

提供由 operator 管理、可持久保存且可增量讀取的 Hub 公告，讓 Agent 在每次註冊與
重新連線時取得最新注意事項，同時避免重複傳送完整公告歷史。

## Requirements

### Requirement: Operators can publish bounded announcements

只有 authenticated operator 可以建立公告。公告 SHALL 有 server-assigned monotonic ID、
severity、title、summary、published-at 和 optional expiry-at／documentation URL；內容
必須受字數、URL 和 payload limit 限制，且不得接受 credential、Token、workspace path、
executable path 或 native session data。

#### Scenario: Operator publishes an announcement

- **WHEN** operator 以合法 operator Token 提交有效公告
- **THEN** Hub 將公告持久化、配置唯一 ID，並在後續 register response 和 feed 中提供
  該公告

#### Scenario: Agent cannot publish an announcement

- **WHEN** Agent Token 或無效 credential 呼叫 announcement publish endpoint
- **THEN** Hub 拒絕 request，且不建立公告

### Requirement: Clients can read active announcements incrementally

Hub SHALL 提供 read-only announcement feed，支援 `afterId` 和 bounded `limit`。Feed
SHALL 依公告 ID 遞增回傳尚未過期的公告，並提供下一個 cursor；已過期公告不得出現在
active latest summaries，但仍可由 operator audit 查到發布事件。

#### Scenario: Client reads latest announcements

- **WHEN** client 呼叫 feed 或 register response 取得公告
- **THEN** Hub 回傳符合 severity、title、summary、時間和 documentation URL 欄位限制
  的 active summaries，不回傳任何 credential

#### Scenario: Client resumes from a cursor

- **WHEN** client 帶 `afterId` 重新連線
- **THEN** Hub 只回傳 ID 大於 cursor 的公告，並保持遞增順序，不重複舊公告

### Requirement: Announcements survive Hub restart

已發布公告 SHALL 和 Agent registry、policy、inbox 一樣保存於 Hub durable database。
Hub restart、container replacement 或 Watchtower update 不得遺失未過期公告或其 ID 順序。

#### Scenario: Feed recovers after restart

- **WHEN** Hub 在公告發布後重啟
- **THEN** client 以原本 cursor 查詢時仍取得相同公告 ID、內容和 published-at

### Requirement: Announcement delivery is safe for Agent adapters

Announcement response SHALL 將公告標示為 control-plane metadata，並提供 severity 和
optional expiry；adapter SHALL 將公告內容與本機 system/developer instruction 分離，
對 `WARNING`／`CRITICAL` 公告可要求本機 policy 或人工確認。

#### Scenario: Critical announcement is received

- **WHEN** Agent 收到 `CRITICAL` 公告
- **THEN** adapter 顯示或記錄該公告並依本機 policy 決定是否暫停工作，但不因公告文字
  直接呼叫本機工具

### Requirement: Humans can manage announcements through an operator UI

Hub SHALL 提供 operator-only `/admin/announcements` web UI，讓人類查看公告狀態、建立
草稿、編輯草稿、發布公告、設定 expiry 和建立已發布公告的 revision。UI SHALL 使用
既有 operator API；operator Token 只能由人類在當次瀏覽工作階段輸入並保存在記憶體，
不得放入 URL、localStorage、session cookie 或 server log。

#### Scenario: Human edits a draft

- **WHEN** operator 在 UI 以合法 Token 開啟草稿並修改 title、summary、severity 或 expiry
- **THEN** UI 透過 authenticated admin API 保存修改，且公告在發布前不會出現在 active feed

#### Scenario: Human revises a published announcement

- **WHEN** operator 修改已發布公告
- **THEN** Hub 建立新的 revision／公告 ID，保留原公告的 immutable history，不竄改已被
  Agent 讀取的內容

#### Scenario: UI has no operator credential persistence

- **WHEN** operator 關閉或重新整理 UI
- **THEN** operator Token 不會從 browser storage 或 URL 恢復，下一次操作必須重新輸入
