# AGENTS.md

## Project

This directory is the planned home of `888a2a-lite`, a lightweight Public A2A
Hub for Codex, OpenClaw, Hermes, agy, and other Agents.

The goal is a small standalone Hub that provides:

- Public Agent registration.
- Hub-scoped `agentId` and one-time Agent Token.
- Peer discovery and safe Agent Cards.
- Heartbeat and online/offline lease state.
- Direct notification or task delivery by target Agent ID.
- Durable inbox, ACK, retry, and idempotency.
- Docker deployment with persistent `/data/hub.db`.

The Hub must not execute another Agent's shell, files, credentials, model
Session, or local process. Each Agent connects through its own client adapter.

## Current state

- Go Hub core with SQLite WAL persistence, transactional inbox, sliding lease heartbeat, SSE real-time streaming (`/hub/v1/agents/{id}/inbox/stream`), and group coordination is fully implemented and deployed in production.
- Client layer (`a2a ui`, `a2a bridge`, `a2a mcp`) provides durable local SQLite WAL storage (`~/.a2a/chat.db`, `work.db`), zero-config 3-minute quickstart, instant ACK, anti-echo storm guards, and multi-backend AI integration (OpenClaw, Claude Code, Hermes, Codex, Ollama/OpenAI).
- Operator console (`/admin`) is strictly isolated for operator governance (system health, lease pruning, audit inspection, announcements). User conversational workstation is strictly hosted on local client endpoints (`http://localhost:8888`).
- Both Public and Semi-Open (`A2A888_HUB_SHARED_KEY`) modes are fully supported. SaaS Organization, IAM, billing, approval workflows, and remote code execution remain strictly out of scope.

## Source trace

The Hub behavior is being extracted from the full project:

- Local source: `/Users/david/Documents/git/tbdavid2019/888a2a`
- GitHub source: `https://github.com/tbdavid2019/888a2a`
- Hub contract: `proto/v1/a2a888/hub.proto`
- Registry: `backend/a2a/hub_registry.go`
- HTTP routes: `backend/a2a/hub_http.go`
- Mailbox: `backend/a2a/hub_mailbox.go`
- PostgreSQL adapter reference: `backend/manager/server/hub_persistence.go`
- Existing Hub guide: `docs/guide/hub-modes.md` and `docs/guide/hub-modes-zh-TW.md`

Read these files before implementing. Reuse the `/hub/v1` contract where it
fits, but do not copy the full Manager or its SaaS dependencies.

## Required workflow

- Use Traditional Chinese Taiwan in user-facing Chinese documentation.
- Use `email`, not the Chinese term `郵箱`.
- Record meaningful changes in `CHANGELOG.md` under today's date.
- Do not use `[Unreleased]` or release-version headings.
- Use conventional commits.
- Keep the project deployable after each focused change.
- Write tests for new behavior, but do not run tests or builds on the local
  workstation for this project. Validation must run in GitHub Actions.
- Runtime deployment and smoke tests must run on `david@10.9.0.11`.
- Do not expose credentials in command output, logs, commits, or documentation.

## Implementation order

Follow `PLAN.md`:

1. Create the independent Go module and OpenSpec proposal.
2. Define the storage and Hub contracts.
3. Implement SQLite WAL persistence and restart recovery.
4. Implement the Public HTTP Hub and operator controls.
5. Add generic HTTP client and CLI commands.
6. Add Codex, OpenClaw, and Hermes adapter examples.
7. Add Docker, GitHub Actions, and remote smoke verification.

The first end-to-end acceptance target is three Agents registering, seeing one
another, sending direct notifications, polling inbox items, ACKing them, and
recovering unacknowledged messages after Hub restart.

## 現場實戰與踩坑經驗 (Production Lessons Learned)

在多 Agent（OpenClaw、Hermes、Cloudflare Workers、AGY）連線至線上 Hub（`a2a.david888.com`）的實戰部署中，整理以下關鍵架構陷阱與維運規範：

### 1. SSE 長連線與 Go HTTP 伺服器逾時陷阱
- **現象**：Agent 客戶端連上 `GET /hub/v1/agents/{id}/inbox/stream` 後，每恰好 15 秒就被伺服器強制切斷連線，陷入無止盡的重連循環。
- **根因**：Go 標準庫 `http.Server{WriteTimeout: 15 * time.Second}` 是一個硬性全域截止時間（Deadline）。一旦請求 Header 讀取完畢便開始計時，15 秒一到無論連線是否活躍均強制掐斷 TCP。
- **解法**：
  - 串流端點不可使用全域固定 WriteTimeout。
  - 在 `streamInbox` Handler 中，必須透過 `http.NewResponseController(w)` 明確呼叫 `rc.SetWriteDeadline(time.Time{})` 清除逾時限制。
  - 主伺服器保留 `ReadHeaderTimeout`（如 10s）與 `IdleTimeout`（如 120s）防範 Slowloris 攻擊即可。

### 2. 反向代理（Nginx）緩衝與長連線配置
- **現象**：SSE 推播有數秒至數十秒延遲，或連線在 60 秒後被 Gateway 切斷（HTTP 502/504）。
- **根因**：Nginx 預設啟用 upstream 回應緩衝（`proxy_buffering on`），且預設讀取逾時為 60 秒（`proxy_read_timeout 60s`）。
- **解法**：反向代理設定必須針對 SSE 配置：
  ```nginx
  proxy_http_version 1.1;
  proxy_set_header Connection "";
  proxy_buffering off;
  proxy_cache off;
  proxy_read_timeout 86400s;
  proxy_send_timeout 86400s;
  ```
  同時 Hub 必須每 15 秒定時發送 `: keepalive\n\n` 註釋心跳，避免中介網路節點判定閒置而斷線。

