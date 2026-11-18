# Design: Circle Shared Resources

## Data & Security Model

- **Unified Table Family (SQLite Migrations v12 & v13)**: `circle_resource_collections`, `circle_resources`, `circle_resource_proposals` (with monotonic `revision` tracking pending edits), `circle_resource_idempotency` (7-day TTL sweep).
- **Resource Validation (`internal/hub/circle_resource.go`)**:
  - `kind == "doc"`: reuses `ValidateCircleSkillContent` (plain text / allowlisted YAML frontmatter / fenced code / `https://` links; blocks raw HTML/JS, dangerous URIs, and real credentials), max `32 KiB` per doc; rejects reserved slug `"proposals"`.
  - `kind == "asset"`: reuses `a2a.ValidatePart` HTTPS URL and mediaType allowlist rules (`image/*`, `audio/*`, `video/*`, `application/pdf`, Office formats, `.zip`/`.gz`, `text/plain`, `text/csv`, `application/octet-stream`), rejecting `http://`, userinfo, fragments, and unsupported MIME types; validates client-supplied SHA-256 `contentHash` when provided and omits it when omitted (never synthesizing a pseudo file hash from metadata), while `collection_version` and full-metadata ETags capture metadata changes.
  - Quotas per Circle: max `64` active resources, `512 KiB` total `doc` content bytes, max `32` pending proposals, optional `ttlSeconds` (`0` permanent or `60..31536000`).
- **Role Governance & SSE**:
  - Follows `A2A888_HUB_CIRCLE_SKILL_WRITE_POLICY` (`human_or_admin` default or `admin_only`, advertised via `/hub/v1/status`): Circle Humans (`browser` under `human_or_admin`) and Hub Operators publish/approve/reject/delete directly with CAS (`expectedVersion`) + proposal `expectedRevision` checks; under `admin_only`, `chat.html` hides direct write/review actions; Bots submit isolated `PENDING_REVIEW` proposals via `PUT /hub/v1/circle/resources/proposals/{slug}`.
  - Capability-gated SSE: `/hub/v1/agents/{id}/inbox/stream?events=task,circle_skill,circle_resource` emits `event: circle_resource` without modifying task `Last-Event-ID` cursors.
- **Browser 888box Integration & CSP**:
  - HTTP CSP adds only the validated secret-free upload origin (default `https://box.david888.com`; rejecting userinfo, fragments, and secret query keys) to `connect-src 'self' ...` while keeping `img-src 'self' data:` strict so untrusted remote URLs never passively load inside Hub DOM.
  - `chat.html` left sidebar displays `Circle 資源` (Docs & Assets) under `Circle 技能`, letting humans upload files directly to `888box` (filling `url`, `filename`, `mediaType`, `sizeBytes`, WebCrypto SHA-256 in secure origins), paste any `https://` link, add text `doc`s, or review Bot proposals.
- **Bridge & MCP**:
  - `CircleResourceCache` stores verified `doc` bodies under `~/.a2a/circles/<hubScope>/<circleScope>/resources/<slug>.md` (`0600`) and `asset` metadata/URLs in `resource_index.json` (`0600`, max `512 KiB`, cross-process `flock` sync, ETag committed only when all active docs verify cleanly, never fetching external asset URLs).
  - `build_task_prompt_envelope` includes read-only `[APPROVED_CIRCLE_RESOURCES_DATA_<nonce>]` metadata index (`@doc:<slug>`, `@asset:<slug>`, query strings stripped on unrequested assets, `32 KiB` doc body budget) separated from `[APPROVED_CIRCLE_SKILLS_DATA_<nonce>]`.
  - MCP exposes `a2a_list_circle_resources`, `a2a_get_circle_resource`, `a2a_propose_circle_resource`.
