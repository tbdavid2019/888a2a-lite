## Context

目前 Hub 的核心是 `/hub/v1` custom durable mailbox：Agent 以 Hub-issued Token 操作，direct task 進入 target inbox，ACK 代表 mailbox receipt，SSE 是 inbox subscription。這些能力適合本專案的 relay 邊界，但與 A2A 標準的 Message/Part、Task lifecycle、Agent Card 和 HTTP binding 不同。參見 proposal.md 的 Why。

A2A 官方目前的 HTTP+JSON binding 定義 `POST /message:send`、`POST /message:stream`、`GET /tasks/{id}`、`GET /tasks`、`POST /tasks/{id}:cancel` 和 task subscribe；標準 Agent Card 需要宣告 supported interfaces、capabilities、security、input/output modes 與 skills。標準 discovery 使用 `/.well-known/agent-card.json`。

## Goals / Non-Goals

**Goals:**

- 讓標準 A2A HTTP+JSON client 可以發現此 Hub、送出文字 Message、取得 Task、訂閱 Task stream、取消可取消 Task。
- 讓標準 Task 的 durable correlation 延伸到現有 mailbox，支援 restart、redelivery、ACK 和 correlated reply。
- 讓 standard route 完整沿用 Agent Token、Multi-Circle isolation、404 masking、rate/payload limits 和 operator audit。
- 讓 `/hub/v1` 既有 client 不需修改，兩套 contract 在同一個 process/database 共存。

**Non-Goals:**

- 不把 `/hub/v1` 改名、刪除或改成 JSON-RPC。
- MVP 不實作 gRPC、JSON-RPC binding、push notification configuration、extended Agent Card、file/data Part 或 A2A group semantics。
- 不讓 Hub 執行 Agent 的模型、shell、檔案、credential 或本機程序。
- 不把 shared key 變成 standard request credential；它仍只屬於 Multi-Circle bootstrap/key rotation。

## Decisions

### 1. 新增 Gateway adapter，保留 `/hub/v1`，並支援根路徑與 `/a2a/v1` 雙向掛載

Standard routes 使用與既有 Hub 清楚分離的 `/a2a/v1` interface base URL，並以標準 HTTP+JSON route names 實作。`/hub/v1` 繼續是本專案的 transport/operations API。
為了防止部分標準 A2A 客戶端直接針對主機根目錄發送請求，Hub 在根目錄（`POST /message:send`、`POST /message:stream`、`GET /tasks/{id}` 等）同時提供掛載或轉發至 `/a2a/v1`，達成最大相容性。

Alternative：直接把 `/hub/v1/agents/{id}/tasks` 宣稱為 A2A。拒絕，因為官方 binding 要求標準 Message/Task 結構、標準 route mapping 和 Agent Card declaration；單純改名會造成錯誤相容宣稱。

### 2. 以 `tenant` 路由 Hub 上的 target Agent，並提供 Per-Agent Agent Card

A2A 規範 Section 4.4.6 與 3.2.1 定義 `tenant` 為多 Agent 後端的可選路由標識符。在單一 Gateway 提供多個 Agent 時，`AgentInterface.tenant` 宣告目標 Agent ID。
- **主機根卡片 (`GET /.well-known/agent-card.json`)**：代表 Hub 網關本身。
- **Per-Agent 卡片**：兩個既定 Card 路徑共用授權邏輯，interface 的 tenant 為 agentId，url 指向 /a2a/v1。固定版本官方 SDK 必須在 CI 證明可依此宣告路由，不能僅依文件推定互通。
- requester 身份由 `Authorization: Bearer <agentToken>` 決定。target tenant 不存在或跨圈時統一回傳 masked 404。

### 3. 新增 durable A2A task correlation，並完整支援 `returnImmediately` 執行語意

新增 A2A task persistence，保存 standard task ID、message ID、context ID、requester、target、circle、mailbox sequence、state、result message/artifact、created/updated timestamps。InboxItem 負責 delivery/ACK；A2A task 負責外部標準 lifecycle。

**執行語意與 `returnImmediately` 支援：**
- **預設阻塞同步**：`configuration.returnImmediately=false` 或省略時，成功回應必須等待 Task 終態或 INPUT_REQUIRED／AUTH_REQUIRED。HTTP 等待期限到達時回傳 504 deadline error，保留已提交 Task；連線中止只取消等待。不得以 200 WORKING 代替阻塞結果。重送同一 messageId 可恢復原 Task，不能重複執行。
- **非同步立即返回 (`returnImmediately: true`)**：Hub 寫入 mailbox 後立即回傳 `TASK_STATE_SUBMITTED` 或 `TASK_STATE_WORKING`，由客戶端自行輪詢或以 SSE 訂閱。

