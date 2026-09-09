## ADDED Requirements

### Requirement: System metadata distinguishes standard A2A from Hub extensions

Hub system declaration SHALL 能讓 client 分辨標準 A2A HTTP+JSON interface、custom `/hub/v1` contract、Multi-Circle extension、group extension 和不支援的 push notification/extended card capability。Hub SHALL 不得把 custom mailbox ACK 或 group routes 宣稱為 A2A core operation。

#### Scenario: Client selects the standard interface

- **WHEN** client 讀取 system metadata 或 `/.well-known/agent-card.json`
- **THEN** client 可以找到 standard HTTP+JSON base URL、protocol version、authentication requirements 和 supported capabilities，並能忽略 custom extensions
