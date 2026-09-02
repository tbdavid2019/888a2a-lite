# 888a2a-lite 規劃

> 日期：2026-09-02
> 狀態：規劃完成，尚未施工

原始 Hub 來源與 extraction boundary 請見 [`SOURCE-TRACE.md`](SOURCE-TRACE.md)。
後續 LLM 工作規則與驗證限制請見 [`AGENTS.md`](AGENTS.md)。

## 1. 目標

建立一個獨立、輕量的 Public A2A Hub，讓 Codex、OpenClaw、Hermes、agy
以及其他 Agent 可以透過同一個 Hub：

- 註冊並取得 Hub 範圍內唯一的 `agentId`。
- 查詢其他已註冊 Agent 與安全的 Agent Card。
- 以目標 `agentId` 發送通知或工作。
- 透過 heartbeat 表示在線狀態。
- 從 inbox 讀取訊息並使用 ACK 確認。
- 在斷線或重試時維持 idempotent，不重複建立工作。

Lite Hub 只負責註冊、發現、驗證與轉送，不執行其他 Agent 的本機程序、
工作區、憑證或模型 Session。

## 2. 與完整 888a2a 的邊界

完整 `888a2a` 保留：

- Human workspace 與聊天介面。
- Organization、IAM、審批、用量與 SaaS 功能。
- 本機 Agent runtime、Machine supervisor 與 provider 管理。
- 完整的 PostgreSQL 業務資料模型。

`888a2a-lite` 只搬移 Hub 的通訊核心，不複製完整 Manager 或前端。

```text
888a2a                         888a2a-lite
完整 SaaS 控制平面              輕量 Public A2A Hub
組織 / IAM / 聊天 / runtime      註冊 / 發現 / inbox / 轉送
PostgreSQL                      SQLite
大型後台                        CLI、API、健康檢查
```

## 3. 資料庫決策：SQLite

### 選擇

第一版使用 SQLite，採 WAL、foreign key、busy timeout 與 transaction。
使用純 Go SQLite driver，避免 Docker runtime 需要 CGO。

### 原因

Hub 的主要負載是交易型工作：註冊、heartbeat、inbox 寫入、ACK、重試與
idempotency 唯一約束。SQLite 對單一 Hub instance 的這類工作較簡單、可靠，
也方便用一個 `/data/hub.db` 備份或搬移。

DuckDB 保留作為未來事件分析選項。它適合批次查詢、統計與報表，不作為
第一版即時 mailbox 的主要資料庫。

### 擴充邊界

所有資料庫操作透過 `Store` interface，避免 HTTP、Registry 直接依賴 SQLite。
未來若需要多個 Hub instance，可以增加 PostgreSQL store，而不改變 A2A API。

### 預計資料表

- `hub_policy`：Hub ID、模式、註冊開關、配額與時間限制。
- `agent`：Agent ID、Token hash、註冊宣告、狀態、租約與過期時間。
- `inbox_item`：序號、發送者、接收者、訊息、idempotency key、ACK 狀態。

必要約束：

- `agent_id` 唯一。
- Token 只儲存 hash，不儲存明文。
- `(target_agent_id, requester_agent_id, idempotency_key)` 唯一。
- Inbox sequence 在單一 Hub 內單調遞增。
- 所有跨表操作使用 transaction。

## 4. Lite v1 功能範圍

### 4.1 Public 註冊

Lite v1 固定使用 Public 模式：知道 Hub URL 的 Agent 可以註冊。

註冊回應一次性提供：

- `hubId`
- `agentId`
- `agentToken`
- `expiresAt`

註冊宣告只允許安全中繼資料，例如名稱、provider family、transport 與
capabilities。不得接受 executable path、API key、工作區路徑或模型憑證。

### 4.2 Peer 發現

已註冊 Agent 可以取得安全的 Peer 清單，內容包括：

- `agentId`
- 顯示名稱
- provider family
- transport ID
- capabilities
- ONLINE / OFFLINE / EXPIRED / REVOKED 狀態
- Agent Card URL

