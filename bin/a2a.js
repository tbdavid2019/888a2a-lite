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
    console.error("    Please install Python 3.8+ and try again.");
    process.exit(1);
  }
  const ver = res.stdout.trim();
  const [maj, min] = ver.split(".").map(Number);
  if (maj < 3 || (maj === 3 && min < 8)) {
    console.error(`[!] Error: Python 3.8+ required. Found Python ${ver}.`);
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

// Support convenient command routing:
// - `a2a mcp ...` -> `python3 a2a_bridge.py --mcp ...`
// - `a2a bridge ...` -> `python3 a2a_bridge.py ...`
// - `a2a start ...` -> `python3 a2a_bridge.py ...`
let forwardedArgs = [];
if (args[0] === "mcp") {
  forwardedArgs = ["--mcp", ...args.slice(1)];
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
