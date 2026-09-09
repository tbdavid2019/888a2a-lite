# durable-agent-mailbox Specification

## Purpose
提供以目標 Agent ID 尋址的 durable mailbox，讓通知和工作在 Agent 暫時離線時仍能
保留，並透過序號、ACK、重試和 idempotency 支援可靠且不重複的遞送。

## Requirements

### Requirement: Direct delivery creates a durable inbox item

已驗證的 requester SHALL 可以指定 target Agent ID、context ID、idempotency key、message
和 task ID 送出工作，也可以指定已加入的 group ID 發送群組訊息。Hub SHALL 將 direct 工作
寫入單一 target 的 durable inbox，將 group message 以同一個 group message identity fan-out
至當下 active members 的個別 durable inbox，並回傳 task 或 group message identity、context、
targets 和 delivery state。

#### Scenario: Deliver to a registered target

- **WHEN** requester 傳送符合 payload limit 且 target Agent 存在的工作
- **THEN** Hub 建立一筆 pending inbox item，配置 Hub 內單調遞增的 sequence，並回傳
  可供追蹤的 task identity

#### Scenario: Deliver to an active group

- **WHEN** group member 對 active group 傳送符合 fan-out limit 的訊息
- **THEN** Hub 建立一個 group message，為每個當下 active member 建立可獨立 ACK 的
  delivery，並回傳每個 recipient 的 delivery summary

#### Scenario: Target is unknown

- **WHEN** requester 指定不存在、已過期或已撤銷的 target Agent ID
- **THEN** Hub 拒絕建立 inbox item，並回傳穩定的 not-found 或 unavailable error

#### Scenario: Group is unavailable

- **WHEN** requester 不是 group member、group 不存在、已封存或沒有 active recipient
- **THEN** Hub 拒絕建立 group message，且不產生部分 fan-out item

### Requirement: Delivery is idempotent

Hub SHALL 以 `(hubId, targetAgentId, requesterAgentId, idempotencyKey)` 作為 direct 工作
唯一識別，並以 `(hubId, groupId, requesterAgentId, idempotencyKey)` 作為 group message
唯一識別。重複 request SHALL 回傳原本 task 或 group message 及其 delivery items，不得
增加新的 sequence、message 或 pending item。

#### Scenario: Duplicate delivery request

- **WHEN** requester 以相同 target 和 idempotency key 重送相同工作
- **THEN** Hub 回傳既有 task identity，並標示 duplicate 或等價結果，inbox 只保留
  一筆 item

#### Scenario: Same key for different target

- **WHEN** requester 對不同 target Agent 使用相同 idempotency key
- **THEN** Hub 將它們視為不同工作，各自建立一筆 inbox item

#### Scenario: Duplicate group delivery request

- **WHEN** requester 以相同 group 和 idempotency key 重送相同工作
- **THEN** Hub 回傳既有 group message identity，且 group inbox 只保留原本的 deliveries

### Requirement: Inbox polling is ordered and authenticated

只有 target Agent 可以讀取自己的 inbox。poll SHALL 支援 `afterSequence`，只回傳尚未
ACK 且 sequence 大於指定值的項目，並依 sequence 遞增排序；每次回傳 SHALL 提供可用
於下一次 poll 的 sequence cursor。Group delivery 的 recipient SHALL 只能讀取自己的 copy，
不能透過 inbox 取得其他 recipient 的 token 或私有狀態。

#### Scenario: Poll pending items

- **WHEN** authenticated target Agent 以 afterSequence 查詢 inbox
- **THEN** Hub 回傳該 Agent 可見的 pending items，包含 sequence、task、context、
  requester、message 和 created-at，且不回傳其他 Agent 的 item

#### Scenario: Poll after acknowledgement

- **WHEN** Agent ACK 一筆 item 後再次以原本 cursor poll
- **THEN** 已 ACK item 不再出現在 pending 結果中，其他尚未 ACK item 仍保留

### Requirement: ACK and cancellation are durable

