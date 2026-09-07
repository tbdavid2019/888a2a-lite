## Why

Currently, task delivery in `888a2a-lite` relies exclusively on passive store-and-forward mailbox polling (`GET /hub/v1/agents/{agentId}/inbox`). Because LLM agents (such as OpenClaw, Hermes, Codex, and chatbot harnesses) are typically request-driven or run behind local NAT/firewalls without an explicit background polling daemon, tasks sent between peers remain indefinitely in the Hub's pending queue with no downstream action.

Introducing Server-Sent Events (SSE) streaming push gives agents an outbound, NAT-friendly persistent connection to the Hub. Incoming tasks and group messages are pushed with near-zero latency, enabling instant LLM wake-up and conversational turn completion while preserving durable SQLite store-and-forward guarantees.

## What Changes

- **SSE Streaming Endpoint**: Add `GET /hub/v1/agents/{agentId}/inbox/stream` supporting `text/event-stream` response, authenticated with `X-Agent-ID` and `Authorization: Bearer <agentToken>`.
- **Real-Time Push on Delivery**: When direct tasks or group messages are submitted to the Hub, the Hub persists them into SQLite and immediately dispatches the event to the active SSE stream for the target agent.
- **Initial Catch-Up & Reconnection**: On stream connection, any un-ACKed pending inbox items (`sequence > afterSequence` or `Last-Event-ID`) are immediately streamed to catch up missed messages.
- **Connection Keep-Alive & Presence Lease**: The Hub emits periodic keep-alive comments (`: keepalive`) every 15-30 seconds to prevent reverse proxy/gateway timeouts and automatically refreshes the agent's sliding presence lease without requiring explicit heartbeat calls.
- **Agent Worker Reference**: Provide a lightweight, runnable Python/CLI agent adapter listener that connects via SSE, invokes local task handling, and returns response tasks.
- **Full Backward Compatibility**: The existing polling endpoint (`GET /inbox`) and ACK workflow (`POST /inbox/{seq}/ack`) remain fully supported and unchanged.

## Capabilities

### New Capabilities
<!-- None -->

### Modified Capabilities
- `durable-agent-mailbox`: Extend mailbox specification to require real-time SSE stream delivery, replay from sequence or `Last-Event-ID`, connection keep-alive, and immediate notification dispatch upon task creation.

## Impact

- `internal/hub`: Event broker interface and notification registry for active agent stream subscribers.
- `internal/service`: HTTP handler for `GET /hub/v1/agents/{agentId}/inbox/stream`, handling header validation, flushing, event serialization, and keep-alive ticker.
- `sdk/httpclient`: Client-side method for opening and streaming inbox events.
- `cmd/888a2a-lite`: New CLI subcommand `listen` for streaming inbox items in real time.
- Documentation & Examples: Update `llms.txt`, `skills/a2a-client/SKILL.md`, and add runnable agent worker examples.
