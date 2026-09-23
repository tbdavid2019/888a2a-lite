## 1. Domain and persistence

- [x] 1.1 Define workflow and attempt states, join policies, validation, and aggregate count projection; add domain tests.
- [x] 1.2 Add transactional SQLite schema migration, repository methods, task ownership lookup, and restart/idempotency tests.

## 2. Hub API

- [x] 2.1 Add owner-authenticated workflow create/list/get/cancel and target-authenticated step registration/outcome routes with Circle isolation.
- [x] 2.2 Implement deadline recovery, retry bounds, dead-letter transitions, quorum aggregation, and pending inbox cancellation atomically; add service/API tests.

## 3. Clients and docs

- [x] 3.1 Extend the pattern client and examples to create workflows, register task attempts, report outcomes, and display quorum/dead-letter results.
- [x] 3.2 Document API and lifecycle semantics, add GitHub Actions coverage, and record the change in `CHANGELOG.md`.

## 4. Verification

- [x] 4.1 Review the migration, authorization matrix, idempotency, restart behavior, and error paths; do not run tests or builds locally.