不回傳 Token、Token hash、私有工作區、程序路徑或 provider secret。

### 4.3 Direct notify / task

第一版支援指定目標 Agent：

```text
sender agentId ──> target agentId ──> durable inbox ──> poll ──> ACK
```

訊息至少包含：

- `targetAgentId`
- `contextId`
- `idempotencyKey`
- `message`
- `taskId`

同一發送者對同一目標使用相同 idempotency key 時，Hub 回傳既有工作，
不得建立重複 inbox item。

### 4.4 Heartbeat 與租約

- Agent 註冊 TTL 預設 24 小時。
- Peer lease 預設 90 秒。
- 超過 lease 沒有 heartbeat 時標記 OFFLINE。
- 超過註冊 TTL 時標記 EXPIRED。
- Agent 必須重新註冊才能取得新的有效租約。

這些限制都可透過環境變數或啟動參數調整，但必須有上限。

### 4.5 管理操作

Lite 不做完整 SaaS 後台，管理操作使用 operator Token：

- 停用或重新啟用註冊。
- 撤銷 Agent。
- 取消待處理工作。
- 查詢健康狀態與基本統計。

## 5. HTTP API 相容性

沿用完整版本目前的 `/hub/v1` 路徑，讓未來 Agent adapter 可以共用：

```text
GET  /healthz
GET  /hub/v1/status
POST /hub/v1/agents/register
GET  /hub/v1/agents
GET  /hub/v1/agents/{agentId}
GET  /hub/v1/agents/{agentId}/agent-card.json
POST /hub/v1/agents/{agentId}/heartbeat
POST /hub/v1/agents/{agentId}/disconnect
POST /hub/v1/agents/{targetAgentId}/tasks
GET  /hub/v1/agents/{agentId}/inbox?afterSequence=0
POST /hub/v1/agents/{agentId}/inbox/{sequence}/ack
POST /hub/v1/admin/registration
POST /hub/v1/admin/agents/{agentId}/revoke
POST /hub/v1/admin/tasks/{taskId}/cancel
```

Lite v1 不提供完整 SaaS 的 Connect RPC、組織 API、聊天 API 或 runtime
執行 API。

## 6. Agent adapter 計畫

Hub 本身不會自動發現本機的 Agent 程序。每種 Agent 都需要一個 client
adapter，負責把本機事件接到 `/hub/v1`。

### 共用 adapter contract

- `register()`：使用穩定 installation key 做 idempotent 註冊。
- `heartbeat()`：週期性更新 lease。
- `listPeers()`：取得 Peer 清單。
- `notify(agentId, message)`：指定 Agent 發送通知。
- `poll()`：讀取自己的 inbox。
- `ack(sequence)`：確認處理完成。
- `reconnect()`：保留 installation key，斷線後恢復原身分或重新註冊。

### 實作順序

1. 通用 HTTP／JSON client 與 CLI。
2. Codex adapter。
3. OpenClaw adapter。
4. Hermes adapter。
5. agy 與其他 Agent 的設定檔 adapter。

Adapter 預設只處理通知與工作訊息，不授予 Hub 遠端執行本機 shell、檔案或
模型的權限。各 Agent 自己決定是否接受、執行或拒絕訊息。

## 7. 專案目錄

```text
888a2a-lite/
├── PLAN.md
├── README.md
├── go.mod
├── cmd/
│   └── 888a2a-lite/
├── internal/
│   ├── hub/
│   ├── store/
│   │   └── sqlite/
│   └── config/
├── sdk/
│   └── httpclient/
├── Dockerfile
├── docker-compose.yml
└── .github/workflows/ci.yml
```

預計 Docker runtime：

- binary：`/usr/local/bin/888a2a-lite`
- database：`/data/hub.db`
- volume：`lite-data:/data`
- health endpoint：`/healthz`

## 8. 實作階段

### Stage 0：專案與規格

- 建立獨立 Go module。
- 建立 OpenSpec change、README、LICENSE 與 CHANGELOG。
- 固定 Public Hub 的 API、錯誤格式與 token 邊界。

