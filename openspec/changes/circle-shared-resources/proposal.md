# Proposal: Circle Shared Resources (`doc` + `asset` via 888box / HTTPS Manifest)

## Summary

Extend `888a2a-lite`'s Circle-scoped collaboration workspace so humans and Agents inside the same Circle can share:
1. **Reference Markdown/Plain-Text Documents (`kind: "doc"`)**: bounded text docs (notes, specs, SOPs, datasets descriptions) hosted directly on the Hub without being treated as executable Agent skills.
2. **External Image, PDF, Audio, Video & File Asset References (`kind: "asset"`)**: opaque HTTPS URL references (with `filename`, `mediaType`, `sizeBytes`, optional SHA-256 `contentHash`) backed by external object storage such as `888box` (`https://box.david888.com/api.php`), never storing or proxying binaries on the Hub.

## Goals & Boundaries

- Zero binary hosting and zero SSRF: the Hub validates HTTPS URL metadata and `doc` Markdown text only; binaries live on `888box` or user-chosen HTTPS object storage.
- Reuse Circle isolation, masked `404` on cross-circle probes, `PENDING_REVIEW` Bot proposals, optional TTL expiration (`ttlSeconds`/`expiresAt`), monotonic `collection_version` (`ETag`), `PruneEmptyCircles` cleanup, and capability-gated SSE (`?events=task,circle_skill,circle_resource`).
- Browser-first UX in `chat.html` left sidebar alongside Circle Skills, supporting direct browser upload to `https://box.david888.com` (via CORS + explicit CSP `connect-src` allowlist) with optional local `WebCrypto` SHA-256 calculation, safe external link opening, and zero passive `img-src` hotlinking.
- Bridge + MCP support (`a2a_list_circle_resources`, `a2a_get_circle_resource`, `a2a_propose_circle_resource`) with read-only sandbox sync for `doc` files and metadata-only `asset` manifests (no automatic remote download).
