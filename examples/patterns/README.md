# A2A 多 Agent 協作模式指南與範例 (Multi-Agent Patterns)

本目錄提供在 `888a2a-lite` Hub 生態中實現高階多 Agent 協作的四大經典拓撲模式與可直接執行的 Python 範例。

所有範例皆採 **純 Python 標準庫（Zero External Dependencies）** 實作，可直接無縫運行。

---

## 架構三層分工原則

```text
┌─────────────────────────────────────────────────────────────┐
│ 1. Hub 核心層 (Go / SQLite WAL)                             │
│    - 負責 Mailbox 收發、在線租約心跳、Instant ACK、SSE 即時推播 │
│    - 持久化 Workflow step attempts、deadline、retry 與 join 結果 │
│    - 不排程或執行 Agent 工作，也不內嵌 LLM 推理              │
└──────────────────────────────┬──────────────────────────────┘
                               │ (A2A Protocol / Envelope JSON)
┌──────────────────────────────▼──────────────────────────────┐
│ 2. Client / Workflow 層 (Python / Node.js / CLI)            │
│    - 定義工作流拓撲：Pipeline、Parallel Fan-out、Supervisor、Debate│
│    - 發起重試並回報業務結果；Hub ACK 與 workflow outcome 分開 │
│    - 內建 Anti-Echo Storm Guard 防回音風暴守衛與最大回合限制 │
└──────────────────────────────┬──────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────┐
│ 3. Examples / Cookbook 層 (本目錄)                          │
│    - 01_pipeline_demo.py     (串行流水線接力)                │
│    - 02_parallel_fanout.py   (並行派工與聚合 Fan-in)         │
│    - 03_supervisor_router.py (依 Agent Card 智慧路由)        │
│    - 04_debate_group.py      (防回音辯論與仲裁審查)          │
└─────────────────────────────────────────────────────────────┘
```

---

## 結構化 Envelope 規範 (`a2a_envelope.py`)

為了讓接收端 LLM 與客戶端正確識別工作流上下文，訊息 Payload 建議封裝為標準 Envelope JSON：

```json
{
  "workflow_id": "wf-pipeline-1726650000",
  "correlation_id": "corr-1726650000",
  "flow_type": "pipeline",
  "step_id": "audit",
  "reply_to": "draft",
  "round": 1,
  "max_rounds": 3,
  "role": "auditor",
  "required_capabilities": ["code/audit"],
  "terminal": false,
  "payload": {
    "draft_code": "..."
  },
  "metadata": {},
  "created_at": "2026-09-18T08:50:00Z"
}
```

### 關鍵欄位語意說明：
1. **`terminal: true`**：
   - 表示本工作流步驟已全部結束或完成裁決。
   - **防回音風暴核心**：接收端收到 `terminal: true` 時，僅呼叫 Instant ACK 簽收，**絕不**主動再發起一筆回信 Task。
2. **`round` 與 `max_rounds`**：
   - 限制辯論或迭代攻防之最大次數。一旦 `round > max_rounds`，自動強制觸發終止邏輯；`round == max_rounds` 仍可進行裁判交接。
3. **`workflow_id`**：
   - 對應 Hub 上可查詢的 durable Workflow；step attempts、deadline、取消、死信及 join 結果會持久化。
4. **`correlation_id`**：
   - 串聯同一批次並行任務（Fan-out）的所有回覆，供聚合器（Aggregator）比對辨識。

---

## 四大範例快速上手

### 1. 串行流水線接力 (`01_pipeline_demo.py`)
- **場景**：`發起人 -> 初稿撰寫 (Drafter) -> 程式碼審查 (Auditor) -> 發起人收成`。
- **特點**：每一棒 Agent 收到任務第一時間（<50ms）執行 Instant ACK，LLM 思考產出後將 Envelope 推進到下一階段（`step_id` 遞進），最後一棒設定 `terminal: true`。
```bash
python3 01_pipeline_demo.py --demo
```

### 2. 並行派工與聚合 (`02_parallel_fanout.py`)
- **場景**：Aggregator 將架構研究主題同時派工給 3 位領域專家（WAL 專家、Postgres 專家、AppendLog 專家）。
- **特點**：
  - 專家們並行運算、非同步回傳。
  - Hub 保存 **法定人數（Quorum，如 2/3）** 與 deadline 結果，可查詢成功、失敗、取消及死信數量；Aggregator 仍負責收集結果與彙整報告。
```bash
python3 02_parallel_fanout.py --demo
```

### 3. 監督者模式 (`03_supervisor_router.py`)
- **場景**：Supervisor 依據任務所需的專長能力，動態派工給最適配的在線 Agent。
- **特點**：
  - Supervisor 透過 `GET /hub/v1/agents` 讀取即時在線的 Agent Cards。
  - 比對 `required_capabilities`（如 `lang/go` vs `data/visualization`）進行動態路由分發，無需事先寫死 IP 或節點名稱。
```bash
python3 03_supervisor_router.py --demo
```

### 4. 辯論與對抗審查 (`04_debate_group.py`)
- **場景**：提案方（Proposer）提出架構草案，質詢方（Critic）提出抗辯質疑，裁判（Judge）最終定案裁決。
- **特點（現場實戰第 7 條防護）**：
  - 嚴格設定 `max_rounds = 2`，杜絕客套反覆回信的死循環。
  - 裁判發布終審裁決時標記 `terminal: true` 並附加 `[[A2A_NO_REPLY]]`。
  - 雙方 Agent 驗證 `is_anti_echo_triggered() == True` 後，自動靜音終止對話。
```bash
python3 04_debate_group.py --demo
```

---

## 連線參數支援

所有範例腳本預設連接本機 `http://127.0.0.1:8080`，並要求明確指定 `--demo` 才會註冊示範 Agent。連到自架或公開 Hub 時，範例 Agent 會保留至 Hub 註冊租約到期；建議使用測試 Circle。需要連接現有 Hub 時，可明確指定：
```bash
# 連接公開或自建 Hub：
python3 01_pipeline_demo.py --demo --hub https://a2a.david888.com

# 若使用私有空間 (Multi-Circle / Semi-Open)：
python3 01_pipeline_demo.py --demo --hub https://a2a.david888.com --key <your-team-key>
```
