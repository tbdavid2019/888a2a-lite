# Proposal

## Why

Teams collaborating inside a shared Circle on `888a2a-lite` need all member Agents (OpenClaw, Hermes, Claude Code, Codex, and custom workers) to share domain `SKILL.md` playbooks and workflows safely. On a public multi-circle Hub, private-circle human participants do not hold Hub-wide `/admin` operator tokens, while Bots also frequently draft `SKILL.md` proposals that must require explicit human review, optional expiration (TTL), real-time notifications, and host-side TOFU verification before activation.

## What Changes

- **Hub Circle Skill Registry, Draft Proposals & Optional TTL**: Add SQLite-backed Circle-scoped `SKILL.md` storage (`circle_skills`, `circle_skill_proposals`, `circle_skill_revisions`, and per-Circle monotonic `collection_version`) keyed by `(hub_id, circle_id, slug)`, integrated with `DB.migrate()` (`schema_migrations`) and `PruneEmptyCircles`. Support optional per-skill `ttlSeconds` / `expiresAt` automatic expiration, bounded quotas, and `SKILL.md`-aware Markdown safety validation (supporting allowlisted YAML frontmatter, fenced code examples, and `https://` doc links while blocking raw HTML/scripts, `on*=` handlers, `javascript:`/`data:` URIs, and real credentials).
- **Two-Role Circle Publishing (Human Approve/Delete + Bot Draft Proposals)**:
  - **Hub Circle Control**: Authenticated Circle Humans (via `chat.html` / human session) and Hub Operators (`/hub/v1/admin/...`) can create, update, approve Bot proposals, set/renew `expiresAt`, or delete Circle skills within that Circle (with CAS `expectedVersion` and `idempotencyKey`).
  - **Bot Proposal Isolation**: Circle Bots/Agents can submit draft proposals (`PENDING_REVIEW`) that NEVER enter the active Circle manifest or Agent contexts until explicitly approved by a human in the UI (subject to per-Circle pending quota).
- **Backward-Compatible SSE & Real-Time UI Notifications**:
  - Capability-gated `event: circle_skill` SSE stream (`?events=task,circle_skill`) carries metadata-only events for active skill updates, expirations, deletions, and `PENDING_REVIEW` Bot proposals—never raw `SKILL.md` bodies and never pushed to legacy Bridges.
  - `chat.html` adds a Circle Skills & Review drawer with unread proposal badges, toast banners, optional browser Desktop Notifications (`Notification` API), `@skill:<slug>` picker, safe `textContent` escaping, and human Approve/Reject/Delete/TTL controls for the Circle.
- **Bridge Scoped Sandbox, Route-2 Dual-Layer HITL & Optional Local `email` Alerts**:
  - Synchronize active Circle skills in `a2a_bridge.py` (parity between `examples/worker/a2a_bridge.py` and `internal/service/a2a_bridge.py`) into `~/.a2a/circles/<hubScope>/<circleScope>/skills/<slug>/SKILL.md` (`0600` / `0700`, `dir_fd`/`O_NOFOLLOW`, `flock`-locked `skill_pins.json`), never modifying `~/.openclaw/skills` or `~/.hermes/skills`.
  - Enforce **Deterministic TOFU Pin-by-Hash + Expiration/Deletion Revocation** (`pending_approval` blocks prompt injection; expired/deleted skills and disabled Circles purge local cache/pins; host owner inspects diff and approves via CLI or `127.0.0.1:8888` `CLIENT_HTML`).
  - Support optional **host-side `email` notifications** in Bridge (Resend API HTTPS `POST` or SMTP, cooldown-throttled, non-actionable GET links so email scanners cannot auto-approve) alongside OpenClaw/Telegram alerts, preventing anonymous spam abuse on the public Hub.
  - Unconditionally inject non-spoofable `[LOCAL_SAFETY_POLICY]` + nonce-delimited `[APPROVED_CIRCLE_SKILLS_DATA_<nonce>]` across all task types and AI backends.

## Capabilities

### New Capabilities
- `circle-shared-skills`: Circle-scoped multi-file `SKILL.md` hosting, Bot proposal queue (`PENDING_REVIEW`), human UI approval, optional per-skill `expiresAt` TTL, `SKILL.md`-aware Markdown validation, monotonic `ETag` + capability-gated SSE, browser & optional Bridge `email` notifications, isolated multi-instance sandbox cache, deterministic TOFU hash-pinning, and universal prompt guardrails.

### Modified Capabilities
<!-- Existing capabilities preserve their contracts unchanged; Multi-Circle isolation, Charter validation, and source/embedded Bridge parity remain intact. -->

## Impact

- **Hub Domain & Storage**: `internal/hub/` (skill/proposal models, `ValidateCircleSkill`, broker Circle events), `internal/store/sqlite/` (`circle_skills`, `circle_skill_proposals`, `circle_skill_revisions`, monotonic `collection_version`, `schema_migrations`, expiration sweep, `PruneEmptyCircles` cleanup).
- **Hub HTTP, SSE & UI**: `internal/service/http.go`, `internal/service/admin.html`, `internal/service/chat.html`, `internal/service/llms.txt`.
- **Universal Bridge**: `examples/worker/a2a_bridge.py` and embedded `internal/service/a2a_bridge.py` (`CircleSkillCache`, `flock`-protected `skill_pins.json`, SSE `event:` dispatch + manifest reconciliation, local `CLIENT_HTML` approval panel, `a2a bridge skill` CLI subcommands, optional Resend/SMTP notifier, universal governance prompt builder).
- **Verification & Docs**: `.github/workflows/ci.yml` (Go, Python parity/security, inline `<script>` checks), Traditional Chinese Taiwan + English documentation (using `email`).
