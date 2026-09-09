## ADDED Requirements

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
