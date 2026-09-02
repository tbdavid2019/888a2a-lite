## 1. Domain and contract

- [ ] 1.1 Define system card, extension declaration, announcement severity, summary, cursor, and register Hub metadata types; verify the package compiles in GitHub Actions.
- [ ] 1.2 Define announcement validation rules for bounded title/summary/URL/expiry fields and control-plane trust semantics; verify invalid secret-like or oversized inputs are rejected by unit tests in CI.

## 2. SQLite persistence

- [ ] 2.1 Add the announcement schema, indexes, and migration version without changing existing Agent, inbox, event, or policy rows; verify migration runs against an existing database in CI.
- [ ] 2.2 Implement operator announcement publish and active cursor-based feed queries with monotonic IDs and expiry filtering; verify persistence, ordering, and restart recovery in integration tests.

## 3. System card and announcements API

- [ ] 3.1 Implement dynamic `GET /hub/v1/system-card.json` and public `GET /hub/v1/announcements?afterId=&limit=` using configured public URL or request origin; verify alternate Host and configured-origin responses in HTTP tests.
- [ ] 3.2 Add optional `hub` metadata to first registration and idempotent retry responses, including system card URL, feed URL, cursor, summaries, and optional extension URI; verify legacy response parsing and one-time Token behavior in HTTP tests.
- [ ] 3.3 Implement operator `POST /hub/v1/admin/announcements` with bounded validation, operator authentication, safe error envelopes, and audit event recording; verify unauthorized, invalid, and successful publish cases in CI.
- [ ] 3.4 Implement draft edit and published revision API semantics, preserving immutable published history and announcement cursor behavior; verify draft edits, revision IDs, expiry, and audit records in CI.

## 4. Client and adapter integration

- [ ] 4.1 Extend the generic HTTP client to fetch the system card and announcement feed incrementally, preserving the existing registration API; verify cursor handling, retry behavior, and response compatibility in CI.
- [ ] 4.2 Update CLI or adapter startup guidance to read system metadata as untrusted control-plane data and never promote announcement text to executable instructions; verify documentation and safe handling tests.
- [ ] 4.3 Add embedded `/admin/announcements` operator UI with in-memory Token handling, draft editing, publish/revision/expiry actions, escaped rendering, CSP, and no-store headers; verify browser behavior and credential non-persistence in CI.

## 5. Deployment and verification

- [ ] 5.1 Update README, `llms.txt`, API documentation, and changelog with the system card, announcement publish/read flows, A2A extension boundary, and no-secret rules; verify links and format in CI.
- [ ] 5.2 Run the three-Agent remote smoke flow on `david@10.9.0.11`, publish a non-sensitive announcement, verify registration summaries and cursor recovery after restart, and confirm audit records remain available; redact all credentials from output.
- [ ] 5.3 Verify GitHub Actions CI and multi-platform Docker publish are green, then confirm Watchtower updates the remote image while preserving `/data/hub.db`; mark complete only after health and announcement feed checks pass.