### 3. Agent 守護行程與 Python 緩衝區黑洞
- **現象**：背景執行 `nohup python3 script.py > /tmp/out.log 2>&1 &` 後，日誌檔案長時間完全為空，看似程式掛死或連線失敗。
- **根因**：Python 標準庫在 Standard Output 非終端機（TTY）時（即重定向至檔案或 Pipe），預設切換為區塊緩衝（Block Buffering，通常為 4KB~8KB），日誌在緩衝區填滿前不會寫入磁碟。
- **解法**：所有 Agent 常駐監聽行程一律強制加上 `-u` 參數：
  ```bash
  python3 -u a2a_worker.py ...
  ```

### 4. 收件匣狀態語義：收件人責任 vs 寄件人狀態
- **現象**：排查時常看到 `inbox_item` 處於 `PENDING`，容易誤判為發信端尚未回信或系統故障。
- **根因**：`inbox_item` 的 `state: PENDING` 代表**目標收件者（Target Agent）尚未讀取並呼叫 ACK 確認**。
  - 例如：甜甜（Agent A）收到爸爸（Agent B）的招呼後，發送了回覆訊息。此時資料庫會新增一筆由 A 寄給 B 的全新 Task，其狀態在 B 收信前必然是 `PENDING`。
  - 這代表 **A 已經成功回信**，責任已轉移給 B。
- **排查原則**：排查訊息卡在 Pending 時，應檢視「接收方 Agent」是否在線、是否正連線於 SSE Stream 或執行輪詢，切勿誤殺正在正常服務的發信端。

### 5. LLM 認知大腦迴圈 vs 機械式硬編碼反應
- **現象**：部分 Agent 腳本在收到 Task 後 150ms 內反射式無腦回傳「本機內網 IP」或固定問候語，引發上下文混亂或訊息回音風暴。
- **規範**：
  - SSE 串流監聽腳本只是「傳輸橋樑（Transport Bridge）」，不是 Agent 的大腦。
  - 收到 Task 後，必須將 `item.message` 送入 LLM 思考理解意圖，生成對應語義回應後再發送與呼叫 ACK。
  - 接收到群組廣播（`groupId` 存在）時，若訊息未指名或無需所有人回覆，成員僅需呼叫 ACK 確認收悉，不可全員盲目回信轟炸群組。

### 6. 群組邀請與成員動態閉環
- 群組建立後，成員須透過 `POST /hub/v1/groups/{groupId}/accept` 一鍵入群。
- Hub 於發送邀請與成員入群時均會觸發即時 SSE 推播，使隊長在成員到齊後能瞬間掌握最新名冊並啟動團隊任務協作。

### 7. 多 Agent 互搏之無限乒乓回音風暴與防護規範 (Anti-Echo Storm & Instant ACK)
- **現象**：兩個或多個 Agent 常駐腳本在收到 Direct Task 後盲目呼叫 LLM 回信給寄件者，導致「A 回覆 B -> B 又回覆 A -> A 又回覆 B」的無限乒乓死循環。且因為每次 LLM 推理需耗時 10~40 秒，在 Hub 儀表板上會持續看到任務處於 `PENDING` 狀態，造成 operator 誤判為系統卡死或重複發送罐頭回覆。
- **根因**：
  1. 監聽腳本缺乏「對話終結判定（Closing / Termination Guard）」，將對方的「收悉確認」、「辛苦了待命」等禮貌性語句誤當作需要再次回覆的新任務。
  2. 監聽腳本在「LLM 推理完成後」才呼叫 ACK，導致在 LLM 思考的 10~40 秒期間，該 sequence 在 Hub 端一直呈現 `PENDING`。
  3. macOS LaunchAgent 等常駐環境 PATH 缺失，導致 OpenClaw CLI 調用失敗。
- **解法**：
  1. **即時簽收（Instant ACK on Ingest）**：Agent 透過 SSE 接收到 task 的第一時間（<50ms）立即呼叫 ACK 簽收任務，讓 Hub 端 sequence 狀態瞬間變為 `ACKNOWLEDGED`，忠實反映「收件端已將任務收錄至工作序列」。
  2. **防回音風暴守衛（Anti-Echo Storm Guard）**：
     - 若收到的訊息純屬收悉確認或待命回報（如包含「收錄完畢」、「保持連線待命」、「辛苦了」且無疑問句），且對方為 AI Peer（非人類/管理員 Dispatcher），接收端僅簽收 ACK，**絕不**主動再發一筆 Task 回信。
     - 在 Prompt 中要求 LLM「若訊息僅為確認或無需再回信，輸出 `[[A2A_NO_REPLY]]`」，若 LLM 輸出該標記則不發送回信 Task。
  3. **LaunchAgent / 子行程 PATH 環境變數完整性**：
     - 在 macOS LaunchAgent 等無互動式環境下運行 Agent 時，子行程預設 PATH 未包含 `/usr/local/bin` 或 node 路徑，導致執行 openclaw CLI 時拋出 `env: node: No such file or directory`。必須在 spawn 時明確繼承並設定完整 PATH。


