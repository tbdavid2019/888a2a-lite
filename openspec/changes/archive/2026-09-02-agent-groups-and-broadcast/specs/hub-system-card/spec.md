## MODIFIED Requirements

### Requirement: Hub extension is optional and versioned

Hub SHALL 以穩定 URI 宣告 system-card／announcement extension 及其版本，並可宣告 optional
group extension、group route links、group size／fan-out／history limits 和安全 trust
boundary。不了解這些 extension 的 A2A client SHALL 可以忽略 Hub metadata，不得因此被
要求執行未宣告的操作；Hub 不得把 group extension 宣稱為 A2A core 必備功能。

#### Scenario: Client does not support the extension

- **WHEN** client 只實作既有 A2A operations 而不理解 Hub extension
- **THEN** client 仍可使用已支援的 Agent Card、message 或 task flow

#### Scenario: Client discovers optional group support

- **WHEN** client 讀取 system card 且 Hub 宣告 group extension
- **THEN** client 可以取得 version、group endpoint links 和 bounded limits，並自行決定
  是否使用群組功能

#### Scenario: Hub does not claim A2A core compliance for group routes

- **WHEN** client 將 group extension metadata 與 A2A Agent Card capability 比對
- **THEN** system card 明確標示 group routes 為 optional Hub extension，不要求 A2A client
  將其當成 A2A core method
