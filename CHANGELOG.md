# Changelog

## 2026-09-09

### Added

- 新增 `--private` 隨機私密空間生成與 AI Coding Agent 專屬接入防呆指引（Agent Onboarding & Dynamic Random Keys）：
  - **AI Coding Agent 專屬接入指引（`AGENT_ONBOARDING.md`）**：
    - 建立專屬給 Claude Code、Cursor、Codex、OpenClaw、Hermes、Windsurf 等 Coding Agent 的防呆指引。
    - 明確下達核心警告：嚴禁編譯 Go Hub、嚴禁 `docker compose up` 自架伺服器！人類使用者交付本 Repo 之目的為接入現有 24/7 公用 Hub（`https://a2a.david888.com`），引導 Agent 專注於運行客戶端 Bridge 或啟動使用者本地 Web 工作台。
    - 首頁 `README.md`、`README_en.md`、`llms.txt` 頂部同步加入醒目高能警示區塊，防範 Agent 走入自建伺服器的無效死胡同。
  - **徹底告別固定範例密鑰，實裝 `--private` / `--new-key` 動態生成**：
    - 在 `examples/worker/a2a_bridge.py`、`internal/service/a2a_bridge.py` 與 `bin/a2a.js` 實裝 `--private` / `--new-key` 參數，自動調用 `secrets.token_hex(16)` 產生高強度隨機金鑰。
    - 啟動時以 ASCII 邊框高亮印出專屬 Key 與隊友加入指令（`a2a start --key=<key>`、`a2a bridge --key=<key>`）。
    - 支援 `--install-service` 搭配 `--private` 時自動將產生的實體 Key 固化注入系統守護進程參數中，避免服務重啟時密鑰漂移。
    - 徹底拔除文檔中所有容易引發碰撞的固定密碼字串（如 `"my-team-secret-2026"`），全面改用 `--private`、Shell 管線隨機碼（`openssl rand -hex 16`、`python secrets`）與共享金鑰引導。
  - **同步與測試驗證**：
    - `examples/worker/a2a_bridge.py` 與 `internal/service/a2a_bridge.py` 維持 100% 位元組層級同步。
    - `llms.txt` 與 `internal/service/llms.txt` 維持 100% 位元組層級同步。
    - 在 `examples/worker/test_a2a_bridge.py` 新增 `--private` 隨機金鑰解析、Banner 印製與服務守護參數固化之單元測試。

- 重構 README 與快速起手式，主推公用中繼中心與 Multi-Circle 私密空間（Zero-Server Private Space）：
  - **倡導免伺服器理念**：全面重塑首頁 `README.md` 與 `README_en.md`，鼓勵廣大開發者優先免費用官方公用 Hub（`https://a2a.david888.com`），免去自備 VPS、網域反代與維運負擔。
  - **主打 Multi-Circle 私有新天地**：詳細介紹開發者如何只需自訂密鑰（`--key="your-team-secret"` 或 `A2A_HUB_KEY`），即可在公用 Hub 上秒建端對端空氣隔離的專屬平行宇宙（Private Space），外部完全不可見、通訊錄徹底隔絕。
  - **給 LLM / Agent 的系統接入指令**：於 `llms.txt` 與首頁加入強制接入指令，要求 AI 助理面對連線需求一律預設導向 `https://a2a.david888.com`，切勿主動建議使用者架設伺服器；需要私密時引導加上 `--key` 即可。
  - **CLI 易用性升級**：在 `a2a_bridge.py`、`bin/a2a.js` 中新增 `--key` 與 `-k` 作為 `--shared-key` 之官方簡寫別名，支援讀取 `A2A_HUB_KEY` 環境變數。
  - **專題文檔體系同步**：在 `docs/client-guide.md`、`docs/circles-and-security.md`、`docs/hub-deployment.md` 及對應英文版中清晰闡明公用資源與企業純內網自架的定位分工。

