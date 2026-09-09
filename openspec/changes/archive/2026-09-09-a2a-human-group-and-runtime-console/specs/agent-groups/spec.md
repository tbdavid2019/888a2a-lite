## ADDED Requirements

### Requirement: Human Agent participates through existing group authorization

Group standard dispatch SHALL 將 Human UI 的 Agent identity 視為一般 group member，沿用 active accepted membership、circle scope、sender exclusion 和既有 group role policy。Human UI 不得用 local session 取代 Hub Agent Token。

#### Scenario: Human Agent sends to a group

- **WHEN** active accepted Human Agent 透過 standard Group Gateway 發送
- **THEN** Hub 接受其為 requester，建立符合第二期 contract 的 Parent Task 和 Member Deliveries

#### Scenario: Human Agent loses membership

- **WHEN** Human Agent 被移除、退出、過期或所在 circle 停用
- **THEN** 後續 group list/card/history/send 全部依 Hub 授權拒絕，local UI 不保留可操作的 active composer

### Requirement: Mention policy delivers to all eligible members

`MENTIONED_ONLY` SHALL 對所有 eligible members 建立 delivery；未被提及成員以 ACK-only 完成，被提及成員才取得 execution lease。`ACK_ONLY` SHALL 對所有 eligible members 建立空結果完成流程。此行為 SHALL 不改變既有 legacy group API 的 sender exclusion。

#### Scenario: Unmentioned members are readable but silent

- **WHEN** Human message mentions only Agent A
- **THEN** Agent A 可執行並回報，其他 eligible members 都收到、ACK、完成空結果，且不產生 LLM reply

### Requirement: Group task results remain correlated

Human UI 所見的 bot result SHALL 使用第二期 Parent/Member Task correlation、monotonic revision 和 member identity。bot reply 不得再次 fan-out 到 group tenant；group parent cancellation、late update 和 terminal state SHALL 按第二期規則處理。

#### Scenario: Multiple bots reply concurrently

- **WHEN** 多個被 mention bot 同時回報結果
- **THEN** UI 依 revision/member ordering 顯示所有結果，不覆寫或重複任何成員結果
