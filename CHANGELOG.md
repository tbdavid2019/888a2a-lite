# Changelog

## 2026-09-08

### Fixed

- 序列化 Multi-Circle 註冊與 Operator 圈圈生命週期操作，避免停用圈圈與新 Agent 註冊交錯後留下未撤銷的 Agent。
- 圈圈金鑰輪換改以目前已持久化的最大版本號產生下一版，避免 migration 或人工修復造成版本缺口時撞號。
- Python Bridge/UI 的預設憑證與本機交談資料庫改按 Hub／圈圈／Agent 身分分隔，避免換圈時誤用舊圈 Token 或混合歷史對話。
- 同步 `skills/a2a-client/SKILL.md` 與使用者全域 Client Skill 的換圈／退出 SOP，並在註冊回應範例補上 `circleId`。

### Added

- Multi-Circle 動態圈圈引導與 AI Agent 智慧詢問機制：
  - 更新 `internal/service/http.go` 之 `/llms.txt` 動態渲染邏輯，依據 `AllowDynamicCircles` 即時輸出動態圈圈啟用狀態與進圈指引。
  - `internal/service/service.go` 於 `GET /hub/v1/status` 結構新增 `allowDynamicCircles` 屬性，供客戶端程式化偵測。
  - `llms.txt`、`skills/a2a-client/SKILL.md` 及全域 Agent Skill 全面補強 AI Agent 連線規範：在 Multi-Circle 模式下主動詢問使用者是否提供私有團隊通關密鑰進入隔離新天地。
  - `.env.example` 增補 Multi-Circle 兩種策略（免改 .env 隨選即用動態新天地 vs 預設固定白名單新天地）生動比喻、天神 Operator Token 權限定位，以及 `A2A888_HUB_CIRCLE_DERIVATION_SECRET` 三大關鍵作用（重啟一致性、防彩虹表破解、跨 Hub 隔離）。

### Fixed

- 修復 SQLite repository 中 `agent` 與 `inbox_item` 新增語句因引入 `circle_id` 後 VALUES 佔位符（`?`）數量短缺導致的 SQL 語法錯誤（`17 values for 18 columns`）。
- 修復 SQLite circle repository 中 nullable time pointer 轉換型別錯誤（改用 `parseNullableTimePtr` 解析 `DisabledAt`、`GraceUntil` 與 `RevokedAt` 指標欄位）。

## 2026-09-07

### Fixed

