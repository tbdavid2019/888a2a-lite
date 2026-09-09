# a2a-group-extension Specification

## Purpose
定義可與 A2A HTTP+JSON Gateway 共存的群組協調擴充，讓支援 extension 的 client 能發現群組、以虛擬 tenant 廣播，並取得可追蹤的成員結果聚合。

## Requirements

### Requirement: Hub advertises and negotiates the Group Extension

Hub SHALL 在根 Agent Card 與 Group Card 的 `capabilities.extensions` 宣告 `AgentExtension` object，URI 固定為 `https://a2a.david888.com/extensions/groups/v1`，並包含 description、`required: false` 和 `params.tenantPrefix: "group:"`。使用群組 tenant 的 request SHALL 以 `A2A-Extensions` header opt in；缺少或不支援 extension 時 SHALL 回傳 `ExtensionSupportRequiredError`，一般 P2P request 不受影響。

#### Scenario: Client discovers group extension

- **WHEN** client 讀取根 Agent Card
- **THEN** response 以 `AgentExtension` object 宣告 URI、群組 tenant prefix、版本與能力，不以單純 URI 字串取代標準結構

#### Scenario: Group request without opt in is rejected

- **WHEN** client 使用 `tenant: "group:<groupId>"` 但沒有宣告 Group Extension
- **THEN** Hub 回傳可判斷的 extension support error，且不建立 task 或 mailbox delivery

### Requirement: Clients can discover active groups and Group Cards

Hub SHALL 提供 extension endpoint `GET /a2a/v1/groups`，只回傳 requester 所屬 circle 中 active group 的 bounded reference、display name、member count 和 Card URL。`GET /a2a/v1/groups/{groupId}/card` SHALL 回傳該 active group 的標準 Agent Card，interface tenant 為 `group:<groupId>`。Group Card SHALL 不包含成員 ID、Token、shared key、key digest 或其他 private infrastructure data；archived/disabled group SHALL 回傳 404 與 `Cache-Control: private, no-store`。

#### Scenario: Member lists groups

- **WHEN** authenticated member 以 bounded pagination 呼叫 group discovery
- **THEN** Hub 只回傳該 member 同 circle 且可見的 active groups 與 Group Card URLs

#### Scenario: Member reads Group Card

- **WHEN** authenticated active member 讀取所屬 group 的 Card
- **THEN** Hub 回傳標準 Card，含 HTTP+JSON interface、`tenant: "group:<groupId>"`、Group Extension 與 group coordination skill

#### Scenario: Cross-circle group is hidden

- **WHEN** Agent 從其他 circle 查詢 group list、Group Card 或已知 group ID
- **THEN** Hub 統一回傳 404，不回傳 group name、member count、state 或存在性線索

### Requirement: Group tenant creates an idempotent Parent Task and member snapshot

支援 extension 的 client 對 `tenant: "group:<groupId>"` 發送標準 `message:send` 或 `message:stream` 時，Hub SHALL 驗證 requester 是 active accepted member、Message Part 支援、group 尚未封存，並在同一 SQLite transaction 建立一個 Parent Task 與當下 eligible member snapshot。eligible member SHALL 為同 circle、active accepted、具 versioned standard executor capability 的成員；發送者沿既有 group 語義不收到自己的 delivery。任一容量、fan-out、成員或 idempotency 預檢失敗 SHALL all-or-nothing，不產生 partial delivery。

#### Scenario: Member broadcasts to group tenant

- **WHEN** active member 發送合法 text Message 到 group tenant
- **THEN** Hub 建立 Parent Task，為 snapshot 中每個其他 eligible member 建立一筆 correlated Member Delivery，並回傳標準 SendMessageResponse

#### Scenario: Group has no eligible recipient

- **WHEN** group 沒有其他 eligible member
- **THEN** Hub 回傳 bounded group error，且不建立 Parent Task 或 mailbox delivery

#### Scenario: Broadcast retry is idempotent

