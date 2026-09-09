## Why

目前 `888a2a-lite` 的 `/hub/v1` 是可用的自訂 Hub mailbox protocol，具備 Agent 註冊、圈圈隔離、任務排隊、ACK 與 SSE，但不是 A2A 標準 wire protocol。外部 A2A client 依標準 Agent Card、`message:send`、Task API 或標準 SSE binding 無法直接互通，因此需要在保留既有 Hub contract 的前提下補上標準相容入口。

## What Changes

- 新增可選的 A2A standard adapter，提供標準 HTTP+JSON binding 的 Agent Card、message、Task、streaming 與 cancellation 入口。
- 將標準 `Message`／`Part` 映射到既有 Hub 的 direct mailbox；將標準 Task view 映射到 durable inbox/task state。
- 支援標準 `returnImmediately` 語意（預設 `false` 阻塞等待 correlated reply，`true` 立即返回提交態）。
- 嚴格遵守標準 Response Envelope（`SendMessageResponse`、`ListTasksResponse`、`StreamResponse`）與 `google.rpc.Status` + `google.rpc.ErrorInfo` 標準錯誤體系。
- 將現有 Agent Token、Multi-Circle principal 與跨圈 404 隔離套用到標準入口。
- 提供主機根目錄 `/.well-known/agent-card.json` 以及個別 Agent 的 Per-Agent Card 端點（含 `AgentInterface.tenant` 路由宣告與 OpenAPI 3.2 `securitySchemes`）。
- 支援 `application/a2a+json` Content-Type 與 `A2A-Version` 服務參數標頭。
- 保留 `/hub/v1`、既有 client、group extension、operator API 與 Multi-Circle 行為，不改變既有 endpoint 語義。
- 補上標準相容性測試、adapter contract 文件與 interoperability quickstart。
- 補齊 Bearer-only 身分解析、指定 executor 的持久結果回報、無回覆完成、多輪 Task 恢復、取消／ACK 原子競爭及串流撤銷。
- 以固定官方規範 revision 和官方 SDK CI 互通作為上線 gate；blocking 逾時保留 Task 並回 deadline error，不以進行中 Task 假裝成功。

## Capabilities

### New Capabilities

- `a2a-standard-transport`: 標準 A2A HTTP+JSON Agent Card、Message、Task、streaming、cancel 與錯誤映射。

### Modified Capabilities

- `lite-hub-http-contract`: 既有 Hub contract 增加與 standard adapter 的邊界、共存和不變性要求。
- `hub-system-card`: system declaration 增加標準 A2A interface 與 capability advertisement 的關係。
- `durable-agent-mailbox`: mailbox/task 狀態增加 standard adapter 所需的 task identity、message mapping 和 lifecycle 規則。
- `multi-circle-isolation`: 標準入口必須沿用既有 circle principal、跨圈遮蔽與 operator scope。
- `public-hub-registration`: standard adapter 使用已核發的 Agent credential，且不得引入另一套註冊身份。

## Impact

- 主要影響 `internal/service`、`internal/hub`、`internal/store`、HTTP route registration、Agent Card models、標準化錯誤與測試。
- 可能新增標準 A2A data model package；不新增遠端執行、外部 SaaS、模型 provider 或第三方 runtime dependency。
- 既有 `/hub/v1` client 不需升級；standard adapter 會以明確版本／capability 宣告與既有接口並存。
- A2A 官方參考：HTTP+JSON binding、`/message:send`、`/message:stream`、Task routes、Agent Card 與 `.well-known/agent-card.json`。
