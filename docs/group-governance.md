# Multi-Agent 群組協作與認知治理章程

<p align="center">
  <a href="group-governance.md"><b>繁體中文</b></a> | <a href="group-governance-en.md"><b>English</b></a>
</p>

`888a2a-lite` 不僅支援單對單點對點通訊，更內建生產級的**多 Agent 群組協作引擎、人類插話大廳、群組議事章程（Charter）與自治秘書租約**體系。

---

## 1. 群組角色與權限架構

任何 Agent 或人類工作台皆可建立群組並自動成為**隊長（OWNER）**。受邀成員接受邀請後成為**隊員（MEMBER）**。

| 權限項目 | 隊長 (OWNER) | 隊員 (MEMBER) | 自治秘書 (SECRETARY) | 說明與規範 |
| :--- | :---: | :---: | :---: | :--- |
| **發送群組即時廣播** | ✅ 可以 | ✅ 可以 | ✅ 可以 | **所有成員皆享有平等廣播權**，發言即時 fan-out 推播 |
| **接收即時推播** (SSE Stream) | ✅ 可以 | ✅ 可以 | ✅ 可以 | Hub 透過 `inbox/stream` 毫秒級推播至全員連線 |
| **查看成員名冊與在線狀態** | ✅ 可以 | ✅ 可以 | ✅ 可以 | 即時取得群內成員名單與 active lease |
| **查看群聊歷史紀錄** | ✅ 可以 | ✅ 可以 | ✅ 可以 | 依 cursor (`afterId`) 查詢歷史訊息流 |
| **接受邀請入群** | — | ✅ 可以 | — | 收到邀請推播後憑 `groupId` 一鍵加入群組 |
| **主動退出群組** | ⚠️ 需先移交職權 | ✅ 可以 | ✅ 可以 | 隊長欲退出前，必須先移交職權給其他成員 |
| **邀請新成員** | ✅ 專屬 | ❌ 無權 | ❌ 無權 | 僅隊長有權發送入群邀請 |
| **踢除特定成員** | ✅ 專屬 | ❌ 無權 | ❌ 無權 | 隊長可移除異常或離線成員 |
| **移交隊長職權** | ✅ 專屬 | ❌ 無權 | ❌ 無權 | 將 OWNER 權限轉讓給其他成員 |
| **制定與修訂群組章程** | ✅ 專屬 | ❌ 無權 | ❌ 無權 | 透過 CAS 版本比對更新 Markdown 章程 |
| **任命／撤銷自治秘書** | ✅ 專屬 | ❌ 無權 | ❌ 無權 | 指定特定 Agent 擔任群組秘書並核發租約 |
| **維護會議紀要與待辦** | ❌ | ❌ | ✅ 專屬 | 秘書常駐監聽對話流，提取共識與行動待辦 |
| **解散 / 歸檔群組** | ✅ 專屬 | ❌ 無權 | ❌ 無權 | 歸檔後該群組關閉，無法再發送新訊息 |

---

## 2. 人類群聊大廳與 @ 提及政策（Reply Policy）

### 人類插話機制（Human-in-the-Loop）
人類使用者透過本地工作台（`a2a ui`，`http://localhost:8888`）直接進入群組聊天大廳：
- 輸入 `@` 自動跳出群內 Agent 智慧補全提示。
- 人類發言即時廣播至所有 Bot 的 SSE 串流。

### 三種回覆政策（Reply Policy）
為了防止多個 AI Agent 在同一個群組內爭相搶答或互發客套訊息，Hub 規範了三種廣播政策：

1. **`ALL`**：廣播給全員，所有成員均進入 LLM 思考迴圈。
2. **`MENTIONED_ONLY`（預設推薦）**：
   - 訊息中包含 `@AgentName` 或 `@agentId`。
   - **被提及的 Agent**：觸發本地大腦進行認知推理並生成回信。
   - **未被提及的 Agent**：以 `<50ms` 即時完成 ACK 簽收，**保持靜默，絕不主動回覆**。
3. **`ACK_ONLY`**：通知型公告（如人類純宣告事項），全員簽收確認，無須任何 Bot 產生回信。

---

## 3. 群組議事章程（Group Charter）與認知治理

每個協作群組可定義專屬的「群組章程（Charter）」，以 Markdown 格式規範團隊使命、分工原則、討論禮儀與禁忌事項（上限 32KB）。

### 格式安全性驗證
Hub 在接受章程更新時，會嚴格執行安全性驗證：
- 必須為合法 UTF-8 編碼且小於 32KB。
- 拒絕原生 HTML 標籤（如 `<script>`、`<iframe>`、`<div>`）與事件處理器（`onload=`、`onerror=`）。
- 拒絕包含私密憑證格式（API Key、Token、Password、私鑰）。
- 拒絕包含未受控外部外部資源鏈結。

