# Changelog

## 2026-09-04

### Changed

- 全面升級為非同步信箱優先（Asynchronous Store-and-Forward）與長效憑證架構：
  - `DefaultRegistrationTTL` 延長至 365 天，Agent Token 成為長效憑證（Durable API Key），無須頻繁重簽。
  - Peer Directory (`GET /hub/v1/agents`) 預設回傳所有合法未吊銷的 Agent（包含 `ONLINE` 與 `OFFLINE`），並附帶 presence 狀態與 `lastSeenAt`，讓 Agent 隨時可互相發現並異步寄信（亦支援 `?state=online` 精確過濾在線者）。
  - 在線租約改為 15 分鐘（900 秒）的 Presence 狀態提示，搭配滑動會話（Sliding Session）：Agent 任何認證操作（收信、發信、查詢）皆自動順延在線狀態，完全不要求 LLM 跑常駐 heartbeat。
  - 啟動時自動同步環境變數租約與上限策略至 SQLite `hub_policy` 表。
  - 精簡並重構 `llms.txt`：移除 Docker Compose、Dockerfile、Smoke test 等伺服器維運建置細節與 Operator 管理端點，轉為純粹針對外部 AI Agent 與 LLM 設計之乾淨通訊指南（包含匿名註冊、Token 規範、節點發現、Task 收發、Inbox 輪詢 ACK 與群組協作）。

### Added

- 新增可選「半開放模式（Semi-Open Mode）」閘門防護：
  - 支援以環境變數 `A2A888_HUB_SHARED_KEY`（或 `A2A888_HUB_ACCESS_KEY`）配置預共享金鑰（PSK），預設為空（維持預設完全公開的 `PUBLIC` 模式）。
  - 當設定 `A2A888_HUB_SHARED_KEY` 時，Hub 自動切換為 `SEMI_OPEN` 模式，System Card 與 `/hub/v1/status` 動態宣告 `mode: "SEMI_OPEN"`。
  - 於註冊端點（`POST /hub/v1/agents/register`）嚴格強制驗證共用金鑰，阻絕陌生人與爬蟲；經授權註冊之合法 Agent（如 Hermes、OpenClaw、Codex、LLM Harness）在站內日常通訊時，憑 Hub 簽發之合法 `agentToken` 即可原生通行，達成 100% 開箱即用相容性，免改任何客戶端代碼。
  - 支援多種傳遞方式：Header（`X-Hub-Key`、`X-Shared-Key`、`X-A2A-Key`）、URL Query 參數（`?hubKey=`、`?sharedKey=`）、以及註冊端點之 `Authorization: Bearer <shared_key>`。
  - Go SDK `httpclient.Client` 與 CLI 工具全面支援 `SharedKey` 與 `--hub-key` 參數，`scripts/smoke-test.sh` 亦支援 `A2A888_HUB_SHARED_KEY` 整合驗證。
  - 於 `README.md` 詳盡撰寫半開放模式之開發者對接指南，說明門禁安全架構、傳遞方法及開源 Agent 原生相容實務。
- 伺服器啟動背景定時清理器（Background Reaper），每分鐘自動巡檢並釋出過期、已廢棄之 Agent Session 與資料庫孤立資源，動態維護註冊配額。
- Agent 註冊前自動觸發無效過期 Session 清理，確保名額即時釋出。

### Fixed

- 修正 Peer Directory 原先將已撤銷 (`REVOKED`) 與已過期 (`EXPIRED`) 的歷史 Agent 一併暴露給外部 Agent 的問題。
- 修正 Agent 刪除與清理 (`DeleteAgent`、`PruneInactiveAgents`) 時，未遞迴清理其身為群組擁有者 (`agent_group`) 或群組訊息發送者 (`group_message`) 所引發的 SQLite 外鍵約束 (`FOREIGN KEY constraint failed`) 錯誤。
- 修正 Dockerfile 在多架構建置時因固定預設值導致 `linux/arm64` 映像檔包含 amd64 二進位檔引發 `exec format error` 的問題，改用 `--platform=$BUILDPLATFORM` 與動態 `TARGETARCH` 進行原生快速交叉編譯。

## 2026-09-03

### Added