- 完成 `a2a_bridge.py` 的 single／multi circle 相容性驗證：保留 legacy single mode，multi mode 可用 share key 註冊並以 Agent Token 使用普通 API。
- 補上 legacy SQLite Agent registration constraint migration test，確認既有資料進入 `public` 並允許相同 registration key 在不同 circle 建立獨立身份。
- 補上 multi-circle Hub restart persistence test，驗證 private circle、Agent Token 與 circleId 在 SQLite 重啟後保持一致。
- 補上 multi-circle public/private isolation、同 key registration scope、Agent Card、Task、Inbox、SSE、Group、status、admin filter 與 key rotation 的 Go integration tests。
- Multi-circle authorization now uses an explicit `AgentPrincipal` carrying `hubId`, `agentId`, and persisted `circleId` through core service checks.
- 施工 multi-circle 第二階段：完成 circle key digest 持久化查找、Operator circle disable／key rotation／key revoke、circle-scoped status 與 admin filter 基礎。
- 開始施工 `strict-air-gapped-multi-circle`：新增 `single|multi` circle 設定、HMAC circle resolver、SQLite circle／key lifecycle 表，以及 Agent／Inbox／Group／Event 的 circle scope 欄位與基礎跨圈授權。
- 補上 Hub 指派不同 task ID 時的 Outbox 調和回歸測試，驗證 API 回應、本機 SQLite 記錄與 SENT 狀態維持一致。
- 修正 Outbox 投遞完成後與 Hub 指派 taskId 對齊調和機制（Reconcile Hub-assigned taskId）：當 Hub 回傳之正式 taskId 與本地暫存 ID 不一致時，自動更新本機 SQLite 記錄並推播 previousId 狀態變更事件，同時維持 API 傳回值與單元測試之精準對齊。
- 調整本機 Outbox 背景 Worker：改用 per-task lock，避免單一慢速 Hub 投遞阻塞其他訊息；補上單筆投遞與 Worker loop 例外隔離，並保留可退避重試的訊息狀態。
- 補強 Outbox 訊息的 `SENT` 終態保護，重複保存或重試不得把已送達訊息退回 `PENDING`／`SENDING`。
- 補上前端 `SENDING` 狀態顯示與 Worker、終態、Hub 固定 idempotency payload 測試。
- 強化本機交談工作台 Outbox：新增 PENDING／SENDING／FAILED 狀態、背景退避重試與程序重啟後的未完成投遞恢復；固定使用本機 task ID 進行 Hub 冪等重送。
- 修復交談重試按鈕將未驗證 task ID 插入 inline JavaScript 的 XSS 風險，改用安全的事件監聽器綁定。
- 修復 conversation summary 以字串比較不同時區 timestamp 的排序錯誤，改用 UTC 數值排序並補上既有資料 migration。
- 補強本機 History API 的非有限時間游標、request body 大小與格式錯誤處理。
- 實作完整本機 Outbox 模式與冪等重試（Idempotent Outbox Delivery & Retry）：`/api/send` 先以確定性 `taskId` 寫入本機 SQLite `PENDING` 狀態，再發送至 Hub；逾時或投遞失敗時標記為 `FAILED`；使用者點選「↻ 重試」時沿用原始固定 `taskId` 與 `idempotencyKey`，確保 Hub 憑內建冪等機制絕不重複建立任務。
- 修復舊訊息重推污染會話摘要問題（Conversation Summary Regression Guard）：`conversations` 更新時以 `excluded.last_timestamp >= COALESCE(conversations.last_timestamp, '')` 為保護條件，舊訊息重推絕不倒退覆蓋最新訊息摘要與時間戳。
- 補強 History API 邊界驗證（Strict Bounded Validation）：`/api/history` 嚴格限制 `limit`（1..500）、`offset`（0..100000）、非負 `before` 游標，並於參數缺漏或非法格式時標準回傳 HTTP 400。
- 修復 `a2a ui` 前端未從 SQLite 載入歷史紀錄之缺陷（History Hydration）：於 `selectPeer` 與頁面初始化時非同步請求 `/api/history` 水合對話紀錄，瀏覽器重新整理後歷史對話完好如初；通訊錄側邊欄支援即時顯示最近訊息摘要。
- 修正出站投遞失敗卻回報成功之假象（Outbound Delivery Failure Guard）：修正 `/api/send` 於 Hub 投遞失敗時仍偽造 UUID 回傳 HTTP 200 之問題；改為在失敗時明確回傳 HTTP 502、於 SQLite 標記 `state="FAILED"`，前端畫面顯示傳送失敗狀態。
- 消除 SSE 重複事件改變訊息順序之缺陷（Conflict Order Preservation）：將 `INSERT OR REPLACE` 改為 `ON CONFLICT(id) DO UPDATE`，於重試推播時完整保留原始 `created_at`，確保訊息在對話流中的排序永不跳動錯位。
- 實作完整會話清單與歷史分頁查詢：新增 `/api/conversations` 端點及 `/api/history` 之 `limit`、`offset`、`before` 參數支援，並增補單元測試覆蓋重推保序與分頁查詢。
- 徹底移除 Hub 維運後台（`internal/service/admin.html`）中呼叫未實作 `POST /hub/v1/admin/tasks/dispatch` 端點之交談分頁與未閉合 HTML 標籤，修復版面錯位並明確界定 `/admin` 專注於 Operator 治理（系統健康、租約修剪、審計日誌與公告廣播）。
- 修復 `a2a ui`（本機交談工作台）重啟後對話歷史遺失問題：以 SQLite WAL 實作 `LocalChatStore`（`~/.a2a/chat.db`），落實「先寫入本機持久化資料庫、再向 Hub 發送 ACK」之安全佇列語義，確保斷電或重啟後會話紀錄完整保留。
- 同步修正 `PLAN.md` 與 `AGENTS.md` 之陳舊狀態說明，明確標記已交付之架構柱石與生產現狀，消弭開發文檔與生產代碼脫節。
- 修正通用 Bridge 收到事件後先 ACK、尚未可靠保存即推理所造成的遺失風險；新增 SQLite WAL 工作佇列、重啟恢復、回信持久化與 idempotent retry。
- 修正 Bridge 註冊 payload 欄位、穩定 registration key、ACK 結果誤報、SSE cursor 跳過事件、過寬回音關鍵字與 systemd 安裝結果未檢查問題。
- 新增 Python Bridge 單元測試與 GitHub Actions 驗證工作；文件明確區分收件 ACK 與推理／回信完成狀態。

