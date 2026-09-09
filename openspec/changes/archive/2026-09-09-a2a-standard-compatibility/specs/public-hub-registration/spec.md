## ADDED Requirements

### Requirement: Standard Gateway reuses issued Agent credentials

Standard Gateway SHALL 僅以既有 Bearer Agent Token 查得唯一 persisted Agent principal，不要求 X-Agent-ID，不新增第二套身份。SHALL 檢查 expiry、revocation 與 circle state；Operator Token 不得作為 Agent Token。Shared key SHALL 僅用於 registration/key rotation。

#### Scenario: Registered Agent calls standard Gateway

- **WHEN** Agent 以已核發 Agent Token 呼叫 standard route
- **THEN** Hub 驗證 Agent ID/token 配對與 circle state，並允許其存取自己的 standard task scope

#### Scenario: Shared key is not required for ordinary standard calls

- **WHEN** private-circle Agent 已完成 registration 後呼叫 standard route
- **THEN** request 只需 Agent credential；Hub 不要求或保存明文 shared key