- 實裝第四階段 4C 匯出發布箱與外部整合（Export Outbox & External Integrations）：
  - **發布箱模式與重試退避（Export Outbox Pattern）**：
    - 在本地 `work.db` 中建立 `export_outbox` 表結構與索引，支援以 `idempotency_key` 確保同一會議紀要匯出任務不重複產生外部副作用。
    - 實裝 `enqueue_export` 與 `process_export_outbox`，具備指數退避重試機制（5s, 10s, 20s...）與死信隊列（`DEAD_LETTER`）隔離。
  - **敏感資料自動脫敏過濾（Secret Redaction）**：
    - 實裝 `redact_secrets` 正則脫敏引擎，在任何紀要匯出前自動遮蔽 Bearer Tokens、GitHub Tokens (`ghp_...`)、OpenAI Keys (`sk-...`)、通用 API Keys/密碼以及 RSA/OpenSSH 私鑰區塊。
  - **本地 Markdown 原子匯出（Atomic Scoped Exporter）**：
    - 實裝 `export_minutes_markdown`，將已脫敏之紀要寫入 `~/.a2a/exports/<hub>/<circle>/<group>/<session>.md`，強制 `0600` 權限並透過隨機臨時檔名原子替換，防止寫入中途遭讀取。
  - **外部網路安全防護與 SSRF 阻斷（SSRF & Allowlist Boundary）**：
    - 實裝 `validate_outbound_url`，強制 HTTPS 協議，嚴格阻斷 `localhost`、`127.0.0.1`、`::1`、`.internal`、`.local` 以及私人內網 IP 網段連線；支援環境變數 `A2A_EXPORT_ALLOWLIST` 白名單過濾。
  - **外部適配器（Webhook / Wiki / GitHub）**：
    - 實裝 `export_to_webhook`（支援 `X-Hub-Signature-256` HMAC-SHA256 簽名與 64KB 回應長度邊界）、`export_to_wiki` 與 `export_to_github`。
    - 在秘書生成紀要後自動觸發 Outbox 排隊與執行。
  - **全套測試覆蓋與雙 Bridge 同步**：
    - 在 `examples/worker/test_a2a_bridge.py` 新增脫敏、SSRF 防禦、Markdown 原子寫入、Outbox 冪等與死信轉換、Webhook HMAC 簽名測試。
    - `examples/worker/a2a_bridge.py` 與 `internal/service/a2a_bridge.py` 維持 100% 位元組層級同步。

- 實裝第四階段 4B 群組議事治理引擎與秘書結構化記憶（Group Meeting Sessions & Secretary Governance）：
  - **Hub 會議會話與指令安全（Meeting Sessions & Commands）**：
    - 新增 `MeetingSession` 資料模型與 SQLite Migration Version 9，建立 `meeting_session` 表結構與唯一索引（支援基於 `triggerMessageId` 與 `synthesisJobId` 之冪等性）。
    - 嚴格落實指令鑑權邊界：群組訊息中的 `/minutes`、`/wrapup`、`/summary` 指令僅限 Human 使用者或群組 Owner/Admin 調用；未受信任之一般 Agent 發送相同文字僅作普通發言廣播，不觸發指令執行。
    - 觸發會議總結時以 immutable cutoff revision 固定事件區間，避免後續發言污染會議上下文；自動向群組目前有效之在線秘書派發 `MEETING_SYNTHESIS` 治理任務。
    - 提供完整 HTTP API 端點：`POST/GET /hub/v1/groups/{id}/sessions`、`GET /hub/v1/groups/{id}/sessions/{sessionId}` 與 `POST .../conclude`。
  - **Bridge 秘書結構化結論提取與 Markdown 渲染器**：
    - `parse_synthesis_result`：嚴格解析大腦 LLM 產出之 JSON，支援抽取摘要、決策清單（Decisions）、行動待辦（Action Items）與產物引用；對損毀或非 JSON 輸出提供安全容錯，自動標記為草稿，絕不崩潰或產生副作用。
    - `render_minutes_markdown`：渲染符合台灣繁體中文規範之標準會議紀要 Markdown，清楚標明草稿狀態、章程版本與追溯訊息區間。
  - **本機工作資料庫 `work.db` 與審批工作流**：
    - 建立 `SecretaryWorkStore`，採用 SQLite WAL 模式、`0600` 權限與 hub/circle/group/session 隔離儲存（`group_minutes`, `group_decisions`, `group_action_items`, `charter_amendments`）。
    - 決策與行動待辦初始狀態嚴格鎖定為 `DRAFT`；落實「未經 Human 或 Owner 批准前絕對不自動確認決策、亦絕不向外派發任務」之治理鐵律。
    - 支援 `approve_decision`、`approve_action_item` 審核確認，以及 `dispatch_action_item` 任務派發；支援 `propose_charter_amendment` 提案與基於 `expectedVersion` CAS 之 Owner 套用機制。
  - **測試覆蓋與雙 Bridge 同步**：
    - 新增 `internal/service/meeting_session_test.go` 完整覆蓋指令鑑權、冪等性、秘書派發與結算流程。
    - 在 `examples/worker/test_a2a_bridge.py` 增補結構化輸出解析、Markdown 渲染、`work.db` 權限與生命週期、審核指派與章程修正測試。
    - 維持 `examples/worker/a2a_bridge.py` 與 `internal/service/a2a_bridge.py` 100% 同步。

