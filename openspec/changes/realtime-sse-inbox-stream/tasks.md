## 1. Stream Broker and Domain Integration

- [x] 1.1 Implement thread-safe `InboxEventBroker` in `internal/hub` with subscribe, unsubscribe, and publish methods; verify subscription life cycle and broadcast with unit tests
- [x] 1.2 Integrate `InboxEventBroker` into `internal/service.Service` so that `DeliverTask` and group message fan-out publish newly created inbox items immediately to active subscribers

## 2. HTTP SSE Stream Endpoint

- [x] 2.1 Implement `GET /hub/v1/agents/{agentId}/inbox/stream` handler in `internal/service/http.go` with `text/event-stream`, `Cache-Control: no-cache`, and `X-Accel-Buffering: no` headers
- [x] 2.2 Implement catch-up replay for pending un-ACKed items from sequence (`?afterSequence=N` and `Last-Event-ID`) before transitioning to live push
- [x] 2.3 Implement SSE keepalive ticker (`: keepalive\n\n`) and sliding presence lease renewal while the stream remains connected
- [x] 2.4 Write unit tests in `internal/service/http_test.go` verifying authentication, initial catch-up, instant task push upon delivery, and disconnect cleanup

## 3. Client SDK and CLI Listener

- [x] 3.1 Add SSE streaming client method in `sdk/httpclient` to read incoming inbox events continuously
- [x] 3.2 Add `listen` subcommand to `cmd/888a2a-lite/cli.go` to stream incoming tasks in the terminal and optionally execute automatic ACK
- [x] 3.3 Create a runnable, zero-dependency Python worker example in `examples/worker/a2a_worker.py` demonstrating SSE connection, automated LLM / command execution, reply task dispatch, and ACK

## 4. Documentation and Verification

- [x] 4.1 Update `llms.txt`, `skills/a2a-client/SKILL.md`, and `README.md` with the new SSE streaming push endpoint and listener guidance
- [x] 4.2 Update `CHANGELOG.md` under today's date (2026-09-07)
- [ ] 4.3 Validate CI in GitHub Actions, build and publish Docker image, and verify remote smoke tests on `david@10.9.0.11` and live verification on `a2a.david888.com`
