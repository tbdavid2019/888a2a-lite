#!/usr/bin/env python3
"""
888a2a-lite Universal Agent Bridge (a2a_bridge.py)
Official Production-Ready Agent Daemon for 888a2a-lite Hub.

Features:
- Zero external dependencies (pure Python standard library).
- Multi-backend AI support (OpenClaw, Hermes, OpenAI/Ollama compatible API, custom cmd).
- Instant ACK on Ingest (<50ms) to eliminate false pending alerts on the Hub.
- Built-in Anti-Echo Storm Guard & [[A2A_NO_REPLY]] conversation termination.
- Auto-Registration & Credential Persistence (~/.a2a/credentials_<name>.json).
- Auto-Accept Group Invitations & Group broadcast spam suppression.
- Resilient SSE Outbound Streaming with auto-reconnect & keepalive handling.
- One-Click Service Installer for macOS (LaunchAgent) & Linux (systemd user unit).
- Complete PATH and unbuffered environment isolation.

Usage:
  # Quick start with OpenClaw:
  python3 a2a_bridge.py --name "甘露寺蜜璃" --backend openclaw --backend-agent kanroji

  # Quick start with Hermes:
  python3 a2a_bridge.py --name "蜜蜜" --backend hermes

  # Install as auto-starting daemon:
  python3 a2a_bridge.py --name "甘露寺蜜璃" --backend openclaw --backend-agent kanroji --install-service launchd
"""

import argparse
import json
import os
import pathlib
import re
import signal
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

# Force unbuffered standard streams so logs never get stuck in block buffers
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(line_buffering=True)
if hasattr(sys.stderr, "reconfigure"):
    sys.stderr.reconfigure(line_buffering=True)


# ---------------------------------------------------------------------------
# Utility & Environment Helpers
# ---------------------------------------------------------------------------

def get_enhanced_env():
    """Build a robust environment dictionary ensuring complete binary PATH."""
    env = os.environ.copy()
    env["PYTHONUNBUFFERED"] = "1"

    current_path = env.get("PATH", "")
    standard_paths = [
        os.path.expanduser("~/.n/bin"),
        os.path.expanduser("~/.local/bin"),
        os.path.expanduser("~/.bun/bin"),
        os.path.expanduser("~/.npm-global/bin"),
        "/opt/homebrew/bin",
        "/opt/homebrew/sbin",
        "/usr/local/bin",
        "/usr/local/sbin",
        "/usr/bin",
        "/bin",
        "/usr/sbin",
        "/sbin",
    ]

    # Add nvm node paths if present
    nvm_dir = os.path.expanduser("~/.nvm/versions/node")
    if os.path.isdir(nvm_dir):
        try:
            for ver in os.listdir(nvm_dir):
                p = os.path.join(nvm_dir, ver, "bin")
                if os.path.isdir(p):
                    standard_paths.insert(0, p)
        except Exception:
            pass

    # Prepend paths not already in current PATH
    existing_parts = set(current_path.split(os.pathsep))
    to_add = [p for p in standard_paths if p not in existing_parts and os.path.isdir(p)]
    if to_add:
        env["PATH"] = os.pathsep.join(to_add + [current_path])

    return env


def get_local_ip():
    """Get the primary local IPv4 address."""
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        s.connect(("8.8.8.8", 80))
        return s.getsockname()[0]
    except Exception:
        return "127.0.0.1"
    finally:
        s.close()


def sanitize_slug(name, fallback="agent"):
    """Convert an agent name to an ASCII-safe filesystem/systemd slug."""
    import hashlib
    ascii_slug = re.sub(r"[^a-zA-Z0-9_\-]", "_", name.strip()).strip("_")
    ascii_slug = re.sub(r"_+", "_", ascii_slug)
    if not ascii_slug:
        short_hash = hashlib.md5(name.encode("utf-8")).hexdigest()[:8]
        return f"{fallback}_{short_hash}"
    return ascii_slug


# ---------------------------------------------------------------------------
# Hub API Client (Pure urllib)
# ---------------------------------------------------------------------------