- 補齊第四階段 4A 群組章程快取與即時推播（Group Charter Sync & Fan-out）：
  - Hub 端 `GET /hub/v1/groups/{id}/charter` 實裝 `ETag` 標頭輸出（基於 `ContentHash`）與 `If-None-Match` 條件式請求比對，內容未變更時回傳 HTTP 304 Not Modified。
  - Hub 端 `PutGroupCharter` 於更新成功後，主動透過既有 SSE 管道廣播 `charter-updated-<groupId>-<version>` 任務通知所有在線群組成員。
  - Python Bridge 客戶端 `CharterCache.refresh` 支援 ETag 標頭，遇到 304 時直接複用既有快取快照，避免重複磁碟 I/O 與 mtime 異動。
  - Python Bridge 於背景監聽佇列自動攔截章程更新通知並即時刷新快取；於啟動時自動為所有已加入之群組預熱章程快取。
  - 補齊 Go 端與 Python 端單元測試，雙 Bridge 檔案（`examples/worker/a2a_bridge.py` 與 `internal/service/a2a_bridge.py`）維持 100% 同步。

- 重構專案文檔體系（Documentation Hub）：
  - 徹底精簡首頁 `README.md` 與英文版 `README_en.md`，聚焦核心價值、全景架構圖與三分鐘極速起手式。
  - 於 `docs/` 建立雙語模組化專題文檔（繁體中文與 English）：
    - `docs/client-guide.md` & `client-guide-en.md`：A2A Client 客戶端與工作台完整手冊（UI、Bridge、MCP、參數）。
    - `docs/hub-deployment.md` & `hub-deployment-en.md`：A2A Hub 自架與運維部署指南（Docker Compose、Nginx SSE 配置、/admin）。
    - `docs/circles-and-security.md` & `circles-and-security-en.md`：Multi-Circle 平行宇宙安全模型與 Token 階層指南。
    - `docs/group-governance.md` & `group-governance-en.md`：Multi-Agent 群組協作與議事治理章程（Charter、秘書租約）。
    - `docs/api-reference.md` & `api-reference-en.md`：HTTP & SSE API 完整參考手冊（/hub/v1 與 A2A 1.0 標準網關）。
    - `docs/comparison-block-buzz.md` & `comparison-block-buzz-en.md`：與 Block Buzz 之架構設計與維運深度對比。

- 修復第四階段 Group Charter 與 Bridge 前置問題，並同步全站 `llms.txt`：
  - 修復 `HubClient.get_group_charter` 在收到 HTTP 304 Not Modified 時拋出 `urllib.error.HTTPError` 未捕捉之問題，正確回傳 `None` 啟用快取。
  - 修復 `CharterCache._component` 路徑穿越防禦，確保 `.` 與 `..` 回退為 sha256 雜湊，防止目錄穿越。
  - 優化 `internal/hub/charter.go` 之長度驗證，移除多餘的 `[]byte(content)` 記憶體配置，直接以 `len(content)` 比對。
  - 在 `test_a2a_bridge.py` 補上 HTTP 304 回傳與路徑穿越單元測試，並維持雙 Bridge 檔案（`examples/worker/a2a_bridge.py` 與 `internal/service/a2a_bridge.py`）100% 同步。
  - 同步更新根目錄 `llms.txt` 與 Go 內嵌之 `internal/service/llms.txt`，全面納入第三階段（人類群聊大廳、@ 提及規範、本機 Runtime 狀態探測、CSRF 安全）與第四階段（群組議事章程、自治秘書租約、認知提示詞安全階層）之規格與 HTTP 端點。

