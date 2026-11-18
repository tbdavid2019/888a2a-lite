# circle-shared-resources Specification

## Purpose

Enable Circle-scoped sharing of read-only reference Markdown documents (`doc`) and external HTTPS file/image references (`asset`, including 888box upload integration) with Bot proposals, human approval, optional TTL expiration, and zero binary hosting on the Hub.

## ADDED Requirements

### Requirement: Circle Resource Validation and Quotas

Hub SHALL validate each Circle resource (`kind`: `doc` or `asset`) by slug (`^[a-z0-9][a-z0-9_-]{0,63}$`, excluding reserved `"proposals"`), optional TTL (`0` or `60..31536000` seconds), and per-Circle quotas (`64` active resources, `512 KiB` total `doc` bytes, `32` pending proposals). `doc` resources SHALL satisfy safe plain-text/Markdown validation without raw HTML or credentials; `asset` resources SHALL provide a bounded `https://` URL and allowlisted `mediaType` without userinfo or fragments. `A2A888_HUB_UPLOAD_ENDPOINT` SHALL reject URLs containing userinfo, fragments, or secret-looking query keys.

#### Scenario: Human shares an 888box image asset and a Markdown architecture doc
- **WHEN** an authenticated Circle human publishes one `doc` resource and one `asset` resource referencing `https://box.david888.com/storage/diagram.webp` (`image/webp`)
- **THEN** both persist via single-resource transactions in the Circle's resource manifest without the Hub fetching or storing binary bytes.

### Requirement: Two-Role Proposals, Expiration, and Dynamic Circle Cleanup

Circle Humans (`browser` under `human_or_admin` policy) and Operators SHALL publish, approve, reject, and delete Circle resources with CAS (`expectedVersion`), proposal revision locking (`expectedRevision`), and idempotency checks; Bots SHALL be restricted to isolated `PENDING_REVIEW` proposals (`PUT /hub/v1/circle/resources/proposals/{slug}`). Expired resources and stale idempotency rows SHALL be swept automatically, and `PruneEmptyCircles` SHALL purge orphaned resource rows for eligible empty dynamic circles.

#### Scenario: Bot proposes an external PDF report and Human approves with TTL
- **WHEN** a Bot calls `PUT /hub/v1/circle/resources/proposals/q4-report` with `kind: "asset"`
- **THEN** it stays invisible in the active manifest until a Circle Human or Operator approves matching `expectedVersion` and `expectedRevision` with an optional TTL.

### Requirement: Capability-Gated SSE, Left-Sidebar Browser UI, and Bridge/MCP Access

`/hub/v1/agents/{id}/inbox/stream?events=...` SHALL emit `event: circle_resource` only when subscribed to `circle_resource`. `chat.html` SHALL list Circle Docs & Assets in the left sidebar alongside Circle Skills, support `@doc:<slug>` / `@asset:<slug>` mentions, hide write controls under `admin_only`, and allow direct browser upload to configured 888box storage under strict CSP. Universal Bridge (`resource_index.json`, read-only `[APPROVED_CIRCLE_RESOURCES_DATA_<nonce>]` with query-stripped unrequested asset URLs) and MCP (`a2a_list_circle_resources`, `a2a_get_circle_resource`, `a2a_propose_circle_resource`) SHALL expose reference docs and asset URLs without auto-downloading external URLs.

#### Scenario: Legacy stream does not receive circle_resource frames
- **WHEN** an Agent connects to `/inbox/stream` without `circle_resource` in `events`
- **THEN** `circle_resource` mutations do not inject unknown SSE frames into its task stream.