### Added

- 極簡化 CLI 與 DX 體驗（Zero-Config 3-Minute Quickstart）：
  - 徹底免除強制要求使用者輸入 `--name`、`--hub` 或 `--backend` 等繁瑣參數；`a2a start`（或直接輸入 `a2a`）自動以系統使用者身分連線 Hub 並開啟 Web UI（`http://localhost:8888`）。
  - `a2a bridge` 自動依據本機 PATH 偵測可用之 AI 工具（OpenClaw、Claude Code、Hermes、Codex），並依據本機主機名稱自動生成簡潔名稱。
  - `a2a bridge --install-service` 自動識別 macOS（`launchd`）或 Linux（`systemd`），免手動指定服務類型。
  - 全面更新 `README.md`、`llms.txt`、`examples/` 與 SKILL 文件，採用如同 `@voko/lite` 般極簡的「3 分鐘啟動」風格。
- 新增單行跨主機一鍵安裝腳本與靜態端點分發（Universal One-Line Installer & Asset Serving）：
  - 實作 POSIX 相容腳本 `scripts/install.sh`，自動偵測 Python 3.10+、下載 `a2a-bridge`、配置 `/usr/local/bin` 捷徑，並支援一鍵安裝 macOS LaunchAgent 或 Linux systemd 系統服務。
  - Hub 端透過 Go `//go:embed` 內嵌並動態提供 `GET /install.sh` 與 `GET /a2a_bridge.py`，支援依伺服器位址動態置換 BaseURL，任何主機皆可透過 `curl -fsSL <HubURL>/install.sh | bash -s -- ...` 單行完成部署。
- 全球 NPM 套件發布封裝（NPM Package Distribution Wrapper）：
  - 新增根目錄 `package.json` 宣告 `888a2a` v0.2.0 套件，提供 `a2a` 與 `a2a-bridge` 可執行 CLI 命令封裝（`bin/a2a.js`、`bin/a2a-bridge.js`）。
  - 支援 `npm install -g git+https://github.com/tbdavid2019/888a2a-lite.git`（或日後 npm 發布後 `npm install -g 888a2a`）全域安裝及 `npm link` 本地連結。
- 擴充認知大腦後端（Extended Cognitive Provider Backends）：
  - 通用 Bridge 新增支援 Anthropic Claude Code CLI（`claudecode`，`claude -p`）、OpenAI Codex CLI（`codex`，`codex exec`）以及自訂 Shell 指令（`command`，`--backend-cmd`）。
  - 新增完備單元測試於 `examples/worker/test_a2a_bridge.py`，覆蓋所有大腦後端之行程調用與逾時防護。
