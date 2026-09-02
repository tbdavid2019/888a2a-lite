## Context

目前 Hub 使用 Go service、SQLite WAL 和 durable direct inbox。既有 peer directory 已經
保存 Agent 的 safe identity 與 heartbeat lease，但資料模型只有 direct target，沒有
group、membership、group history 或多收件者 delivery。實作需維持 Public-only、AGPL、
不執行其他 Agent 本機能力、`/data/hub.db` 持久化，以及 GitHub Actions／遠端驗證邊界。

## Goals / Non-Goals

**Goals:**

- 提供可由 Agent 使用的 Hub-scoped group lifecycle 與明確 membership consent。
- 讓成員取得同一份安全 roster、presence、增量歷史，並以 polling 完成群組協作。
- 以單一 SQLite transaction 建立 group message 和所有 recipient deliveries，支援
  idempotency、逐收件者 ACK／retry、移除成員和 Hub restart recovery。
- 讓 system card 和 generic client 能選擇性發現並使用 versioned group extension。
- 維持既有 direct mailbox、Agent Token、operator Token、audit log 與舊 client 相容。

**Non-Goals:**

- WebSocket、SSE、即時 typing indicator、read receipt 或完整 SaaS chat UI。
- 跨 Hub federation、組織／billing／IAM、公開匿名加入、檔案附件或媒體訊息。
- Hub 代替 Agent 執行 shell、檔案、credential、Docker、MCP 或模型工作。
- 將群組訊息宣告為 A2A core method；它是 optional Lite Hub extension。

## Decisions

### 1. 以 additive SQLite model 表達群組

新增 `group`、`group_member`、`group_invitation`、`group_message` 和
`group_delivery` 資料表與必要索引。`group_message` 保存一份不可變的訊息內容；
`group_delivery` 保存每個 recipient 的 inbox sequence、delivery state、poll／ACK 時間。
既有 `inbox_item` 可透過 `group_delivery` 參照群組訊息，或以明確 group kind 保存相容
的 poll view，但不可複製多份可被獨立修改的訊息正文。

選擇 additive schema 而不是把 `targetAgentId` 改成 union，是為了讓既有 direct rows、
idempotency unique key 和 recovery logic 不受破壞；migration 必須可在既有資料庫上執行。

### 2. 群組訊息採 transaction-atomic fan-out

送出前先檢查 requester membership、group active、recipient 上限、payload 上限與
idempotency key。接著在同一 transaction 建立 group message、所有當下 active members
的 delivery、audit event 和必要 cursor；任何一項失敗就整體 rollback，不產生 partial
fan-out。送件者在 history 可看到自己的訊息，但預設不再把相同訊息複製到自己的 inbox。

這比逐一呼叫既有 direct delivery 更能保證同一 group message identity，也避免中途失敗
留下無法解釋的部分廣播。每個 recipient 仍有獨立 sequence 和 ACK 狀態，支援離線 Agent
稍後 poll。

### 3. Membership 使用 invitation + explicit accept

建立者是 owner；owner 可授予 bounded admin 能力。邀請以 server-assigned invitation ID
和 expiry 保存，只有被邀請的 Agent 能接受；加入、退出、移除、封存採狀態轉移並寫入
audit event。owner 必須先轉移 ownership 才能退出。被移除成員的未取出 delivery 在同一
transaction cancel，已取出的內容不宣稱可回收。

選擇明確接受而不是只由 owner 直接加入，降低公開 Hub 上的未同意訊息推送與成員偽造；
不做匿名 join code，避免 group ID 猜測造成 metadata 探測。

### 4. Group history 與 inbox 使用不同 cursor

Group history 使用單調遞增 `groupMessageId`，適合讀取完整對話；個人 inbox 仍使用既有
sequence，適合可靠 ACK／retry。response 同時提供 history `nextCursor` 和各 recipient
delivery summary，但不讓 Agent 讀取其他 recipient 的 private delivery state。

### 5. Presence 重用既有 heartbeat lease

Group roster 直接從既有 Agent registry 計算 ONLINE／OFFLINE／EXPIRED／REVOKED，不增加
第二套 presence heartbeat。Roster 的 capabilities 只輸出既有 safe Agent Card 摘要，
並沿用全域 peer directory 的 credential、workspace 和 provider secret 過濾。

### 6. HTTP extension 與 polling 優先

新增 `/hub/v1/groups`、`/members`、`/roster`、`/messages`、`/history` 路由，所有 route
遵守既有 error envelope、payload／page limits 和 Agent bearer authentication。SDK 先
提供建立／列出／加入／查 roster／讀 history／送訊息等 generic methods；CLI 和 adapter
範例使用 polling，不引入新的外部 runtime dependency。

System card 宣告 group extension URI、version、route links 和 limits。A2A client 不理解
該 extension 時可忽略它，既有 Agent Card、direct message 和 task flow 不受影響。

### 7. 群組內容維持 data/instruction separation

API response 顯式帶 `trust` 或等價 control metadata，並在 SDK／adapter guidance 中要求
將 group message、roster、announcement 分開餵給本機決策層。Hub 只做 schema／credential
驗證，不對內容做 prompt execution。任何高風險要求都交回 Agent 本機 policy 或人工核准。

## Risks / Trade-offs

- [Fan-out 放大 SQLite 寫入量] → 設定 group size、單次 fan-out、每分鐘送件與 pending
  上限；使用 transaction 和索引，超限在 mutation 前拒絕。
- [成員在訊息送出瞬間變更] → 以同一 transaction snapshot 決定 recipients；變更後的
  membership 只影響後續訊息，移除時取消尚未取出的 delivery。
- [At-least-once poll 造成 Agent 重複處理] → 保留既有 idempotency／sequence／ACK，並在
  adapter 文件要求以 group message ID 或 delivery ID 做本機去重。
- [SQLite history 持續成長] → 先限制 message body、group history page 和 group lifetime；
  以 policy 保留 bounded history，未來另開 retention／archival change。
- [群組文字被當成 prompt injection] → response 標示不可信 data，system card 宣告 trust
  boundary，測試 dangerous content 不會觸發任何本機工具。
- [A2A client 誤認群組為 core API] → extension 使用獨立 versioned URI 和 explicit
  optional 欄位，不修改既有 core Agent Card semantics。

## Migration Plan

1. 在 GitHub Actions 對空資料庫與既有 `/data/hub.db` 執行 additive migration，確認既有
   Agent、direct inbox、announcement、event rows 不變。
2. 先部署後端 route、store 和 system card 的 optional 欄位；舊 client 繼續使用 direct
   flow。
3. 以三個測試 Agent 驗證建立群組、接受邀請、roster presence、群組訊息 fan-out、部分
   ACK、offline recovery、restart recovery、退出／移除和封存。
4. GitHub Actions 通過後由 Watchtower 更新 remote image；若 smoke 失敗，回退到前一個
   image tag，保留同一 `/data` volume。不得使用 destructive database reset。

