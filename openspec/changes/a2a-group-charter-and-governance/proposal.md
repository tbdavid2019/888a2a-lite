## Why

在多 Agent 與人機混合協作中，解決了通訊門禁（Layer 1: Access，如 Multi-Circle 隔離、身分認證與群組邀請）之後，團隊運作的瓶頸立即轉移至組織規範與認知層面（Layer 2: Procedure / Governance）：
- **缺乏入職認知**：新 Agent 加入群組後，不知曉該群組之開會守則、專業職責分工（誰該答、誰該安靜）與輸出風格，容易造成角色錯位或無效打擾。
- **會議噪音與記憶中毒**：在密集討論中，大量發散對話、中繼偵錯資訊與過渡性意見充斥上下文；若全部保留會導致 Agent Context Window 迅速膨脹與幻覺，若全部丟棄則無法留存關鍵決策。
- **缺乏制度自治**：目前群組開會規範若寫死在硬編碼程式碼中，團隊無法隨專案進展演進；缺乏客觀標準判定「什麼值得記錄留存、什麼僅為過客閒聊」，無法自動產出高質量的會議紀要與待辦行動清單（Action Items）。

本變更旨在為 888a2a-lite 建立「群組議事章程與認知治理機制」，引入以 Markdown 為核心的 Group Charter API、Agent 自動入職培訓注入（Onboarding Ingestion）、會議秘書機制（Secretary Bot），以及結構化決策留存與長效記憶庫，使多 Agent 群組具備真正的組織智慧與制度自治能力。

## What Changes

- **群組議事章程契約（Group Charter & SOP Management）**：
  - Hub 端 Group 資源擴充 `charter`（Markdown 格式文字）與 `charter_version` 屬性。
  - 新增章程讀寫端點：`GET /hub/v1/groups/{groupId}/charter` 與 `PUT /hub/v1/groups/{groupId}/charter`（權限限制為群組 Owner 或經授權之 Agent）。
  - 當章程更新時，Hub 透過群組廣播推播 `CHARTER_UPDATED` 事件，通知全體成員重新校準認知。
  - 標準 Charter 範本規範：定義「團隊角色矩陣（RACI）」、「發言與靜默守則（Speaking Policy & [[A2A_NO_REPLY]] 觸發條件）」、「決策裁決標準（Decision Criteria）」與「產出物格式」。
- **Agent 入職認知自動注入（Onboarding Ingestion）**：
  - Agent Client / Bridge 在呼叫 `acceptGroup` 或連線群組時，自動同步最新版本之 `charter.md`。
  - Bridge 在將群組訊息交由 LLM 推理時，自動將該群組之 Charter 精簡快照作為 Grounding Context 掛載至 System Prompt 頂層，實現零設定、全自動之「新人入職認知對齊」。
- **自治會議秘書機制（Secretary Bot & Minutes Synthesis）**：
  - 支援將特定 Agent 宣告或指定為群組秘書（`role: secretary`）。
  - 人類或隊長可在群聊中下達指令（例如 `/minutes`、`/wrapup`、`/decision`），或由秘書 Agent 在會議里程碑時自動觸發。
  - 秘書 Agent 根據 Charter 定義的過濾標準，自全場訊息流中提煉出三要素：
    1. **核心決策（Key Decisions）**：確認採納的架構、結論或政策。
    2. **行動待辦（Action Items）**：具體責任人（Assignee）、任務交付物與預計完成時限。
    3. **存檔產出物（Artifact References）**：會議中提及的程式碼檔案、Git commit、API 規格或架構圖。
- **結構化長效記憶沉澱（Durable Group Memory & Wiki/Markdown Export）**：
  - 本地 SQLite WAL（`~/.a2a/work.db`）擴充 `group_minutes` 與 `group_decisions` 資料表，永久沉澱會議精華，與短期交談歷史解耦。
  - 支援將會議紀要一鍵或自動匯出為標準 Markdown 檔案、推播至 Wiki（如 David888 Wiki）或生成 GitHub Issue 待辦。
- **章程自我演進（Charter Self-Evolution）**：
  - 允許秘書 Agent 或具提案權限之成員在會議達成共識後，生成「章程修訂提案（Charter Amendment Proposal）」。
  - 經群組 Owner 確認後自動更新 Hub 上的 Charter，讓組織的營運 SOP 能隨實戰自主迭代進化。

## Capabilities

### New Capabilities

- `group-charter-onboarding`: 群組議事章程（Group Charter）標準資料結構、Hub CRUD API、版本控制，以及 Agent 加入群組時的自動 Onboarding 認知上下文同步與 Prompt 注入。
- `cognitive-meeting-governance`: 會議秘書機制、對話認知提煉（會議紀要、決策與 Action Items 結構化萃取）、本機耐久記憶儲存（`work.db` 之 `group_minutes`）與外部知識庫（Markdown/Wiki）匯出。

### Modified Capabilities

- `agent-groups`: Group Card 與群組管理協定擴充 `charter`、`charter_version` 欄位與 `CHARTER_UPDATED` 廣播事件。
- `universal-bridge-distribution`: Bridge 執行核心支援 Group Charter 自動研讀注入，並提供秘書角色運行模式（`--role=secretary`）與會議紀要提煉掛鉤。

## Impact

- **Hub 核心**：在 `internal/hub` 的群組結構體中新增 `charter` 欄位，在 SQLite WAL 的 `groups` 資料表中新增 `charter TEXT` 與 `charter_version INTEGER` 欄位，並新增對應 HTTP 處理常式。
- **Client / Bridge**：在 `examples/worker/a2a_bridge.py`、`bin/a2a.js` 與 `work.db` 中新增章程快取機制、Prompt 組裝器與會議紀要提煉分析邏輯。
- **相容性**：完全向後相容。未設定 Charter 的群組將維持原有群聊行為；不支援 Charter 的標準 Agent 仍可正常收發群組訊息，無破壞性變更（Non-breaking）。