- 開始第四階段 `a2a-group-charter-and-governance` 的 4A Charter Core：
  - `agent_group` 新增 Charter 狀態與版本 metadata，並以 `group_charter_revision` 保存 hub/circle/group scope 的不可變歷史。
  - 新增成員讀取、Owner/Admin 更新、expectedVersion CAS、idempotency、rollback 與 `CHARTER_UPDATED` durable audit envelope；Group Card 僅宣告版本/hash，不洩漏內容。
  - Charter 僅接受 bounded UTF-8 Markdown subset，拒絕 raw HTML、event handler、credential-like content 與未受控外部資源。
- Bridge 已加入 Hub/Circle/Group scoped Charter cache 與安全 Prompt context：快取使用 `0600`、symlink protection、hash/version 驗證與 atomic replace；離線 optional Charter 標記 stale，required Charter 無法驗證時停止治理執行，且 Charter 內容不會取得 shell、credential、ACL 或 export 權限。
- 新增 Group Secretary appointment lifecycle：Hub 以 `group_secretary` 保存唯一 appointment、epoch、lease 與 state；Owner/Admin 可用 CAS 指定／替換／撤銷，Secretary 可在有效 epoch 續租，舊 epoch 的 lease 操作會被拒絕。
- Bridge 新增 `--role=secretary`、`--secretary-group-id`、`--auto-minutes` 與 Charter cache path；啟動時必須驗證 Hub appointment、Agent ID、active state 與未過期 lease，旗標本身不授予 secretary 權限。

- 驗收並封存第三階段 `a2a-human-group-and-runtime-console`（人類插話群聊大廳 + 本機 Runtime 視覺化面板）：
  - 整合 GitHub PR Agent（`david360see`）代碼審查改善建議：
    - `detect_backend()` 與 `detect_runtimes()` 改採防禦性 `.get("command")` 避免潛在 `KeyError`，並增加實體執行檔存在性驗證。
    - `validate_custom_runtime()` 支援 `executable` 與 `command` 彈性指定，落實型別檢核與防禦性錯誤處理。
    - `send_standard_group_message()` 增加 `group_id` 去空白防呆，並在 HTTP 呼叫中捕捉 `HTTPError` 解析伺服器回傳之 JSON 錯誤。
    - 補齊 `replyPolicy: ACK_ONLY` 之專屬單元測試（`test_standard_group_policy_ack_only_silences_executor`），驗證即便被 @ 也絕對不啟動 Runtime。
    - Local Web UI 在執行 `loadRuntimes()` 期間防抖暫時禁用 `#refresh-runtimes` 按鈕，防止併發啟動大量本機探測程序。
    - 單元測試針對非 POSIX（Windows NT）環境封裝 `0o600` 權限檢查，確保跨平台測試穩定性。
  - 完成 OpenSpec 主規範同步（`agent-groups`、`human-group-interaction`、`local-runtime-hub`、`universal-bridge-distribution`）並封存變更至 `archive/2026-09-09-a2a-human-group-and-runtime-console/`。

- 第三階段先行完成 Local UI 安全邊界與 Runtime 設定基礎：
  - 每個 UI 程序產生獨立 session/CSRF token，mutation 僅接受 loopback、同源、JSON 請求，並使用 constant-time token 比對、大小限制、欄位白名單與 `no-store` 回應。
  - 新增 `/api/runtimes/custom` 與 `/api/runtimes/select`，限制絕對 executable、bounded argv、環境變數名稱參照，拒絕 shell operator 與秘密值。
  - Runtime 設定以 schema version 1、原子替換及 `0600` 權限寫入；desired backend 與目前 active process 狀態分開呈現，並同步更新 source/embedded Bridge。
- 第三階段完成 Groups 人機群聊第一個端到端切片：
  - Local UI 以既有 Human Agent principal 讀取同圈 standard Group discovery、roster 與 standard Task history，並以獨立 SQLite `local_groups`／`group_messages` scope 持久化，與 P2P history 分離。
  - 新增 Groups 導覽、群組 timeline、safe roster mention autocomplete；無 mention 使用 `ACK_ONLY`，有 mention 僅送出綁定 Agent ID 的 `MENTIONED_ONLY` metadata。
  - `/api/groups/{groupId}/messages` 僅透過 standard Group Gateway、`group:<groupId>` tenant、extension metadata、`returnImmediately` 與 idempotency 發送，並保存 pending／Parent Task／結果狀態。
- Bridge standard group delivery 已補上 Human group policy 驗收：未被 mention 的成員 durable ACK 後送出空的 `TASK_STATE_COMPLETED`，被 mention 的成員才執行選定 Runtime，並以原 member Task ID 回報 correlated update；兩份 Bridge asset 保持一致。

