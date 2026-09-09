## 1. Contract and capability foundation

- [ ] 1.0 固定官方 A2A 1.0 規範 tag/commit、schema checksum、官方 SDK exact version 與 source manifest；在 GitHub Actions 驗證 SDK 支援 HTTP+JSON/tenant，未通過不得宣告互通或啟用上線。

- [x] 1.1 Define the standard A2A HTTP+JSON version (1.0), `/a2a/v1` base URL and root route aliases, supported operations, text-only MVP boundary, OpenAPI 3.2 `securitySchemes` (Bearer token), and gateway `tenant` routing extension; verify the contract is documented independently from `/hub/v1`.
- [x] 1.2 Add standard A2A data models for AgentCard, AgentInterface, Message, Part, Task, TaskStatus, SendMessageResponse, ListTasksResponse, StreamResponse, and `google.rpc.Status` / `google.rpc.ErrorInfo` error models; verify JSON field names match camelCase A2A 1.0 fixtures.
- [x] 1.3 Add Content-Type negotiation (`application/a2a+json` and `application/json`) and `A2A-Version` header validation; verify unsupported version requests return `VERSION_NOT_SUPPORTED`.

## 2. Durable task correlation

- [x] 2.0 定義 task/turn/message/delivery/revision/event 關聯與 retention、execution deadline、retry budget；CI 驗證 Task 不因 Agent prune 提前遺失，migration 重啟可恢復。

- [x] 2.1 Add SQLite persistence for standard A2A tasks and their relation to mailbox sequence, message ID, requester, target, context, circle, status, and result; verify fresh schema and restart migration preserve existing mailbox rows.
- [x] 2.2 Implement atomic creation/idempotency for a standard Message and its mailbox item; verify duplicate `messageId` or equivalent idempotency input returns one Task and one delivery.
- [x] 2.3 Implement conditional lifecycle transitions for submitted, working, input_required, auth_required, completed, failed, canceled, and rejected states; verify invalid backward transitions are rejected and terminal states remain terminal.
- [x] 2.4 Add correlated reply persistence and result retrieval; verify a target reply updates the original standard Task instead of creating an unlinked result.

## 3. Standard Agent Card and HTTP routes

- [x] 3.0 新增 Token hash 唯一索引與 Bearer-only principal lookup，重複 hash migration fail closed；CI 驗證無 X-Agent-ID、失效 Token、Operator Token 混用及私有 Card 的 401/404/no-store 行為。

- [x] 3.1 Implement `GET /.well-known/agent-card.json` with standard supported interface, HTTP+JSON protocol version ("1.0"), OpenAPI 3.2 Bearer security scheme, text modes, streaming capability, and skills; verify no credential or private-circle data appears.
- [x] 3.2 Implement Per-Agent Card endpoint `GET /a2a/v1/agents/{agentId}/card` (and `.well-known` alias) populating `supportedInterfaces[0].tenant = agentId`; verify standard client SDK can discover and target individual agents.
- [x] 3.3 Implement authenticated `POST /a2a/v1/message:send` (and root alias) with `SendMessageRequest` validation, `tenant` target routing, `SendMessageResponse` envelope, and `returnImmediately` support (blocking wait on false/unset, immediate return on true); verify valid text requests return standard response.
- [x] 3.4 Implement `GET /a2a/v1/tasks/{id}` with authorization and `GET /a2a/v1/tasks` with bounded pagination wrapped in `ListTasksResponse` envelope; verify task data is masked across callers or circles.
- [x] 3.5 Implement `POST /a2a/v1/tasks/{id}:cancel` with requester/operator authorization and idempotent cancellation; verify submitted mailbox delivery is canceled and non-cancelable tasks return `TASK_NOT_CANCELABLE`.
- [x] 3.6 Implement standard error serialization using `google.rpc.Status` and `google.rpc.ErrorInfo` (`domain: "a2a-protocol.org"`) across all 4xx/5xx responses; verify standard A2A error reasons are correctly populated.

## 4. Standard streaming and event mapping

- [ ] 4.0 使用 durable revision/event 驅動 snapshot 與補發，限制並行等待及慢讀者；CI 驗證 snapshot/subscribe 競態、撤銷後停止推送、keepalive 檢查及不持有 DB transaction 等待網路。

