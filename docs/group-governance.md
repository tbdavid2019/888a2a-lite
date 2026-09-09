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