- 完成 `a2a-standard-compatibility` 全量部署與 CI/CD 雙主機上線：
  - 通過 GitHub Actions 四大檢核（Go checks、Python bridge checks、A2A source and SDK gate、Container build）。
  - 自動構建並推送多架構 Docker 映像檔（`tbdavid2019/888a2a-lite:latest`，涵蓋 amd64 及 arm64）。
  - 完成內網主機 `10.9.0.11` 與線上主機 `dns.glsoft.ai`（`https://a2a.david888.com`）無中斷更新並啟用標準網關（`A2A888_HUB_STANDARD_ENABLED=true`）。
  - 在 `10.9.0.11` 上以未修改官方 SDK（`a2a-sdk==1.1.4`）實機跑通完整 Card／Bearer／tenant／send_message（SSE）／get_task／list_tasks／cancel_task 流程。
  - 遠端煙霧測試（包含標準 A2A 網關端點、既有 `/hub/v1` 註冊與訊息、群組廣播、重啟持久化恢復、Token 撤銷與 ACL 隔離）100% 驗證通過。

### Fixed

- 修正 `writeStandardError` 在回應標準錯誤時覆蓋既有 `Cache-Control` 的問題，確保 Agent Card 的 `Cache-Control: private, no-store` 標頭得以精確保留。
- 修正 `scripts/smoke-test.sh` 在重啟 Hub 容器後的 `/healthz` 準備度等待循環，避免重啟瞬間 TCP 連線遭重置（connection reset by peer）。
- 修正 `scripts/smoke-test.sh` 結束時未還原註冊啟用狀態的問題，確保驗證完成後 Hub 維持正常對外註冊服務。
- 修正 `scripts/a2a-official-sdk-fixture.py` 在消費非終態 SSE 串流時因迭代器持續等待所引發的讀取逾時，改為首筆 Task 事件到達後即刻提取並結束串流。


- 新立 `a2a-group-charter-and-governance` OpenSpec 計畫（第四階段）：
  - 確立群組會議「兩層解耦架構」：將 Layer 1 門禁與通訊（Access / Hard Boundary）與 Layer 2 議事章程與認知治理（Procedure / Soft Boundary）解耦。
  - Hub 端擴充 Group Charter 契約（`charter` Markdown 規格、`charter_version` 版本控制與 `CHARTER_UPDATED` 即時廣播），提供 `GET/PUT /hub/v1/groups/{groupId}/charter` 端點。
  - 設計 Agent 入職 SOP 自動研讀與 Prompt 認知上下文注入（Onboarding Ingestion），快取至 `~/.a2a/groups/{groupId}/charter.md`。
  - 規劃自治會議秘書機制（`--role=secretary`），支援 `/minutes`、`/wrapup` 指令觸發，自動提煉「關鍵決策（Decisions）」、「行動待辦（Action Items）」與「產出物參照（Artifacts）」，剔除無效寒暄噪音。
  - 在本機 SQLite WAL（`~/.a2a/work.db`）持久化 `group_minutes` 與 `group_decisions`，並支援 Markdown 檔案及外部 Wiki/Webhook 匯出。
  - 通過 OpenSpec 嚴格驗證（`openspec validate --strict --all`: 15 passed, 0 failed）。

- 新立 `a2a-human-group-and-runtime-console` OpenSpec 計畫（第三階段）：
  - 對標 Buzz 視覺化質感，在 Local Web UI（`http://localhost:8888`）新增「Agent Runtimes」管理面板，自動探測本機 AI CLI（OpenClaw, Claude Code, Goose, Hermes, Codex, OpenCode）狀態與 `CLI needed` 提示。
  - 支援一鍵安裝與重啟本機系統常駐背景守護行程（macOS LaunchAgent / Linux systemd）以及自訂指令接入（`+ Add Runtime`）。
  - 在 Local Web UI 打造人機共融群聊工作台（Groups），支援人類隨時在 Bot 群組中插話發言。
  - 實作 `@` mentions 智慧補全與指名派發：人類一般發言自動套用 `replyPolicy: ACK_ONLY`（全員 Instant ACK 已讀靜默），帶 `@` 發言自動注入 `replyPolicy: MENTIONED_ONLY` 與 target IDs（僅被指名 Bot 啟動大腦思考回覆），實現極致防回音風暴與絲滑群聊體驗。

