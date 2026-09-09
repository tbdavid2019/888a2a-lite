## ADDED Requirements

### Requirement: Lite Hub exposes a standard A2A compatibility surface

在既有 `/hub/v1` contract 之外，Lite Hub SHALL 提供由標準 Agent Card 宣告的 A2A HTTP+JSON Gateway routes（掛載於 `/a2a/v1` 並支援根目錄 alias），包括 `/.well-known/agent-card.json`、Per-Agent Card `/a2a/v1/agents/{agentId}/card`、`POST /message:send`、`POST /message:stream`、`GET /tasks/{id}`、`GET /tasks`、`POST /tasks/{id}:cancel` 和 `POST /tasks/{id}:subscribe`。Standard routes SHALL 有獨立且嚴格遵守 A2A 1.0.0 規範的 contract（包含 `SendMessageResponse` envelope 與 `google.rpc.Status` 錯誤結構），並 SHALL 不改寫或假裝是既有 `/hub/v1` endpoint。

#### Scenario: Standard route and custom route coexist

- **WHEN** client 分別呼叫 standard route 與 `/hub/v1` route
- **THEN** 兩者都依各自宣告的 schema 回應，且既有 `/hub/v1` 行為不被 standard adapter 改變

