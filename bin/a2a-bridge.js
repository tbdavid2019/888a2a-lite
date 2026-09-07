#!/usr/bin/env node
const { spawnSync, spawn } = require("child_process");
const path = require("path");
const fs = require("fs");

const res = spawnSync("python3", ["--version"], { encoding: "utf-8" });
if (res.error || res.status !== 0) {
  console.error("[!] Error: python3 is required by a2a-bridge but was not found in PATH.");
  process.exit(1);
}

const bridgePath = path.resolve(__dirname, "../examples/worker/a2a_bridge.py");
if (!fs.existsSync(bridgePath)) {
  console.error(`[!] Error: Universal Bridge not found at ${bridgePath}`);
  process.exit(1);
}

const child = spawn("python3", [bridgePath, ...process.argv.slice(2)], {
  stdio: "inherit",
  env: { ...process.env, PYTHONUNBUFFERED: "1" }
});

child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
  } else {
    process.exit(code || 0);
  }
});
