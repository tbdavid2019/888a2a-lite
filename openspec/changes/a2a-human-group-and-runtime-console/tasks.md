## 1. Local Runtime Hub (Backend & Inspection API)

- [ ] 1.1 Implement `detect_runtimes()` in `a2a_bridge.py` scanning PATH for openclaw, claude, goose, hermes, codex, and opencode; verify returns structured list with binary paths and ready/needed status.
- [ ] 1.2 Implement `GET /api/runtimes` and `POST /api/runtimes/custom` in `LocalUIHandler`; verify unit tests cover JSON serialization and custom command validation.
- [ ] 1.3 Implement service inspection and management endpoints `POST /api/service/install` and `POST /api/service/restart`; verify LaunchAgent and systemd status interrogation.

## 2. Agent Runtimes Visual Panel (Web UI)

- [ ] 2.1 Add "Agent Runtimes" navigation tab and container in `CLIENT_HTML`; verify smooth switching between Chat and Runtimes views.
- [ ] 2.2 Render runtime cards with status badges (`Ready` green badge vs `CLI needed` amber badge), active backend indicator, and binary execution paths.
- [ ] 2.3 Add `+ Add Runtime` modal in UI for registering custom AI commands; verify custom runtime appears in list and is selectable.
- [ ] 2.4 Add one-click service control buttons ("Install Background Daemon" / "Restart Service") to runtime cards; verify live status updates.

## 3. Human Group Chat Workspace (Web UI)

- [ ] 3.1 Add "Groups" sidebar navigation in `CLIENT_HTML` fetching active groups from Hub; verify group switching loads active conversation context.
- [ ] 3.2 Implement group message timeline rendering human messages and bot replies with distinct avatars, sender badges, and timestamps.
- [ ] 3.3 Implement `@` mention autocomplete popup in group message input composer; verify typing `@` filters the active group member roster.

## 4. Selective Mention Dispatch & Anti-Echo Verification

- [ ] 4.1 Implement client-side mention parsing on submit: untagged messages send `replyPolicy: ACK_ONLY`, tagged messages send `replyPolicy: MENTIONED_ONLY` with extracted `mentions` array.
- [ ] 4.2 Verify receiving bridge handles `replyPolicy: MENTIONED_ONLY`: unmentioned bots send Instant ACK (<50ms) and terminate silently without LLM execution.
- [ ] 4.3 Verify receiving bridge handles `replyPolicy: MENTIONED_ONLY`: mentioned bots send Instant ACK, invoke local LLM backend, and dispatch reply to group.
- [ ] 4.4 Add unit and integration tests in `test_a2a_bridge.py` covering runtime inspection, group mention parsing, and silent ACK behavior; verify all tests pass.