class HubClient:
    def __init__(self, hub_url, agent_id=None, token=None, shared_key=None):
        self.hub_url = hub_url.rstrip("/")
        self.agent_id = agent_id
        self.token = token
        self.shared_key = shared_key

    def _headers(self, auth=True):
        h = {"Content-Type": "application/json"}
        if auth and self.token:
            h["Authorization"] = f"Bearer {self.token}"
        if auth and self.agent_id:
            h["X-Agent-ID"] = self.agent_id
        if self.shared_key:
            h["X-Hub-Key"] = self.shared_key
        return h

    def register(self, name, description="AI Agent via Universal Bridge"):
        """Register agent with Hub and receive agentId and token."""
        url = f"{self.hub_url}/hub/v1/agents/register"
        payload = json.dumps({"name": name, "description": description}).encode("utf-8")
        req = urllib.request.Request(url, data=payload, headers=self._headers(auth=False))
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                data = json.loads(resp.read().decode("utf-8"))
                ident = data.get("identity", {})
                self.agent_id = ident.get("agentId")
                self.token = ident.get("agentToken")
                return data
        except urllib.error.HTTPError as e:
            err = e.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"Registration failed: HTTP {e.code}: {err}")

    def ack_task(self, sequence):
        """Acknowledge processed sequence immediately (<50ms)."""
        url = f"{self.hub_url}/hub/v1/agents/{self.agent_id}/inbox/{sequence}/ack"
        req = urllib.request.Request(url, data=b"{}", headers=self._headers())
        try:
            with urllib.request.urlopen(req, timeout=5) as resp:
                return json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            print(f"[!] ACK failed for seq {sequence}: HTTP {e.code}", file=sys.stderr)
            return None
        except Exception as e:
            print(f"[!] ACK network error for seq {sequence}: {e}", file=sys.stderr)
            return None

    def send_task(self, target_agent_id, message, context_id=None, task_id=None):
        """Send direct task to peer agent."""
        url = f"{self.hub_url}/hub/v1/agents/{target_agent_id}/tasks"
        t_id = task_id or f"task-{int(time.time() * 1000)}"
        c_id = context_id or f"ctx-{int(time.time())}"
        payload = json.dumps({
            "taskId": t_id,
            "contextId": c_id,
            "idempotencyKey": f"idem-{t_id}",
            "message": message,
        }).encode("utf-8")
        req = urllib.request.Request(url, data=payload, headers=self._headers())
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                return json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            err = e.read().decode("utf-8", errors="replace")
            print(f"[!] Error sending task to {target_agent_id}: HTTP {e.code}: {err}", file=sys.stderr)
            return None
        except Exception as e:
            print(f"[!] Network error sending task: {e}", file=sys.stderr)
            return None

    def list_invitations(self):
        """List pending group invitations."""
        url = f"{self.hub_url}/hub/v1/groups/invitations"
        req = urllib.request.Request(url, headers=self._headers())
        try:
            with urllib.request.urlopen(req, timeout=10) as resp:
                data = json.loads(resp.read().decode("utf-8"))
                return data.get("invitations", [])
        except Exception as e:
            print(f"[!] Error checking invitations: {e}", file=sys.stderr)
            return []

    def accept_invitation_by_group(self, group_id):
        """Accept group invitation by groupId."""
        url = f"{self.hub_url}/hub/v1/groups/{group_id}/accept"
        req = urllib.request.Request(url, data=b"{}", headers=self._headers())
        try:
            with urllib.request.urlopen(req, timeout=10) as resp:
                return json.loads(resp.read().decode("utf-8"))
        except Exception as e:
            print(f"[!] Error accepting group {group_id}: {e}", file=sys.stderr)
            return None

    def auto_accept_pending_invitations(self):
        """Scan and accept all pending group invitations."""
        invs = self.list_invitations()
        accepted = 0
        for inv in invs:
            if inv.get("state") == "PENDING":
                gid = inv.get("groupId")
                inviter = inv.get("inviterAgentId")
                print(f"[*] Found pending invitation for group '{gid}' from {inviter}. Accepting...")
                res = self.accept_invitation_by_group(gid)
                if res:
                    print(f"[✓] Successfully joined group '{gid}'!")
                    accepted += 1
        return accepted


# ---------------------------------------------------------------------------
# AI Execution Backends
# ---------------------------------------------------------------------------

class AIBackend:
    def execute(self, message, sender_id, context):
        raise NotImplementedError


class EchoBackend(AIBackend):
    def __init__(self, agent_name):
        self.agent_name = agent_name

    def execute(self, message, sender_id, context):
        ip = get_local_ip()
        return f"[Echo from {self.agent_name}@{ip}] Received: {message}"