- [x] 4.1 Implement `POST /a2a/v1/message:stream` (and root alias) using standard SSE `StreamResponse` envelopes (OneOf task, message, statusUpdate, artifactUpdate); verify immediate initial response and `rc.SetWriteDeadline(time.Time{})` with 15s keepalive comments.
- [x] 4.2 Map mailbox ACK, correlated reply, failure, and cancellation to standard TaskStatusUpdateEvent or TaskArtifactUpdateEvent; verify every emitted event references the same task and context IDs.
- [x] 4.3 Close standard streams after terminal state and support `POST /a2a/v1/tasks/{id}:subscribe` from durable state; verify missed updates can be reconstructed without duplicating mailbox deliveries.
- [x] 4.4 Keep `/hub/v1/agents/{agentId}/inbox/stream` on its existing InboxItem event format; verify legacy SSE client fixtures continue to parse the original `event: task` stream.

## 5. Adapter and client integration

- [x] 5.0 實作 versioned executor capability 與 updates 端點（updateId/turnId/expectedRevision/state/message/artifacts）；CI 驗證舊 executor 不接 standard Task、非 target 偽造回報被拒、同 update 防重及過期 turn 被拒。
- [x] 5.4 Bridge 結果使用持久 outbox，NO_REPLY 回報空 COMPLETED、拒絕 REJECTED、重試耗盡 FAILED；CI 驗證回報前後 crash 均可恢復且無禮貌回信風暴，同步 embedded 與分發腳本。
- [x] 5.5 實作 interrupted Task 多輪恢復與 context/tenant/requester 一致性；CI 驗證同 task 新 turn、並行輸入 CAS、一致 Message 重試與終態禁止重啟。

- [x] 5.1 Extend the Hub/client correlation payload so the universal Bridge can acknowledge standard delivery and return a reply linked to the originating standard task; verify old `/hub/v1` clients can omit the optional correlation fields.
- [x] 5.2 Update the Python Bridge and local UI to preserve standard task/message correlation across durable queue, retry, restart, and reply delivery; verify Instant ACK and Anti-Echo guard remain active for standard tasks.
- [x] 5.3 Add a standard HTTP+JSON client fixture using only the published Agent Card and Agent Token; verify it can discover, send text with blocking wait or non-blocking, subscribe, cancel, and receive a correlated reply.

## 6. Isolation, security, and compatibility tests

- [x] 6.6 驗證 configuration.returnImmediately 的終態／中斷等待、HTTP 504、client 斷線與相同 messageId 恢復；不得以 200 WORKING 通過 blocking 測試。
- [x] 6.7 驗證取消與 ACK 兩種交易勝負、SSE 到達但未 ACK 的取消、重複取消 CANCELED 成功、遲到回報及 Operator mailbox cancel 一致性；確認取消勝出時 executor 零執行。
- [x] 6.8 驗證原 requester Task ACL、同圈非 requester 拒絕、tenant/context 不匹配、pagination scope、mixed unsupported Part、historyLength=0 與同 messageId 改內容拒絕。
- [ ] 6.9 以未修改官方 SDK 跑完整 Card/Bearer/tenant/send/get/list/stream/multi-turn/cancel 流程；保存版本與結果，不以自製 client 或 serializer patch 替代。

- [x] 6.1 Apply existing Agent principal authentication, rate limits, payload limits, and no-remote-execution boundary to every standard route; verify malformed JSON, unsupported parts, invalid auth, and oversized input produce bounded `google.rpc.Status` errors.
- [ ] 6.2 Add public/private/dynamic Multi-Circle integration coverage for standard send, get, list, stream, subscribe, cancel, and reply; verify cross-circle operations return masked `TASK_NOT_FOUND` (404) and create no state.
- [x] 6.3 Add standard/custom coexistence regression tests; verify the same Agent identity and mailbox are visible through both contracts without changing legacy response or ACK semantics.
- [x] 6.4 Add restart, duplicate, redelivery, terminal-transition, and stream-reconnect tests for standard tasks; verify CI test names and assertions cover each lifecycle invariant.
- [x] 6.5 Add standard Agent Card, `returnImmediately` sync/async, `SendMessageResponse` envelope, and `google.rpc.Status` conformance fixtures; verify the fixture suite runs in GitHub Actions.

## 7. Documentation and rollout

- [x] 7.1 Document the standard Gateway, Per-Agent Cards, `tenant` routing, `returnImmediately` behavior, supported text-only scope, authentication, task lifecycle, and explicit differences from `/hub/v1`; verify README and `llms.txt` do not claim unsupported A2A capabilities.
- [x] 7.2 Document Multi-Circle behavior for standard routes and the fact that `X-Hub-Key` is registration-only; verify public/private examples use separate circle-scoped credentials.
- [x] 7.3 Add deployment configuration, health/capability visibility, and rollback/disable instructions for the standard Gateway; verify existing Docker single-mode deployments remain deployable.
- [ ] 7.4 Run GitHub Actions Go/Python tests and standard conformance fixtures, then run the required remote smoke test on `david@10.9.0.11`; verify both standard and legacy flows before enabling production rollout.