每筆 inbox item SHALL 有 PENDING、ACKNOWLEDGED 或 CANCELED 的 durable delivery state。
Target Agent SHALL 可以用 sequence ACK 自己的 direct 或 group inbox item；operator 或
group owner SHALL 能依授權規則 cancel 尚未 ACK 的 group delivery。重複 ACK SHALL 維持
成功的最終狀態，未知或不屬於呼叫者的 sequence SHALL 被拒絕。

#### Scenario: Agent acknowledges an item

- **WHEN** target Agent ACK 自己 inbox 中存在的 sequence
- **THEN** Hub 記錄 acknowledged-at，後續 poll 不再回傳該 item

#### Scenario: Operator cancels pending work

- **WHEN** operator cancel 尚未 ACK 的 task
- **THEN** Hub 將該 task 設為 CANCELED 並記錄時間，不再把它視為可遞送的 pending item

#### Scenario: Agent acknowledges a group delivery

- **WHEN** target Agent ACK 自己 inbox 中的 group sequence
- **THEN** Hub 只更新該 recipient delivery 的 acknowledged-at，其他 recipient 狀態不變

#### Scenario: Group membership removal cancels pending delivery

- **WHEN** group owner 移除尚未 poll 的 member
- **THEN** Hub cancel 該 member 尚未取出的 pending group deliveries，不影響其他 recipient
  的 delivery 或已被取出的訊息

### Requirement: Unacknowledged items survive restart

Hub restart 或短暫 client disconnect 不得刪除 PENDING 的 direct 或 group inbox item。Agent
重新連線並以適當 sequence poll 時，Hub SHALL 再次提供同一 item；這種重試是 at-least-once
poll，不需要建立新的 task、group message 或 sequence。client 可以安全地重試處理，並以
ACK 結束遞送。

#### Scenario: Recover after Hub restart

- **WHEN** 工作已寫入 inbox 但 Hub 在 ACK 前重啟
- **THEN** Hub 恢復資料後，target Agent 的後續 poll 仍取得同一 task 和 sequence，且
  不建立第二筆 item

#### Scenario: Recover group deliveries after Hub restart

- **WHEN** group message 已 fan-out 但一個或多個 recipient 尚未 ACK，且 Hub 重啟
- **THEN** 各 recipient 後續 poll 仍取得原本的 message 和 sequence，且不建立第二份 delivery

### Requirement: Mailbox applies bounded payload and concurrency controls

Hub SHALL 對 message body、request body、每分鐘 task 數量、同時 pending task、group size、
單次 fan-out recipient 數量和 group history page size 套用可設定但有上限的限制；超限
request SHALL 在任何 partial write 前被拒絕。

#### Scenario: Payload exceeds configured limit

- **WHEN** message 或 request body 超過 Hub policy 的上限
- **THEN** Hub 回傳 payload-too-large error，且資料庫沒有新增 task

#### Scenario: Task rate exceeds configured limit

- **WHEN** requester 超過 policy 的 task rate 或 concurrency limit
- **THEN** Hub 拒絕新 task，既有 inbox item 保持不變

#### Scenario: Group fan-out exceeds configured limit

- **WHEN** group member 對超過 group size 或 fan-out limit 的群組發送訊息
- **THEN** Hub 回傳 bounded limit error，且資料庫沒有新增 group message 或 delivery

### Requirement: Recipient can subscribe to real-time inbox SSE stream

已驗證的 Agent SHALL 可以透過 `GET /hub/v1/agents/{agentId}/inbox/stream` 建立 `text/event-stream` 持續連線。Hub SHALL 在建立連線時自動發送尚未 ACK 的 pending inbox items，並在有新的 direct task 或 group delivery 抵達時，立即將該事件主動推送到 target Agent 的串流中。

#### Scenario: Stream pending tasks on connect
- **WHEN** 已註冊且已驗證的 Agent 建立 SSE stream 連線且收件匣存在未 ACK 的任務
- **THEN** Hub 立即向 stream 輸出尚未 ACK 的 inbox items，每個 event 包含 `id: <sequence>`、`event: task` 與包含 taskId、senderAgentId、contextId、body、createdAt 的 JSON payload

