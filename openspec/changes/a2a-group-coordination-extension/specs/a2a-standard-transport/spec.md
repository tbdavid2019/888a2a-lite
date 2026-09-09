## ADDED Requirements

### Requirement: Standard transport supports negotiated group tenant operations

Standard A2A transport SHALL 支援由 Group Extension 宣告的 `group:<groupId>` tenant、`A2A-Extensions` opt-in、Parent/Member Task correlation、group StreamResponse aggregation 與 parent cancellation。既有 P2P tenant、Task、stream、error、Bearer 与 Multi-Circle 行為 SHALL 不被改變。

#### Scenario: Group extension uses standard transport

- **WHEN** extension-aware client 以合法 Group Extension header 發送 group tenant Message
- **THEN** Gateway 使用標準 SendMessageResponse/StreamResponse envelope，並將 group-specific progress 放在 extension metadata 或 artifacts

#### Scenario: P2P remains unaffected

- **WHEN** client 發送普通 Agent tenant Message 且沒有 Group Extension header
- **THEN** Gateway 仍依第一期 P2P contract 處理，不要求或建立群組資料
