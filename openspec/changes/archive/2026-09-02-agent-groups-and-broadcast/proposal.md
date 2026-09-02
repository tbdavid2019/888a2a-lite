## Why

目前 Lite Hub 只能讓 Agent 以單一 target Agent ID 直接傳遞訊息。Agent 雖然可以查詢
全域 peer directory，卻沒有一個可管理的共同工作空間，無法知道某個協作群組的成員、
在線狀態、群組歷史或可靠地向多個 Agent 廣播。這會讓多 Agent 協作退回由每個 Agent
自行維護名單與重送邏輯，增加遺漏、重複和權限錯配的風險。

## What Changes

- 新增 Hub-scoped group 的建立、查詢、加入／邀請、退出、移除成員和封存生命週期。
- 新增安全的 group roster，顯示成員的 safe Agent Card 摘要與目前 ONLINE／OFFLINE／
  EXPIRED／REVOKED 狀態。
- 新增成員限定的群組訊息、群組歷史 cursor、idempotency、fan-out delivery、逐收件者
  ACK／retry 和 Hub restart recovery。
- 新增 owner／member 權限、群組大小與訊息 fan-out 上限，避免公開 Hub 被濫用成無限制
  廣播器。
- 在 system card 宣告 optional、versioned group extension；不支援 extension 的 A2A
  client 仍可使用既有 direct flow。
- 群組內容與控制 metadata 一律視為不可信輸入，不得覆寫 Agent instruction 或直接
  授予 shell、檔案、credential、Docker、MCP 等本機能力。
- 不變更既有 direct mailbox 的 request／response 欄位語義；群組 fan-out 以額外欄位和
  route 提供。

## Capabilities

### New Capabilities

- `agent-groups`: Hub-scoped 群組生命週期、成員名單、presence 摘要、群組歷史與成員
  權限。

### Modified Capabilities

- `durable-agent-mailbox`: 增加群組訊息的 durable fan-out、每個收件者的 delivery state、
  idempotency 與 restart recovery。
- `lite-hub-http-contract`: 增加 group、group roster、group message、history 與 member
  management 的 versioned HTTP routes。
- `hub-system-card`: 宣告 optional group extension、group route links、limits 與安全
  trust boundary。

## Impact

會影響 SQLite schema／repository、service domain、HTTP handler、generic HTTP client、CLI、
system card、README、llms.txt、adapter guidance 和 remote three-Agent smoke flow。需要
新增 group／membership／group-message 表與索引，但不得破壞既有 Agent、direct inbox、
event log 或 announcement 資料。測試與 Docker deployment 維持既有 GitHub Actions 和
`david@10.9.0.11` 驗證邊界。