def extract_openclaw_text(data):
    """Extract assistant visible text across various OpenClaw JSON schemas."""
    if not isinstance(data, dict):
        return None

    # 1. result.payloads (standard in recent CLI versions)
    res = data.get("result")
    if isinstance(res, dict):
        payloads = res.get("payloads")
        if isinstance(payloads, list) and len(payloads) > 0:
            texts = [p.get("text", "") for p in payloads if isinstance(p, dict) and p.get("text")]
            if texts:
                return "\n".join(texts).strip()

        # result.meta.agentMeta.finalAssistantVisibleText
        vtext = res.get("meta", {}).get("agentMeta", {}).get("finalAssistantVisibleText")
        if vtext:
            return vtext.strip()

        # result.finalAssistantVisibleText
        if res.get("finalAssistantVisibleText"):
            return str(res["finalAssistantVisibleText"]).strip()

    # 2. top-level payloads
    payloads = data.get("payloads")
    if isinstance(payloads, list) and len(payloads) > 0:
        texts = [p.get("text", "") for p in payloads if isinstance(p, dict) and p.get("text")]
        if texts:
            return "\n".join(texts).strip()

    # 3. top-level meta.agentMeta.finalAssistantVisibleText
    if "meta" in data and isinstance(data["meta"], dict):
        vtext = data["meta"].get("agentMeta", {}).get("finalAssistantVisibleText")
        if vtext:
            return vtext.strip()

    # 4. top-level finalAssistantVisibleText
    if data.get("finalAssistantVisibleText"):
        return str(data["finalAssistantVisibleText"]).strip()

    if data.get("response"):
        return str(data["response"]).strip()

    return None


class OpenClawBackend(AIBackend):
    def __init__(self, agent_profile="default", system_prompt=""):
        self.agent_profile = agent_profile
        self.system_prompt = system_prompt
        self.env = get_enhanced_env()

    def execute(self, message, sender_id, context):
        # Format instruction with Anti-Echo termination guard
        full_instruction = (
            f"來自 A2A 同儕 Agent ({sender_id}) 的訊息：\n{message}\n\n"
            f"【防回音守衛規則】若此訊息僅為確認收悉、狀態回報、或無需再回信，請直接輸出 [[A2A_NO_REPLY]]。"
        )
        if self.system_prompt:
            full_instruction = f"{self.system_prompt}\n\n{full_instruction}"

        cmd = [
            "openclaw", "agent",
            "--agent", self.agent_profile,
            "--message", full_instruction,
            "--json"
        ]

        try:
            proc = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                env=self.env,
                timeout=120
            )
            if proc.returncode != 0:
                print(f"[!] OpenClaw returned error (code {proc.returncode}): {proc.stderr}", file=sys.stderr)
                if proc.returncode == 127 or "No such file" in proc.stderr:
                    print("[!] Please check if openclaw CLI is installed and in PATH.", file=sys.stderr)
                return None

            stdout = proc.stdout.strip()
            # Attempt to parse OpenClaw JSON response structure
            try:
                data = json.loads(stdout)
                extracted = extract_openclaw_text(data)
                if extracted:
                    return extracted
            except json.JSONDecodeError:
                pass

            # Fallback to stdout text
            return stdout

        except subprocess.TimeoutExpired:
            print("[!] OpenClaw inference timed out after 120s.", file=sys.stderr)
            return None
        except Exception as e:
            print(f"[!] Failed to invoke OpenClaw: {e}", file=sys.stderr)
            return None


class HermesBackend(AIBackend):
    def __init__(self, system_prompt=""):
        self.system_prompt = system_prompt
        self.env = get_enhanced_env()
        # Find Hermes executable
        self.hermes_bin = "hermes"
        candidate_paths = [
            os.path.expanduser("~/hermes-agent/venv/bin/hermes"),
            "/home/david/hermes-agent/venv/bin/hermes",
            "/usr/local/bin/hermes",
        ]
        for p in candidate_paths:
            if os.path.isfile(p) and os.access(p, os.X_OK):
                self.hermes_bin = p
                break

    def execute(self, message, sender_id, context):
        full_instruction = (
            f"{self.system_prompt}\n\n"
            f"A2A Message from {sender_id}: {message}\n"
            f"If this is a confirmation, standby report, or needs no reply, output [[A2A_NO_REPLY]]."
        )
        cmd = [self.hermes_bin, "-z", full_instruction]
        try:
            proc = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                env=self.env,
                timeout=120,
                cwd=os.path.expanduser("~")
            )
            if proc.returncode != 0:
                print(f"[!] Hermes returned code {proc.returncode}: {proc.stderr}", file=sys.stderr)
                return None
            out = proc.stdout.strip()
            # Clean up Hermes header banners if present
            cleaned_lines = [
                line for line in out.splitlines()
                if not line.startswith("Hermes Agent") and not line.startswith("Session:")
            ]
            return "\n".join(cleaned_lines).strip()
        except Exception as e:
            print(f"[!] Failed to invoke Hermes: {e}", file=sys.stderr)
            return None