- 原生 Model Context Protocol (MCP) Stdio Server：
  - Bridge 新增 `--mcp` 模式（支援 `a2a mcp` 或 `npx 888a2a mcp`），實作標準 JSON-RPC 2.0 Stdio 協定。
  - 將一般日誌全面導向 `sys.stderr`，確保標準輸出專屬 JSON-RPC 傳輸，徹底避免 Claude Desktop 與 Cursor 發生串流污染解析錯誤。
  - 原生暴露 `a2a_list_agents`、`a2a_send_task`、`a2a_broadcast_group`、`a2a_poll_inbox`、`a2a_status` 5 大工具，讓 Claude Desktop 與 Cursor 可直接發現 Peer、調度任務與發送群聊廣播。
- 新增 `a2a ui` 本機使用者對話工作台（User-to-Agent Web Console）：
  - 實作純 Python 標準庫零外部相依之 `LocalUIServer`（`http://localhost:8888`），使用者可透過 `a2a ui` 或 `npx 888a2a ui` 一鍵啟動並自動開啟預設瀏覽器。
  - 自動以使用者名稱註冊客戶端 Agent 身分憑證（`~/.a2a/user_credentials.json`），嚴格遵循 Hub 鑑權與外鍵約束。
  - 提供左側即時在線 Agent 名單瀏覽、右側點選即時交談、雙向任務收發、本機 SSE 即時推播（`/api/events`）與對話歷史展示。
- 明確劃分體系架構為 A2A Hub（中心服務端）與 A2A Client（本機工作台與 Agent 體系）：
  - 澄清架構邊界：Hub 端嚴格維持「零遠端程式碼執行」原則與乾淨的 Operator 維運後台（`/admin`，負責系統狀態、在線 Agent 心跳租約、金鑰吊銷、公告廣播發布與訊息審計）；使用者互動交談則完全由 Client 端 `a2a ui` 工作台承載。
  - 全面更新 `llms.txt`、`internal/service/llms.txt` 與 `README.md`，統一為 A2A Client（`a2a ui`、`a2a bridge`、`a2a mcp`）與 A2A Hub 雙支柱架構導覽。
- 新增 Agent Skill 規範文件 `skills/a2a-client/SKILL.md`，提供 AI Agent 完整對接指南，涵蓋 Hub 狀態查詢、共用金鑰註冊、Peer 發現、Direct Task 投遞、Inbox 輪詢與 ACK 確認。
- 於 `examples/openclaw`、`examples/hermes`、`examples/codex` 補充半開放模式之金鑰傳遞說明。
- 系統級升級支援 Server-Sent Events (SSE) 即時流式推播：
  - 新增 `GET /hub/v1/agents/{agentId}/inbox/stream` 端點，Agent 透過標準出站 HTTP 長連線即可穿透 NAT/防火牆接收即時任務推播。
  - 核心引入 `InboxEventBroker` 記憶體事件中繼器，於 Direct Task 與 Group Fan-out 寫入 SQLite 瞬間以毫秒級延遲主動推播至活躍連線。
  - 支援連線建立時自動補發尚未 ACK 的 pending 任務，並支援以 `Last-Event-ID` 與 `?afterSequence=` 斷線重連無縫續傳。
  - 每 15 秒發送 `: keepalive` 註釋防止反向代理逾時，並自動展延 Agent 滑動在線租約。
  - SDK 新增 `StreamInbox` 方法，CLI 新增 `listen` 命令，並提供零外部相依性的 Python 監聽守護行程範例 `examples/worker/a2a_worker.py`。
  - `examples/worker/a2a_worker.py` 增強群組廣播（Group Broadcasts）支援：連線與收到邀請通知時自動接受群組邀請（Auto-accept invitations），即時識別 `groupId` 群組廣播事件，並自動向發起端回傳確認報告，避免群內廣播風暴。
  - 增強群組邀請與入群動態之即時流式推播（Group Invitation & Membership SSE Push）：
    - 解決群組發起邀請時未推播被邀請者的盲點：發送邀請（`POST /hub/v1/groups/{groupId}/invitations`）時，Hub 自動為受邀 Agent 生成收件匣邀請通知，並瞬間透過 SSE 長連線推播至受邀 Agent 終端。
    - 新增 `POST /hub/v1/groups/{groupId}/accept` 便捷端點：Agent 收到邀請推播後可直接憑 `groupId` 一鍵入群，無須查詢數字 `invitationId`；CLI `group-accept` 同步支援 `--group` 參數。
    - 成員接受邀請入群時，自動 ACK 信箱中的邀請通知，並透過 SSE 即時向群主（隊長）推播「成員已入群動態」，使隊長無須定時輪詢名冊即可在全員到齊時自動啟動廣播。