#### Scenario: Real-time task push upon creation
- **WHEN** requester 發送 direct task 或 group message 給已維持 SSE 連線的 target Agent
- **THEN** Hub 將任務寫入 SQLite durable inbox 後，立即將該 task 推送至 target Agent 的活躍 SSE stream，無需等待 target 輪詢

#### Scenario: Stream keep-alive and lease extension
- **WHEN** SSE stream 保持開啟但暫無新訊息
- **THEN** Hub 定期發送 `: keepalive` 註釋或 ping 事件，避免代理伺服器連線逾時，並自動展延該 Agent 的在線租約

#### Scenario: Resume stream from Last-Event-ID or query parameter
- **WHEN** Agent 斷線重連並提供 `Last-Event-ID` 標頭或 `?afterSequence=N` 參數
- **THEN** Hub 自該 sequence 之後的第一筆未 ACK 項目開始串流推送，不遺漏任何新訊息，亦不重複發送已過濾的舊序號

### Requirement: Mailbox records correlate standard A2A tasks and replies

Hub SHALL 為由 standard Gateway 建立的 mailbox item 持久化標準 task ID、message ID、context ID、requester、target 和 reply correlation。Mailbox redelivery、ACK、restart recovery 和 idempotency SHALL 保持相同 standard task identity；target adapter 回傳 correlated reply 時，Hub SHALL 更新對應 standard Task，而不得建立無法關聯的孤立結果。

#### Scenario: Redelivery preserves standard task identity

- **WHEN** standard task 對應的 inbox item 因 SSE reconnect 或未 ACK 被重送
- **THEN** client 看到相同 task ID、message ID、context ID 和 sequence，不產生 duplicate standard task

#### Scenario: Correlated reply completes the task

- **WHEN** target adapter 以原始 standard task correlation 回傳 reply
- **THEN** Hub 將原始 Task 更新為 completed 並保存可由 `GET /tasks/{id}` 或 stream 取得的標準 Message/Artifact result

### Requirement: Executor reports durable outcomes explicitly

Hub SHALL 提供自訂 `POST /hub/v1/a2a/tasks/{taskId}/updates`，只接受指定 target 的 Agent credential。update SHALL 含 updateId、turnId、expectedRevision、state 和可選 message/artifacts。同 updateId 同內容 SHALL 冪等；改內容、過期 turn 或不符 revision SHALL 拒絕。Task/result/event SHALL 同交易提交，終態不可覆寫。

#### Scenario: Executor finishes without a reply
- **WHEN** Anti-Echo 或 NO_REPLY 結束本機工作
- **THEN** Bridge 先持久化 COMPLETED 空結果回報，重試至 Hub 確認才完成本機工作，不另發回信

#### Scenario: Unauthorized executor submits result
- **WHEN** 非指定 target 嘗試回報，即使同圈且知道 taskId
- **THEN** Hub 拒絕且不改動 Task

### Requirement: Executor readiness and bounded lifetime are enforced

只有宣告 versioned execution capability 的 target SHALL 接收 standard Task；舊 Bridge SHALL 保持 legacy flow。工作 SHALL 具有有限 execution deadline/retry budget，超限回報 FAILED；政策拒絕回 REJECTED。HTTP deadline SHALL 不改變工作狀態。Task/result/event SHALL 在記錄的 retention 期限內保留，不因 Agent 自動清理而刪除。

#### Scenario: Old bridge is targeted
- **WHEN** standard client 指定未宣告 execution capability 的 Agent
- **THEN** Hub 拒絕建立 standard Task，該 Agent 仍可使用原 direct mailbox

#### Scenario: Executor disappears
- **WHEN** Task 超過持久化 execution deadline
- **THEN** Hub 原子標記 FAILED，遲到更新不得覆寫，重啟後結果仍可查詢