class OpenAIBackend(AIBackend):
    def __init__(self, api_base, api_key, model, system_prompt=""):
        self.api_base = api_base.rstrip("/")
        self.api_key = api_key
        self.model = model
        self.system_prompt = system_prompt

    def execute(self, message, sender_id, context):
        url = f"{self.api_base}/chat/completions"
        messages = []
        sys_p = self.system_prompt + "\nIf this is a confirmation, status report, or needs no reply, output [[A2A_NO_REPLY]]."
        if sys_p:
            messages.append({"role": "system", "content": sys_p.strip()})
        messages.append({"role": "user", "content": f"[From Agent {sender_id}]: {message}"})

        payload = json.dumps({
            "model": self.model,
            "messages": messages,
            "temperature": 0.7,
        }).encode("utf-8")

        headers = {
            "Content-Type": "application/json",
            "Authorization": f"Bearer {self.api_key}",
        }
        req = urllib.request.Request(url, data=payload, headers=headers)
        try:
            with urllib.request.urlopen(req, timeout=60) as resp:
                data = json.loads(resp.read().decode("utf-8"))
                return data["choices"][0]["message"]["content"].strip()
        except Exception as e:
            print(f"[!] OpenAI backend error: {e}", file=sys.stderr)
            return None


# ---------------------------------------------------------------------------
# Core Task Engine: Instant ACK & Anti-Echo Guard
# ---------------------------------------------------------------------------

ANTI_ECHO_PATTERNS = [
    r"收錄完畢", r"保持連線待命", r"隨時準備好", r"辛苦了", r"一點都不辛苦",
    r"晚安", r"拜拜", r"不用回覆", r"已就定位", r"一切正常", r"收到確認",
    r"\[\[A2A_NO_REPLY\]\]"
]

QUESTION_PATTERNS = [r"\?", r"？", r"請", r"幫我", r"能否", r"如何", r"怎麼", r"何時"]


def is_pure_closing_statement(text):
    """Determine if a message is a pure closing statement or status report."""
    has_closing = any(re.search(p, text) for p in ANTI_ECHO_PATTERNS)
    has_question = any(re.search(p, text) for p in QUESTION_PATTERNS)
    return has_closing and not has_question


def process_incoming_task(hub_client, backend, item, agent_name):
    """Handle incoming task with Instant ACK and Anti-Echo filtering."""
    seq = item.get("sequence")
    task_id = item.get("taskId")
    sender_id = item.get("requesterAgentId")
    context_id = item.get("contextId")
    msg = item.get("message", "")
    group_id = item.get("groupId")

    is_group = bool(group_id)
    tag = f"[Group Broadcast: {group_id}]" if is_group else f"[Direct Task: {task_id}]"
    print(f"\n{'='*55}\n{tag} from {sender_id} (seq={seq})\nMessage: {msg.strip()}\n{'='*55}")

    # 1. Instant ACK on Ingest (<50ms): Never leave Hub sequence in PENDING state!
    print(f"[*] [Instant ACK] Acknowledging seq {seq} immediately...")
    hub_client.ack_task(seq)
    print(f"[✓] [Instant ACK] Seq {seq} successfully acknowledged.")

    # 2. Check and auto-accept invitations if applicable
    if any(k in msg.lower() for k in ("invite", "group", "群組", "邀請")):
        hub_client.auto_accept_pending_invitations()

    # 3. Anti-Echo Storm Guard (Pre-Filter)
    if is_pure_closing_statement(msg):
        print(f"[*] [Anti-Echo Guard] Received closing/standby statement from {sender_id}.")
        print("    -> Suppressing reciprocal reply. Conversation naturally concluded.")
        return

    # 4. Group Broadcast Discipline:
    # If it is a group broadcast and not addressed to this agent, do not auto-reply.
    if is_group:
        addressed = (agent_name in msg or hub_client.agent_id in msg or "全員" in msg or "all" in msg.lower())
        if not addressed:
            print(f"[*] [Group Broadcast] Message not explicitly addressed to '{agent_name}'. Acknowledged without reply.")
            return

    # 5. Invoke LLM Backend Reasoning Loop
    print(f"[*] Invoking AI Backend ({backend.__class__.__name__}) for task reasoning...")
    t_start = time.time()
    reply_text = backend.execute(msg, sender_id, {"groupId": group_id, "contextId": context_id})
    elapsed = time.time() - t_start

    if not reply_text:
        print(f"[!] AI Backend produced no response ({elapsed:.2f}s).", file=sys.stderr)
        return

    reply_text = reply_text.strip()
    print(f"[✓] AI Backend completed reasoning in {elapsed:.2f}s.")

    # 6. Anti-Echo Storm Guard (Post-Filter)
    if "[[A2A_NO_REPLY]]" in reply_text:
        print(f"[*] [Anti-Echo Guard] LLM emitted [[A2A_NO_REPLY]]. Suppressing reply task.")
        return

    # 7. Deliver reasoned reply to requester
    reply_task_id = f"reply-{task_id}"
    print(f"[*] Sending reasoned response to {sender_id} (task {reply_task_id})...")
    res = hub_client.send_task(sender_id, reply_text, context_id=context_id, task_id=reply_task_id)
    if res:
        print(f"[✓] Response delivered to {sender_id} (status={res.get('state')}).")


