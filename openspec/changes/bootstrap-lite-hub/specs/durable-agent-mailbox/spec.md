## Purpose

提供以目標 Agent ID 尋址的 durable mailbox，讓通知和工作在 Agent 暫時離線時仍能
保留，並透過序號、ACK、重試和 idempotency 支援可靠且不重複的遞送。

## ADDED Requirements

### Requirement: Direct delivery creates a durable inbox item

已驗證的 requester SHALL 可以指定 target Agent ID、context ID、idempotency key、
message 和 task ID 送出工作。Hub SHALL 將有效工作寫入 target 的 durable inbox，並
回傳 task ID、context ID、target 和 delivery state。

#### Scenario: Deliver to a registered target

- **WHEN** requester 傳送符合 payload limit 且 target Agent 存在的工作
- **THEN** Hub 建立一筆 pending inbox item，配置 Hub 內單調遞增的 sequence，並回傳
  可供追蹤的 task identity

#### Scenario: Target is unknown

- **WHEN** requester 指定不存在、已過期或已撤銷的 target Agent ID
- **THEN** Hub 拒絕建立 inbox item，並回傳穩定的 not-found 或 unavailable error

### Requirement: Delivery is idempotent

Hub SHALL 以 `(hubId, targetAgentId, requesterAgentId, idempotencyKey)` 作為唯一工作
識別。重複 request SHALL 回傳原本 task 和 inbox item，不得增加新的 sequence 或
pending item。

#### Scenario: Duplicate delivery request

- **WHEN** requester 以相同 target 和 idempotency key 重送相同工作
- **THEN** Hub 回傳既有 task identity，並標示 duplicate 或等價結果，inbox 只保留
  一筆 item

#### Scenario: Same key for different target

- **WHEN** requester 對不同 target Agent 使用相同 idempotency key
- **THEN** Hub 將它們視為不同工作，各自建立一筆 inbox item

### Requirement: Inbox polling is ordered and authenticated

只有 target Agent 可以讀取自己的 inbox。poll SHALL 支援 `afterSequence`，只回傳尚未
ACK 且 sequence 大於指定值的項目，並依 sequence 遞增排序；每次回傳 SHALL 提供可用
於下一次 poll 的 sequence cursor。

#### Scenario: Poll pending items

- **WHEN** authenticated target Agent 以 afterSequence 查詢 inbox
- **THEN** Hub 回傳該 Agent 可見的 pending items，包含 sequence、task、context、
  requester、message 和 created-at，且不回傳其他 Agent 的 item

#### Scenario: Poll after acknowledgement

- **WHEN** Agent ACK 一筆 item 後再次以原本 cursor poll
- **THEN** 已 ACK item 不再出現在 pending 結果中，其他尚未 ACK item 仍保留

### Requirement: ACK and cancellation are durable

每筆 inbox item SHALL 有 PENDING、ACKNOWLEDGED 或 CANCELED 的 durable delivery state。
Target Agent SHALL 可以用 sequence ACK 自己的 inbox item；operator SHALL 可以依 task
ID cancel 尚未 ACK 的工作。重複 ACK SHALL 維持成功的最終狀態，未知或不屬於呼叫者
的 sequence SHALL 被拒絕。

#### Scenario: Agent acknowledges an item

- **WHEN** target Agent ACK 自己 inbox 中存在的 sequence
- **THEN** Hub 記錄 acknowledged-at，後續 poll 不再回傳該 item

#### Scenario: Operator cancels pending work

- **WHEN** operator cancel 尚未 ACK 的 task
- **THEN** Hub 將該 task 設為 CANCELED 並記錄時間，不再把它視為可遞送的 pending item

### Requirement: Unacknowledged items survive restart

Hub restart 或短暫 client disconnect 不得刪除 PENDING 的 inbox item。Agent 重新連線並
以適當 sequence poll 時，Hub SHALL 再次提供同一 item；這種重試是 at-least-once
poll，不需要建立新的 task 或 sequence。client 可以安全地重試處理，並以 ACK 結束遞送。

#### Scenario: Recover after Hub restart

- **WHEN** 工作已寫入 inbox 但 Hub 在 ACK 前重啟
- **THEN** Hub 恢復資料後，target Agent 的後續 poll 仍取得同一 task 和 sequence，且
  不建立第二筆 item

### Requirement: Mailbox applies bounded payload and concurrency controls

Hub SHALL 對 message body、request body、每分鐘 task 數量和同時 pending task 套用可
設定但有上限的限制；超限 request SHALL 在寫入 inbox 前被拒絕。

#### Scenario: Payload exceeds configured limit

- **WHEN** message 或 request body 超過 Hub policy 的上限
- **THEN** Hub 回傳 payload-too-large error，且資料庫沒有新增 task

#### Scenario: Task rate exceeds configured limit

- **WHEN** requester 超過 policy 的 task rate 或 concurrency limit
- **THEN** Hub 拒絕新 task，既有 inbox item 保持不變