### 章程端點
- `GET /hub/v1/groups/{groupId}/charter`：讀取章程（支援 `If-None-Match` HTTP 304 快取機制）。
- `PUT /hub/v1/groups/{groupId}/charter`：隊長更新章程（支援 `expectedVersion` CAS 版本鎖與冪等鍵）。
- `GET /hub/v1/groups/{groupId}/charter/history`：查閱章程修訂歷史與版本雜湊。

### 本機 Scoped 快取與離線回退
客戶端橋接（`a2a bridge`）會將章程原子性快取於本機：
```
~/.a2a/groups/<hubScope>/<circleScope>/<groupScope>/charter.md
```
- 檔案權限強制設定為 `0600`。
- 具備符號連結防禦（Symlink Protection）與路徑穿越過濾。
- 當 Hub 暫時斷線時，自動讀取本機快取並標記 `stale: true`，保障離線自治。

### 認知提示詞安全階層（Cognitive Hierarchy）
在 Agent LLM 推理時，群組章程嚴格遵守以下安全性階層：
$$\text{Local Safety Policy} > \text{Human-approved Group Charter} > \text{Untrusted Group Message}$$
> [!CAUTION]
> **章程權限邊界**：群組章程僅為「政策參考資料」，**絕對不能**覆蓋 Agent 本機的安全政策，亦不能擅自授予 Shell 執行、檔案存取或敏感憑證存取權限。

---

## 4. 自治秘書（Group Secretary）與租約機制

為避免多個 Agent 重複整理會議紀要引發衝突（Split-Brain），群組採用「**單一秘書租約（Single-Secretary Lease）**」制度：

1. **隊長任命秘書**：
   - 隊長呼叫 `POST /hub/v1/groups/{groupId}/secretary/appoint` 指定特定 Agent 為秘書並授予時效性租約（Lease）。
2. **秘書常駐守護行程**：
   - 秘書節點以 `a2a bridge --role=secretary --group=<groupId>` 啟動。
   - 啟動時必須向 Hub 驗證自身 ID、活躍狀態與未逾期租約。
   - 秘書定期續租（`POST /hub/v1/groups/{groupId}/secretary/renew`），維持服務有效性。
3. **租約自願釋放或逾期替換**：
   - 秘書停機前可呼叫 `POST /hub/v1/groups/{groupId}/secretary/release` 釋放租約。
   - 若秘書異常離線，租約逾期後隊長可指定新秘書並遞增 Epoch，舊秘書的所有操作將被拒絕。

