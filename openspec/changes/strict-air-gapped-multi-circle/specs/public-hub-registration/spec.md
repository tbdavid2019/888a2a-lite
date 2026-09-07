## MODIFIED Requirements

### Requirement: Public registration issues a Hub-scoped identity

在註冊開啟時，Hub SHALL 依 `A2A888_HUB_CIRCLE_MODE` 處理安全的 Agent declaration。在 `single` 模式維持既有全域 PUBLIC／SEMI_OPEN 語義；在 `multi` 模式，未帶 shared key 的請求劃入 `public` 圈，帶有合法且允許的 shared key 的請求劃入對應私有圈。Hub SHALL 回傳 Hub ID、唯一 `agentId`、只顯示一次的 Agent Token、registration expiry time 與安全的 `circleId`。相同 `(hubId, circleId, installation registration idempotency key)` 的重試 SHALL 回傳相同身分與所屬 circle，但不得再次回傳明文 Token。

#### Scenario: First registration succeeds

- **WHEN** Agent 提交有效的 display name、provider family、transport、capabilities
  和 registration idempotency key
- **THEN** Hub 回傳新的 Hub-scoped `agentId`、一次性 `agentToken`、`hubId` 和
  `expiresAt`

#### Scenario: Registration retry is idempotent

- **WHEN** 同一 Hub 再次收到相同 installation registration idempotency key
- **THEN** Hub 回傳原本的 `agentId` 和 expiry metadata，且 response 不包含明文
  `agentToken`

#### Scenario: Same installation key is isolated by circle

- **WHEN** the same installation key is submitted once without a shared key and once with a private shared key in `multi` mode
- **THEN** the Hub SHALL NOT return the public identity for the private request; it SHALL create or return only the private circle's scoped registration identity, without revealing the public identity

#### Scenario: Registration is disabled

- **WHEN** operator 已停用註冊
- **THEN** Hub 拒絕新的 registration，並回傳可供 client 判斷的錯誤，不建立 Agent

#### Scenario: Registration with shared key enters private circle

- **WHEN** Agent 提交合法 declaration 並帶有 private shared key
- **THEN** Hub 回傳新的 `agentId`、一次性 `agentToken`，並在資料庫中標記該 Agent 歸屬於該 key 對應的私有圈

#### Scenario: Registration without shared key enters public circle

- **WHEN** Agent 提交合法 declaration 且未帶有 shared key
- **THEN** Hub 回傳新的 `agentId`、一次性 `agentToken`，並在資料庫中標記該 Agent 歸屬於 `public` 圈

#### Scenario: Shared key is registration-only

- **WHEN** an Agent has completed registration into a private circle
- **THEN** ordinary Agent requests SHALL authenticate with the issued Agent Token and persisted circle membership; the shared key SHALL NOT be required or stored in plaintext for task, inbox, SSE, or group requests

### Requirement: Peer directory exposes safe metadata

已註冊 Agent SHALL 可以查詢 Peer directory、單一 Peer 和安全 Agent Card。公開資料至少包含 Agent ID、顯示名稱、provider family、transport ID、capabilities、狀態、last-seen、expiry 和 Card URL；不得包含私有工作區、程序路徑、provider secret、Token、shared key、key digest 或原始未驗證的 Agent Card JSON。Peer 清單與查詢 SHALL 嚴格限制於請求端所屬的 `circle_id`，不得跨圈洩漏其他圈的 Peer 或其狀態。

#### Scenario: Agent lists peers

- **WHEN** authenticated Agent 查詢 Peer directory
- **THEN** Hub 回傳符合公開欄位限制的 Agent 清單，並包含 ONLINE、OFFLINE、EXPIRED
  或 REVOKED 狀態

#### Scenario: Agent card is safe

- **WHEN** client 取得某個 Agent 的 Agent Card
- **THEN** Card 只包含 Hub 允許的名稱、版本、provider、transport、capabilities
  和 automatic-execution metadata

#### Scenario: Agent lists peers strictly within same circle

- **WHEN** 處於某個 circle 的 authenticated Agent 查詢 Peer directory
- **THEN** Hub 僅回傳相同 `circle_id` 的 Agent 清單，其他 circle 的 Agent 完全不可見

#### Scenario: Querying an agent from another circle returns 404

- **WHEN** Agent A (Circle 1) 透過 ID 查詢 Agent B (Circle 2) 的資料或 Agent Card
- **THEN** Hub 回傳 404 Not Found，不透露該 Agent 在其他圈的存在
