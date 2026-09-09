## Why

A2A 1.0.0 定義了 Message、Task 和 streaming，但沒有群組廣播與成員協作語義。本專案既有 `/hub/v1/groups` 已具備群組成員、邀請、歷史與 fan-out；第二期要讓支援本 extension 的 A2A client 能用標準 Message/Task binding 呼叫群組。

本變更建立 `https://a2a.david888.com/extensions/groups/v1`，以虛擬群組 Agent 作為群組入口，並將每次廣播拆成可追蹤的 Parent Task 與 Member Delivery。它借鑑 Buzz 的 relay、channel membership 和事件可追蹤方向，但本期聚焦群組協調，不宣稱提供完整 workspace/channel 平台。

## What Changes

- 定義 A2A Group Coordination Extension，並以正確的 `AgentExtension` object 與 `A2A-Extensions` request opt-in 宣告。
- 提供有界的群組 discovery 與 Per-Group Agent Card；群組 tenant 固定使用 `group:<groupId>`。
- 將 standard `message:send`、`message:stream` 映射到 Parent Task 和 Member Delivery，保存成員快照、冪等鍵、進度與聚合結果。
- 支援 `returnImmediately`、多成員狀態聚合、群組 cancellation、stream reconnect 與遲到結果保護。
- 由 Bridge 執行 Instant durable ACK、明確 reply policy 與 Anti-Echo；Hub 不以自然語言自行判讀是否回覆。
- 嚴格沿用 Multi-Circle、Agent Token、成員角色、group size/fan-out limits 與既有 `/hub/v1/groups` 授權。
- 保持 `/hub/v1/groups` 行為和既有 P2P A2A contract 不變。
- 以固定 A2A specification revision、官方 SDK 及 extension-aware fixtures 作為驗收 gate。

## Capabilities

### New Capabilities

- `a2a-group-extension`: 定義虛擬群組 Agent、Group Card、群組 discovery、fan-out、成員結果聚合、extension negotiation 和 Anti-Echo policy。

### Modified Capabilities

- `a2a-standard-transport`: standard Message/Task/Stream 增加 group tenant、extension opt-in、Parent/Member correlation 和 group cancellation 映射。
- `agent-groups`: 既有群組增加 standard virtual tenant、成員快照、delivery aggregation 與群組任務生命週期。
- `hub-system-card`: system declaration 與根 Agent Card 增加正確的 Group Extension capability advertisement。

## Impact

- 影響 `internal/hub`、`internal/service`、`internal/store`、standard Gateway、群組路由、Bridge adapter、Agent Card 與 CI interoperability fixtures。
- 不新增外部大型訊息佇列或資料庫；沿用 SQLite WAL 與現有 broker，並以 durable event/revision 支援重連。
- 需要先完成 `a2a-standard-compatibility` 並取得官方 SDK 綠燈；第二期不可繞過第一期的標準 Task/結果回報契約。
- 不實作 Buzz 的 reaction、thread、搜尋、workflow、git forge、voice huddle、signed Nostr event 或完整 workspace surface。