State 狀態枚舉完全對齊官方定義：
- `TASK_STATE_SUBMITTED`（已入列尚未 ACK）
- `TASK_STATE_WORKING`（target 已 ACK 簽收）
- `TASK_STATE_COMPLETED`（收到 correlated reply）
- `TASK_STATE_FAILED` / `TASK_STATE_REJECTED`
- `TASK_STATE_CANCELED`（尚未 ACK 前被取消）
- 保留 `TASK_STATE_INPUT_REQUIRED` 與 `TASK_STATE_AUTH_REQUIRED` 枚舉

### 4. 嚴格遵守標準 Response Envelope 與 text Part MVP

各標準端點依自己的 Protocol Buffer response 定義序列化；GetTask／CancelTask 回 Task，Send／List／Stream 使用各自 envelope：
- `POST /message:send`：回傳 `SendMessageResponse` 結構，包含 `task` 或 `message` 欄位（`{"task": {...}}`）。
- `GET /tasks`：回傳 `ListTasksResponse` 結構，包含 `tasks` 陣列與 `nextPageToken`。
- MVP 階段先接受 A2A Message 中的 text Part，並將其映射為 Hub string message。Agent Card 宣告 `text/plain` 為預設輸入輸出模式。其他未支援之媒體類型依規範回傳 `ContentTypeNotSupportedError`。

### 5. Streaming 使用標準 SSE `StreamResponse` 包裝與長連線保護

`/a2a/v1/message:stream` 與 `/a2a/v1/tasks/{id}:subscribe` 使用標準 Server-Sent Events：
- 每個 `data:` 負載必須是標準 `StreamResponse`，內含 `task`、`message`、`statusUpdate` 或 `artifactUpdate` 四選一（OneOf）。
- 首個事件必為 `Task` 或 `Message`，後續為更新事件，進入 terminal state 後發送最後事件並主動關閉連線。
- 嚴格遵守生產長連線防護：使用 `http.NewResponseController(w).SetWriteDeadline(time.Time{})` 清除全域寫入截止時間，並以每 15 秒發送 `: keepalive\n\n` 註釋防範代理逾時。
- 既有 `/hub/v1/agents/{id}/inbox/stream` 繼續發送自訂 `event: task` 與 `InboxItem`，互不干擾。

### 6. Agent Card 宣告 OpenAPI 3.2 `securitySchemes` 與真實 Capabilities

標準 Agent Card 遵循 A2A 規範：
- 宣告 `securitySchemes.bearerAuth.httpAuthSecurityScheme`（`scheme: "bearer"`，`bearerFormat: "AgentToken"`），以及 `securityRequirements: [{"schemes":{"bearerAuth":{"list":[]}}}]`。
- `capabilities` 明確宣告 `streaming: true`，未實作的 `pushNotifications: false`、`extendedAgentCard: false`。
- `supportedInterfaces` 明確宣告 `protocolBinding: "HTTP+JSON"`、`protocolVersion: "1.0"`。

### 7. 嚴格採用 `google.rpc.Status` 與 `google.rpc.ErrorInfo` 標準錯誤體系

所有標準端點的 4xx/5xx 錯誤均遵循 A2A 官方 Section 11.6 與 Section 5.4 規範：
```json
{
  "error": {
    "code": 404,
    "status": "NOT_FOUND",
    "message": "Task not found",
    "details": [
      {
        "@type": "type.googleapis.com/google.rpc.ErrorInfo",
        "reason": "TASK_NOT_FOUND",
        "domain": "a2a-protocol.org",
        "metadata": {}
      }
    ]
  }
}
```
涵蓋 9 大 Canonical Reason：`TASK_NOT_FOUND`, `TASK_NOT_CANCELABLE`, `UNSUPPORTED_OPERATION`, `CONTENT_TYPE_NOT_SUPPORTED`, `VERSION_NOT_SUPPORTED`, `INVALID_AGENT_RESPONSE` 等。跨圈操作統一映射為 `TASK_NOT_FOUND`，嚴格遮蔽跨圈存在性。

### 8. 支援 `application/a2a+json` 與 `A2A-Version` 服務參數標頭

