# A2A 多 Agent 協作模式與工作流架構規範 (Collaboration Patterns)

本規範定義在 `888a2a-lite` 通訊基礎架構之上，多 Agent 系統如何進行結構化協同運作（串行流水線、並行聚合、監督者路由、對抗辯論），並明確劃分 Hub、Client Workflow 與應用層的責任邊界。

---

## 1. 核心責任邊界 (Responsibility Boundaries)

在分散式 Multi-Agent 協作系統中，系統可劃分為三個層級：

```text
┌─────────────────────────────────────────────────────────────┐
│ 1. Hub 通訊核心層 (Go + SQLite WAL)                         │
│    - 負責 Mailbox 佇列、在線租約 (Lease)、SSE 推播、Instant ACK│
│    - 保持輕量與高吞吐，不理解業務完成狀態，不內嵌 LLM 推理   │
└──────────────────────────────┬──────────────────────────────┘
                               │ A2A Message Payload (Envelope JSON)
┌──────────────────────────────▼──────────────────────────────┐
│ 2. Client / Workflow 流程層 (Python / Node.js / CLI)        │
│    - 拓撲狀態管理：串行接力、並行聚合、能力匹配路由、回合控制 │
│    - 區分「傳輸簽收 (Transport ACK)」與「業務完成 (Result)」│
│    - 嚴格實施防回音風暴守衛 (Anti-Echo Storm Guard)           │
└──────────────────────────────┬──────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────┐
│ 3. Agent 認知與執行層 (LLM / Agent Runtime)                 │
│    - OpenClaw、Claude Code、Hermes、Codex、本機 Python/Bash  │
│    - 解析 Envelope.payload 執行思考、工具調用並產出成果      │
└─────────────────────────────────────────────────────────────┘
```

### 關鍵認知：「傳輸簽收（ACK）」與「業務完成（Result）」的徹底解耦
* **Hub 的 ACK（`POST /hub/v1/agents/{id}/inbox/{seq}/ack`）**：
  - 僅代表**目標接收端行程已將訊息安全收錄至本機記憶體或磁碟佇列**。
  - 必須在收到推播的第一時間（<50ms）立即完成簽收（Instant ACK on Ingest），使 Hub sequence 狀態迅速脫離 `PENDING`。
  - **ACK 絕不代表 LLM 已經思考完畢或任務已成功**。
* **業務完成（Result Delivery）**：
  - LLM 歷經 10~60 秒的推理與工具執行後，將產出成果打包為全新的 A2A Task 傳送給下游或發起端。
  - 透過 `step_id`、`correlation_id` 與 `terminal` 標記明確標示業務進展。

---

## 2. 結構化 Envelope 協定規範

所有進階協作模式中，A2A Task 的 `message` 欄位建議序列化為以下結構化 JSON Envelope：

```json
{
  "workflow_id": "wf-1726650000-a1b2",
  "correlation_id": "corr-1726650000-c3d4",
  "flow_type": "pipeline",
  "step_id": "code_review",
  "reply_to": "code_generate",
  "round": 1,
  "max_rounds": 3,
  "role": "auditor",
  "required_capabilities": ["lang/go", "security/audit"],
  "terminal": false,
  "payload": {
    "status": "IN_PROGRESS",
    "artifacts": ["..."]
  },
  "metadata": {
    "initiator_id": "agent-user-001"
  },
  "created_at": "2026-09-18T08:55:00Z"
}
```

### 欄位語意說明

| 欄位名稱 | 型別 | 說明 |
| :--- | :--- | :--- |
| `workflow_id` | `string` | 全域工作流追蹤識別碼，貫穿整個業務生命週期。 |
| `correlation_id` | `string` | 上下文相關識別碼。並行 Fan-out 時所有子任務共享此 ID，供聚合器配對。 |
| `flow_type` | `string` | 拓撲類型：`pipeline`（串行）、`parallel`（並行）、`supervisor`（監督）、`debate`（辯論）。 |
| `step_id` | `string` | 當前工作流步驟識別名稱（如 `draft`、`audit`、`completed`）。 |
| `reply_to` | `string?` | 本次回應所針對之上游 `step_id` 或任務 ID。 |
| `round` | `int` | 當前互動或辯論攻防回合數（起始為 1）。 |
| `max_rounds` | `int` | 允許之最大回合數，防止無休止對話死循環。 |
| `role` | `string` | 當前發信者角色：`proposer`、`critic`、`judge`、`supervisor`、`worker`、`aggregator`。 |
| `required_capabilities` | `string[]` | 執行本步驟所需的 Agent 專長標籤（供 Supervisor 匹配）。 |
| `terminal` | `bool` | **終止旗標**。為 `true` 時代表工作流終結，收件方簽收後**絕不准**再發送回信 Task。 |
| `payload` | `any` | 業務真實資料載體（文字、物件、結構化成果）。 |
| `metadata` | `object` | 輔助除錯與鏈路追蹤資訊。 |

