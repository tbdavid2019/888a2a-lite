## 1. Hub One-Line Installer & Asset Serving

- [x] 1.1 Create standalone POSIX shell script `scripts/install.sh` supporting `--name`, `--backend`, `--install-service`, and `--shared-key`, verifying python3, downloading bridge, and bootstrapping service
- [x] 1.2 Embed `install.sh` and `examples/worker/a2a_bridge.py` in the Go Hub server and expose `GET /install.sh` and `GET /a2a_bridge.py`
- [x] 1.3 Verify Hub routes respond with HTTP 200 and valid MIME types without token requirement

## 2. NPM Package Distribution Wrapper

- [x] 2.1 Create root `package.json` defining `888a2a` with bin entry points (`a2a`, `a2a-bridge`)
- [x] 2.2 Implement executable launcher scripts in `bin/a2a.js` and `bin/a2a-bridge.js` that check for python3 and forward commands to the bridge
- [x] 2.3 Verify `npm pack` or `npm link` allows global execution of `a2a` and `a2a-bridge`

## 3. Extended Cognitive Provider Backends

- [x] 3.1 Implement `ClaudeCodeBackend` in `a2a_bridge.py` invoking `claude -p` with timeout and anti-echo guard
- [x] 3.2 Implement `CodexBackend` in `a2a_bridge.py` invoking `codex exec` with proper CLI arguments
- [x] 3.3 Implement `CommandBackend` in `a2a_bridge.py` for `--backend command --backend-cmd "<cmd>"` supporting custom process invocation
- [x] 3.4 Add unit tests for each new backend in `examples/worker/test_a2a_bridge.py`

## 4. Model Context Protocol (MCP) Stdio Server

- [x] 4.1 Implement `--mcp` command mode in `a2a_bridge.py` with standard JSON-RPC 2.0 stdio handling
- [x] 4.2 Expose MCP tools `a2a_list_agents`, `a2a_send_task`, `a2a_broadcast_group`, and `a2a_poll_inbox`
- [x] 4.3 Ensure all logging in `--mcp` mode strictly writes to `sys.stderr` and test initialization handshake over stdio

## 5. Admin Console Interactive Chat

- [x] 5.1 Add "線上對話 (Interactive Chat)" navigation tab and workspace view in `internal/service/admin.html`
- [x] 5.2 Implement active agent selector and real-time message stream with task dispatch form
- [x] 5.3 Test sending a task to an active agent from the browser and receiving live reply rendering

## 6. Remote Verification & Documentation

- [x] 6.1 Update `README.md` and `llms.txt` with the new one-line installer, npm package instructions, and MCP client configuration
- [x] 6.2 Record changes in `CHANGELOG.md` under today's date
- [x] 6.3 Deploy to live hub and verify cross-host single-line installation against remote nodes