- 請求解析與回應預設支援 `Content-Type: application/a2a+json`，並向下相容 `application/json`。
- 解析 `A2A-Version` HTTP Request Header，若客戶端請求不支援的 major version，回傳標準 `VersionNotSupportedError`。

## Risks / Trade-offs

- **[Risk]** 現有 bridge 的回覆目前是獨立 Hub task，未必帶有原始 standard task correlation。→ **Mitigation:** 在 bridge payload 和 Hub mailbox model 增加 optional reply correlation；沒有 correlation 的舊 client 仍只走 `/hub/v1`，不宣稱可完成 standard Task。
- **[Risk]** 使用 `tenant` 路由 target Agent 不是典型單一 remote-agent endpoint。→ **Mitigation:** 在 Agent Card skill/extension 文件明確說明 gateway routing semantics，並提供標準 client 端到端測試。
- **[Risk]** Task table 與 inbox table 可能出現不同步。→ **Mitigation:** 建立 task + mailbox 的 transaction boundary；所有 state transition 使用 conditional update 和 idempotent event handling。
- **[Risk]** standard stream 長連線在 proxy 下被緩衝或切斷。→ **Mitigation:** 沿用現有 SSE deadline、keepalive、`X-Accel-Buffering: no` 設定，並增加 reconnect/subscribe 測試。
- **[Risk]** A2A protocol 版本與 JSON field requirements 持續演進。→ **Mitigation:** Card 固定宣告實際支援的 minor version，adapter 加入 contract/conformance fixtures，不把未實作能力宣告為 supported。
- **[Risk]** 新標準入口擴大 public attack surface。→ **Mitigation:** 共用既有 authentication、rate/payload bounds、circle masking、no remote execution 和 audit policy。

## Migration Plan

1. 先新增 data model、migration、標準 Agent Card 與 read-only contract fixtures，不改既有 `/hub/v1`。
2. 在 feature flag 或明確設定下啟用 standard Gateway；預設可先宣告 disabled，避免未完成時對外誤用。
3. 部署後以 public、private circle、跨圈、invalid Part、restart、cancel、stream reconnect 和 legacy client fixtures 驗證。
4. 確認 CI、遠端 smoke test 和 interoperability fixture 通過後，再將 standard Gateway 設為預設啟用。
5. 回滾時關閉 standard Gateway routes；保留 task table 與 migration，避免破壞既有 `/hub/v1` mailbox 資料。

## 執行契約與驗收補強

以下決策補齊前述 Gateway 的執行邊界，施工與測試均必須落實。

### 身分、卡片與路由

標準入口以 Bearer Token hash 的唯一索引查得既有 Agent principal，不要求 X-Agent-ID。migration 遇到重複 hash 必須停止並報告，不猜測身份；仍檢查 expiry、revocation、circle state。Operator Token 不可作為一般 Agent Token。Operator 操作沿用獨立 admin 路徑。

根卡為公開 bootstrap metadata；實際呼叫以同圈 directory 提供的 Per-Agent Card URL 為入口。Per-Agent Card 需 Bearer 認證，未知／跨圈／不支援標準執行的目標統一 404，回應使用 private, no-store。未指定 tenant 的發送回傳 400 INVALID_ARGUMENT。所有 Task 操作都驗證 tenant 與原 target 一致；tenant 絕非授權依據。Task get/list/subscribe/cancel 僅原 requester 可用，即使同圈其他 Agent 也不得讀取。執行者僅能使用下面的回報端點。

### 執行回報與無回覆完成

新增自訂 `POST /hub/v1/a2a/tasks/{taskId}/updates`，使用既有 Agent ID/Token 認證。body 包含 updateId、turnId、expectedRevision、state，以及可選 message/artifacts。只接受 Task 固定 target 的回報。回報、revision 與 durable event 在同一交易提交；同 updateId 同內容回傳原結果，同 ID 改內容回傳 409。turnId 過期或 revision 不符拒絕，終態不可被遲到結果覆蓋。

Bridge 在本機保存完整輸入後 ACK；確認 ACK 成功才執行。執行結果先存本機 outbox，Hub 確認 update 後才完成本機工作。NO_REPLY／closing guard 回報 COMPLETED、空 artifacts；不另發禮貌回信。可重試錯誤維持工作，超過有限重試期限回報 FAILED；政策拒絕回報 REJECTED；需輸入或授權回報 interrupted state。AUTH_REQUIRED 不授予本機工具權限，敏感授權留在既有本機流程。