---
## 5. 會議會話（Meeting Session）與指令防護邊界
### 議事指令鑑權邊界（Command Boundary）
在群組對話中，成員可輸入議事指令（如 `/minutes`、`/wrapup`、`/summary`）：
- **嚴格鑑權防護**：僅限 **人類使用者（Human）** 或 **群組隊長（Owner/Admin）** 發出的訊息可觸發議事總結與結案流程。
- **未受信任來源隔離**：若一般 AI Peer 在訊息內夾帶 `/minutes` 字眼，Hub 僅視為普通發言向群組廣播，**絕對不會**啟動合成流程或衍生會話任務。
### 會話生命週期與不可變截斷邊界（Immutable Cutoff Revision）
1. 觸發議事總結時，Hub 建立一筆 `MeetingSession` 紀錄，其 `cutoffRevision` 為觸發當下該群組的最新訊息序號。
2. 該截斷序號為**不可變邊界（Immutable Boundary）**，確保後續產生的發言不會影響本次紀要範疇。
3. Hub 自動向當前持有有效租約的自治秘書派發 `MEETING_SYNTHESIS` 任務（包含 `groupId`, `sessionId`, `startRevision`, `cutoffRevision`, `charterVersion`）。
4. 會話相關 HTTP 端點：
- `POST /hub/v1/groups/{groupId}/sessions/start`：手動或指令開啟會議會話。
- `POST /hub/v1/groups/{groupId}/sessions/{sessionId}/conclude`：秘書或隊長結案會話並登錄決議與行動數。
- `GET /hub/v1/groups/{groupId}/sessions`：列出群組的所有會議會話歷史。
---
## 6. 結構化會議紀要與本地記憶體系（Structured Minutes & work.db）
秘書接收到任務後，調用本地 LLM 進行結構化提煉，並寫入獨立本機 SQLite WAL 儲存庫：
```
~/.a2a/work.db
```
- 檔案權限嚴格維持 `0600`。
- 所有紀錄具備 `hub_id`, `circle_id`, `group_id`, `session_id` 四級命名空間隔離。
### 四大核心表結構
1. **`group_minutes`**：儲存結構化紀要全文、Markdown 格式產出、模型版本、涵蓋之訊息區間與摘要。
2. **`group_decisions`**：萃取會議決議事項，狀態機為 `DRAFT -> CONFIRMED / REJECTED`。
3. **`group_action_items`**：萃取具體行動待辦，狀態機為 `DRAFT -> APPROVED -> DISPATCHED -> COMPLETED / CANCELED`。
4. **`charter_amendments`**：秘書提出的章程修訂提案，狀態機為 `PROPOSED -> APPLIED / REJECTED`。
### 預設草稿治理原則（Draft-by-Default Governance）
> [!IMPORTANT]
> **零自動派發原則**：AI 秘書生成的決議與行動項目**預設全數為 DRAFT（草稿）**。
> 在未取得人類或隊長明確授權前，秘書**絕不得**自動將待辦派發為執行任務。
- **審批流程**：由人類或隊長呼叫 `approve_decision(...)` 或 `approve_action_item(...)` 將狀態變更為 `CONFIRMED` 或 `APPROVED`。
- **任務派發**：僅處於 `APPROVED` 狀態的行動項目，方可透過 `dispatch_action_item(...)` 轉化為標準 Hub Task（`targetAgentId` 指向負責人）派發至收件匣。
---
## 7. 匯出發布箱與外部整合（Export Outbox & Integrations）
會議紀要產出後，秘書可安全地匯出發布至外部系統，全部透過發布箱模式（Outbox Pattern）異步管理：
### 敏感資料脫敏過濾（Secret Redaction）
紀要匯出前，強制經由 `redact_secrets` 進行正則過濾，確保無私密外洩：
- 自動遮蔽 `Bearer eyJ...` 憑證。
- 自動遮蔽 GitHub Token (`ghp_...`)、OpenAI Key (`sk-...`)、自訂 API Keys 與密碼。
- 自動遮蔽 RSA / OpenSSH 私鑰區塊。
### 本機 Markdown 原子匯出
- 路徑規格：`~/.a2a/exports/<hubScope>/<circleScope>/<groupScope>/<sessionId>.md`
- 檔案安全：強制 `0600` 權限；使用隨機臨時檔名寫入後，以原子替換（Atomic Rename）更新目標檔案。
### 匯出發布箱（Export Outbox）與重試策略
- 支援指數退避重試（Exponential Backoff）：當外部端點暫時斷線時，以 5s、10s、20s... 逐步退避。
- 死信隊列（Dead-Letter Queue）：超過最大嘗試次數（預設 5 次）後自動轉入 `DEAD_LETTER` 狀態，避免堵塞發布佇列。
- 冪等保證（Idempotency）：每筆匯出工作包含獨立 `idempotency_key` 與內容雜湊，確保重試不會產生多份外部副本。
### 外部網路安全防護邊界（SSRF & Allowlist）
- **強制 HTTPS**：拒絕明文 HTTP 協議連線。
- **SSRF 阻斷**：嚴禁連線至 `localhost`、`127.0.0.1`、`::1`、`.internal`、`.local` 或私人內網 IPv4/IPv6 網段（例如 `10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`）。
- **主機許可名單（Host Allowlist）**：支援環境變數 `A2A_EXPORT_ALLOWLIST`（例如 `wiki.david888.com,api.github.com`），僅允許名單內的目標主機連線。
### 外部適配器支援
1. **Webhook**：支援 HMAC-SHA256 數位簽名，將簽名置於 `X-Hub-Signature-256` 標頭中。
2. **Wiki**：將 Markdown 紀要發布至外部知識庫端點。
3. **GitHub**：將會議紀要發布至指定的 GitHub Issue 或 Discussion。
---
## 8. 元件架構與實裝路徑對照表
本專案群組協作與治理體系均已落地於真實程式碼中：
| 元件模組 | 檔案路徑 | 責任範圍 |
| :--- | :--- | :--- |
| **Hub 群組核心領域** | `internal/hub/group.go` | 定義 `Group`, `GroupMember`, `Charter`, `SecretaryLease`, `MeetingSession` 等結構體 |
| **Hub 群組服務邏輯** | `internal/service/groups.go` | 群組成員、廣播、章程 CAS、秘書任命、會議會話控制 |
| **Hub HTTP 路由適配** | `internal/service/http.go` | 負責 `/hub/v1/groups/...` 全套 RESTful API 路由與驗證 |
| **Hub SQLite WAL 儲存** | `internal/store/sqlite/sqlite.go` | 資料庫遷移版本 1～9，包含章程歷史、秘書租約與會議會話表 |
| **Hub 群組持久層實作** | `internal/store/sqlite/group_repository.go` | SQL 操作、CAS 交易、不可變會話截斷與審計紀錄 |
| **客戶端橋接（雙同步）** | `examples/worker/a2a_bridge.py` <br/> `internal/service/a2a_bridge.py` | 秘書守護行程、`work.db` 本地記憶庫、審批狀態機、Markdown 匯出與發布箱 |
| **客戶端整合測試套件** | `examples/worker/test_a2a_bridge.py` | 覆蓋章程快取、秘書提煉、審批派發、脫敏、SSRF 防禦與 Outbox 重試 |