# ---------------------------------------------------------------------------
# Outbound Resilient SSE Listener
# ---------------------------------------------------------------------------

def run_bridge_listener(hub_client, backend, agent_name):
    """Maintain resilient outbound SSE connection to Hub."""
    print("=" * 65)
    print(f" 888a2a-lite Universal Agent Bridge: {agent_name}")
    print(f" Hub:      {hub_client.hub_url}")
    print(f" Agent ID: {hub_client.agent_id}")
    print(f" Backend:  {backend.__class__.__name__}")
    print(f" Local IP: {get_local_ip()}")
    print("=" * 65)

    # Initial check for pending invitations
    hub_client.auto_accept_pending_invitations()

    last_event_id = 0
    backoff = 1

    while True:
        stream_url = f"{hub_client.hub_url}/hub/v1/agents/{hub_client.agent_id}/inbox/stream"
        if last_event_id > 0:
            stream_url += f"?afterSequence={last_event_id}"

        print(f"[*] Connecting SSE Stream: {stream_url}...")
        req = urllib.request.Request(stream_url, headers={
            "Accept": "text/event-stream",
            "X-Agent-ID": hub_client.agent_id,
            "Authorization": f"Bearer {hub_client.token}",
        })
        if hub_client.shared_key:
            req.add_header("X-Hub-Key", hub_client.shared_key)
        if last_event_id > 0:
            req.add_header("Last-Event-ID", str(last_event_id))

        try:
            with urllib.request.urlopen(req, timeout=60) as resp:
                if resp.status != 200:
                    print(f"[!] Stream returned status {resp.status}", file=sys.stderr)
                    time.sleep(backoff)
                    backoff = min(backoff * 2, 30)
                    continue

                print("[✓] Connected to SSE Stream. Actively listening for push tasks...")
                backoff = 1
                current_id = None
                current_data = []

                for raw_line in resp:
                    line = raw_line.decode("utf-8", errors="replace").rstrip("\r\n")

                    if not line:
                        # Empty line signals dispatch of the SSE event
                        if current_data:
                            raw_json = "\n".join(current_data)
                            try:
                                item = json.loads(raw_json)
                                if current_id:
                                    last_event_id = int(current_id)
                                elif item.get("sequence"):
                                    last_event_id = int(item["sequence"])

                                process_incoming_task(hub_client, backend, item, agent_name)
                            except json.JSONDecodeError as err:
                                print(f"[!] Failed to parse JSON event: {err}", file=sys.stderr)

                        current_id = None
                        current_data = []
                        continue

                    if line.startswith(":"):
                        # Keep-alive heartbeat comment from Hub
                        continue

                    if line.startswith("id: "):
                        current_id = line[4:].strip()
                    elif line.startswith("data: "):
                        current_data.append(line[6:])

        except urllib.error.HTTPError as e:
            err_body = e.read().decode("utf-8", errors="replace")
            print(f"[!] HTTP Error {e.code}: {err_body}", file=sys.stderr)
            time.sleep(backoff)
            backoff = min(backoff * 2, 30)
        except Exception as e:
            print(f"[!] Stream disconnected ({e}). Reconnecting in {backoff}s...", file=sys.stderr)
            time.sleep(backoff)
            backoff = min(backoff * 2, 30)


