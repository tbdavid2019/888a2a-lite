## Context

完整 Hub 已有 dynamic `/llms.txt` 和 durable `event_log`，但 register response 只有
identity、policy 和 duplicate flag。新功能要服務不固定 host 的部署，並且不能把 Hub
公告誤當成 Agent 的 executable instruction。詳見 proposal 和兩份 delta spec。

## Goals / Non-Goals

**Goals:**

- 用穩定 JSON schema 讓 Agent 在 register/reconnect 時取得 Hub 規則和公告摘要。
- 以 SQLite 保存公告並提供 cursor-based feed，避免每次傳送完整歷史。
- 保持既有 register response 和 A2A client 的向後相容性。
- 讓 system card、公告 URL 和 documentation URL 隨 public origin 動態產生。
- 讓 operator 可以發布 bounded、可過期且不含秘密的公告。

**Non-Goals:**

- 不把公告變成 A2A core 的 login、Message、Task 或 Agent Card 欄位。
- 不允許公告授予 shell、filesystem、network、MCP、credential 或模型權限。
- v1 不做公告簽章、複雜審批工作流、個別 Agent targeting、push notification 或群聊。
- 不保存完整 HTTP request/response 或公告以外的敏感內容作為 audit log。

## Decisions

### 1. Separate Hub system card from Agent Card

新增 `/hub/v1/system-card.json` 描述 Hub control-plane；每個 Agent 的 A2A identity 和
skills 仍由既有 Agent Card 表示。這避免把 Hub policy、operator notice 和 Agent
capabilities 混在同一個資源。

替代方案是把公告加入 Agent Card，但 A2A Agent Card 的目的在於描述 remote Agent 的
identity、interface、capability 和 skills，不是 Hub-wide operational notice。

### 2. Optional register metadata

register response 新增 optional `hub` object，內容包含 `systemCardUrl`、
`announcementFeedUrl`、`announcementCursor`、`announcements` 和 extension URI。既有
client 忽略未知 JSON 欄位即可繼續使用；duplicate retry 也回傳同一 metadata，但不會
再次回傳 Agent Token。

### 3. Public read, operator write

System card 和 active announcement feed 不需要 Agent credential，因為 Agent 在完成
register 前也需要知道 Hub 規則；publish 只接受 operator bearer Token。公開 response
只包含 bounded summary，不保存或回傳任何 token、secret、workspace 或 executable data。

替代方案是要求 Agent Token 才能讀公告，但會讓首次 onboarding 無法取得最基本安全
規則，並增加 adapter 的 bootstrap cycle。

### 4. SQLite announcement table and monotonic cursor

新增 `announcement` table，以 AUTOINCREMENT ID 作為 cursor。欄位保存 severity、title、
summary、published-at、optional expiry-at、documentation URL 和 created-by metadata。
Feed 以 `id > afterId`、active expiry 和 bounded limit 查詢，公告發布後不可重排或修改
既有 ID。

替代方案是只使用 event_log，但 audit event 的 schema 和 retention 目的不同；announcement
需要 public feed 的 active/expiry semantics。

### 5. Versioned optional extension

使用 `https://github.com/tbdavid2019/888a2a-lite/extensions/hub-announcements/v1` 作為
optional extension identifier。這是 Hub control-plane extension；若未來需要在正式
A2A binding 中宣告，adapter 可透過 A2A Agent Card extension metadata 或
`A2A-Extensions` header 宣告支援，不改寫 A2A core login semantics。

### 6. Treat content as data

System card 和 announcement response 加入明確 trust semantics；adapter 只把它們當作
控制面 metadata，不能把 title/summary 提升為 system/developer prompt。severity 只
影響本機顯示和 policy decision，不能直接觸發工具。

### 7. Embedded operator UI with immutable published revisions

Hub 以 embedded static HTML/JavaScript 提供 `/admin/announcements`，UI 只保存 operator
Token 在 runtime memory，呼叫既有 bearer-authenticated admin API。Server 不建立 browser
session cookie，也不接受 URL token。Announcement lifecycle 分為 DRAFT、PUBLISHED 和
EXPIRED；DRAFT 可 PATCH，PUBLISHED 不做 in-place update，修改會建立新的 revision ID，
讓 Agent cursor、audit log 和歷史內容可追溯。

替代方案是直接讓 UI 修改已發布 row，但這會破壞已讀 Agent 對公告內容的歷史一致性；
另一個方案是新增獨立 admin service，會重複 operator authentication 和 deployment。

### 8. Dynamic origin and caching

URL 優先使用 `A2A888_HUB_PUBLIC_URL`，未設定時使用 request 的 scheme/Host。System card
和 feed response 加入 `Cache-Control` 和 ETag；register response 仍提供 cursor，讓
adapter 可用增量 feed 取得更新。

## Risks / Trade-offs

- [公告文字可能含 prompt injection] → response 明確標示 metadata，adapter 與 tool policy
  分離；CRITICAL 只觸發人工或本機 policy review。
- [Public feed 被大量輪詢] → 設定 response cache、bounded limit、HTTP rate limit 和
  cursor；不提供無限制全文歷史。
- [公告 database 持續成長] → 保留 immutable ID 和 expiry，未來可加入 operator retention
  policy；v1 不自動刪除以保留 auditability。
- [部署 origin 設定錯誤] → 使用 request-origin fallback，並在 system card 回傳自身 URL，
  CI 驗證不同 Host 與 configured public URL。
- [A2A client 不理解 extension] → 所有新 register 欄位 optional，extension 不標示 required，
  既有 direct A2A flow 不受影響。
- [Browser UI 被植入 XSS] → 使用 textContent／escaped rendering，不使用 innerHTML 顯示
  公告；設定 CSP、X-Content-Type-Options 和 no-store，並不把 Token 寫入 browser storage。

## Migration Plan

1. 在啟動 migration 建立 announcement table 和必要 index；既有 `hub.db` 不需資料轉換。
2. 以環境設定的 public origin 產生 system card 和 feed URL；未設定時使用 request origin。
3. 更新 server、generic client 和 adapter examples；既有 client 只忽略 `hub` 欄位。
4. 由 operator publish 一筆非敏感 test announcement，驗證 register response、cursor feed
   和 expiry behavior。
5. Watchtower 更新 image 後確認既有 Agent、inbox、event log 和公告仍存在；若失敗，回復
   前一個 image，保留 `/data/hub.db`。

## Open Questions

無。公告 severity、cursor、extension URI、公開讀取和 operator publish 邊界已在規格與
本設計中固定。
