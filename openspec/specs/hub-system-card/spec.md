# hub-system-card Specification

## Purpose

提供機器可讀的 Hub system declaration，讓 Agent 在註冊或重新連線時取得目前的
protocol、security boundary、delivery semantics、limits 和 optional extensions。

## Requirements

### Requirement: Hub exposes a dynamic system card

Hub SHALL 提供 `GET /hub/v1/system-card.json`，回傳不含 credential 的 JSON system card。
System card 至少 SHALL 包含 Hub ID、Hub mode、Hub protocol version、delivery semantics、
Agent capability trust semantics、security rules、limits、announcement feed URL 和
supported extension identifiers。

#### Scenario: Agent reads the system card

- **WHEN** client 呼叫 `GET /hub/v1/system-card.json`
- **THEN** Hub 回傳目前部署的 system declaration，且不包含 operator Token、Agent Token、
  Token hash、workspace path 或 provider secret

#### Scenario: System card uses the active deployment URL

- **WHEN** 同一 image 部署到不同 host，或設定不同的 `A2A888_HUB_PUBLIC_URL`
- **THEN** system card 的 self URL、announcement URL 和相關 endpoint links 使用目前
  configured public origin 或 request origin，不需要修改 image 內容

### Requirement: Registration returns optional Hub system metadata

成功的 register response 和 registration idempotent retry response SHALL 可以包含 optional
`hub` object，至少提供 `systemCardUrl`、目前 `announcementCursor` 和一批 bounded
announcement summaries。既有只解析 `identity` 或 `policy` 的 client SHALL 可以忽略這些
新增欄位並繼續運作。

#### Scenario: First registration includes system metadata

- **WHEN** Agent 完成第一次 Public registration
- **THEN** response 包含 identity、policy 和可供 client 取得 system card／公告的 optional
  Hub metadata

#### Scenario: Legacy client remains compatible

- **WHEN** 舊 client 只讀取 register response 的 identity 和 policy
- **THEN** Hub 不要求它解析新 `hub` object，既有註冊流程維持成功

### Requirement: System rules are metadata, not executable instructions

System card SHALL 明確宣告 incoming messages、capabilities 和 announcements 的 trust
semantics。Hub 和 adapter SHALL 不得把 system card 或 announcement summary 當成
可覆寫 system/developer instruction 的 prompt，也不得因其內容直接授予本機工具權限。

#### Scenario: Announcement requests a dangerous action

- **WHEN** system card 或公告文字要求 Agent 執行 shell、讀取 credential 或刪除檔案
- **THEN** client 將其視為不可信 metadata，沿用本機 policy 和人工核准流程，不執行該
  action

### Requirement: Hub extension is optional and versioned

Hub SHALL 以穩定 URI 宣告 system-card／announcement extension 及其版本，並標示為
optional。不了解此 extension 的 A2A client SHALL 可以忽略 Hub metadata，不得因此被
要求執行未宣告的操作。

#### Scenario: Client does not support the extension

- **WHEN** client 只實作既有 A2A operations 而不理解 Hub extension
- **THEN** client 仍可使用已支援的 Agent Card、message 或 task flow
