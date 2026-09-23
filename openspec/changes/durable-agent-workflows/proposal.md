# Proposal

## Why

Collaboration examples currently carry workflow identity and fan-out coordination only inside client-side `Envelope` values. A Hub restart or coordinator exit leaves no queryable workflow summary, while retries, deadlines, cancellation, dead letters, and quorum outcomes are spread across clients and individual tasks. Persisting a small workflow record in the Hub makes coordination recoverable without moving model execution into the Hub.

## What Changes

- Add a Circle-scoped durable workflow record with owner, join policy, quorum, deadline, lifecycle state, and timestamps.
- Track workflow steps and their task attempts, retry limits, outcomes, error summaries, and dead-letter state.
- Add authenticated create, step registration, outcome, owner read/list, and cancel endpoints. Participants receive only the status of the step they mutate.
- Recompute workflow state transactionally from step outcomes and the selected join policy; preserve state across Hub restart.
- Update the pattern client and examples to report step completion, retry, timeout, cancellation, and quorum outcomes through the Hub.
- Keep actual Agent execution and retry scheduling in clients; the Hub records and coordinates state only.

## Capabilities

### New Capabilities
- `durable-agent-workflows`: Persistent, Circle-isolated workflow lifecycle and query API for multi-Agent tasks.

### Modified Capabilities
- `a2a-standard-transport`: Require executor reply message identity and task/context correlation; keep Agent Card MIME declarations aligned with accepted URL attachment formats.

## Impact

- Go workflow domain, SQLite migrations and repository, service and HTTP routes.
- `PatternHubClient` and collaboration examples.
- OpenSpec specs, API documentation, `CHANGELOG.md`, and GitHub Actions fixtures.
- No new runtime dependency; no Hub-side model/runtime execution.
