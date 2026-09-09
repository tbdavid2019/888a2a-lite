## ADDED Requirements

### Requirement: Standard A2A routes enforce the same circle boundary

Standard Gateway SHALL 使用 authenticated Agent persisted `circleId` 做 requester scope，並以 target `tenant` 的 persisted circle 做 authorization。跨圈 standard message、task lookup、stream、subscribe 和 cancel SHALL 使用 masked 404 或等價不洩漏存在資訊的 error；Operator 可以依既有 control-plane 權限查看全域資料。

#### Scenario: Standard message cannot cross circles

- **WHEN** public Agent 以 standard `message:send` 將 `tenant` 設為 private-circle Agent
- **THEN** Hub 回傳 masked 404，且不建立 standard Task 或 mailbox item

#### Scenario: Standard task remains circle-scoped

- **WHEN** Agent 從另一個 circle 以 task ID 查詢或 subscribe
- **THEN** Hub 回傳 masked 404，不回傳 task status、message、artifact 或 target metadata

### Requirement: Cards and task streams enforce requester access

根卡 SHALL 公開；Per-Agent Card SHALL 需 Bearer 並限制同圈，未知與跨圈目標一致 404。Task get/list/subscribe/cancel SHALL 僅原 requester 可用，同圈不等於 Task 存取權。串流 SHALL 在送資料前重新驗證授權，且至少每 15 秒檢查撤銷；失效 SHALL 關閉，不再推送資料。

#### Scenario: Peer attempts to read another requester task
- **WHEN** 同圈另一 Agent 查詢或訂閱該 Task
- **THEN** Hub 回 404，無 task/message/artifact 洩漏

#### Scenario: Circle disabled during open stream
- **WHEN** 已訂閱的 requester 所屬 Circle 停用
- **THEN** 後續資料不得送出，串流至遲於下次 15 秒授權檢查關閉
