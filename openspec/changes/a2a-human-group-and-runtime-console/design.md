## Context

目前 888a2a-lite 的客戶端核心由 `examples/worker/a2a_bridge.py` 驅動，透過純 Python 標準庫啟動 `LocalUIServer`（`http://localhost:8888`）。既有介面僅具備點對點 Peer 聊天功能。

在協作層面，第二階段規格與 SQLite 儲存已為 Hub 和 Bridge 奠定了 `reply_policy`（`ALL`、`MENTIONED_ONLY`、`ACK_ONLY`）與 `mentions_json` 欄位。本設計針對使用者體驗進行最後一哩路突破：提供如 Buzz 般絲滑之「本機 Agent Runtimes」視覺化監控面板，並在 Web UI 開放群聊大廳，支援人類隨時插話與 `@` 智慧指名。

## Goals / Non-Goals

**Goals:**
- **本機 Runtime 視覺化面板**：於 `http://localhost:8888` 列出本機支援之後端（OpenClaw, Claude Code, Goose, Hermes, Codex, OpenCode 等），展示就緒標籤（`Ready` 或 `CLI needed`），並支援自訂 Command 擴充。
- **背景常駐服務一鍵管理**：在 UI 上直接查看 macOS LaunchAgent 或 Linux systemd 常駐狀態，支援一鍵安裝與重啟。
- **人機共融群聊大廳**：在 UI 導覽列新增協作群組分頁，呈現多 Agent 與人類的交談時間軸。
- **`@` Mentions 智慧補全與防回音政策**：
  - 輸入 `@` 即時彈出群成員候選單。
  - 無 `@` 發言自動套用 `replyPolicy: ACK_ONLY`（全員已讀靜默）。
  - 帶 `@` 發言自動套用 `replyPolicy: MENTIONED_ONLY`，精準喚醒被指名之 Bot，其他 Bot 僅秒級 Instant ACK 簽收。
- **極致輕量零依賴**：維持純 Python 標準庫（http.server + sqlite3 + urllib）與單檔內嵌現代化 CSS/JS，不引入任何 npm 打包或 Electron 臃腫外殼。

**Non-Goals:**
- 不打包肥大的 Electron 桌面安裝檔（維持標準瀏覽器開啟，資源佔用 <30MB）。
- 不修改 Hub 伺服器端核心合約（完全沿用既有群組與 Mailbox 欄位）。
- 不在前端引入肥大之外掛框架（如 React/Vue/Tailwind CDN），避免離線或內網環境無法載入。

## Decisions

### 1. 純原生現代化 Web 介面 (Vanilla ES6 + CSS Variables)
- **選擇**：以原生 HTML5/ES6 模組化撰寫前端元件，直接內嵌於 `CLIENT_HTML`。
- **考量與權衡**：相較於引入 React 或 Vue 編譯鏈，原生方案讓單一 Python 檔案即可獨立運行於任何 Linux/macOS/Windows 機器，冷啟動耗時 <100ms，無任何版本衝突問題。

### 2. 端點分離：本機 Runtime 檢測 API (`GET /api/runtimes`)
- **選擇**：在 `LocalUIHandler` 新增 `/api/runtimes` 端點，每次呼叫時以 `shutil.which` 探測系統環境：
  - `openclaw` (OpenClaw)
  - `claude` (Claude Code)
  - `goose` (Goose)
  - `hermes` (Hermes Agent)
  - `codex` (Codex CLI)
  - `opencode` (OpenCode)
- **回傳結構**：
  ```json
  {
    "active": "openclaw",
    "runtimes": [
      {"id": "openclaw", "name": "OpenClaw", "status": "ready", "path": "/usr/local/bin/openclaw"},
      {"id": "claudecode", "name": "Claude Code", "status": "cli_needed", "path": null},
      {"id": "hermes", "name": "Hermes Agent", "status": "ready", "path": "/home/david/.hermes/bin/hermes"}
    ]
  }
  ```

### 3. 輸入框 `@` Mentions 本地解析與雙軌政策
- **選擇**：在瀏覽器端掛載 `input` 事件監聽。當游標遇到 `@` 字符時，動態渲染 Floating Member Picker。
- **發送政策決定**：
  - **一般公告／備忘（無 `@`）**：`payload = {message: text, replyPolicy: "ACK_ONLY", mentions: []}`。群組所有 Bot 在 <50ms 內簽收 Instant ACK（已讀），隨後終止，絕不在群內產生廢話回信。
  - **精準提問（帶 `@BotName`）**：前端將 `@BotName` 替換為高亮標記，並解析出其對應之 `agentId`：`payload = {message: text, replyPolicy: "MENTIONED_ONLY", mentions: [targetAgentId]}`。
- **防回音保證**：未在 `mentions` 中的 Bot 收到 SSE 推播後，判定 `hub_client.agent_id not in mentions`，立刻 Instant ACK 並退出；唯有被 @ 的 Bot 喚醒本地 LLM 推理並發送回覆。

### 4. 本地群聊歷史與 SQLite WAL 儲存擴充
- **選擇**：`~/.a2a/chat.db` 擴充 `group_messages` 表，保存 `group_id`、`sender_id`、`sender_name`、`mentions_json` 與 `reply_policy`，斷線重連透過 `afterId` cursor 自動增量補齊。

## Risks / Trade-offs

- **[Risk] Bot 顯示名稱重複導致 @ 誤認**  
  → **Mitigation**：Autocomplete 候選單中同時展示 `DisplayName` 與末 6 碼 `Agent ID`；選中後內部儲存綁定唯一 `Agent ID`。
- **[Risk] 長時間未關閉之網頁 SSE 中斷**  
  → **Mitigation**：前端加入指數退避重連機制（1s, 2s, 4s, 8s），重連後自動比對最新訊息序號補發缺失段落。
- **[Risk] 自訂 Runtime 指令注入風險**  
  → **Mitigation**：自訂指令只允許單一執行檔路徑與陣列參數，禁止 shell 管道（`|`）或任意語句串接。
