# A2A Multi-Agent Collaboration Patterns & Workflow Specification

This specification defines how multi-agent systems coordinate over the `888a2a-lite` communication infrastructure across four classic patterns (Sequential Pipeline, Parallel Fan-out/Fan-in, Supervisor Dynamic Routing, and Adversarial Debate), clearly delineating the responsibilities between the Hub core, Client Workflow, and Agent runtimes.

---

## 1. Responsibility Boundaries

In a distributed multi-agent system, responsibilities are structured into three distinct layers:

```text
┌─────────────────────────────────────────────────────────────┐
│ 1. Hub Core Layer (Go + SQLite WAL)                         │
│    - Durable Mailbox, Online Lease/Heartbeat, Instant ACK,  │
│      SSE real-time streaming, and group distribution        │
│    - Keeps transport lightweight; does not execute LLM code │
└──────────────────────────────┬──────────────────────────────┘
                               │ A2A Message Payload (Envelope JSON)
┌──────────────────────────────▼──────────────────────────────┐
│ 2. Client / Workflow Layer (Python / Node.js / CLI)         │
│    - Topology state management: Pipeline, Parallel Fan-out, │
│      Supervisor capability routing, bounded Debate rounds   │
│    - Decouples Transport ACK from Business Completion       │
│    - Enforces Anti-Echo Storm Guard & terminal conditions   │
└──────────────────────────────┬──────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────┐
│ 3. Agent Cognitive & Runtime Layer (LLM / Agent Harness)    │
│    - OpenClaw, Claude Code, Hermes, Codex, Python/Bash      │
│    - Parses Envelope.payload to reason, invoke tools, and   │
│      generate artifacts                                     │
└─────────────────────────────────────────────────────────────┘
```

### Critical Concept: Transport ACK vs. Business Completion
* **Hub ACK (`POST /hub/v1/agents/{id}/inbox/{seq}/ack`)**:
  - Only acknowledges that the target recipient process has ingested the message into its local queue.
  - Must be invoked immediately (<50ms) upon receipt (Instant ACK on Ingest), clearing the Hub sequence out of `PENDING`.
  - **Hub ACK does NOT mean the LLM finished inference or that the business task succeeded.**
* **Business Completion (Result Delivery)**:
  - After 10~60 seconds of LLM reasoning and tool execution, the final output is wrapped into a new task message sent to downstream peers or the initiator.
  - Business lifecycle progress is marked explicitly using `step_id`, `correlation_id`, and `terminal: true`.

---

## 2. Structured Envelope Specification

For multi-agent workflows, the task `message` payload should be serialized as a JSON Envelope:

```json
{
  "workflow_id": "wf-1726650000-a1b2",
  "correlation_id": "corr-1726650000-c3d4",
  "flow_type": "pipeline",
  "step_id": "code_review",
  "reply_to": "code_generate",
  "round": 1,
  "max_rounds": 3,
  "role": "auditor",
  "required_capabilities": ["lang/go", "security/audit"],
  "terminal": false,
  "payload": {
    "status": "IN_PROGRESS",
    "artifacts": ["..."]
  },
  "metadata": {
    "initiator_id": "agent-user-001"
  },
  "created_at": "2026-09-18T08:55:00Z"
}
```

### Field Definitions

| Field | Type | Description |
| :--- | :--- | :--- |
| `workflow_id` | `string` | Global workflow trace ID throughout the business lifecycle. |
| `correlation_id` | `string` | Context correlation ID. Shared across parallel fan-out subtasks for aggregator matching. |
| `flow_type` | `string` | Topology type: `pipeline`, `parallel`, `supervisor`, `debate`. |
| `step_id` | `string` | Current workflow step identifier (e.g. `draft`, `audit`, `completed`). |
| `reply_to` | `string?` | Upstream `step_id` or task ID this step responds to. |
| `round` | `int` | Current iteration or debate round (starts at 1). |
| `max_rounds` | `int` | Maximum allowable rounds before forced termination. |
| `role` | `string` | Active sender role: `proposer`, `critic`, `judge`, `supervisor`, `worker`, `aggregator`. |
| `required_capabilities` | `string[]` | Required agent capability tags for dynamic routing. |
| `terminal` | `bool` | **Terminal Flag**. When `true`, signals end of workflow. Recipient MUST NOT reply. |
| `payload` | `any` | Business data payload (text, object, or structured report). |
| `metadata` | `object` | Contextual and trace metadata. |

---

## 3. Four Core Patterns

### Pattern 1: Sequential Pipeline
* **Topology**:
  `[Initiator] -> [Agent A: Drafter] -> [Agent B: Auditor] -> [Agent C: Publisher] -> [Initiator]`
* **Rules**:
  1. Each relay agent sends an Instant ACK upon ingest (<50ms).
  2. Agent executes reasoning and invokes `envelope.next_step(...)` to advance `step_id`.
  3. Final stage sets `terminal: true` and delivers output back to the initiator.
* **Example**: `examples/patterns/01_pipeline_demo.py`

---

### Pattern 2: Parallel Fan-out / Fan-in Aggregator
* **Topology**:
  `[Aggregator] => [Worker 1, Worker 2, Worker 3] => [Aggregator Synthesis]`
* **Rules**:
  1. Subtasks share the same `workflow_id` and `correlation_id`.
  2. Workers process tasks concurrently and return results asynchronously.
  3. **Quorum & Timeout**: Aggregator enforces a deadline (e.g. 10s) and minimum quorum (e.g. 2/3), tolerating unresponsive nodes and producing a consolidated report.
* **Example**: `examples/patterns/02_parallel_fanout.py`

---

### Pattern 3: Supervisor Dynamic Router
* **Topology**:
  `[User] -> [Supervisor] -> (Query GET /hub/v1/agents) -> [Match Capabilities] -> [Worker Execution] -> [Supervisor Synthesis]`
* **Rules**:
  1. Zero hardcoded addresses: Supervisor queries online Agent Cards in real time.
  2. Matches `required_capabilities` against agent capability tags (`lang/go`, `security/audit`, etc.).
  3. Handles fallback gracefully if no matching agent is currently online.
* **Example**: `examples/patterns/03_supervisor_router.py`

---

### Pattern 4: Adversarial Debate & Bounded Review
* **Topology**:
  `[Proposer] <-> [Critic] (Max 2 Rounds) -> [Judge Arbiter] (terminal: true) -> [Clean Exit]`
* **Safety Guards (Production Lesson #7)**:
  1. **Bounded Rounds**: Strictly capped at `max_rounds=2`.
  2. **Judge Arbitration**: Handed over to Judge once max rounds are reached; the max round itself remains processable, while only an exceeded round is rejected.
  3. **Anti-Echo Storm Guard**: Judge outputs `terminal: true` and `[[A2A_NO_REPLY]]`. Both sides ACK on ingest and terminate cleanly without reciprocal chatter.
* **Example**: `examples/patterns/04_debate_group.py`

---

## 4. Verification & Testing

Unit tests for Envelope serialization, step transitions, capability matching, and Anti-Echo Storm triggers are available:

```bash
python3 -m unittest examples/patterns/test_patterns.py
```
