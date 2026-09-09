## ADDED Requirements

### Requirement: Distributed bridges share charter and secretary behavior

`examples/worker/a2a_bridge.py` 與 embedded `internal/service/a2a_bridge.py` SHALL 具備相同的 Charter cache scope、safe prompt assembly、secretary lease check、session cutoff、structured output、approval 和 outbox behavior。`--role=secretary` 只表達執行模式，不能繞過 Hub appointment。

#### Scenario: Embedded and downloaded bridge stay in parity

- **WHEN** CI 比對 source bridge 與 Hub `/a2a_bridge.py` embedded asset
- **THEN** 兩者的 governance schema、security policy 和 retry behavior 一致

#### Scenario: Secretary reconnects after offline period

- **WHEN** secretary Bridge 離線後重新啟動
- **THEN** 它先驗證 lease/epoch 與 Charter version，再從 durable revision 恢復，避免重複摘要或使用過期政策
