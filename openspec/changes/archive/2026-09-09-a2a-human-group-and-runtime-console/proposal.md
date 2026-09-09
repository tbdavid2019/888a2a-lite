## Why

目前 888a2a-lite 的 Local Web UI（`http://localhost:8888`）僅支援 1 對 1 私聊，且本機 AI 引擎（OpenClaw、Claude Code、Hermes、Codex 等）之探測與常駐服務僅能在終端機文字模式下運作；使用者缺乏如 Buzz 般直觀的「視覺化 Agent Runtimes」儀表板與狀態反饋（如 Ready 綠燈或 `CLI needed` 提示）。

此外，在多 Bot 協作群組中，人類使用者需要隨時插話發言的空間。若缺乏明確的指名控制，人類一句發言可能引發群內所有 Bot 爭相搶答或客套客氣，造成嚴重的 Token 浪費與上下文混亂。本變更旨在將本機工作台升級為「人類插話群聊大廳 + 視覺化本機 Runtime 控制中心」，透過 `@` mentions 智慧指名與 `replyPolicy: MENTIONED_ONLY`，達成「人類發話、全員簽收已讀、僅被 @ 的特定 Bot 啟動大腦思考回覆」的自然群聊人機共融體驗。

## What Changes

- **本機 Agent Runtimes 視覺化控制面板**：
  - 在 Local Web UI (`http://localhost:8888`) 新增「Agent Runtimes」專屬管理分頁。
  - 本機自動探測系統 PATH 中已安裝之 AI CLI（OpenClaw, Claude Code, Goose, Hermes, Codex, OpenCode 等），即時顯示就緒狀態徽章（綠燈 Ready 或琥珀色 `CLI needed` 提示）。
  - 支援客製化 Runtime（`+ Add Runtime`），允許使用者自訂指令名稱、環境變數與啟動參數。
  - 提供一鍵切換當前 Bridge 執行引擎，並支援在 UI 上一鍵安裝/重啟系統背景常駐守護程序（macOS LaunchAgent / Linux systemd）。
- **人類插話群聊工作台（Human-in-the-Loop Group Chat）**：
  - Local Web UI 左側導覽列新增「協作群組（Groups）」分頁，支援建立、加入、瀏覽與進入群聊大廳。
  - 支援多 Bot 與人類之共同交談訊息流，訊息依時間序列與寄件者（人類／特定 Bot）清晰標示氣泡與頭像。
- **`@` Mentions 智慧輸入與指名派發**：
  - 群聊輸入框鍵入 `@` 時，自動浮現當前群組在線成員之 Auto-complete 下拉選單。
  - 人類一般發言（無 `@`）：預設附帶 `replyPolicy: ACK_ONLY`（純通知），群內所有 Bot 僅在背景執行 Instant ACK 簽收已讀，保持靜默不消耗 Token 回覆。
  - 人類指名發言（帶 `@BotName`）：前端自動解析 `@` 並提取其 `agentId`，發送時注入 `replyPolicy: MENTIONED_ONLY` 與 `mentions: [targetAgentId]`。
  - 接收端 Bridge 機制保證：未被 @ 的成員秒級 ACK 標記已讀後退出；僅有被 @ 到的 Bot 會被喚醒並調用其 LLM 推理回覆群組。

## Capabilities

### New Capabilities

- `local-runtime-hub`: 本機 Agent Runtime 視覺化掃描、CLI 狀態探測（就緒／`CLI needed`）、客製指令接入與背景常駐服務狀態管理。
- `human-group-interaction`: 本機 Web UI 之人類插話群聊工作台，支援 `@` mention 智慧補全、發送時自動注入 `replyPolicy: MENTIONED_ONLY` 與 `mentions` 清單，實現未被指名之 Bot 純讀取（Instant ACK）、指名 Bot 獨立回覆之群聊體驗。

### Modified Capabilities

- `universal-bridge-distribution`: Local UI (`http://localhost:8888`) 本機端點新增 Runtime 探測 API、群組歷史分頁、群聊 SSE 串流與 mentions 封裝。
- `agent-groups`: 人類客戶端支援以標準 Client 身分在群聊中發話並附帶 mentions 與 replyPolicy。

## Impact

- 影響 `examples/worker/a2a_bridge.py`（LocalUIServer, LocalUIHandler, 前端 CLIENT_HTML / JS）、`bin/a2a.js` 與 `internal/service/a2a_bridge.py`。
- Hub 核心端點與 SQLite 資料庫完全向下相容，沿用既有之 `reply_policy`、`mentions_json` 與群組廣播契約。
- 無需引入大型前端框架（如 React/Vue）或重量級桌面包裝（如 Electron），維持純 Python 標準庫零外部相依極致輕量設計。
- 使用者體驗全面對標 Buzz 視覺化質感，同時保有 888a2a-lite 超低延遲（<50ms Instant ACK）、防回音風暴與 Multi-Circle 隔離優勢。
