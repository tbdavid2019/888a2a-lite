## 1. Hub Group Charter Schema & Endpoints

- [ ] 1.1 在 `internal/hub/storage.go` 的 SQLite `groups` 表中新增 `charter TEXT` 與 `charter_version INTEGER DEFAULT 1`，並驗證資料庫啟動遷移相容性
- [ ] 1.2 在 `internal/hub/http.go` 實作 `GET /hub/v1/groups/{groupId}/charter` 與 `PUT /hub/v1/groups/{groupId}/charter` 端點，並驗證僅 Owner/Admin 可更新、非成員存取回傳 403/404
- [ ] 1.3 在 `internal/hub/events.go` 實作章程更新時之 `CHARTER_UPDATED` SSE 即時廣播，並驗證連線中成員能接收到最新 `charter_version`
- [ ] 1.4 在 `internal/hub/types.go` 與 Group Card 序列化中加入 `charter_version` 與 `has_charter` 欄位，並驗證查詢群組資訊時包含此詮釋資料

## 2. Client-side Onboarding & Charter Ingestion

- [ ] 2.1 在 `examples/worker/a2a_bridge.py` 實作 `fetch_and_cache_charter(groupId)`，將章程存入 `~/.a2a/groups/{groupId}/charter.md` 並驗證檔案落盤
- [ ] 2.2 在 Agent 執行 `acceptGroup` 及監聽群組事件串流時接入自動同步邏輯，收到 `CHARTER_UPDATED` 時自動更新本地快取
- [ ] 2.3 在 `a2a_bridge.py` 實作 Prompt 注入組裝器，在呼叫 LLM CLI（OpenClaw、Claude Code、Hermes 等）前自動掛載精簡章程上下文，並驗證 Prompt 包含開會規範與靜默守則

## 3. Autonomous Secretary Role & Meeting Distillation

- [ ] 3.1 在 `a2a_bridge.py` 與 `bin/a2a.js` 新增 `--role=secretary` 啟動參數與專屬監聽模式，並驗證秘書 Agent 可全程追蹤群聊上下文
- [ ] 3.2 實作對話流指令觸發器（`/minutes`、`/wrapup`、`/summary`）與段落總結鉤子，並驗證指令辨識精確度
- [ ] 3.3 設計三要素認知提煉 Prompt（關鍵決策 Decisions、行動待辦 Action Items、產出物參照 Artifacts），過濾寒暄廢話並驗證結構化 Markdown 輸出

## 4. Durable Storage & Export Integration

- [ ] 4.1 在本地 SQLite WAL（`~/.a2a/work.db`）初始化 `group_minutes` 與 `group_decisions` 資料表，並驗證本地資料庫 schema 完整性
- [ ] 4.2 實作提煉紀要與決策項目寫入 `work.db` 之耐久化邏輯，並驗證跨重啟能按 `groupId` 查詢過往決策
- [ ] 4.3 實作會議紀要自動匯出至 `~/.a2a/minutes/<groupId>-<timestamp>.md` 及 Wiki / Webhook 介面，並驗證 Markdown 格式符合標準

## 5. Documentation & End-to-End Verification

- [ ] 5.1 撰寫群組章程範本指南（`docs/guide/group-charter-template.md`），並在中文與英文 README 補強 Layer 2 認知治理章節
- [ ] 5.2 執行多 Agent 實戰模擬：包含 Agent 入職自動同步 Charter、遵守發言規範、人類輸入 `/wrapup`，以及秘書 Bot 自動沉澱高質量會議紀要至 `work.db`
