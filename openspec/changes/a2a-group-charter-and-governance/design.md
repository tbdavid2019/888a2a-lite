## Context

在完成 Phase 1（A2A v1.1.4 標準相容）、Phase 2（群組多工協同）與 Phase 3（人類介入與本機控制台）之後，多 Agent 群聊已具備完整的通訊與門禁基礎（Layer 1: Access）。然而，當多個不同廠商、不同專長之 Agent（OpenClaw、Claude Code、Hermes、Codex）聚集在同一個會議室時，缺乏組織規範（Layer 2: Procedure / Governance）會導致群聊淪為無序的雜音與 Context 浪費。

本設計旨在提供一條低摩擦、輕量且自治的認知規範通道，讓 Hub 提供 SOP 章程版本託管，讓 Agent 本機端（Bridge）能自動進行入職研讀、Prompt 認知約束掛載、會議秘書紀要萃取，以及決策成果持久化。

## Goals / Non-Goals

**Goals:**
- **Hub 端章程契約**：在 SQLite WAL 的 `groups` 表中引入 `charter` (TEXT) 與 `charter_version` (INT)，提供 `GET/PUT /hub/v1/groups/{groupId}/charter` 端點，並在變更時發送 `CHARTER_UPDATED` SSE 廣播。
- **客戶端自動入職（Zero-Config Onboarding）**：Agent 在加入群組或啟動時，自動拉取並快取 `~/.a2a/groups/{groupId}/charter.md`，並在 LLM 推理前自動組裝至 System Prompt 頂部。
- **自治會議秘書（Secretary Role）**：支援 `a2a bridge --role=secretary`，監聽全場對話並在 `/minutes`、`/wrapup` 指令或對話段落時提煉結構化紀要。
- **三要素認知提煉（Three-Pillar Distillation）**：紀要自動剔除寒暄噪音，萃取「關鍵決策（Decisions）」、「待辦指派（Action Items）」與「產出物參照（Artifacts）」。
- **本機耐久記憶儲存**：在 `~/.a2a/work.db` 中建立 `group_minutes` 與 `group_decisions` 資料表，將高價值組織資產與短期聊天歷史永久分離。
- **知識庫與檔案匯出**：支援產出獨立 Markdown 檔案，並預留 Wiki/GitHub 待辦同步介面。

**Non-Goals:**
- **中心端向量資料庫（No Hub Vector Database）**：Hub 保持極致輕量與純 Go/SQLite 架構，不引入 Qdrant/Milvus 等重量級向量搜尋引擎；深度語義檢索由 Client 端或專用 Agent 負責。
- **遠端程式代碼自動執行**：提煉出的 Action Items 僅做結構化記錄或派發，絕不在未經授權下由 Hub 直接在遠端主機執行指令。
- **拜占庭容錯投票演算法**：群組自治採用實用主義設計（Owner 權限管理或秘書提案經由人類/隊長同意），不引入複雜分散式共識鏈。

## Decisions

### 1. 以 Markdown 作為章程（Charter）的規範載體
- **決策**：`charter` 統一採用標準 Markdown 格式儲存與傳輸。
- **原因**：Markdown 對所有主流 LLM 具有最高的天生理解力，人類在 Local Web UI 或終端文字編輯器中可直接閱讀與修訂，且與 Git 版本控制相性極佳。
- **替代方案**：JSON Schema（過於僵化，無法自然表達開會氛圍與語義準則）、純文字 Plain Text（缺乏階層式大綱）。

### 2. 快取優先（Cache-First）的入職與版本校準
- **決策**：Bridge 本機保存 `charter.md` 快取，並記錄本地 `charter_version`。僅在收到 `CHARTER_UPDATED` 事件或版本落後時才向 Hub 請求最新內容。
- **原因**：避免每次 LLM 推理或收發訊息都產生一次 HTTP 請求，降低 Hub 負載並保障離線與弱網環境下的推理流暢度。

### 3. 三要素紀要萃取模型（Decisions / Actions / Artifacts）
- **決策**：會議秘書 Prompt 強制執行三要素結構化解析，並將非實質性發言（如「收到」、「謝謝」）過濾。
- **原因**：防止會議紀要變成第二份逐字稿。組織的真正價值在於「定了什麼（Decision）」與「誰去做什麼（Action）」。

### 4. 決策記憶存於本機 `work.db` 而非中央 Hub
- **決策**：提煉出的紀要與決策項目寫入各節點的 `~/.a2a/work.db`，由秘書或相關 Agent 本機維護。
- **原因**：符合 888a2a-lite 的「非託管、去中心化隱私、Zero-SaaS」哲學，中央 Hub 不窺探亦不沉澱企業機密認知資產。

## Architecture & Data Flow

```
[ 人類 / Owner ]
       │ (1) PUT /hub/v1/groups/{id}/charter (Markdown SOP)
       ▼
[ 888a2a-lite Hub ] ─── (2) SSE Broadcast: CHARTER_UPDATED ───┐
       │                                                      │
       │ (3) GET /charter                                     │
       ▼                                                      ▼
[ Agent A: Bridge ]                                    [ Agent B: Secretary ]
       │                                                      │
  (4) 快取至 ~/.a2a/groups/                               (4) 快取至 ~/.a2a/groups/
  (5) 注入 System Prompt                                 (5) 監聽全體對話流
  (6) 依 SOP 規範發言 / 靜默                                  (6) 收到 /minutes 觸發提煉
                                                              │
                                                              ▼
                                                    [ 認知提煉 (LLM) ]
                                                              │ 萃取 Decisions & Actions
                                                              ▼
                                                    [ 持久化至 work.db ]
                                                    [ 匯出 group-minutes.md ]
```

## Risks / Trade-offs

- **[風險：章程長度導致 Prompt Token 膨脹]**
  - *緩解機制*：建議 Charter 保持在 2,000 字元以內的精煉 SOP。Bridge 在注入 System Prompt 時支援「精簡快照模式」，只擷取關鍵的 Role、Speaking Policy 與 Output Format 區塊。
- **[風險：多個 Agent 搶任秘書產生衝突]**
  - *緩解機制*：群組透過參數宣告單一官方秘書；若多個秘書同時在線，僅有具備 `--role=secretary` 且 Agent ID 排序在前者主導回覆與歸檔，其餘簽收 ACK 後靜默。
- **[風險：LLM 提煉出幻覺 Action Items]**
  - *緩解機制*：Prompt 規範 Action Items 必須精確標註群內既有成名稱（Assignee）以及來源發言之上下文摘要，在未經人類確認前僅標記為 `STATUS_DRAFT`。
