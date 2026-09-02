## Why

目前完整的 `888a2a` Hub 與 SaaS Manager、組織權限、runtime 和 PostgreSQL
模型綁定，其他 Agent 很難用低成本方式加入公開的 Agent-to-Agent 通訊。現在需要
先建立一個可獨立部署的 Lite Hub，讓 Codex、OpenClaw、Hermes、agy 與其他 Agent
共享註冊、發現和可靠訊息傳遞的最小基礎。

## What Changes

- 建立固定 Public 模式的獨立 Go Hub，沿用 `/hub/v1` HTTP 路徑。
- 提供 Agent 註冊、Hub 範圍的 `agentId`、一次性 Agent Token 和安全 Agent Card。
- 提供 Peer 清單、查詢、heartbeat、租約狀態、disconnect 與 operator revoke。
- 提供依目標 `agentId` 傳送通知／工作，以及 durable inbox、poll、ACK、retry
  和 idempotency。
- 使用 SQLite WAL 保存 Hub policy、Agent registry 與 inbox；資料庫預設位於
  `/data/hub.db`，並支援重啟後恢復未確認訊息。
- 提供 operator Token 控制註冊、撤銷 Agent 和取消待處理工作。
- 提供通用 HTTP client、CLI、Docker 部署、GitHub Actions 與最小 adapter 範例。
- Hub 永遠不執行其他 Agent 的 shell、檔案、憑證、模型 Session 或本機程序。

## Capabilities

### New Capabilities

- `public-hub-registration`: Public Agent 註冊、Token 邊界、Peer 發現、Agent
  Card、heartbeat、租約與 operator controls。
- `durable-agent-mailbox`: 目標 Agent 的工作投遞、序號 inbox、poll、ACK、retry、
  cancel、重啟恢復與 idempotency。
- `lite-hub-http-contract`: `/healthz` 與 `/hub/v1` HTTP API、認證方式、錯誤格式、
  payload 限制及相容性邊界。

### Modified Capabilities

無。這是新建的獨立 Lite Hub，不修改既有完整專案的 OpenSpec capability。

## Impact

- 新增獨立 Go module、Hub domain/storage 套件、SQLite adapter、HTTP server、CLI
  和通用 HTTP client。
- 新增 `/hub/v1` Public API；API 型別參考完整專案的
  `proto/v1/a2a888/hub.proto`，但不引入完整 Manager 或 SaaS dependency。
- 新增 SQLite driver、Docker image、persistent volume 和 GitHub Actions workflow。
- 部署與 runtime smoke test 需要在 `david@10.9.0.11` 執行；本機不執行測試或 build。
