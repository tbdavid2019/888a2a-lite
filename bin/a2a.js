#!/usr/bin/env node
const { spawnSync, spawn } = require("child_process");
const path = require("path");
const fs = require("fs");

function checkPython() {
  const res = spawnSync("python3", ["-c", "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')"], {
    encoding: "utf-8"
  });
  if (res.error || res.status !== 0) {
    console.error("[!] Error: python3 is required by 888a2a but was not found in PATH.");
    console.error("    Please install Python 3.10+ and try again.");
    process.exit(1);
  }
  const ver = res.stdout.trim();
  const [maj, min] = ver.split(".").map(Number);
  if (maj < 3 || (maj === 3 && min < 10)) {
    console.error(`[!] Error: Python 3.10+ required. Found Python ${ver}.`);
    process.exit(1);
  }
}

checkPython();

const bridgePath = path.resolve(__dirname, "../examples/worker/a2a_bridge.py");
if (!fs.existsSync(bridgePath)) {
  console.error(`[!] Error: Universal Bridge not found at ${bridgePath}`);
  process.exit(1);
}

const args = process.argv.slice(2);

if (args.length === 0 || args[0] === "help" || args[0] === "--help" || args[0] === "-h") {
  console.log(`
888a2a - Universal A2A Client & Hub Suite

Usage:
  a2a ui [options]       Launch local User Chat Web UI (http://localhost:8888)
  a2a bridge [options]   Run background Agent bridge daemon
  a2a mcp [options]      Run Stdio JSON-RPC 2.0 MCP server (for Claude Desktop / Cursor)

Commands:
  ui, web      Start local user web console and open default browser
  bridge       Run autonomous agent daemon (openclaw, hermes, claudecode, codex, openai, command)
  mcp          Launch Model Context Protocol (MCP) server for IDEs

Common Options:
  --hub <url>            Hub Base URL (default: https://a2a.david888.com)
  --name <name>          Agent or User display name
  --backend <name>       Cognitive backend: openclaw, hermes, claudecode, codex, openai, command
  --backend-agent <id>   Agent profile for OpenClaw (default: default)
  --shared-key <key>     Pre-shared key (for SEMI_OPEN hub mode)
  --install-service      Install as background OS service (launchd on macOS, systemd on Linux)
  --port <port>          Port for local web UI (default: 8888)

Examples:
  a2a ui --name "User"
  a2a bridge --name "MyAgent" --backend openclaw --backend-agent default
  a2a bridge --name "ClaudeBot" --backend claudecode
  a2a mcp --name "MyCursor"
`);
  process.exit(0);
}

// Support convenient command routing:
// - `a2a mcp ...` -> `python3 a2a_bridge.py --mcp ...`
// - `a2a ui ...`  -> `python3 a2a_bridge.py --ui ...`
// - `a2a bridge ...` -> `python3 a2a_bridge.py ...`
// - `a2a start ...` -> `python3 a2a_bridge.py ...`
let forwardedArgs = [];
if (args[0] === "mcp") {
  forwardedArgs = ["--mcp", ...args.slice(1)];
} else if (args[0] === "ui" || args[0] === "web") {
  forwardedArgs = ["--ui", ...args.slice(1)];
} else if (args[0] === "bridge" || args[0] === "start") {
  forwardedArgs = args.slice(1);
} else {
  forwardedArgs = args;
}

const env = { ...process.env, PYTHONUNBUFFERED: "1" };

const child = spawn("python3", [bridgePath, ...forwardedArgs], {
  stdio: "inherit",
  env
});

child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
  } else {
    process.exit(code || 0);
  }
});
