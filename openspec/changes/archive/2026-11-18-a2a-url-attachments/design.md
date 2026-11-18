## Context

See `proposal.md` for motivation. The existing standard adapter keeps the complete A2A-shaped Part and Artifact fields, but `TextFromParts` only permits text for inbound messages. The executor update route can persist Artifact JSON without applying equivalent Part validation. The Hub uses bounded JSON bodies and SQLite WAL persistence and must remain a relay, not an execution or object-storage service.

## Goals / Non-Goals

**Goals:**

- Add one validation policy shared by standard Message input and executor Artifact output.
- Support external HTTPS URL references for common media and file types.
- Preserve original Part order and Task correlation through SQLite restart and SSE projection.
- Keep binary content out of SQLite and keep 888box credentials outside the Hub.
- Make Agent Card media declarations match the implemented URL-reference profile.

**Non-Goals:**

- No inline `raw` bytes or `data` Parts.
- No Hub-side upload endpoint, URL fetcher, proxy, content scanner, or thumbnailer.
- No direct server-side 888box API integration or 888box token configuration.
- No migration of existing text messages or task rows.
- No change to legacy `/hub/v1` direct message semantics beyond the standard A2A task update route.

## Decisions

### 1. Use a shared validator at both public standard boundaries

Add a pure validator in `internal/a2a` that validates a Part, a Message, and an Artifact collection. The HTTP service calls it before task creation and before applying executor updates. This prevents the current split where input Messages are text-only but output Artifacts bypass validation.

The validator will model the A2A oneof rule explicitly because the Go JSON struct can otherwise contain multiple content fields. It will accept text and URL content, reject raw/data, require HTTPS and a media type for URL Parts, and enforce bounded URL, filename, MIME, Part, Artifact, and aggregate metadata limits.

Alternative: keep validation in `standard.go` and duplicate it in `standard_http.go`. Rejected because the two paths would drift again.

### 2. Deliver validated standard Parts through the durable inbox

Extend the optional standard delivery projection on `hub.InboxItem` with the original validated Parts and persist them in a bounded JSON column. The existing flattened `Message` string remains for legacy adapters and human-readable fallback; standard-aware adapters use `parts` to recover URL, MIME type, filename, and ordering. Legacy `/hub/v1` clients remain compatible because the field is optional and empty for non-standard deliveries.

Alternative: encode attachments into the flattened Message as Markdown. Rejected because it loses MIME and filename semantics, makes signed URL redaction harder, and prevents a standard-aware adapter from reconstructing the original A2A Message.

### 3. Allow URL references without server-side dereferencing

The Hub will treat `Part.url` as opaque data after syntax and policy validation. It will not make an outbound request, follow redirects, or inspect the referenced resource. This keeps SSRF and external availability outside the Hub request path and makes 888box integration adapter-owned.

Alternative: add a Hub download proxy. Rejected for this change because it requires DNS/IP policy, redirect handling, streaming limits, malware scanning, cache/retention policy, and a separate credential model.

### 4. Keep URL values in Task data but redact audit summaries

Task history and Artifact results need the original URL so the target adapter and requester can use the attachment. Durable task JSON therefore continues to preserve the bounded URL Part. Audit events must use attachment metadata or a redacted indicator and never copy a complete signed URL or query credential.

Alternative: strip URL query strings before persistence. Rejected because that would break presigned URL retrieval and would silently change the A2A Part value.

### 5. Advertise a conservative MIME profile

The Card will retain `text/plain` and add URL-reference modes for `image/*`, `audio/*`, `video/*`, `application/pdf`, and `application/octet-stream`. The Card description will state that modes describe relay support; adapter capability determines whether the target can actually process the resource.

Alternative: advertise `*/*`. Rejected because it overstates supported content and prevents clients from making useful capability decisions.

### 6. Preserve storage compatibility

One additive SQLite migration adds an optional `parts_json` column to `inbox_item`; existing rows default to `[]`. Existing task and artifact JSON columns can store the bounded URL metadata. Inline bytes remain rejected, so this change does not create a binary amplification path. Existing rows remain valid and continue to be readable as text-only data.

## Risks / Trade-offs

- **[Risk]** A valid URL may expire or become unavailable after task creation. → **Mitigation:** document that URL lifetime is owned by the adapter/storage workflow; optionally use a short-lived URL only for the expected processing window.
- **[Risk]** A signed URL is still a bearer secret while present in Task history. → **Mitigation:** bound retention through existing task policy, redact audit/log output, and recommend opaque attachment IDs or short TTL URLs for adapters.
- **[Risk]** A client may confuse relay support with multimodal reasoning support. → **Mitigation:** Card skill description explicitly separates URL transport from adapter processing.
- **[Risk]** Existing tests assume all non-text Parts fail. → **Mitigation:** update only standard URL-reference scenarios; preserve raw/data rejection and legacy `/hub/v1` behavior.
- **[Risk]** Future append-style Artifact updates may repeat the full Artifact list. → **Mitigation:** preserve current task update semantics in this change and add a follow-up if true chunk append is required.

## Migration Plan

1. Ship shared URL Part validation and tests while the standard Gateway remains feature-flagged.
2. Enable URL modes only when CI passes the standard, persistence, and security fixtures.
3. Deploy the Hub with existing SQLite data; no migration is needed.
4. Update adapters to upload files to 888box or another object store and submit bounded HTTPS URLs.
5. Roll back by disabling `A2A888_HUB_STANDARD_ENABLED` or reverting the change; existing text tasks and legacy routes remain readable.

## Open Questions

None for this change. Inline bytes, Hub-managed uploads, and object-store token brokering require a separate design.