- **WHEN** requester 以相同 circle、group、target、messageId 與相同內容重送
- **THEN** Hub 回傳既有 Parent Task，不新增 Parent、Member Delivery、sequence 或通知事件；相同 key 不同內容回傳 conflict

### Requirement: Parent and Member Task outcomes are aggregated

Hub SHALL 保存 Parent Task 與 Member Delivery 的 task/turn/message/revision 關聯。Member ACK 只將 Member 從 SUBMITTED 推進為 WORKING；Member correlated update/result 才能進入 terminal state。Parent SHALL 在至少一個 member ACK 後為 WORKING；只有所有 Member 都完成或明確無需回覆時才為 COMPLETED。任一 Member FAILED、REJECTED 或 execution deadline 超時時，Parent SHALL 為 FAILED，並以 extension metadata 或 artifacts 保存 partial member outcomes；不得新增非 A2A 的 `PARTIAL` Task state。

#### Scenario: All members complete

- **WHEN** 每個 Member Delivery 都回報 COMPLETED 或空結果完成
- **THEN** Parent Task 進入 COMPLETED，結果以 member identity 分段聚合，且不產生群組回音訊息

#### Scenario: One member fails

- **WHEN** 任一 Member Delivery FAILED、REJECTED 或逾時
- **THEN** Parent Task 進入 FAILED，保留其他 member 的結果與 failure metadata，且不覆寫已完成 Member

#### Scenario: Group task returns immediately

- **WHEN** request configuration.returnImmediately 為 true
- **THEN** Hub 立即回傳 SUBMITTED 或 WORKING 的 Parent Task，client 可用 GetTask 或 subscribe 取得聚合結果

### Requirement: Group reply policy prevents echo storms

Group Extension SHALL 定義 versioned metadata `replyPolicy`（`ALL`、`MENTIONED_ONLY`、`ACK_ONLY`）與經驗證的 `mentions` Agent ID list。Hub SHALL 僅驗證、保存與轉送政策，不以自然語言或 LLM 判斷成員是否應回覆。Bridge SHALL 在 durable local commit 後立即 ACK，只有 ACK 成功才取得 execution lease；不需回覆時回報 Member COMPLETED 空結果，不向 group tenant 發送禮貌回信。

#### Scenario: ACK-only broadcast completes quietly

- **WHEN** group message 的 replyPolicy 為 ACK_ONLY 或成員未被 mentions 指定
- **THEN** Bridge 保存、ACK、回報空結果，且不建立任何 reciprocal group message

#### Scenario: Addressed member returns a result

- **WHEN** replyPolicy 允許且成員被明確 mentions 或被要求回答
- **THEN** 成員結果只更新 Parent aggregation，不向群組重新 fan-out 一筆回覆

### Requirement: Group cancellation and streams are durable and authorized

Parent Task 的 cancellation SHALL 只允許原 requester 或 Operator 在任一 Member 尚未 ACK 時執行；取消與 ACK SHALL 以條件更新原子決定勝負。取消勝出時所有未 ACK Member Delivery 一起 canceled，遲到 ACK/result 不得復活 Parent；ACK 勝出時回 `TASK_NOT_CANCELABLE`。Group stream SHALL 先送 Parent snapshot，再按 monotonic revision 送 Member/aggregate StreamResponse，並在 Parent terminal state 關閉；重連 SHALL 從 durable revision 補發。

#### Scenario: Cancel wins before member ACK

- **WHEN** requester 取消 Parent 且所有 Member 尚未 ACK
- **THEN** Parent 與未 ACK deliveries 原子變為 CANCELED，Bridge 不執行，遲到結果被拒絕

#### Scenario: ACK wins before cancellation

- **WHEN** 任一 Member ACK 先提交後 requester 嘗試取消
- **THEN** Hub 回 `TASK_NOT_CANCELABLE`，已開始的 Parent/Member processing 不被假裝取消

#### Scenario: Stream resumes after reconnect

- **WHEN** client 對同 circle、同 requester 的未完成 Parent Task 重新 subscribe
- **THEN** Hub 依 durable revision 補發一致的 Parent 與 Member progress，不重複 fan-out
