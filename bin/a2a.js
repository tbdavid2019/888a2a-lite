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

if (args[0] === "help" || args[0] === "--help" || args[0] === "-h") {
  console.log(`
888a2a - Universal A2A Client & Hub Suite

3-Minute Quickstart:
  a2a start              Launch User Chat Web UI (http://localhost:8888)
  a2a bridge             Connect local AI Agent (auto-detects OpenClaw, Claude, etc.)
  a2a bridge --install-service   Install Agent as OS background service
  a2a mcp                Launch Stdio MCP server (Claude Desktop / Cursor)

Zero Configuration:
  By default, connects to https://a2a.david888.com and uses your system user name.
  All flags below are completely optional overrides.

Optional Overrides:
  --key <key>            Private Space / Circle Key (creates an isolated workspace on a2a.david888.com)
  --hub <url>            Hub Base URL (default: https://a2a.david888.com)
  --name <name>          Custom display name (default: auto-detected)
  --backend <name>       Backend: openclaw, claudecode, hermes, codex, openai, command
  --install-service      Install OS service (auto-detects macOS launchd / Linux systemd)
  --port <port>          Web UI port (default: 8888)
  --shared-key <key>     Alias for --key
`);
  process.exit(0);
}

// Support convenient command routing:
// - `a2a` or `a2a start` or `a2a ui` -> launch local Web UI
// - `a2a bridge ...` -> launch Agent daemon
// - `a2a mcp ...` -> launch Stdio MCP server
let forwardedArgs = [];
if (args.length === 0 || args[0] === "start" || args[0] === "ui" || args[0] === "web") {
  const subArgs = (args[0] === "start" || args[0] === "ui" || args[0] === "web") ? args.slice(1) : args;
  forwardedArgs = ["--ui", ...subArgs];
} else if (args[0] === "mcp") {
  forwardedArgs = ["--mcp", ...args.slice(1)];
} else if (args[0] === "bridge") {
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
