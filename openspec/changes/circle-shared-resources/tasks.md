# Tasks

- [x] 1.1 Define Circle Resource types, quotas, `ValidateCircleResource*` (`doc` Markdown safety & `asset` HTTPS URL/MIME checks), and `CircleResourceEvent` broker fanout in `internal/hub/`.
- [x] 1.2 Implement SQLite Schema v12 (`migrateCircleResources`), `CircleResourceRepository` CRUD/proposals/expiry/CAS/idempotency, and `PruneEmptyCircles` cleanup in `internal/store/sqlite/`.
- [x] 1.3 Wire HTTP endpoints (`/hub/v1/circle/resources*`, `/hub/v1/admin/circles/{circleId}/resources*`), `BuildSystemCard` extension & limits, `RunScheduledMaintenance` TTL sweep, upload origin CSP config (`A2A888_HUB_UPLOAD_ENDPOINT`), and `?events=circle_resource` SSE stream.
- [x] 1.4 Integrate `Circle 資源 (文件 / 888box 素材)` into `chat.html` left sidebar + drawer (direct browser 888box upload, WebCrypto SHA-256, Bot proposals, 3-language i18n, zero passive `img-src`) and `admin.html` management view.
- [x] 1.5 Add `CircleResourceCache`, `[APPROVED_CIRCLE_RESOURCES_DATA_<nonce>]` prompt index, and `a2a_list_circle_resources` / `a2a_get_circle_resource` / `a2a_propose_circle_resource` MCP tools to `a2a_bridge.py` (with parity copy + tests) and update `llms.txt`, `README*.md`, `docs/`, and `CHANGELOG.md`.
- [ ] 1.6 Pass GitHub Actions CI, deploy to `dns.glsoft.ai`, verify live HTTPS endpoints, and archive `circle-shared-resources`.