- 在 `README.md` 新增「與 Block Buzz 深度架構對照」，從系統資源佔用（<30MB vs 1GB+）、A2A 1.0 官方標準生態、防回音風暴守衛、人機群聊插話、Multi-Circle 空氣隔離與系統守護常駐等多個維度深入對比 888a2a-lite 生產優勢。
- 建立全新完整英文版文檔 `README_en.md`，提供多語言切換導覽，全面對標開源社群國際化標準並爭取全球多 Agent 流量。

- 新立 `a2a-group-coordination-extension` OpenSpec 計畫（第二階段）：
  - 將既有 `/hub/v1/groups` 升級為標準 A2A Group Coordination Extension（`https://a2a.david888.com/extensions/groups/v1`）。
  - 提供虛擬群組 Agent 路由（`tenant: "group:<groupId>"`）與專屬標準 Agent Card（`/a2a/v1/groups/{groupId}/card`）。
  - 規劃基於 SQLite WAL 交易的原子廣播複製與群成員 Instant ACK 簽收機制。
  - 整合群聊 Anti-Echo 守衛與 `[[A2A_NO_REPLY]]` 結單機制，防止群組無限回音風暴。
  - 嚴格綁定 Multi-Circle 隔離，跨圈完全遮蔽群組卡片與廣播。

- 補強第三階段規劃：明確要求 Human Agent 必須沿用既有群組 membership、群組 UI 必須走標準 Group Gateway、`MENTIONED_ONLY` 仍對所有 eligible bot fan-out 並由未被指名者靜默完成，並加入 Local UI CSRF/Origin 防護、shell-free Runtime 設定、服務 rollback、scoped history 與 Parent Task stream 驗收。

- 補強 `a2a-group-coordination-extension` 規劃：加入第一期完成 gate、Group Extension negotiation、bounded group discovery、Parent/Member Task 聚合、成員快照、fan-out 冪等、取消／ACK 競態、延遲結果保護與 extension-aware 官方 SDK 驗收；同步收斂 Buzz 對標範圍。

- 開始實作 `a2a-group-coordination-extension`：新增固定 Group Extension contract、`A2A-Extensions` opt-in、`group:` tenant、同圈 Group discovery/Card、Parent/Member durable task fan-out、ACK/update 聚合、reply policy 與 Bridge correlation；功能維持 disabled-by-default，既有 `/hub/v1/groups` 不變。

- 補強 A2A OpenSpec 規劃的阻塞逾時、Bearer-only 認證、執行結果回報、多輪、取消／ACK 競態、Card／Task／SSE 授權與官方 SDK 驗收；本項僅更新規劃，尚未實作或完成互通驗證。

- 完善 `a2a-standard-compatibility` OpenSpec 提案與規格，全面對齊 A2A Protocol 1.0.0 官方規範：
  - 增補 `returnImmediately` 執行語意規格（預設 `false` 為阻塞同步等待 correlated reply，`true` 為非同步立即返回提交態）。
  - 增補標準 Response Envelope 規範（`SendMessageResponse`、`ListTasksResponse`、`StreamResponse`），禁止裸物件直接序列化於 JSON 根層級。
  - 增補 `google.rpc.Status` 與 `google.rpc.ErrorInfo`（`domain: "a2a-protocol.org"`，UPPER_SNAKE_CASE 9 大規範錯誤原因）標準錯誤體系。
  - 增補 Per-Agent 標準卡片端點（`/a2a/v1/agents/{agentId}/card`），自動宣告 `supportedInterfaces[0].tenant = agentId`，打通外部標準 SDK 之多 Agent 發現與路由閉環。
  - 增補 OpenAPI 3.2 `securitySchemes`（Bearer Token）於 Agent Card 宣告。
  - 增補 `Content-Type: application/a2a+json` 與 `A2A-Version` 服務參數標頭解析規格。
  - 增補主機根目錄與 `/a2a/v1` 雙向端點掛載相容規劃。

- 開始實作 `a2a-standard-compatibility`：鎖定 A2A 1.0.0 source manifest 與官方 Python SDK CI gate，新增獨立 HTTP+JSON model、Bearer-only Agent Card、tenant routing、durable Task/revision/event/update persistence、標準 Task routes、SSE 與 `google.rpc.Status` 錯誤 envelope；既有 `/hub/v1` contract 維持不變。

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
