#!/usr/bin/env python3
"""Run only on the remote acceptance host against a disposable loopback Hub.

Exercises the real bridge CLI and Hub HTTP transport; the inference endpoint
is deliberately deterministic so crashes and retries are reproducible.
"""

import argparse
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse
from urllib.error import URLError
from urllib.request import Request, urlopen
import uuid


def wait_for(check, label, timeout=30):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            value = check()
        except URLError:
            value = None
        if value:
            return value
        time.sleep(0.1)
    raise RuntimeError(f"Timed out: {label}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hub", required=True)
    parser.add_argument("--hub-container", required=True)
    args = parser.parse_args()
    if urlparse(args.hub).hostname not in ("127.0.0.1", "localhost"):
        parser.error("Use a disposable loopback Hub, never the public Hub")
    if not args.hub_container.startswith("a2a-bridge-smoke-"):
        parser.error("Container name must start with a2a-bridge-smoke-")

    def request(path, body=None, identity=None):
        headers = {"Content-Type": "application/json"}
        if identity:
            headers.update({"X-Agent-ID": identity["agentId"],
                            "Authorization": "Bearer " + identity["agentToken"]})
        payload = None if body is None else json.dumps(body).encode()
        with urlopen(Request(args.hub + path, payload, headers), timeout=5) as response:
            return json.load(response)

    def register(name):
        return request("/hub/v1/agents/register", {
            "displayName": name, "providerFamily": "smoke", "transportId": "http-json",
            "capabilities": ["text/plain"], "registrationIdempotencyKey": uuid.uuid4().hex,
        })["identity"]

    gate = threading.Event()
    entered = threading.Event()
    fail_next = threading.Event()
    failed_once = threading.Event()

    class Backend(BaseHTTPRequestHandler):
        def log_message(self, *unused):
            pass

        def do_POST(self):
            data = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            entered.set()
            gate.wait(30)
            if fail_next.is_set():
                fail_next.clear()
                failed_once.set()
                self.send_error(503, "deliberate inference failure")
                return
            message = data["messages"][-1]["content"]
            text = "[[A2A_NO_REPLY]]" if message.endswith("辛苦了") else "smoke-result: " + message
            payload = json.dumps({"choices": [{"message": {"content": text}}]}).encode()
            try:
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)
            except (BrokenPipeError, ConnectionResetError):
                pass  # Expected when the bridge is killed during inference.

    backend = ThreadingHTTPServer(("127.0.0.1", 0), Backend)
    threading.Thread(target=backend.serve_forever, daemon=True).start()
    bridge = Path(__file__).resolve().parents[1] / "examples/worker/a2a_bridge.py"
    process = None
    try:
        with tempfile.TemporaryDirectory(prefix="a2a-bridge-smoke-") as directory:
            work = Path(directory)
            credentials = work / "credentials.json"
            command = [sys.executable, "-u", str(bridge), "--hub", args.hub,
                       "--name", "smoke-bridge", "--credentials", str(credentials),
                       "--queue-db", str(work / "work.db"), "--backend", "openai",
                       "--api-base", f"http://127.0.0.1:{backend.server_port}/v1",
                       "--model", "deterministic-smoke"]
            with (work / "bridge.log").open("w") as log:
                def start():
                    return subprocess.Popen(command, stdout=log, stderr=log)

                def stop():
                    nonlocal process
                    if process is not None:
                        process.kill()
                        process.wait(timeout=5)
                        process = None

                def load_identity():
                    if not credentials.exists():
                        return None
                    try:
                        saved = json.loads(credentials.read_text())
                        identity = saved.get("identity", saved)
                        return identity if identity.get("agentToken") else None
                    except json.JSONDecodeError:
                        return None

                def inbox(identity):
                    return request(f"/hub/v1/agents/{identity['agentId']}/inbox", identity=identity)["items"]

                sender = register("smoke-sender")
                third = register("smoke-third")
                process = start()
                identity = wait_for(load_identity, "fresh CLI registration")
                if credentials.stat().st_mode & 0o077:
                    raise RuntimeError("Credentials are accessible to other users")
                peers = request("/hub/v1/agents", identity=sender)["agents"]
                expected = {sender["agentId"], third["agentId"], identity["agentId"]}
                if not expected.issubset({peer["agentId"] for peer in peers}):
                    raise RuntimeError("Three-agent discovery failed")
                print("PASS: fresh bridge registration, private credentials, three-agent discovery", flush=True)

                def send(message, source=sender):
                    task = uuid.uuid4().hex
                    return request(f"/hub/v1/agents/{identity['agentId']}/tasks", {
                        "taskId": task, "contextId": "smoke-context",
                        "idempotencyKey": task, "message": message,
                    }, source)

                send("smoke-first")
                wait_for(entered.is_set, "blocked inference started")
                send("smoke-second", third)
                wait_for(lambda: not inbox(identity), "second task ACK while first inference is blocked", 10)
                if gate.is_set() or inbox(sender) or inbox(third):
                    raise RuntimeError("Inference barrier did not hold")
                print("PASS: continuous intake ACKs while inference is blocked", flush=True)
                stop()
                gate.set()
                process = start()
                first = wait_for(lambda: inbox(sender), "first reply after bridge restart")
                second = wait_for(lambda: inbox(third), "second reply after bridge restart")
                if len(first) != 1 or len(second) != 1:
                    raise RuntimeError("Unexpected duplicate replies after restart")
                if "smoke-first" not in first[0]["message"] or "smoke-second" not in second[0]["message"]:
                    raise RuntimeError("Recovered reply content mismatch")
                for source, items in ((sender, first), (third, second)):
                    request(f"/hub/v1/agents/{source['agentId']}/inbox/{items[0]['sequence']}/ack", {}, source)
                print("PASS: ACKed tasks survive process kill and reply after restart", flush=True)

                fail_next.set()
                send("辛苦了，整理今天的錯誤紀錄")
                wait_for(failed_once.is_set, "injected backend failure")
                replies = wait_for(lambda: inbox(sender), "actionable command retried after inference failure", 45)
                if len(replies) != 1 or "整理今天的錯誤紀錄" not in replies[0]["message"]:
                    raise RuntimeError("Actionable command was lost or duplicated")
                request(f"/hub/v1/agents/{sender['agentId']}/inbox/{replies[0]['sequence']}/ack", {}, sender)
                print("PASS: polite actionable command executes; inference failure retries", flush=True)

                stop()
                send("smoke-hub-restart")
                subprocess.run(["docker", "restart", args.hub_container], check=True, stdout=subprocess.DEVNULL)
                process = start()
                recovered = wait_for(lambda: inbox(sender), "pending task after Hub restart")
                if len(recovered) != 1 or "smoke-hub-restart" not in recovered[0]["message"]:
                    raise RuntimeError("Hub restart recovery failed")
                print("PASS: Hub restart preserves pending task and bridge identity", flush=True)
                request(f"/hub/v1/agents/{sender['agentId']}/inbox/{recovered[0]['sequence']}/ack", {}, sender)
                stop()
                for _ in range(105):
                    send("辛苦了")
                process = start()
                wait_for(lambda: not inbox(identity), "backlog beyond one SSE replay page", 45)
                print("PASS: 105 offline tasks are reconciled beyond the 100-item SSE replay page", flush=True)
                stop()
    finally:
        gate.set()
        if process is not None:
            process.kill()
            process.wait(timeout=5)
        backend.shutdown()
        backend.server_close()
    print("Bridge remote smoke passed (deterministic backend, no real LLM calls)", flush=True)


if __name__ == "__main__":
    main()
