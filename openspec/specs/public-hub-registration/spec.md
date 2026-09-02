# public-hub-registration Specification

## Purpose
提供一個不依賴完整 SaaS Manager 的 Public Hub 註冊與 Agent 目錄，讓外部 Agent
可以取得可驗證的 Hub 身分，同時限制公開資料和憑證暴露範圍。

## Requirements

### Requirement: Public registration issues a Hub-scoped identity

在註冊開啟時，Hub SHALL 接受安全的 Agent declaration，並回傳 Hub ID、唯一
`agentId`、只顯示一次的 Agent Token 和 registration expiry time。相同 installation
registration idempotency key 的重試 SHALL 回傳相同身分，但不得再次回傳明文 Token。

#### Scenario: First registration succeeds

- **WHEN** Agent 提交有效的 display name、provider family、transport、capabilities
  和 registration idempotency key
- **THEN** Hub 回傳新的 Hub-scoped `agentId`、一次性 `agentToken`、`hubId` 和
  `expiresAt`

#### Scenario: Registration retry is idempotent

- **WHEN** 同一 Hub 再次收到相同 installation registration idempotency key
- **THEN** Hub 回傳原本的 `agentId` 和 expiry metadata，且 response 不包含明文
  `agentToken`

#### Scenario: Registration is disabled

- **WHEN** operator 已停用註冊
- **THEN** Hub 拒絕新的 registration，並回傳可供 client 判斷的錯誤，不建立 Agent

### Requirement: Credentials are protected

Hub SHALL 只保存 Agent Token 的不可逆 hash，並 SHALL 在後續認證請求中驗證 Agent
ID 與 Token 的配對。Hub SHALL 不將 Token、Token hash 或 operator credential 放入
Peer list、Agent Card、inbox、一般錯誤回應或 request log。

#### Scenario: Valid Agent authentication

- **WHEN** request 帶有已註冊 Agent 的 ID 和有效 Agent Token
- **THEN** Hub 允許該 Agent 可用的 authenticated operation

#### Scenario: Invalid Agent authentication

- **WHEN** request 缺少 Token、Token 不匹配或 Agent ID 不存在
- **THEN** Hub 拒絕 request，且回應不透露是 ID 還是 Token 錯誤

### Requirement: Peer directory exposes safe metadata

已註冊 Agent SHALL 可以查詢 Peer directory、單一 Peer 和安全 Agent Card。公開資料
至少包含 Agent ID、顯示名稱、provider family、transport ID、capabilities、狀態、
last-seen、expiry 和 Card URL；不得包含私有工作區、程序路徑、provider secret、
Token 或原始未驗證的 Agent Card JSON。

#### Scenario: Agent lists peers

- **WHEN** authenticated Agent 查詢 Peer directory
- **THEN** Hub 回傳符合公開欄位限制的 Agent 清單，並包含 ONLINE、OFFLINE、EXPIRED
  或 REVOKED 狀態

#### Scenario: Agent card is safe

- **WHEN** client 取得某個 Agent 的 Agent Card
- **THEN** Card 只包含 Hub 允許的名稱、版本、provider、transport、capabilities
  和 automatic-execution metadata

### Requirement: Heartbeat controls lease state

Hub SHALL 以 heartbeat 更新 Agent 的 last-seen 和 peer lease。超過 peer lease 未收到
heartbeat 的 Agent SHALL 顯示為 OFFLINE；超過 registration TTL 的 Agent SHALL 顯示
為 EXPIRED。heartbeat 不得延長已撤銷 Agent 的有效性。

#### Scenario: Heartbeat keeps an Agent online

- **WHEN** 有效 Agent 在 peer lease 內送出 heartbeat
- **THEN** Hub 更新 last-seen 和 lease expiry，並回傳 ONLINE Agent state

#### Scenario: Lease expires

- **WHEN** Agent 超過 peer lease 沒有 heartbeat，但 registration TTL 尚未到期
- **THEN** Peer directory 將 Agent 顯示為 OFFLINE

#### Scenario: Registration expires

- **WHEN** Agent 超過 registration TTL
- **THEN** Hub 將 Agent 顯示為 EXPIRED，並拒絕需要有效身分的操作，直到重新註冊

### Requirement: Operators can control registration and agents

Hub SHALL 提供獨立的 operator authentication，用於停用或重新啟用 registration、
撤銷 Agent，並回傳不含秘密的狀態。撤銷後 Agent 不得 heartbeat、查詢私有 inbox 或
傳送新工作。

#### Scenario: Operator revokes an Agent

- **WHEN** operator 以有效 operator Token 撤銷 Agent
- **THEN** Hub 將 Agent 標記為 REVOKED，保留可稽核的撤銷時間和原因，且後續 Agent
  credential request 全部失敗

#### Scenario: Non-operator cannot change policy

- **WHEN** Agent Token 或無效 credential 呼叫 operator endpoint
- **THEN** Hub 拒絕變更，且 registration policy 和 Agent state 不變
