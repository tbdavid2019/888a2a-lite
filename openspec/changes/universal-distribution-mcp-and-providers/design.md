## Context

888a2a-lite currently consists of a Go Hub and a Python universal bridge (`a2a_bridge.py`). While runtime durability and protocol safety are achieved, multi-host agent provisioning still relies on manual Git operations or file copying. Furthermore, popular developer environments (Cursor, Claude Desktop) increasingly depend on Model Context Protocol (MCP) servers for agent tool integration. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**
- Provide zero-dependency single-command installation (`curl ... | bash`) directly served by the Hub.
- Provide global npm package distribution (`npm install -g git+https://...` or `npm install -g 888a2a`) exposing global `a2a` and `a2a-bridge` commands.
- Implement a pure Python stdio JSON-RPC MCP server (`a2a mcp`) without external packages.
- Expand bridge providers to include Claude Code (`claude -p`), Codex CLI, and arbitrary shell commands.
- Embed a real-time interactive chat testbed tab in the Hub Web Admin Console (`admin.html`).

**Non-Goals:**
- Reintroducing full SaaS user chat or multitenant authorization models.
- Running agent cognitive processes inside the Hub server.
- Adding third-party heavyweight dependencies (FastMCP, npm heavy frameworks) to the bridge.

## Decisions

### Decision 1: Hub-Hosted Standalone Installer Script
- **Choice**: Embed `install.sh` in the Go binary using `//go:embed` and serve it at `GET /install.sh`, along with `GET /a2a_bridge.py`.
- **Rationale**: Any machine with `curl` and `python3` can bootstrap without cloning git repositories or configuring package registries.
- **Alternatives considered**:
  - Distributing via GitHub raw URLs: Requires GitHub public access which fails in restricted private clouds or internal networks.
  - Distributing via Docker only: Inconvenient for host-native agent CLIs (OpenClaw, Claude Code, Hermes) that need direct access to local host tools.

### Decision 2: NPM Package Wrapper (`888a2a`)
- **Choice**: Create a root `package.json` with entry points in `bin/` that wrap the Python bridge and Go CLI binaries.
- **Rationale**: OpenClaw users already have Node.js and npm in their PATH. `npm install -g git+https://github.com/tbdavid2019/888a2a-lite.git` (or `npm install -g 888a2a`) provides an instantly familiar workflow matching `voko`.
- **Alternatives considered**:
  - Python PyPI package only: Requires `pipx` or `pip` which may collide with system Python on Debian/Ubuntu (PEP 668 externally managed environment).

### Decision 3: Zero-Dependency JSON-RPC Stdio MCP Server
- **Choice**: Implement the MCP stdio server natively in `a2a_bridge.py` using Python's standard `sys.stdin`/`sys.stdout` and `json`.
- **Rationale**: Adding `mcp` or `fastmcp` dependencies would break the zero-dependency promise of `a2a_bridge.py`. The MCP JSON-RPC protocol is straightforward to implement for the four required tool operations.
- **Protocol discipline**: All debug/diagnostic output MUST go to `sys.stderr`. `sys.stdout` is strictly reserved for JSON-RPC messages.

### Decision 4: Provider Backend Abstraction Extensions
- **Choice**: Subclass `AIBackend` in `a2a_bridge.py` for:
  - `ClaudeCodeBackend`: calls `claude -p "<prompt>"`.
  - `CodexBackend`: calls `codex exec "<prompt>"`.
  - `CommandBackend`: executes a user-provided binary or script (`--backend-cmd`).
- **Rationale**: Maintains a clean, decoupled interface where the core durable queue, instant ACK, and anti-echo guards apply uniformly to all backends.

### Decision 5: Operator Console Interactive Chat Tab
- **Choice**: Embed an "線上對話 (Interactive Chat)" tab in `internal/service/admin.html` reusing existing Hub endpoints (`GET /hub/v1/agents`, `POST /hub/v1/agents/{id}/tasks`, `GET /hub/v1/agents/{id}/inbox`).
- **Rationale**: Provides instant visual verification of multi-agent connectivity directly from the browser without needing third-party chat software.

## Risks / Trade-offs

- [Host has Node but no Python 3] → Mitigation: `bin/a2a-bridge.js` verifies `python3 --version >= 3.10` and gives an explicit error message with installation instructions.
- [MCP stdio pollution] → Mitigation: In `--mcp` mode, reassign root logger and stdout handlers to ensure zero extraneous characters on stdout.
- [Shell script pipe security] → Mitigation: `install.sh` uses strict quoting, `set -euo pipefail`, and supports checksum/version validation.
