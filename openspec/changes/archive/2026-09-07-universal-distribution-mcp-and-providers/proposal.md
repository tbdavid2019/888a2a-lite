## Why

888a2a-lite currently provides a robust Go Hub and a resilient Python Universal Bridge (`a2a_bridge.py`) with WAL-backed local queuing and system service installation. However, onboarding new machines and agents still suffers from friction:
1. **Deployment friction**: Deploying an agent across multiple remote nodes (e.g. `10.0.0.10`, `10.9.0.9`, Mac) currently requires cloning the repository or copying scripts manually via `scp`/`git pull`, whereas modern tools like VOKO offer one-line global installation (`npm install -g @voko/lite` or `curl ... | bash`).
2. **Ecosystem accessibility**: Coding environments like Claude Desktop, Cursor, and Windsurf require a standard Model Context Protocol (MCP) stdio interface to interact with the A2A network as first-class tools.
3. **Provider coverage**: While OpenClaw, Hermes, and OpenAI backends are supported, emerging tools like Claude Code (`claude -p`), Codex CLI, and arbitrary shell command runners are not yet first-class backends in the bridge.
4. **Interactive human visibility**: While the Hub provides an Operator Control Console (`/admin`) for announcements and message monitoring, operators lack an interactive chat interface to directly converse with active agents from the browser.

Closing these gaps will make 888a2a-lite as effortlessly deployable as VOKO while retaining its lean, crash-safe, zero-dependency Go/Python core.

## What Changes

- **Hub One-Line Installer (`/install.sh`)**: The Hub serves an official POSIX shell installer at `GET /install.sh` that downloads the standalone bridge, registers the agent, and configures systemd (Linux) or launchd (macOS) with a single command (`curl -fsSL https://.../install.sh | bash -s -- ...`).
- **NPM Package Distribution**: Provide a lightweight `package.json` package (`888a2a`) exposing global binaries `a2a` and `a2a-bridge`, allowing Node.js/OpenClaw users to run `npm install -g git+https://github.com/tbdavid2019/888a2a-lite.git` (or `npm install -g 888a2a` upon registry publication) for instant cross-host installation.
- **Model Context Protocol (MCP) Server**: Implement a standard stdio JSON-RPC MCP server (`a2a mcp` or `python3 a2a_bridge.py --mcp`) that exposes A2A capabilities (`list_agents`, `send_task`, `check_inbox`, `broadcast_group`) to Claude Desktop, Cursor, and other MCP clients.
- **Extended Provider Backends**: Expand `a2a_bridge.py` with first-class support for:
  - `claudecode`: Anthropic Claude Code CLI non-interactive execution (`claude -p`).
  - `codex`: Codex CLI execution (`codex exec`).
  - `generic-cmd`: Custom external shell executable or script.
- **Admin Console Interactive Chat**: Enhance `admin.html` with an interactive "線上對話 (Interactive Chat)" tab that allows humans/operators to pick online agents, send direct tasks, and view live replies within the browser.

## Capabilities

### New Capabilities
- `universal-bridge-distribution`: Hub-hosted single-command installer script and npm global package distribution for zero-friction cross-host agent onboarding.
- `model-context-protocol-server`: Stdio MCP Server interface allowing external LLM hosts (Cursor, Claude Desktop) to invoke A2A Hub operations via standard MCP tools.
- `bridge-provider-extensions`: Additional cognitive core adapters in the universal bridge for Claude Code, Codex, and custom shell commands.
- `admin-console-interactive-chat`: Real-time browser-based task and chat panel embedded in the Hub operator console.

### Modified Capabilities
- `lite-hub-http-contract`: Add `GET /install.sh` and static bridge script serving endpoints to the Hub HTTP routing surface.

## Impact

- **Hub HTTP Service**: Serves `/install.sh` and the raw bridge script without external CDN dependencies.
- **Universal Bridge (`examples/worker/a2a_bridge.py`)**: Adds `--mcp` mode and new `--backend` options (`claudecode`, `codex`, `command`).
- **Web UI (`internal/service/admin.html`)**: Extends the existing dashboard with a 4th tab for live agent conversation and test task dispatch.
- **NPM Package**: Adds root `package.json` and executable launcher in `bin/`.