### Stage 1：核心 contract

- 搬移並整理 Agent declaration、identity、status、Agent Card 型別。
- 搬移 Registry、heartbeat、租約與 revoke 邏輯。
- 搬移 mailbox、sequence、ACK、retry 與 idempotency contract。

### Stage 2：SQLite store

- 建立 schema migration／bootstrap。
- 實作 agent persistence。
- 實作 inbox persistence。
- 實作 transaction、unique constraint 與 restart recovery。

### Stage 3：Lite HTTP server

- 實作 Public register、status、list、lookup、Agent Card。
- 實作 heartbeat、disconnect、send task、poll、ACK。
- 實作 operator revoke、cancel 與 registration control。
- 加入 request body、payload、rate limit 與錯誤回應限制。

### Stage 4：CLI 與 adapter

- `888a2a-lite register`
- `888a2a-lite peers`
- `888a2a-lite notify --to <agentId>`
- `888a2a-lite inbox`
- `888a2a-lite ack`
- 建立通用 HTTP client。
- 建立 Codex、OpenClaw、Hermes adapter 的最小接入範例。

### Stage 5：Docker 與遠端驗證

- 建立單一 binary Docker image。
- 建立 SQLite `/data` persistent volume。
- GitHub Actions 執行 Go lint、test、build、Docker build。
- 在 `10.9.0.11` 部署。
- 用至少三個 fake Agent 驗證註冊、發現、通知、heartbeat、ACK、重試與重啟。
- 再以實際可用的 Codex、OpenClaw、Hermes adapter 驗證互通。

## 9. 驗收條件

- 新 Hub 不需要 Go 或 Node.js，Docker 可直接啟動。
- 三個 Agent 可同時註冊並取得不同 `agentId`。
- Agent 可查詢彼此的安全資料。
- Agent A 可使用 Agent B 的 ID 發送通知。
- Agent B 斷線後，訊息會保留在 inbox。
- Agent B 重連後可以 poll 並 ACK 未完成訊息。
- 相同 idempotency key 不會產生重複工作。
- Hub 重啟後 Agent 與未 ACK inbox 仍存在。
- Token 不會出現在 list、lookup、Agent Card 或 log。
- 被撤銷的 Agent 無法 heartbeat、poll 或發送工作。
- Public 註冊有 rate limit、payload 上限與註冊數上限。
- GitHub Actions 全部通過。
- `10.9.0.11` Docker 部署與三 Agent A2A smoke test 通過。

## 10. 明確不做

- Hub 代替 Agent 執行 Codex、OpenClaw、Hermes。
- Hub 直接存取 Agent 的 shell、檔案、API key 或工作區。
- 完整聊天 UI。
- Organization、IAM、審批與計費。
- 第一版多 Hub federation。
- 無限制的全域 broadcast。

## 11. 風險與處理

| 風險 | 處理方式 |
|---|---|
| Public Hub 被大量註冊 | IP rate limit、註冊數上限、TTL、operator revoke |
| Agent Token 外洩 | 只存 hash、只回傳一次、支援 revoke |
| SQLite 寫入競爭 | WAL、busy timeout、短 transaction、單一 Hub instance |
| 重複通知 | requester／target／idempotency key 唯一約束 |
| Agent 假冒能力 | capabilities 只作宣告，不代表 Hub 授權執行 |
| DuckDB 與即時交易不匹配 | DuckDB 僅保留給事件分析，不放入 v1 mailbox path |
| Hub 與 Agent adapter 版本漂移 | 沿用 `/hub/v1`、錯誤格式與 contract test |

## 12. 建議的第一個可交付版本

第一個可交付版本只包含：

```text
Public Hub + SQLite + Docker
        │
        ├── generic HTTP client
        ├── register / peers / notify / inbox / ACK
        └── Codex / OpenClaw / Hermes integration examples
```

先讓三個實際 Agent 能互相看見並互傳通知，再逐步增加 broadcast、事件分析
與更完整的 adapter。這樣能直接驗證最初的核心期待，不會再次被完整 SaaS
功能拖慢。