---

## 3. 四大協作模式詳解

### 模式一：串行流水線接力 (Sequential / Pipeline)
* **拓撲模型**：
  ```text
  [Initiator] ──► [Agent A: 撰寫] ──► [Agent B: 審查] ──► [Agent C: 發布] ──► [Initiator]
  ```
* **核心規範**：
  1. 每個中繼 Agent 接收到任務後，即刻呼叫 Instant ACK。
  2. 處理完成後，調用 `envelope.next_step(next_step_id, role, payload)` 推進狀態。
  3. 最後一棒將 `terminal` 設為 `true`，將成果定向回傳給發起人。
* **參考實作**：`examples/patterns/01_pipeline_demo.py`

---

### 模式二：並行派工與聚合 (Parallel Fan-out / Fan-in Aggregator)
* **拓撲模型**：
  ```text
                      [Aggregator]
                           │
      ┌────────────────────┼────────────────────┐
      ▼ (Subtask 1)        ▼ (Subtask 2)        ▼ (Subtask 3)
  [Worker 1]           [Worker 2]           [Worker 3]
      │                    │                    │
      └────────────────────┼────────────────────┘
                           ▼ (Fan-in)
                      [Aggregator] ──► (Consolidated Report)
  ```
* **核心規範**：
  1. **共享關聯鍵**：派發出的所有子任務必須攜帶相同的 `workflow_id` 與 `correlation_id`。
  2. **非同步自主簽收**：各 Worker 獨立進行 Instant ACK，並行運算後獨立回傳。
  3. **超時與法定人數（Timeout & Quorum）**：
     - Aggregator 不能無限制等待所有節點（防止個別節點掛死牽制整體流程）。
     - 設定超時時間（如 10 秒）與最小法定人數（如 `min_quorum = 2`），滿足條件即可收斂合成報告，並對未完成節點進行優雅降級標記。
* **參考實作**：`examples/patterns/02_parallel_fanout.py`

---

### 模式三：監督者動態路由 (Supervisor / Dynamic Router)
* **拓撲模型**：
  ```text
                 [User / Caller]
                        │
                        ▼
              [Supervisor / Router]
                        │
       ┌────────────────┴────────────────┐
       ▼ (GET /hub/v1/agents)            ▼ (Capability Match)
  [Agent Cards 池]                  [最適 Worker]
                                         │ (Delegated Task)
                                         ▼
                                  [Worker Execution]
                                         │
                                         ▼
                              [Supervisor Verification]
  ```
* **核心規範**：
  1. **零硬編碼節點**：Supervisor 透過 `GET /hub/v1/agents` 即時探索當前在線且租約有效的 Agent Cards。
  2. **能力矩陣比對**：根據任務的 `required_capabilities`（如 `lang/go`、`security/audit`、`data/viz`）動態選擇最佳承接者。
  3. **成果查驗與回退**：若無任何節點具備該能力，Supervisor 負責產出降級提示或放入暫存隊列。
* **參考實作**：`examples/patterns/03_supervisor_router.py`

---

### 模式四：辯論與對抗審查 (Debate / Adversarial Review)
* **拓撲模型**：
  ```text
  [Proposer] ◄──── Round 1 ────► [Critic]
      │                              │
      └────────── Round 2 ───────────┘
                     │ (Max rounds reached)
                     ▼
              [Judge / Arbiter]
                     │ (terminal: true, [[A2A_NO_REPLY]])
                     ▼
          [Clean Workflow Exit]
  ```
* **嚴格防護措施（現場實戰第 7 條）**：
  1. **強制回合上限（`max_rounds`）**：預設為 2~3 回合，禁止無約束對話。
  2. **裁判定案（Judge Handoff）**：達到最大回合數後，案例強制移交裁判節點進行終審裁決。
  3. **防回音風暴守衛（Anti-Echo Storm Guard）**：
     - 裁判輸出時強制標記 `terminal: true` 與 `[[A2A_NO_REPLY]]`。
     - 辯論雙方接收到終審判定時，呼叫 Instant ACK 簽收後**直接結束並靜音**，絕不可再回覆「收到了」、「謝謝指導」等客套廢話。
* **參考實作**：`examples/patterns/04_debate_group.py`

---

## 4. 驗證與測試

本協同模式套件提供完整單元測試，涵蓋 Envelope 序列化、狀態推進、能力比對與防回音風暴邏輯：

```bash
# 執行單元測試
python3 -m unittest examples/patterns/test_patterns.py
```