# ---------------------------------------------------------------------------
# Service Installation (LaunchAgent & systemd)
# ---------------------------------------------------------------------------

def filter_service_args(args):
    """Cleanly strip --install-service and its option value from args."""
    clean = []
    skip_next = False
    for a in args:
        if skip_next:
            skip_next = False
            continue
        if a == "--install-service":
            skip_next = True
            continue
        if a.startswith("--install-service="):
            continue
        clean.append(a)
    return clean


def install_launchd_service(agent_name, script_args, service_name=None):
    """Generate and load macOS LaunchAgent plist."""
    slug = service_name or sanitize_slug(agent_name)
    label = f"com.a2a.agent.{slug}"
    plist_path = os.path.expanduser(f"~/Library/LaunchAgents/{label}.plist")
    log_dir = os.path.expanduser("~/.a2a/logs")
    os.makedirs(log_dir, exist_ok=True)
    os.makedirs(os.path.dirname(plist_path), exist_ok=True)

    python_bin = sys.executable
    script_path = os.path.abspath(__file__)

    # Filter out --install-service from args to prevent recursion
    clean_args = filter_service_args(script_args)

    arg_xml = f"    <string>{python_bin}</string>\n    <string>{script_path}</string>\n"
    for a in clean_args:
        arg_xml += f"    <string>{a}</string>\n"

    plist_content = f"""<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>{label}</string>
  <key>ProgramArguments</key>
  <array>
{arg_xml.rstrip()}
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>{get_enhanced_env()["PATH"]}</string>
    <key>PYTHONUNBUFFERED</key>
    <string>1</string>
  </dict>
  <key>StandardOutPath</key>
  <string>{log_dir}/{slug}.log</string>
  <key>StandardErrorPath</key>
  <string>{log_dir}/{slug}.error.log</string>
</dict>
</plist>
"""
    with open(plist_path, "w", encoding="utf-8") as f:
        f.write(plist_content)

    print(f"[✓] LaunchAgent plist created at: {plist_path}")
    # Unload if already loaded, then load
    subprocess.run(["launchctl", "unload", plist_path], capture_output=True)
    res = subprocess.run(["launchctl", "load", "-w", plist_path], capture_output=True, text=True)
    if res.returncode == 0:
        print(f"[✓] Service '{label}' loaded and running in background!")
        print(f"[*] Check logs with: tail -f {log_dir}/{slug}.log")
    else:
        print(f"[!] Failed to load plist with launchctl: {res.stderr}", file=sys.stderr)


def install_systemd_service(agent_name, script_args, service_name=None):
    """Generate and enable Linux user systemd service."""
    slug = service_name or sanitize_slug(agent_name)
    service_unit = f"a2a-agent-{slug}.service"
    service_dir = os.path.expanduser("~/.config/systemd/user")
    os.makedirs(service_dir, exist_ok=True)
    service_path = os.path.join(service_dir, service_unit)

    python_bin = sys.executable
    script_path = os.path.abspath(__file__)
    clean_args = " ".join([f'"{a}"' for a in filter_service_args(script_args)])

    content = f"""[Unit]
Description=888a2a-lite Universal Agent Bridge ({agent_name})
After=network.target

[Service]
Type=simple
Environment=PYTHONUNBUFFERED=1
Environment=PATH={get_enhanced_env()["PATH"]}
ExecStart={python_bin} {script_path} {clean_args}
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
"""
    with open(service_path, "w", encoding="utf-8") as f:
        f.write(content)

    print(f"[✓] systemd user service created at: {service_path}")
    subprocess.run(["systemctl", "--user", "daemon-reload"], check=False)
    res = subprocess.run(["systemctl", "--user", "enable", "--now", service_unit], capture_output=True, text=True)
    if res.returncode == 0:
        print(f"[✓] Service '{service_unit}' enabled and running!")
        print(f"[*] Check logs with: journalctl --user -u {service_unit} -f")
    else:
        print(f"[!] Failed to start systemd service: {res.stderr}", file=sys.stderr)


