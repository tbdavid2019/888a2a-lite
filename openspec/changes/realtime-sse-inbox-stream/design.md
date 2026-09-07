## Context

888a2a-lite provides durable task delivery via SQLite store-and-forward tables. While polling works for periodic agents, autonomous LLM agents (such as OpenClaw and Hermes) require instant event notification to respond in conversational timeframes without polling lag or missing messages. See `proposal.md` for motivation.

## Goals / Non-Goals

**Goals:**
- Provide an event-driven SSE streaming endpoint `GET /hub/v1/agents/{agentId}/inbox/stream` with near-zero latency.
- Thread-safe, in-memory notification broker within `internal/hub` and `internal/service` without external dependencies.
- Catch up any pending un-ACKed inbox items upon connection.
- Emit periodic keep-alives (`: keepalive\n\n`) to prevent proxy timeouts and maintain presence lease.
- Standard ACK and task submission endpoints remain identical.

**Non-Goals:**
- Bidirectional WebSockets: Outbound task replies and ACKs continue to use existing standard HTTP POST endpoints.
- External pub-sub backends (Redis/NATS): SQLite + in-memory channels provide the exact single-instance Hub requirements.

## Decisions

### Decision 1: Use Server-Sent Events (SSE) over WebSockets
- **Rationale**: SSE is unidirectional HTTP/1.1 streaming using standard `text/event-stream`. It reuses existing HTTP authentication headers (`X-Agent-ID`, `Authorization: Bearer`), works with standard HTTP clients (Python `requests`/`urllib3`, Go `http.Client`, curl, EventSource), and requires no third-party protocol dependencies in Go.
- **Alternatives considered**:
  - WebSockets: Required protocol upgrade framing, external library or `golang.org/x/net/websocket`, and custom authentication handshakes.

### Decision 2: In-Memory Channel Broker for Instant Dispatch
- **Rationale**: When `DeliverTask` or group fan-out writes to the SQLite database, it notifies an in-memory `StreamBroker`. The broker broadcasts the newly assigned inbox item directly to any open channels for that `targetAgentId`.
- **Alternatives considered**:
  - SQLite change polling: Polling SQLite every 50-100ms introduces wasteful disk I/O and latency. In-memory channel dispatch is instant and zero-overhead.

### Decision 3: Initial Catch-Up and Last-Event-ID Resumption
- **Rationale**: When a client connects, the handler queries un-ACKed items from SQLite (`sequence > afterSequence` or parsed from `Last-Event-ID`) and flushes them first. Once caught up, the connection subscribes to live notifications from the broker. This guarantees that messages sent while the agent was disconnected are never lost.

### Decision 4: Sliding Presence Lease via Keep-Alive
- **Rationale**: The SSE loop executes a `time.Ticker` every 15-30 seconds emitting `: keepalive\n\n`. Concurrently, it updates `LastSeenAt` in the repository, automatically maintaining the agent's `ONLINE` presence without requiring separate heartbeat calls.

## Risks / Trade-offs

- **[Reverse Proxy Buffering]** → Mitigation: Set `X-Accel-Buffering: no`, `Cache-Control: no-cache, no-transform`, and call `http.Flusher.Flush()` immediately after every event and keepalive chunk.
- **[Slow Consumer / High Volume Fan-out]** → Mitigation: Use buffered notification channels per subscriber (e.g. buffer capacity 64) with non-blocking send; if a client stalls, it drops notification and subsequent reconnect catches up via durable SQLite sequence replay.
