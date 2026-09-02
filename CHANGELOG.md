# Changelog

## 2026-09-02

### Added

- 獨立 Go module `github.com/tbdavid2019/888a2a-lite`。
- 專案目錄結構：`cmd/`、`internal/hub`、`internal/store/sqlite`、`internal/config`、`sdk/httpclient`。
- `PLAN.md`、`AGENTS.md`、`SOURCE-TRACE.md` 規劃文件。
- OpenSpec specs 與 changes 目錄。
- 統一以 `bootstrap-lite-hub` 作為唯一 Lite Hub 施工 change，並固定 Public-only
  認證、durable mailbox 狀態與遠端 smoke 驗證邊界。
- 授權改為 GNU Affero General Public License version 3 或更新版本。

### Changed

- 完成 SQLite WAL persistence、Public `/hub/v1` HTTP server、通用 HTTP client、CLI、
  Docker Compose 和三種 adapter 文件範例的第一版實作。
- 在 `david@10.9.0.11` 完成三-Agent註冊、Peer discovery、通知、ACK、重啟恢復、revoke
  和 registration control smoke verification。
- 新增 Docker Hub publish workflow、image-based Compose 設定與 Watchtower label-only
  更新設定。
- 在 `10.9.0.11` 啟用 scope `888a2a-lite` 的 Watchtower，並指定 Docker API version
  `1.40` 以相容該主機 daemon。
- 將 GitHub Actions 的 golangci-lint 更新至 Go 1.25 相容版本 `v2.13.2`。
- GitHub Actions CI 與 Docker Hub publish 已通過，並完成 `10.9.0.11` 的 image-based
  Lite Hub 部署。
- 新增符合 llmstxt.org 格式的 `/llms.txt`，並提供 Agent 安裝、註冊與安全邊界入口。
- `/llms.txt` 改為依 `A2A888_HUB_PUBLIC_URL` 或目前 request origin 動態產生部署連結。
- 新增持久化 `event_log` 與 operator-only `GET /hub/v1/admin/events`，供日後調閱 Hub
  操作摘要。
- README 新增 Docker image／Compose 安裝方式，並將 Docker Hub publish 改為
  `linux/amd64` 與 `linux/arm64` 雙平台 image。