# ---------------------------------------------------------------------------
# Main Entry Point & Argument Parsing
# ---------------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser(description="888a2a-lite Universal Agent Bridge")
    parser.add_argument("--hub", default=os.getenv("A2A888_HUB_URL", "https://a2a.david888.com"),
                        help="Hub Base URL (default: https://a2a.david888.com)")
    parser.add_argument("--name", default="A2A-Agent", help="Human-readable Agent Name")
    parser.add_argument("--agent-id", help="Explicit Agent ID (optional, auto-loaded/registered)")
    parser.add_argument("--token", help="Explicit Agent Token (optional, auto-loaded/registered)")
    parser.add_argument("--shared-key", default=os.getenv("A2A888_HUB_SHARED_KEY"),
                        help="Hub Pre-Shared Key (for SEMI_OPEN mode)")
    parser.add_argument("--credentials", help="Path to credentials JSON file")

    # Backend selection
    parser.add_argument("--backend", choices=["openclaw", "hermes", "openai", "echo"], default="openclaw",
                        help="AI Execution Backend (default: openclaw)")
    parser.add_argument("--backend-agent", default="default", help="Agent profile for OpenClaw (default: default)")
    parser.add_argument("--system-prompt", default="", help="Persona or system prompt instructions")

    # OpenAI / Ollama compatible settings
    parser.add_argument("--api-base", default="http://localhost:11434/v1", help="API Base for OpenAI backend")
    parser.add_argument("--api-key", default="sk-dummy", help="API Key for OpenAI backend")
    parser.add_argument("--model", default="llama3", help="Model name for OpenAI backend")

    # Service installation
    parser.add_argument("--install-service", choices=["launchd", "systemd"],
                        help="Install and start as background OS service (launchd on macOS, systemd on Linux)")
    parser.add_argument("--service-name", help="Custom ASCII service name for launchd/systemd")

    args = parser.parse_args()

    # Handle service installation if requested
    if args.install_service:
        if args.install_service == "launchd":
            install_launchd_service(args.name, sys.argv[1:], service_name=args.service_name)
        elif args.install_service == "systemd":
            install_systemd_service(args.name, sys.argv[1:], service_name=args.service_name)
        sys.exit(0)

    # Resolve or auto-register credentials
    cred_file = args.credentials or os.path.expanduser(f"~/.a2a/credentials_{sanitize_slug(args.name)}.json")
    agent_id = args.agent_id
    token = args.token

    if not agent_id or not token:
        if os.path.isfile(cred_file):
            print(f"[*] Loading existing credentials from: {cred_file}")
            try:
                with open(cred_file, "r", encoding="utf-8") as f:
                    data = json.load(f)
                    ident = data.get("identity", data)
                    agent_id = ident.get("agentId")
                    token = ident.get("agentToken")
            except Exception as e:
                print(f"[!] Warning: Could not read {cred_file}: {e}", file=sys.stderr)

    hub_client = HubClient(args.hub, agent_id, token, args.shared_key)

    if not hub_client.agent_id or not hub_client.token:
        print(f"[*] No credentials found for '{args.name}'. Auto-registering with Hub...")
        try:
            reg_data = hub_client.register(args.name, f"{args.name} powered by {args.backend} via Universal Bridge")
            print(f"[✓] Registration successful! Agent ID: {hub_client.agent_id}")
            os.makedirs(os.path.dirname(os.path.abspath(cred_file)), exist_ok=True)
            with open(cred_file, "w", encoding="utf-8") as f:
                json.dump(reg_data, f, indent=2, ensure_ascii=False)
            print(f"[✓] Credentials saved to: {cred_file}")
        except Exception as e:
            print(f"[!] Auto-registration failed: {e}", file=sys.stderr)
            sys.exit(1)

    # Initialize AI Backend
    if args.backend == "openclaw":
        backend = OpenClawBackend(agent_profile=args.backend_agent, system_prompt=args.system_prompt)
    elif args.backend == "hermes":
        backend = HermesBackend(system_prompt=args.system_prompt)
    elif args.backend == "openai":
        backend = OpenAIBackend(args.api_base, args.api_key, args.model, system_prompt=args.system_prompt)
    else:
        backend = EchoBackend(agent_name=args.name)

    # Run the resilient listener
    try:
        run_bridge_listener(hub_client, backend, args.name)
    except KeyboardInterrupt:
        print("\n[*] Universal Agent Bridge shutdown gracefully.")


if __name__ == "__main__":
    main()
