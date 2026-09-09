## Context

第一期 `a2a-standard-compatibility` 提供標準 A2A HTTP+JSON Gateway、Agent Card、Task、stream、Bearer principal 與執行結果回報。現有 `/hub/v1/groups` 則提供群組成員、邀請、歷史和 durable fan-out。第二期把兩者接起來，保留既有 custom contract。

官方 A2A 的 `AgentCapabilities.extensions` 是 `AgentExtension` object array；`A2A-Extensions` 是 client 對單次 request 的 comma-separated opt-in header。A2A Task 以 Task/turn/message/history/artifact 表達結果，ACK 本身不代表工作完成。

Buzz 的核心是 relay single source of truth，訊息、channel membership、workflow、git 與 audit 都是可追蹤事件；本期只取群組協調需要的 membership、事件與可回放進度，不追求 Buzz 全功能。

## Goals / Non-Goals

**Goals:**

- 讓支援此 extension 的 A2A client 能發現群組、取得 Group Card，並使用標準 Message/Task/Stream 操作群組。
- 讓一次群組廣播具備 Parent Task、每成員 delivery、結果聚合、冪等、重試、取消與重新訂閱。
- 讓群組成員的執行回報只能由指定 target 提交，且遲到或重複回報不破壞 Task 狀態。
- 延續既有 group role、Multi-Circle、safe card、rate/size/fan-out limits 和 `/hub/v1/groups` compatibility。

**Non-Goals:**

- 不宣稱一般不支援 extension 的 A2A client 自動理解群組 discovery 或 `group:` 語義。
- 不修改 A2A core data model；群組 metadata、reply policy 和 member progress 走 versioned extension metadata。
- 不把 ACK 當作完成，不讓 Hub 以 regex/LLM 判斷訊息是否需要回覆。
- 不做 Buzz 的 channel/thread/reaction/search/workflow/git/voice/signed-event 功能。

## Decisions

### 1. 第二期依賴第一期完成 gate

施工第一項固定第一期的 specification revision、schema checksum、官方 SDK exact version，並要求第一期官方 SDK discovery/send/get/list/stream/cancel/reply 全部通過。第一期未完成前，第二期只能建立 fixture 和 data model，不能宣告 group interoperability。

### 2. 使用虛擬群組 Agent tenant

群組以 `group:<groupId>` 作為 standard `tenant`。Group Card 的 `supportedInterfaces[0].tenant` 宣告此值，standard Gateway 將 requester 從 Bearer principal 取得，不能把 tenant 當成授權來源。

Group Card 及 discovery 是 extension surface：

- `GET /a2a/v1/groups`：回傳同圈、active 群組的 bounded references、Card URL、display name 和成員數摘要。
- `GET /a2a/v1/groups/{groupId}/card`：回傳同圈且 active 群組的標準 Agent Card。
- 可提供 `/.well-known` 相容 alias，但不得把每個動態群組誤當成主機根 Card。

未帶 Group Extension opt-in 的群組 Message SHALL 回 `ExtensionSupportRequiredError`；根 Card 宣告 extension `required: false`，因此一般 P2P 仍可使用。Group Card 不列出成員 ID、Token、key、主機資訊；群組 archive/disable 後 Card 與 discovery reference 失效並使用 `private, no-store`。

### 3. 正確宣告 extension

根 Card 與 Group Card 的 `capabilities.extensions` 使用：

```json
{
  "uri": "https://a2a.david888.com/extensions/groups/v1",
  "description": "Virtual group tenant fan-out and member outcome aggregation",
  "required": false,
  "params": {"tenantPrefix": "group:"}
}
```

client 使用群組 request 時，透過 `A2A-Extensions` header opt in。Hub 驗證 URI、版本與 request metadata；不把 response header 當成唯一 advertisement。

### 4. Parent Task、Member Delivery 與聚合狀態

群組 send 在單一 SQLite transaction 內建立：

- 一個 Parent A2A Task，保存 requester、group、circle、message、context、policy、revision。
- 一份當下 active 且具 standard executor capability 的成員快照；發送者沿用既有 `/hub/v1/groups` 語義，不收到自己的廣播。
- 每個成員一筆 deterministic Member Delivery/Member Task，保存 parent ID、target、sequence、message ID、turn ID 和狀態。

fan-out 先驗證整份成員快照、成員 circle、group limits、每個 target capacity 與 idempotency；任一預檢失敗則整筆不寫入。重送相同 `hub/circle/requester/group/messageId/contentDigest` 返回既有 Parent Task，不新增 delivery。

狀態映射：

- Parent 初始 `SUBMITTED`；全部 child delivery 仍未 ACK 時維持 submitted。
- 任一 child ACK 後 Parent `WORKING`；ACK 只代表本機 durable ingest。
- 所有 child 都是 `COMPLETED` 或無需回覆完成時 Parent `COMPLETED`，並可附聚合 artifacts。
- 任一 child `FAILED`、`REJECTED` 或 execution deadline 超時，Parent `FAILED`，並以 extension metadata/artifact 保存 partial member outcomes。
- Parent 不新增自訂 A2A state；`PARTIAL` 只作 result metadata。

