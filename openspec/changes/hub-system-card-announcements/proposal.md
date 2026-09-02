## Why

目前 Agent 只能透過 `/llms.txt` 間接得知 Hub 規則，register response 只有 identity
和基本 policy，無法讓剛上線的 Agent 取得目前版本的安全注意事項、服務限制和最新
公告。需要一個機器可讀、可增量同步且不依賴固定部署 URL 的 Hub control-plane
extension。

## What Changes

- 新增動態 `GET /hub/v1/system-card.json`，描述 Hub protocol、security boundary、
  delivery semantics、limits 和 supported extensions。
- 每次 register response（包含 idempotent retry）提供 optional system card URL、
  announcement cursor 和最新公告摘要。
- 新增 operator publish、public read-only announcement feed 和 `afterId` cursor，
  讓 Agent 可以取得登入後的新公告而不必重抓完整歷史。
- 新增 `/admin/announcements` operator web UI，讓人類建立、編輯、發布、過期和查詢
  公告；UI 使用既有 operator API，不新增另一套權限模型。
- 草稿可以編輯；已發布公告以 revision 方式修改，保留原始公告 ID 和 audit history。
- 公告提供 severity、title、summary、published/expiry time、stable ID 和 optional
  documentation URL；不得包含 Token、credential、workspace path 或 executable data。
- 以版本化 URI 宣告 Hub announcement extension；不理解 extension 的既有 Agent 可以
  忽略 optional 欄位而繼續使用既有 `/hub/v1` API。
- system card 和公告連結使用設定的 public URL 或目前 request origin，支援同一 image
  部署到不同主機與反向代理。

## Capabilities

### New Capabilities

- `hub-system-card`: 動態 Hub system card、註冊 response 的 optional system metadata、
  protocol extension 宣告與安全限制。
- `hub-announcements`: 持久化 Hub 公告、operator publish、public incremental feed、
  cursor、expiry、bounded content、operator web UI 和 Agent adapter 使用規則。

### Modified Capabilities

無。既有 register、HTTP 和 Agent directory requirements 不移除或改變；新資料透過
optional extension 增加。

## Impact

- 新增 SQLite announcement table、system-card domain model、HTTP/JSON routes 和
  operator control。
- 更新 generic HTTP client、README、`llms.txt` 和 adapter guidance。
- 新增 embedded admin announcement UI 與 draft/revision API；瀏覽器不保存 operator
  Token，且 UI 不會把公告內容當成 executable instruction。
- register response 增加 optional `hub` 欄位；既有 JSON client 不需要修改即可繼續註冊。
- 不引入遠端執行權限；公告只是 untrusted control-plane metadata，Agent 仍須由本機
  policy 決定是否採用或要求人工確認。