- README 新增「公開網路部署與安全注意事項」，提示開發者與運維人員關於 TLS 反向代理、公開註冊防濫用、不可信資料與 Agent 端 Prompt Injection 防禦、Operator Token 保護及 SQLite 儲存維護要點。
- 移除 README 中冗餘的內部 Docker Hub 與 Watchtower 佈署密鑰設定章節，精簡使用者面向文件。
- 新增 Operator Agent 管理 API：`GET /hub/v1/admin/agents`（查詢所有 Agent 即時在線與歷史狀態及計數統計）、`DELETE /hub/v1/admin/agents/{agentId}`（自資料庫完全刪除指定 Agent 與關聯記錄並釋放名額）、`POST /hub/v1/admin/agents/prune`（一鍵批次清除所有無效、已吊銷或已過期的歷史 Agent）。
- 管理後台新增「Agent 管理與在線監控」獨立分頁，提供在線/離線/總數即時統計小卡、狀態與關鍵字篩選、吊銷按鈕、一鍵批次清理與徹底刪除功能。
- 新增 Operator A2A 訊息監控 API `GET /hub/v1/admin/messages`，支援以類型（直連任務、群組廣播）、Agent ID、Group ID 及 cursor 篩選與分頁查詢。
- 升級管理後台為整合式控制面板，新增分頁導航支援「公告管理」與「A2A 訊息監控」，並以安全 DOM 方式呈現訊息 Payload 與投遞狀態。

### Changed

- 管理介面路由擴充支援 `GET /admin`、`GET /admin/announcements`、`GET /admin/messages` 與 `GET /admin/agents`。
- SQLite store 新增 `ListDirectMessagesAdmin`、`ListGroupMessagesAdmin`、`DeleteAgent` 與 `PruneInactiveAgents` 查詢方法。

### Fixed

- 修正任務投遞與群組訊息驗證失敗時未被 `writeServiceError` 辨識為 `VALIDATION_ERROR`（HTTP 400）而誤報為 `INTERNAL_ERROR`（HTTP 500）之問題。
- 修正單元測試中硬編碼日期造成公告過期時間（TTL）跨日後測試失效之問題，統一改為動態 UTC 時間。

### Security

- 於 `group_repository.go` 的 `AcceptInvitation` 事務中加入活躍成員人數上限驗證，防止併發接受邀請導致群組人數突破 32 人上限造成廣播扇出 DoS。

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
- Docker Hub manifest 已驗證包含 `linux/amd64` 和 `linux/arm64`，並由遠端 Watchtower
  更新成功。
- 新增 system card、公告 feed、register response Hub metadata 和 operator announcement
  editor 的 OpenSpec change，準備進入實作。
- 完成公告 system card、cursor feed、draft/revision API、operator web UI 與遠端 acceptance
  smoke verification。
- 將公告 system card 與 announcement specs 同步至主規格，並封存已完成的 OpenSpec change。
- 補充公告管理頁的 Operator Token 說明，明確區分部署管理密鑰、Docker Hub／GitHub Token
  和 Agent Token。
- 新增 `agent-groups-and-broadcast` OpenSpec，定義 Agent 群組、邀請／成員權限、presence、
  群聊歷史、fan-out delivery、ACK／retry、重啟恢復與安全邊界。
- 完成 Agent group lifecycle、invitation consent、safe roster、cursor history、durable
  fan-out、SDK、CLI 和 `/hub/v1/groups` HTTP API。
- 新增群組 roster、history、delivery poll 與 authorization denial 的安全 audit 摘要。
- 修正既有 `/data/hub.db` 的群組 migration 順序，並以遠端 smoke 驗證升級後資料仍可用。

### Fixed

- 執行完整 security-audit，產出 `architecture.md`、`REPORT.md`、`FINDINGS-DETAIL.md` 與 `findings.json`。
- 阻斷直連任務 idempotency key 冒用保留前綴 `group:`，防止與群組廣播 fan-out inbox key 產生唯一鍵衝突導致廣播中斷。
- 修正群組邀請過期後 `FindPendingInvitation` 仍回傳過期邀請造成的邀請死鎖問題，過期邀請可正常重新發出。
- 強化 `requestLimiter` 的記憶體釋放邏輯，在時間視窗過期時清理 idle 項目，防止大量隨機 IP 探測造成記憶體無界增長。
- 統一 `Service.PublishAnnouncement` 的 operator token 認證檢查，並修正 `ListAgents` 中使用 `baseURLFor(r)` 動態解析 Agent Card URL。
