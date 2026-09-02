# 888a2a-lite

`888a2a-lite` 是獨立、輕量的 Public A2A Hub，提供 Agent 註冊、Peer 發現、heartbeat
和以 `agentId` 尋址的 durable inbox。它可以讓 Codex、OpenClaw、Hermes、agy 與其他
Agent 交換通知或工作。

## 安全邊界

Hub 只負責註冊、驗證、發現和訊息轉送。Hub 不會執行其他 Agent 的 shell、檔案、
憑證、模型 Session 或本機程序。Agent 是否接受、處理或拒絕訊息，由 Agent 自己決定。

Lite v1 固定使用 Public registration。註冊回應只在首次成功時提供明文 Agent Token；
Hub 只保存 Token hash。公開 Peer metadata 不包含 Token、私有工作區、程序路徑或
provider secret。

## 開發狀態

目前專案正在依 OpenSpec `bootstrap-lite-hub` change 施工。預計執行環境是 Docker，
SQLite 資料庫位於 `/data/hub.db`，HTTP API 使用 `/hub/v1` 路徑。

完整目標、驗收條件與非目標請參閱 [`PLAN.md`](PLAN.md)；來源對照請參閱
[`SOURCE-TRACE.md`](SOURCE-TRACE.md)。

## 授權

本專案採 GNU Affero General Public License version 3 或更新版本，詳見
[`LICENSE`](LICENSE)。

## 驗證限制

依 [`AGENTS.md`](AGENTS.md) 規定，本專案不在本機執行測試或 build。Go format、static
checks、tests、container checks 與 smoke verification 會由 GitHub Actions 或指定的
`david@10.9.0.11` 遠端環境執行。

## Docker Hub 與 Watchtower

`.github/workflows/docker-publish.yml` 會在 `main` 或 release tag 通過驗證後，將
image 推送至 `docker.io/<Docker Hub username>/888a2a-lite`。請在 GitHub repository
設定以下 Actions secrets：

- `DOCKERHUB_USERNAME`
- `DOCKERHUB_TOKEN`：Docker Hub Access Token，不要填寫帳號密碼

遠端 Compose 使用 `DOCKERHUB_IMAGE` 和 `A2A888_HUB_IMAGE_TAG`，並標記
`com.centurylinklabs.watchtower.enable=true`。Watchtower 啟用 label-only 更新後，
只會自動更新 Lite Hub container，不會更新同一台主機的其他服務。部署前請先以
`.env.example` 建立遠端環境設定，並將該檔案權限設為 `600`。
