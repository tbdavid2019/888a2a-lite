## Context

目前 repository 只有規劃文件；來源 Hub 的 contract、registry、mailbox 和 PostgreSQL
adapter 位於完整 `888a2a` checkout。Lite v1 固定為 Public mode，必須保持可獨立部署，
使用 SQLite 作為 live mailbox，並遵守不在本機執行測試或 build、改由 GitHub Actions
驗證的專案限制。

## Goals / Non-Goals

**Goals:**

- 用清楚的 domain contract 隔離 HTTP、registry、mailbox 和 persistence。
- 讓 SQLite 在 WAL、foreign key、busy timeout 和 transaction 條件下安全保存 Agent
  registry、policy 與 inbox。
- 以一次性 Token issuance、hash-only credential storage、safe projection 和 bounded
  request validation 建立 Public Hub 的安全邊界。
- 保留 `/hub/v1` 的 HTTP/JSON 相容形狀，讓日後 client adapter 可以共用。
- 讓每個實作階段都能以 GitHub Actions 和遠端 smoke test 驗證。

**Non-Goals:**

- 不搬移完整 Manager、organization/IAM、聊天、billing、approval 或 runtime。
- 不讓 Hub 執行 peer 的 shell、檔案、網路、憑證、模型或本機程序。
- v1 不做多 Hub replication、DuckDB analytics、WebSocket streaming 或完整 A2A
  protocol translation。
- 不在本機執行測試、build 或 production smoke test。

## Decisions

### 1. 分層的 Go module 邊界

以 `internal/hub` 保存 registry、policy、delivery 和 HTTP orchestration；以
`internal/store` 定義 persistence interface；以 `internal/store/sqlite` 實作 SQLite；
以 `sdk/httpclient` 保存可重用的 HTTP client；以 `cmd/888a2a-lite` 提供 server 與 CLI
入口。HTTP handler 不直接寫 SQL，domain 不依賴 HTTP request type。

替代方案是直接沿用完整專案的 Manager package，但會把不需要的登入、組織和 PostgreSQL
依賴帶入 Lite，破壞獨立部署和清楚的 extraction boundary。

### 2. SQLite 是 v1 唯一 live store

SQLite connection 啟用 WAL、foreign keys 和 busy timeout；schema bootstrap/migration
在啟動時執行。Agent、policy 和 inbox mutation 以 transaction 完成，並由資料庫 unique
constraint 保證 registration 和 delivery idempotency。Inbox sequence 使用單一 Hub
資料庫內的 monotonic allocation。

替代方案是直接使用 in-memory mailbox，雖然容易開始，但無法滿足重啟恢復；DuckDB 保留給
未來事件分析，不能取代即時 mailbox 的 transactional path。

### 3. Credential 與公開資料採 hash / projection 分離

註冊時以 cryptographically secure random value 產生 Agent ID 和 Token；只保存 Token
hash，並透過固定時間比較驗證。Peer list 與 Agent Card 從 registry record 建立 safe
projection，明確排除原始 card、secret、workspace 和 runtime metadata。Token 只存在
首次 register response 和 client 自己的 secret storage。

替代方案是保存明文 Token 以簡化重連，但一旦 database 或 debug output 外洩，所有 Agent
都會受到影響，且不符合既定安全邊界。

### 4. Poll + ACK 作為可靠遞送模型

送出工作先寫入 durable inbox；target Agent 以 sequence cursor poll，處理完成後 ACK。
Hub restart 或 client disconnect 時，未 ACK item 保持 pending，下一次 poll 會再次出現。
同一 requester、target 和 idempotency key 只對應一筆 item。這是 at-least-once delivery；
adapter 必須把處理邏輯做成可重入。

替代方案是以 ephemeral push 或 WebSocket 作為必要通道，會要求 Agent 持續在線並增加
connection lifecycle 複雜度，無法直接滿足第一個三-Agent restart acceptance target。

### 5. HTTP API 先固定在 `/hub/v1`

沿用來源 proto 和既有 Hub HTTP path 的 camelCase JSON 形狀，另外以 Lite-specific error
envelope 和 bounded validation 限定行為。正式 API type 會在 Stage 1 contract task 中
整理，不直接把 protobuf runtime 或完整 Connect RPC 帶入 v1。

Lite v1 的 Agent Card version 固定為協定常數 `1`，不接受 declaration 自行覆寫，讓
公開 Card 的語意不會因不可信 Agent metadata 而漂移。

`llms.txt` 使用 embedded Markdown template；部署請求有設定
`A2A888_HUB_PUBLIC_URL` 時使用該 origin，否則以目前 request 的 scheme 和 Host 產生
連結。如此同一個 image 可部署於不同 URL，不需重建內容。

安全操作摘要寫入 SQLite `event_log`，operator 透過 cursor endpoint 調閱。Event log
和 inbox 共用 `/data` volume，但只保存 identity、類型、時間與 bounded safe details；
message payload 和 credential 不會複製到 audit log。

### 6. 驗證在 CI 與遠端環境完成

GitHub Actions 執行 format、static checks、unit/integration tests 和 container checks；
部署／smoke workflow 在 `david@10.9.0.11` 執行。Secrets 只透過 CI 或遠端 secret store
注入，禁止寫入文件、commit 或 command output。

## Risks / Trade-offs

- [SQLite 單一 writer 限制] → 使用 WAL、busy timeout、短 transaction 和明確的單 Hub
  deployment boundary；需要水平擴展時再增加 PostgreSQL store。
- [At-least-once delivery 可能造成 Agent 重複處理] → 保留 task/idempotency identity，
  要求 adapter 以 task ID 或 idempotency key 做可重入處理。
- [Public registration 容易被濫用] → 預設設定上限、payload/rate/concurrency limit、
  operator revoke、TLS/ingress 建議和不接受 executable/runtime metadata。
- [來源完整 Hub 與 Lite contract 可能逐漸分歧] → 以來源 proto、HTTP routes 和 source
  trace 作為 Stage 1 contract review 的輸入，並用 contract tests 固定相容欄位。
- [無法在本機執行 build/test] → 把每個 slice 的驗證命令寫入 CI workflow，並以遠端 smoke
  test 作為 deployment gate。

## Migration Plan

1. 建立新 Go module、schema 和 contract；不改動完整 `888a2a` repository。
2. 在 CI 建立 SQLite test database，執行 schema、registry、mailbox、HTTP 和 restart
   recovery tests。
3. 建立 Docker image，將 `/data` 掛載 persistent volume，再部署到遠端主機。
4. 以三個測試 Agent 完成 register → discover → notify → poll → ACK → restart recovery
   smoke flow。
5. 若部署失敗，停止新 instance，保留 `/data/hub.db`，回復上一個 container image；
   schema migration 必須在啟動前檢查版本，避免不可逆資料破壞。