- 發布生產級官方通用 Agent 橋接守護程式 `examples/worker/a2a_bridge.py`（Universal Agent Bridge）：
  - 零外部相依性（純 Python 標準庫），開箱即用支援所有 Linux、macOS 與 Docker 環境。
  - 整合式多後端適配：內建支援 OpenClaw CLI、Hermes CLI、OpenAI 相容端點（Ollama、vLLM、DeepSeek 等）與 Echo 模式。
  - 核心內建即時簽收（Instant ACK <50ms）：任務收錄瞬間立即確認簽收，根除 Hub PENDING 焦慮與狀態誤判。
  - 核心內建防回音風暴守衛（Anti-Echo Storm Guard）：具備前置語義過濾與後置 `[[A2A_NO_REPLY]]` 標記判定，終結 AI 同儕間無限互發客套訊息之死循環。
  - 一鍵系統級常駐服務安裝：支援 `--install-service launchd`（macOS LaunchAgent）與 `--install-service systemd`（Linux user unit），自動補全完整 PATH 環境變數與日誌記錄，徹底終結手動寫腳本與維護進程之痛點。
  - 完成跨節點全員升級：甜甜（10.0.0.10）、蜜蜜（10.0.0.10）、甘露寺（10.9.0.9）與彌彌（本地 Mac）全數遷移至標準 Bridge 常駐服務，4 大 Agent 實時對話驗證 100% 通過。

### Changed

- 全面重構與升級 `README.md`：
  - 移除首段暫存語句，結構化整合官方通用 Agent Bridge（`examples/worker/a2a_bridge.py`）之完整架構說明。
  - 新增系統架構 Mermaid 流程圖，清晰呈現 Hub、SSE 串流、本機 SQLite WAL 佇列、防回音守衛與各類 AI 大腦（OpenClaw、Hermes、OpenAI、Codex）之協作邊界。
  - 明確規範核心設計哲學、零遠端執行原則、Safe Agent Card 與單向出站穿透。
  - 補齊通用 Bridge 一行啟動、多後端適配（OpenClaw / Hermes / Ollama / OpenAI）及 launchd / systemd 一鍵服務安裝指南。
  - 完整整理核心 API、群組廣播權限矩陣、Nginx SSE 緩衝防坑設定與實戰踩坑導覽。
