# Design

## Context

The Hub already persists inbox deliveries and standard A2A task states. Pattern examples send opaque JSON `Envelope` values through the legacy task route, so the Hub cannot currently group those deliveries into a workflow or compute a fan-out outcome. The Lite project uses a `Store` interface, SQLite WAL, Circle-scoped Agent principals, and transactional SQLite repositories.

## Goals / Non-Goals

**Goals:** Persist workflow policy, step attempts, outcomes, deadline, cancellation, dead letters, and computed join results. Expose owner-authorized queries and target-authorized outcome reporting. Keep retry scheduling and Agent execution in clients.

**Non-Goals:** Execute or schedule Agent work in the Hub; replace A2A Task state; provide cross-Hub federation; interrupt a running remote model process; add UI or analytics dashboards.

## Decisions

- Add a `workflow` domain model plus workflow and attempt tables. Store the Hub ID, Circle ID, owner Agent ID, policy, expected step count, quorum, retry limit, deadline, state, and timestamps. Preserve each attempt as a separate row so retry and dead-letter history survives.
- Use Hub/Circle-unique workflow IDs and owner-scoped idempotency keys. Workflow reads/list/cancel require the owner token. Step outcome reports require the target Agent token and must match the step's assigned target. A participant already assigned to a step may register child steps; task ownership checks bind each child attempt to the authenticated sender. All reads and writes include Hub and Circle scope.
- Link attempts to already persisted inbox task IDs. Registration checks that the task was sent by the authenticated owner/participant to the stated target in the same Circle. This keeps existing delivery APIs stable and supports sequential delegation as well as coordinator fan-out.
- Compute terminal outcomes in the same SQLite transaction as each report. `QUORUM` completes once minimum successes are reached and fails once quorum becomes impossible; `FIRST_SUCCESS` completes on first success; `ALL_SUCCESS` fails on any exhausted failure and completes when all expected steps succeed; `PARTIAL_FAILURE` completes after all expected steps terminate when minimum successes are reached.
- Clients own retries. A failed attempt can be followed by a new monotonically numbered attempt within the configured retry limit and deadline. Exhausted failures become `DEAD_LETTER`; deadline expiry moves active steps to `TIMED_OUT` and the workflow to `TIMED_OUT`.
- Cancellation marks active workflow steps canceled and cancels linked inbox items that remain pending. The Hub cannot interrupt an already running Agent process; a late outcome is rejected as a conflict.
- Keep the API additive under `/hub/v1/workflows`; no new dependency or change to existing task semantics is required.

## Risks / Trade-offs

- [The owner can crash after sending a task but before registering its step] → Step registration is idempotent and accepts the original task ID, allowing local outbox recovery to finish registration.
- [Workflow deadline transitions may be delayed while the Hub is down] → Every query and write lazily expires due workflows; startup recovery also marks expired records before serving requests.
- [Quorum policy can be misunderstood] → Return policy, expected count, quorum, per-state counts, and per-step attempt history in every workflow query.
- [Cancellation cannot stop already-running external inference] → Document cancellation as durable Hub cancellation; late reports are rejected and clients remain responsible for runtime interruption.

## Migration Plan

Create additive SQLite tables and indexes through the existing versioned migration path. Existing records remain untouched. Rollback uses a prior application binary; the additive workflow tables can remain in the database without affecting older binaries.