`returnImmediately=true` 立即返回 Parent；未指定或 false 必須等 Parent terminal/interrupted，HTTP deadline 回 504 並保留 task。群組 stream 先送 Parent snapshot，再送有序 member/aggregate updates，終態後關閉。

### 5. Reply policy 與 Anti-Echo

Extension metadata 定義 `replyPolicy`：`ALL`、`MENTIONED_ONLY`、`ACK_ONLY`，以及經驗證的 `mentions` Agent ID list。Hub 只保存與轉送政策；Bridge 根據 policy 執行。自然語言是否「像公告」不能成為 Hub 授權依據。

Bridge 先保存完整輸入和 parent/member correlation，再 ACK；ACK 成功後才取得 execution lease。`ACK_ONLY`、未被 mentions 指定或 LLM 輸出 `[[A2A_NO_REPLY]]` 時，Bridge 回報該 Member `COMPLETED` 且空 artifacts，不發群組禮貌回信。有效回覆只送給 Parent Task 的結果聚合，不再回送群 tenant，避免回音風暴。

Instant ACK 是「durable commit 後立即執行」的行為要求，p95 <50ms 是觀測目標，不是跨網路硬 SLA。

### 6. Cancellation、membership race 與 late result

Parent 只有在尚未有 child ACK 時可取消；取消與 child ACK 使用 conditional update/CAS，同一 transaction 決定誰勝出。Parent cancel 勝出時所有未 ACK child delivery 一起 canceled，Bridge 不得取得 execution lease；ACK 勝出時 Parent 回 `TASK_NOT_CANCELABLE`。

重複取消已 canceled Parent 回既有 Task 成功；working、interrupted 和其他 terminal 狀態回 `TASK_NOT_CANCELABLE`。遲到 update、reply 或 member join/leave 都不得復活 Parent。成員快照在 fan-out transaction 固定，後續加入者不追溯收到該廣播。

### 7. Stream authorization and durable replay

只有 Parent requester 可取得 Parent Task、list/filter、subscribe 或 cancel；同圈其他成員只可取得自己被派送的 Member Delivery 結果。每個 stream 使用 monotonic revision，broker 只作喚醒，重連從 SQLite 補發 snapshot/update。每次送資料前、每次 keepalive 至少每 15 秒檢查 requester/token/circle/group state；撤銷或停用後關閉 stream。

### 8. Buzz comparison boundary

Buzz 的 channel/event log、membership、audit、search 和 workflow 是更大的 workspace substrate。本期只提供群組廣播的 event/revision、membership snapshot、結果聚合與 audit correlation；thread、reaction、全文搜尋、channel event signature、workflow trigger 和人類 workspace UI 另立 change。

## Risks / Trade-offs

- **[Risk]** 成員結果聚合造成 Parent 永久等待。→ **Mitigation:** Parent 使用 execution deadline/retry budget；超時原子 FAILED，保留已完成與未完成成員摘要。
- **[Risk]** fan-out 中途 membership 或 capacity 改變。→ **Mitigation:** transaction 內固定 eligible member snapshot 並全量預檢，採 all-or-nothing。
- **[Risk]** 多個成員同時回覆造成結果順序不穩。→ **Mitigation:** 用 monotonic revision 與 member ID tie-breaker 排序，結果以 member identity 分段保存。
- **[Risk]** 舊 Bridge 不懂 standard group update。→ **Mitigation:** standard task 只派給宣告 capability 的 executor；legacy `/hub/v1/groups` 維持原流程。
- **[Risk]** Extension URI 或 header 被錯誤解析。→ **Mitigation:** 固定 extension schema、URI、版本和 official SDK/negative fixtures；缺少 opt-in 時拒絕 group operation。
- **[Risk]** 標準 Card 暴露群組成員或跨圈資訊。→ **Mitigation:** Group Card 只給同圈 requester，使用 no-store，不輸出成員 ID 或秘密，所有 unknown/cross-circle 統一 404。

## Migration Plan

1. 完成第一期 official SDK gate 與 result update contract。
2. 新增 Group Extension schema、extension documentation、models、migration 和 read-only discovery/Card fixtures。
3. 在 feature flag 下啟用 group tenant，先驗證同圈單成員/多成員與既有 `/hub/v1/groups` compatibility。
4. 加入 duplicate、membership snapshot、capacity rollback、ACK/cancel race、late result、restart、stream replay 與 Anti-Echo CI fixtures。
5. 以未修改的 official SDK 加 extension-aware client 完成端到端驗收，再在遠端 smoke test 後開啟 production。
6. 回滾時關閉 group tenant routes；保留已寫入 Parent/Member records 供查詢與清理，不刪除既有 group mailbox。

## Open Questions

沒有會改變本期邊界的未決問題。Quorum/first-success 聚合、跨群組 workflow、thread/reaction、簽名事件、搜尋與 Buzz workspace parity 另立 change。
