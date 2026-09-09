## Context

Phase 3 builds upon the multi-agent transport and group infrastructure from Phase 1 and Phase 2. To directly rival the user experience of Block Buzz while maintaining 888a2a-lite's lightweight zero-dependency architecture, Phase 3 upgrades the local workspace (`http://localhost:8888`) into an interactive command center with visualized runtime management and human-in-the-loop group chatting.

## Goals / Non-Goals

**Goals:**
- Provide a visual Agent Runtimes tab in `http://localhost:8888` displaying PATH detection status (Ready vs `CLI needed`).
- Enable adding custom runtimes and generating one-click background services (launchd / systemd).
- Implement human-in-the-loop group chat with `@` mention auto-completion.
- Enforce silent observation via `replyPolicy: ACK_ONLY` for general human chatter, and wake targeted LLMs via `replyPolicy: MENTIONED_ONLY` when `@` mentioned.

**Non-Goals:**
- Electron or heavy desktop packaging (remains zero-dependency browser UI powered by Python standard library).
- Modifying remote Hub database schemas (leverages existing Phase 2 group routing primitives).

## Decisions

### 1. In-process UI Server extension
- **Decision**: Extend `LocalUIServer` in `examples/worker/a2a_bridge.py` directly without external web frameworks.
- **Rationale**: Keeps total deployment size under 1MB, guarantees instant startup (<100ms), and runs across any POSIX environment with Python 3.10+.

### 2. Selective replyPolicy for human input
- **Decision**: Messages without `@` mentions automatically receive `replyPolicy: ACK_ONLY`; messages with `@` receive `replyPolicy: MENTIONED_ONLY`.
- **Rationale**: Eliminates echo storms and prevents all bots in the group from replying at once to a simple human comment.

## Risks / Trade-offs

- **[Risk: Stale runtime status if CLI installed while UI open]**
  - *Mitigation*: Provide an active "Rescan Runtimes" button on the UI to re-probe PATH without restarting the server.
