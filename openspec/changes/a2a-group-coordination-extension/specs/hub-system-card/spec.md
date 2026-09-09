## ADDED Requirements

### Requirement: System cards advertise the Group Extension with standard objects

`/hub/v1/system-card.json` 與 `/.well-known/agent-card.json` SHALL 宣告 Group Coordination Extension 的 URI、版本、`group:` tenant prefix、discovery/Card URL template、group member limit、fan-out limit 與 reply policy。標準 Agent Card 的 `capabilities.extensions` SHALL 使用 `AgentExtension` objects；`A2A-Extensions` SHALL 僅作為 client request opt-in。

#### Scenario: Client reads group capability

- **WHEN** client 讀取 system card 或 root Agent Card
- **THEN** 可以區分 standard A2A core、Group Extension 與 `/hub/v1` custom routes，並取得 bounded group limits

#### Scenario: Client does not opt into group extension

- **WHEN** client 只使用 P2P standard Message/Task
- **THEN** Hub 不要求 group extension header，P2P flow 維持可用
