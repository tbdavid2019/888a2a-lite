## 1. SQLite Schema Migration & Store Scoping

- [ ] 1.1 Add `circle_id` column with default `'public'` to `agents`, `tasks`, and `groups` tables in `internal/store/sqlite/store.go`
- [ ] 1.2 Update Go data structures (`Agent`, `Task`, `Group`, `GroupMember`) with `CircleID string` field
- [ ] 1.3 Add database indices for `(circle_id, status)` on agents and tasks
- [ ] 1.4 Update query methods in SQLite store (`ListAgents`, `GetAgent`, `ListGroups`, `CreateTask`, `CreateGroup`) to support circle scoping

## 2. Key-to-Circle Derivation & Registration

- [ ] 2.1 Implement circle derivation logic supporting `A2A888_HUB_SHARED_KEYS` aliases and deterministic `circle-<sha256[:12]>` fallback
- [ ] 2.2 Update `POST /hub/v1/agents/register` to assign agents without a key to `public`, and agents with a key to their corresponding circle
- [ ] 2.3 Store caller's `circle_id` in authenticated HTTP request context across all `/hub/v1` routes

## 3. Strict Air-Gapped Routing & 404 Error Masking

- [ ] 3.1 Restrict `GET /hub/v1/agents` peer directory strictly to caller's `circle_id`
- [ ] 3.2 Enforce 404 Not Found in `GET /hub/v1/agents/{id}` and Agent Card lookup when target belongs to a different circle
- [ ] 3.3 Enforce 404 Not Found in `POST /hub/v1/agents/{target}/tasks` when target belongs to a different circle, masking existence
- [ ] 3.4 Restrict group creation to inherit creator's `circle_id`, and reject cross-circle invitations with 404 Not Found

## 4. Operator Admin & Audit Inspection

- [ ] 4.1 Update operator admin endpoints (`/hub/v1/admin/agents`, `/hub/v1/admin/events`) to include `circle_id` in responses and support filtering
- [ ] 4.2 Add Circle filtering selector and Circle badge rendering in the Web Admin Console (`admin.html`)

## 5. End-to-End Testing & Verification

- [ ] 5.1 Write tests for deterministic circle derivation from shared keys and aliases
- [ ] 5.2 Write integration tests for public vs private circle isolation (registration, peer listing, task sending)
- [ ] 5.3 Write integration tests verifying cross-circle task dispatch and group invitations return HTTP 404
- [ ] 5.4 Verify backward compatibility with existing client bridge (`a2a_bridge.py`)

## 6. Documentation & Sync

- [ ] 6.1 Update `README.md`, `llms.txt`, and architecture documentation with the Multi-Circle Parallel Universe guide
- [ ] 6.2 Record changes in `CHANGELOG.md` under today's date
