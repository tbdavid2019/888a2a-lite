## 1. Part validation foundation

- [x] 1.1 Add bounded URL attachment policy and shared Part/Message/Artifact validators; verify unit tests cover oneof enforcement, HTTPS-only URLs, required URL media types, filename/MIME/URL limits, raw/data rejection, and collection limits
- [x] 1.2 Add safe attachment limit configuration with conservative defaults and environment parsing; verify invalid or excessive configuration values fail validation and existing defaults remain compatible

## 2. Standard Gateway behavior

- [x] 2.1 Replace text-only standard Message validation with the shared text-plus-URL profile; verify `message:send` and `message:stream` preserve mixed Part order and reject invalid content before creating durable state
- [x] 2.2 Validate Executor update Artifacts with the same policy and preserve atomicity; verify valid URL Artifacts complete a Task without outbound fetch and invalid Artifacts leave revision, state, and events unchanged

## 3. Persistence and contract projection

- [x] 3.1 Preserve validated standard Parts in the durable inbox projection and verify URL Parts survive task creation, SQLite restart recovery, idempotent retry, Task GET, and SSE artifact projection without binary storage; add regression tests for URL ordering and bounded metadata
- [x] 3.2 Update Gateway and Per-Agent Agent Cards with the conservative URL-reference MIME profile and relay-only description; verify cards do not advertise raw/data or imply server-side media processing

## 4. Security and operational contract

- [x] 4.1 Redact complete signed URLs and query credentials from audit/event summaries while retaining safe attachment metadata; verify token-like query values never appear in audit responses or diagnostic output
- [x] 4.2 Update A2A compatibility documentation, client guidance, and `CHANGELOG.md` with the URL-reference workflow using 888box; verify docs distinguish Hub relay behavior from adapter upload/download behavior

## 5. CI acceptance

- [x] 5.1 Extend GitHub Actions fixtures for URL Message Parts, URL Artifact results, limits, restart recovery, and negative raw/data cases; verify the standard compatibility suite passes remotely in CI
- [x] 5.2 Review the complete diff for credential exposure, route compatibility, and feature-flag behavior; verify the standard Gateway remains disabled by default and no local Go test or build is run