- 強化 `llms.txt` 第一步註冊端點說明，直接標註半開放模式下的金鑰標頭與 URL 參數傳遞規範，使 LLM 能精準識別認證要求。
- `llms.txt` 服務端動態樣板化：`/llms.txt` 端點根據 Hub 當前實際運行模式（`PUBLIC` 或 `SEMI_OPEN`）動態注入模式名稱與註冊驗證要求，徹底消除外部 LLM 判斷分支歧義，避免 LLM 在 PUBLIC 模式下混淆或誤停下來向使用者詢問金鑰。
- 補齊並同步全系列說明文件之群組角色與權限架構矩陣（`README.md`、`skills/a2a-client/SKILL.md`、`llms.txt`）：明確規範群組隊長（`OWNER`）與隊員（`MEMBER`）之權限邊界，強調全體活躍成員享有平等廣播權，並完整補齊移交隊長、踢除成員、主動退出與解散歸檔之完整端點與 CLI 指令。
- 明確規範 LLM 任務處理大腦迴圈（Task Processing Architecture & LLM Cognitive Loop）：
  - 於 `llms.txt`、`internal/service/llms.txt` 與 `skills/888a2a-client/SKILL.md` 新增核心大腦處理規範，嚴格禁止外部 AI Agent 採用未經思考的硬編碼腳本或反射 Hook（如 150ms 內機械式回傳本機 IP 或固定字串）。
  - 明確規範收信後必須將 `item.message` 送入 LLM 思考理解意圖，並針對性生成回覆後再發送與呼叫 ACK，群組廣播非點名時避免全體盲目回信引發回音風暴。
  - 重構 `examples/worker/a2a_worker.py` 範例守護程式，移除預設自動盲目回傳 IP 之展示邏輯（改以 `--reply-ip-demo` 選項提供純測試），並標註生產環境對接大腦的架構規範。
- HTTP API 容錯友善增強：
  - `POST /hub/v1/agents/{targetAgentId}/tasks` 與 `POST /hub/v1/groups/{groupId}/messages` 在外部 Agent 未傳遞 `taskId`、`contextId` 或 `idempotencyKey` 時，自動由伺服器端補全安全唯一預設值，避免外部 LLM 因忽略協定欄位而收到 400 驗證錯誤。
  - 於 `llms.txt` 與 `internal/service/llms.txt` 明確補齊群組操作各端點之請求 Payload 格式。
- 修復 SSE 長連線逾時中斷問題（SSE Long-Polling Timeout Fix）：
  - 移除 Go `http.Server` 之全域 15 秒 `WriteTimeout` 硬性限制，並於 `streamInbox` 處理常式透過 `http.NewResponseController` 清除寫入超時，確保 SSE 串流能持續長保連線而不被伺服器每 15 秒中斷。
  - 同步調整生產環境反向代理 Nginx 設定，停用代理緩衝（`proxy_buffering off`、`proxy_cache off`）並將讀寫逾時延長至 86400 秒。
- 於 `AGENTS.md` 完整增補「現場實戰與踩坑經驗 (Production Lessons Learned)」章節，詳細記錄並標準化 Go HTTP Server WriteTimeout、Nginx SSE 緩衝與逾時、Python 守護行程 `-u` 緩衝區黑洞、Inbox PENDING 責任邊界語義、LLM 思考大腦非反射回信規範，以及群組動態閉環等六大實戰陷阱與解法。
- 解決多 Agent 互搏無限回音風暴與狀態 Pending 假象（Anti-Echo Storm & Instant ACK Protocol）：
  - 於各端點守護行程部署「即時簽收（Instant ACK）」機制：Agent 接收到 SSE 任務後於 50ms 內第一時間簽收 ACK，將 Hub 上的任務狀態立即由 PENDING 轉為 ACKNOWLEDGED，杜絕因等待 LLM 推理（10~40秒）導致 Hub 儀表板長期顯示 Pending 之維運焦慮。
  - 部署「防回音風暴守衛（Anti-Echo Storm Guard）」與 `[[A2A_NO_REPLY]]` 終止協議：識別 AI 同儕間之禮貌性結尾與待命狀態更新，避免 Agent 之間無限往返互發任務引發回音風暴。
  - 修復 macOS LaunchAgent 下子行程 PATH 缺失導致 OpenClaw CLI 調用失敗問題，並修復 OpenClaw CLI JSON 輸出解析格式。
  - 全面完成四個真實 AI Agent 實時問答驗證（甜甜 OpenClaw 10.0.0.10、蜜蜜 Hermes 10.0.0.10、甘露寺 OpenClaw 10.9.0.9、彌彌 OpenClaw macOS），達成 100% 真實 LLM 認知對話通過率。

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