Agent 以 optional、versioned execution capability 登記支援；舊 Bridge 不宣告則不接收 standard Task，仍可收既有 direct task。UI 作為 requester，不自動宣告 executor。註冊欄位、SDK 與兩份分發 Bridge 必須同步。

### 多輪、冪等與資料模型

每個 Task 可有多個 turn/message/delivery。首次未帶 taskId 由 Hub 產生 task/context；帶 taskId 的新 Message 只可恢復 INPUT_REQUIRED 或 AUTH_REQUIRED，同時驗證 requester、tenant、context。省略 context 時沿用原值；不符回傳 400。WORKING 時新的並行輸入回傳 409，終態輸入回傳 UNSUPPORTED_OPERATION。同一 Message 的重試先作冪等查找，仍回傳原 Task。

message 去重鍵為 hub/circle/requester/target/messageId，並保存正規化內容摘要；同鍵不同內容回傳 409。恢復 interrupted task 以 revision CAS 決定唯一勝出者，配置新 turnId，狀態改 SUBMITTED，ACK 後 WORKING。保存原始 parts 順序與 history，不只保存拼接字串；任一不支援 Part（含混合文字與檔案）整筆拒絕。遵守 historyLength=0 和 bounded pagination；page token 綁定 caller/tenant/filter，不可跨 scope 重用。

### 取消與 ACK 競爭

SUBMITTED 且本輪尚未 ACK 可以取消，Task、mailbox cancellation 與 event 同交易提交。ACK 與 cancel 使用條件更新串行化：cancel 勝出，ACK 回報 canceled，Bridge 刪除執行資格；ACK 勝出，cancel 回 TASK_NOT_CANCELABLE。SSE 已送出但尚未 ACK 也適用，不能把收到 event 當作准許執行。

再次取消 CANCELED 回傳既有 Task 成功；WORKING、interrupted 及其他終態回 TASK_NOT_CANCELABLE。保留記錄期間行為固定。Operator 對關聯 mailbox 的取消必須走同一狀態交易。逾時或遲到 update 不得使 canceled task 復活。

### 串流與持久化

Task snapshot、monotonic revision、event 與 mailbox 關聯採 SQLite 交易。通知 broker 只作喚醒，訂閱以 durable revision 補抓事件，解決 snapshot/subscribe 競態與程序重啟。每次送資料前重查授權，keepalive 至少每 15 秒檢查撤銷；失效即關閉。限制每 Agent 同時串流與阻塞等待數量，慢讀者以單次 write deadline 中斷，不能持有 DB transaction 等待網路。

request timeout 不等於 Task failure。標準工作使用有限 execution deadline 與 retry budget，逾期後原子 FAILED 並拒絕遲到結果；deadline 不宣稱能回滾本機副作用。Agent revoke／circle disable 結束未完成 Task 並停止資料推送。reaper 不得連帶刪除尚在 retention 期間的 Task/result/event；migration 與清理須測試現有 Agent dependency cleanup。

### 標準來源與外部驗收

目標為 A2A 1.0 HTTP+JSON、text profile。施工第一步將官方規範 tag/commit、schema checksum、SDK exact version 與相容矩陣存入 source manifest；不可只引用會變動的 latest。官方 SDK 的 tenant 自動傳遞是待 CI 證明的驗收條件，不是既成事實。找不到可驗收的官方 SDK 版本時不宣稱互通完成。

CI 使用未修改的官方 SDK，允許正常 Bearer 和 Card URL 配置，不允許 monkey patch serializer 或自行重造 client 冒充互通。覆蓋 discovery、tenant、blocking/interrupted、stream、multi-turn、cancel、重試及隔離。版本、錯誤碼、Get/Cancel 裸 Task 與 Send/List/Stream 各自 envelope 依固定 binding fixtures 驗證；不能對所有端點一律加 task wrapper。

參考：[執行語義](https://a2a-protocol.org/latest/specification/#322-sendmessageconfiguration)、[資料模型](https://a2a-protocol.org/latest/definitions/)、[HTTP binding](https://a2a-protocol.org/latest/specification/#11-httpjsonrest-protocol-binding)。這些網址供查閱，source manifest 才是施工鎖定版本。

## Open Questions

SDK 精確版本由第一項相容性調查固定並記錄證據；在通過該 gate 前不得宣告 standard Gateway 可上線。JSON-RPC、gRPC、file/data Part、push notifications 和 extended Agent Card 留待後續 change。
