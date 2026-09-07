#!/usr/bin/env python3
"""
888a2a-lite Universal Agent Bridge (a2a_bridge.py)
Official Production-Ready Agent Daemon for 888a2a-lite Hub.

Features:
- Zero external dependencies (pure Python standard library).
- Multi-backend AI support (OpenClaw, Hermes, OpenAI/Ollama compatible API, custom cmd).
- Durable local enqueue before receipt ACK; independent inference and retry workers.
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
import contextlib
import fcntl
import hashlib
import json
import os
import re
import shlex
import socket
import sqlite3
import subprocess
import sys
import tempfile
import threading
import time
import uuid
import plistlib
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


def acquire_process_lock(queue_path):
    lock_path = os.path.abspath(os.path.expanduser(queue_path)) + ".lock"
    os.makedirs(os.path.dirname(lock_path), exist_ok=True)
    handle = open(lock_path, "a+", encoding="utf-8")
    try:
        fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        handle.close()
        raise RuntimeError(f"another Bridge process owns queue {queue_path}")
    return handle


def save_credentials(path, data):
    """Atomically save credentials without exposing a partially written secret."""
    parent = os.path.dirname(os.path.abspath(path))
    os.makedirs(parent, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix=".credentials.", dir=parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            json.dump(data, handle, indent=2, ensure_ascii=False)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
        directory_fd = os.open(parent, os.O_RDONLY)
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)
    except Exception:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass
        raise


def systemd_unit_quote(value, exec_arg=True):
    value = value.replace("\\", "\\\\").replace('"', '\\"').replace("\n", "\\n")
    if exec_arg:
        value = value.replace("%", "%%").replace("$", "$$")
    return '"' + value + '"'


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

    def register(self, name, registration_key, provider_family="generic", transport_id="http-json", capabilities=None):
        """Register agent with Hub and receive agentId and token."""
        url = f"{self.hub_url}/hub/v1/agents/register"
        payload = json.dumps({
            "displayName": name,
            "providerFamily": provider_family,
            "transportId": transport_id,
            "capabilities": capabilities or ["text/plain"],
            "registrationIdempotencyKey": registration_key,
        }).encode("utf-8")
        req = urllib.request.Request(url, data=payload, headers=self._headers(auth=False))
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                data = json.loads(resp.read().decode("utf-8"))
                ident = data.get("identity", {})
                self.agent_id = ident.get("agentId")
                self.token = ident.get("agentToken")
                if not self.agent_id or not self.token:
                    raise RuntimeError(
                        "Hub returned an existing identity without agentToken; "
                        "restore the original credential file instead of retrying registration"
                    )
                return data
        except urllib.error.HTTPError as e:
            err = e.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"Registration failed: HTTP {e.code}: {err}")

    def poll_inbox(self, after=0, limit=100):
        url = f"{self.hub_url}/hub/v1/agents/{self.agent_id}/inbox?afterSequence={after}&limit={limit}"
        req = urllib.request.Request(url, headers=self._headers())
        with urllib.request.urlopen(req, timeout=15) as resp:
            data = json.loads(resp.read().decode("utf-8"))
            return data.get("items", [])

    def ack_task(self, sequence):
        """Acknowledge a sequence after durable local enqueue."""
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

    def list_agents(self):
        """List active peer agents on Hub."""
        url = f"{self.hub_url}/hub/v1/agents"
        req = urllib.request.Request(url, headers=self._headers())
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read().decode("utf-8"))
            return data.get("agents", [])

    def status(self):
        """Query Hub health status and mode."""
        url = f"{self.hub_url}/hub/v1/status"
        req = urllib.request.Request(url, headers=self._headers(auth=False))
        with urllib.request.urlopen(req, timeout=10) as resp:
            return json.loads(resp.read().decode("utf-8"))

    def send_group_message(self, group_id, message, idempotency_key=None):
        """Send a broadcast message to a multi-agent group."""
        url = f"{self.hub_url}/hub/v1/groups/{group_id}/messages"
        payload = json.dumps({
            "message": message,
            "idempotencyKey": idempotency_key or f"grp-msg-{int(time.time()*1000)}"
        }).encode("utf-8")
        req = urllib.request.Request(url, data=payload, headers=self._headers())
        with urllib.request.urlopen(req, timeout=15) as resp:
            return json.loads(resp.read().decode("utf-8"))


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


class ClaudeCodeBackend(AIBackend):
    def __init__(self, system_prompt=""):
        self.system_prompt = system_prompt
        self.env = get_enhanced_env()

    def execute(self, message, sender_id, context):
        full_instruction = (
            f"來自 A2A 同儕 Agent ({sender_id}) 的訊息：\n{message}\n\n"
            f"【防回音守衛規則】若此訊息僅為確認收悉、狀態回報、或無需再回信，請直接輸出 [[A2A_NO_REPLY]]。"
        )
        if self.system_prompt:
            full_instruction = f"{self.system_prompt}\n\n{full_instruction}"

        cmd = ["claude", "-p", full_instruction]
        try:
            proc = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                env=self.env,
                timeout=120
            )
            if proc.returncode != 0:
                print(f"[!] Claude Code returned error (code {proc.returncode}): {proc.stderr}", file=sys.stderr)
                return None
            return proc.stdout.strip()
        except subprocess.TimeoutExpired:
            print("[!] Claude Code timed out after 120s.", file=sys.stderr)
            return None
        except Exception as e:
            print(f"[!] Failed to invoke Claude Code: {e}", file=sys.stderr)
            return None


class CodexBackend(AIBackend):
    def __init__(self, system_prompt=""):
        self.system_prompt = system_prompt
        self.env = get_enhanced_env()

    def execute(self, message, sender_id, context):
        full_instruction = (
            f"來自 A2A 同儕 Agent ({sender_id}) 的訊息：\n{message}\n\n"
            f"【防回音守衛規則】若此訊息僅為確認收悉、狀態回報、或無需再回信，請直接輸出 [[A2A_NO_REPLY]]。"
        )
        if self.system_prompt:
            full_instruction = f"{self.system_prompt}\n\n{full_instruction}"

        cmd = ["codex", "exec", full_instruction]
        try:
            proc = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                env=self.env,
                timeout=120
            )
            if proc.returncode != 0:
                print(f"[!] Codex returned error (code {proc.returncode}): {proc.stderr}", file=sys.stderr)
                return None
            return proc.stdout.strip()
        except subprocess.TimeoutExpired:
            print("[!] Codex timed out after 120s.", file=sys.stderr)
            return None
        except Exception as e:
            print(f"[!] Failed to invoke Codex: {e}", file=sys.stderr)
            return None


class CommandBackend(AIBackend):
    def __init__(self, command_cmd, system_prompt=""):
        self.command_cmd = command_cmd
        self.system_prompt = system_prompt
        self.env = get_enhanced_env()

    def execute(self, message, sender_id, context):
        full_instruction = (
            f"來自 A2A 同儕 Agent ({sender_id}) 的訊息：\n{message}\n\n"
            f"【防回音守衛規則】若此訊息僅為確認收悉、狀態回報、或無需再回信，請直接輸出 [[A2A_NO_REPLY]]。"
        )
        if self.system_prompt:
            full_instruction = f"{self.system_prompt}\n\n{full_instruction}"

        if not self.command_cmd:
            print("[!] Error: CommandBackend requires --backend-cmd parameter.", file=sys.stderr)
            return None

        try:
            if "{message}" in self.command_cmd:
                cmd = self.command_cmd.format(
                    message=shlex.quote(full_instruction),
                    sender_id=sender_id
                )
                proc = subprocess.run(
                    cmd,
                    shell=True,
                    capture_output=True,
                    text=True,
                    env=self.env,
                    timeout=120
                )
            else:
                proc = subprocess.run(
                    self.command_cmd,
                    shell=True,
                    input=full_instruction,
                    capture_output=True,
                    text=True,
                    env=self.env,
                    timeout=120
                )

            if proc.returncode != 0:
                print(f"[!] Custom command returned error (code {proc.returncode}): {proc.stderr}", file=sys.stderr)
                return None
            return proc.stdout.strip()
        except subprocess.TimeoutExpired:
            print("[!] Custom command timed out after 120s.", file=sys.stderr)
            return None
        except Exception as e:
            print(f"[!] Failed to invoke custom command: {e}", file=sys.stderr)
            return None


# ---------------------------------------------------------------------------
# Core Task Engine: Instant ACK & Anti-Echo Guard
# ---------------------------------------------------------------------------

class DurableWorkQueue:
    """Small local WAL queue. Hub ACK means accepted into this queue."""
    def __init__(self, path, scope=""):
        self.path = os.path.abspath(os.path.expanduser(path))
        os.makedirs(os.path.dirname(self.path), exist_ok=True)
        fd = os.open(self.path, os.O_CREAT, 0o600)
        os.close(fd)
        if os.path.exists(self.path):
            os.chmod(self.path, 0o600)
        self.lock = threading.Lock()
        with self._db() as db:
            db.executescript("""
                PRAGMA journal_mode=WAL;
                CREATE TABLE IF NOT EXISTS work (
                    sequence INTEGER PRIMARY KEY, item_json TEXT NOT NULL,
                    state TEXT NOT NULL DEFAULT 'pending', acked INTEGER NOT NULL DEFAULT 0,
                    reply_json TEXT, attempts INTEGER NOT NULL DEFAULT 0,
                    updated_at REAL NOT NULL, retry_after REAL NOT NULL DEFAULT 0
                );
            """)
            columns = {row[1] for row in db.execute("PRAGMA table_info(work)")}
            if "retry_after" not in columns:
                db.execute("ALTER TABLE work ADD COLUMN retry_after REAL NOT NULL DEFAULT 0")
            db.execute("UPDATE work SET state='pending' WHERE state='inflight'")
            db.execute("CREATE TABLE IF NOT EXISTS queue_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)")
            previous = db.execute("SELECT value FROM queue_meta WHERE key='scope'").fetchone()
            if previous and previous[0] != scope:
                raise RuntimeError("queue database belongs to a different Hub or agent")
            if not previous:
                db.execute("INSERT INTO queue_meta(key,value) VALUES('scope',?)", (scope,))
        os.chmod(self.path, 0o600)
        for sidecar in (self.path + "-wal", self.path + "-shm"):
            if os.path.exists(sidecar):
                os.chmod(sidecar, 0o600)

    def _connect(self):
        db = sqlite3.connect(self.path, timeout=30)
        db.row_factory = sqlite3.Row
        db.execute("PRAGMA synchronous=FULL")
        return db

    @contextlib.contextmanager
    def _db(self):
        db = self._connect()
        try:
            yield db
            db.commit()
        finally:
            db.close()

    def enqueue(self, item):
        sequence = int(item["sequence"])
        with self.lock, self._db() as db:
            db.execute("INSERT OR IGNORE INTO work(sequence,item_json,updated_at) VALUES(?,?,?)",
                       (sequence, json.dumps(item, ensure_ascii=False), time.time()))

    def set_acked(self, sequence):
        with self.lock, self._db() as db:
            db.execute("UPDATE work SET acked=1,updated_at=? WHERE sequence=?", (time.time(), sequence))

    def next(self):
        with self.lock, self._db() as db:
            row = db.execute("SELECT * FROM work WHERE state='pending' AND retry_after<=? ORDER BY sequence LIMIT 1", (time.time(),)).fetchone()
            if not row:
                return None
            db.execute("UPDATE work SET state='inflight',attempts=attempts+1,updated_at=? WHERE sequence=?",
                       (time.time(), row["sequence"]))
            return dict(row)

    def save_reply(self, sequence, reply):
        with self.lock, self._db() as db:
            db.execute("UPDATE work SET reply_json=?,updated_at=? WHERE sequence=?",
                       (json.dumps(reply, ensure_ascii=False), time.time(), sequence))

    def finish(self, sequence):
        with self.lock, self._db() as db:
            db.execute("UPDATE work SET state='done',updated_at=? WHERE sequence=?", (time.time(), sequence))

    def retry(self, sequence):
        with self.lock, self._db() as db:
            db.execute("UPDATE work SET state='pending',retry_after=?,updated_at=? WHERE sequence=?", (time.time() + 2, time.time(), sequence))


def is_pure_closing_statement(text):
    """Only suppress exact, short closing phrases; actionable text reaches the LLM."""
    normalized = re.sub(r"\s+", " ", text.strip()).strip("。.!！~～ ")
    closings = {
        "收到", "收到確認", "收錄完畢", "保持連線待命", "隨時準備好", "辛苦了",
        "一點都不辛苦", "晚安", "拜拜", "不用回覆", "已就定位", "一切正常",
        "收到～", "辛苦啦",
    }
    return normalized in closings


def process_incoming_task(hub_client, backend, queue, item, agent_name):
    """Persist first, then ACK; inference runs only from durable work."""
    seq = item.get("sequence")
    task_id = item.get("taskId")
    sender_id = item.get("requesterAgentId")
    context_id = item.get("contextId")
    msg = item.get("message", "")
    group_id = item.get("groupId")
    is_group = bool(group_id)
    tag = f"[Group Broadcast: {group_id}]" if is_group else f"[Direct Task: {task_id}]"
    print(f"\n{'='*55}\n{tag} from {sender_id} (seq={seq})\nMessage: {msg.strip()}\n{'='*55}")

    queue.enqueue(item)
    print(f"[*] Persisted seq {seq}; acknowledging immediately...")
    if hub_client.ack_task(seq):
        queue.set_acked(seq)
        print(f"[✓] ACK confirmed for seq {seq}.")
    else:
        print(f"[!] ACK not confirmed for seq {seq}; durable retry remains queued.", file=sys.stderr)


def process_queued_task(hub_client, backend, queue, row, agent_name):
    item = json.loads(row["item_json"])
    seq = item.get("sequence")
    task_id = item.get("taskId")
    sender_id = item.get("requesterAgentId")
    context_id = item.get("contextId")
    msg = item.get("message", "")
    group_id = item.get("groupId")
    is_group = bool(group_id)

    if not row["acked"]:
        if hub_client.ack_task(seq):
            queue.set_acked(seq)
        else:
            queue.retry(seq)
            return

    # 2. Check and auto-accept invitations if applicable
    if any(k in msg.lower() for k in ("invite", "group", "群組", "邀請")):
        hub_client.auto_accept_pending_invitations()

    # 3. Anti-Echo Storm Guard (Pre-Filter)
    if is_pure_closing_statement(msg):
        print(f"[*] [Anti-Echo Guard] Received closing/standby statement from {sender_id}.")
        print("    -> Suppressing reciprocal reply. Conversation naturally concluded.")
        queue.finish(seq)
        return

    # 4. Group Broadcast Discipline:
    # If it is a group broadcast and not addressed to this agent, do not auto-reply.
    if is_group:
        addressed = (agent_name in msg or hub_client.agent_id in msg or "全員" in msg or "all" in msg.lower())
        if not addressed:
            print(f"[*] [Group Broadcast] Message not explicitly addressed to '{agent_name}'. Acknowledged without reply.")
            queue.finish(seq)
            return

    saved_reply = json.loads(row["reply_json"]) if row.get("reply_json") else None
    if saved_reply:
        reply_text = saved_reply["message"]
        print(f"[*] Retrying persisted response for seq {seq}.")
    else:
        print(f"[*] Invoking AI Backend ({backend.__class__.__name__}) for task reasoning...")
        t_start = time.time()
        reply_text = backend.execute(msg, sender_id, {"groupId": group_id, "contextId": context_id})
        elapsed = time.time() - t_start
        if not reply_text:
            print(f"[!] AI Backend produced no response ({elapsed:.2f}s); retaining work.", file=sys.stderr)
            queue.retry(seq)
            return
        reply_text = reply_text.strip()
        print(f"[✓] AI Backend completed reasoning in {elapsed:.2f}s.")

    # 6. Anti-Echo Storm Guard (Post-Filter)
    if "[[A2A_NO_REPLY]]" in reply_text:
        print(f"[*] [Anti-Echo Guard] LLM emitted [[A2A_NO_REPLY]]. Suppressing reply task.")
        queue.finish(seq)
        return

    # 7. Deliver reasoned reply to requester
    reply_task_id = saved_reply["task_id"] if saved_reply else f"reply-{hub_client.agent_id}-{seq}"
    reply = saved_reply or {"target": sender_id, "message": reply_text, "context_id": context_id, "task_id": reply_task_id}
    if not saved_reply:
        queue.save_reply(seq, reply)
    print(f"[*] Sending reasoned response to {sender_id} (task {reply_task_id})...")
    res = hub_client.send_task(reply["target"], reply["message"], context_id=reply["context_id"], task_id=reply["task_id"])
    if res:
        print(f"[✓] Response delivered to {sender_id} (status={res.get('state')}).")
        queue.finish(seq)
    else:
        queue.retry(seq)


def run_work_worker(hub_client, backend, queue, agent_name):
    while True:
        try:
            row = queue.next()
        except Exception as exc:
            print(f"[!] Work queue claim failed; retrying: {exc}", file=sys.stderr)
            time.sleep(2)
            continue
        if row is None:
            time.sleep(0.25)
            continue
        try:
            process_queued_task(hub_client, backend, queue, row, agent_name)
        except Exception as exc:
            print(f"[!] Work seq {row['sequence']} failed; retrying: {exc}", file=sys.stderr)
            queue.retry(row["sequence"])
            time.sleep(min(30, 2 ** min(row["attempts"], 4)))


# ---------------------------------------------------------------------------
# Outbound Resilient SSE Listener
# ---------------------------------------------------------------------------

def run_bridge_listener(hub_client, backend, agent_name, queue):
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

    backoff = 1

    threading.Thread(target=run_work_worker, args=(hub_client, backend, queue, agent_name), daemon=True).start()

    def reconcile_pending():
        while True:
            try:
                after = 0
                while True:
                    items = hub_client.poll_inbox(after=after, limit=100)
                    if not items:
                        break
                    for item in items:
                        process_incoming_task(hub_client, backend, queue, item, agent_name)
                    next_after = max(int(item.get("sequence", after)) for item in items)
                    if next_after <= after:
                        break
                    after = next_after
            except Exception as exc:
                print(f"[!] Inbox reconciliation failed: {exc}", file=sys.stderr)
            time.sleep(5)

    threading.Thread(target=reconcile_pending, daemon=True).start()

    while True:
        stream_url = f"{hub_client.hub_url}/hub/v1/agents/{hub_client.agent_id}/inbox/stream"
        print(f"[*] Connecting SSE Stream: {stream_url}...")
        req = urllib.request.Request(stream_url, headers={
            "Accept": "text/event-stream",
            "X-Agent-ID": hub_client.agent_id,
            "Authorization": f"Bearer {hub_client.token}",
        })
        if hub_client.shared_key:
            req.add_header("X-Hub-Key", hub_client.shared_key)

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
                                process_incoming_task(hub_client, backend, queue, item, agent_name)
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
# Model Context Protocol (MCP) Server (stdio JSON-RPC 2.0)
# ---------------------------------------------------------------------------

def run_mcp_server(hub_client, agent_name, out_stream=None):
    """Serve Model Context Protocol (MCP) over stdio using JSON-RPC 2.0."""
    print(f"[*] Starting 888a2a MCP Server for '{agent_name}' ({hub_client.agent_id})", file=sys.stderr)
    out_pipe = out_stream or sys.stdout

    tools = [
        {
            "name": "a2a_list_agents",
            "description": "List all active and registered peer agents on the 888a2a Hub.",
            "inputSchema": {
                "type": "object",
                "properties": {}
            }
        },
        {
            "name": "a2a_send_task",
            "description": "Send a direct task or message to a specific agent by target agent ID.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "targetAgentId": {
                        "type": "string",
                        "description": "The unique Agent ID of the recipient"
                    },
                    "message": {
                        "type": "string",
                        "description": "The task instructions or message to send"
                    }
                },
                "required": ["targetAgentId", "message"]
            }
        },
        {
            "name": "a2a_broadcast_group",
            "description": "Broadcast a message to an A2A multi-agent group.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "groupId": {
                        "type": "string",
                        "description": "The group ID to broadcast to"
                    },
                    "message": {
                        "type": "string",
                        "description": "The broadcast message content"
                    }
                },
                "required": ["groupId", "message"]
            }
        },
        {
            "name": "a2a_poll_inbox",
            "description": "Poll the current agent's inbox for pending tasks or incoming replies.",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "afterSequence": {
                        "type": "integer",
                        "description": "Poll messages with sequence strictly greater than this value",
                        "default": 0
                    },
                    "limit": {
                        "type": "integer",
                        "description": "Maximum number of messages to return",
                        "default": 20
                    }
                }
            }
        },
        {
            "name": "a2a_status",
            "description": "Get current 888a2a Hub health, operational mode (PUBLIC/SEMI_OPEN), and active peer count.",
            "inputSchema": {
                "type": "object",
                "properties": {}
            }
        }
    ]

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError:
            continue

        req_id = req.get("id")
        method = req.get("method")
        params = req.get("params", {})

        # Handle notifications (no id)
        if req_id is None:
            if method in ("notifications/initialized", "initialized"):
                print("[*] MCP client session initialized.", file=sys.stderr)
            continue

        resp = {"jsonrpc": "2.0", "id": req_id}

        if method == "initialize":
            resp["result"] = {
                "protocolVersion": "2024-11-05",
                "capabilities": {
                    "tools": {}
                },
                "serverInfo": {
                    "name": "888a2a-lite-mcp",
                    "version": "0.2.0"
                }
            }
        elif method == "tools/list":
            resp["result"] = {"tools": tools}
        elif method == "tools/call":
            tool_name = params.get("name")
            args = params.get("arguments", {})
            try:
                if tool_name == "a2a_list_agents":
                    agents = hub_client.list_agents()
                    resp["result"] = {
                        "content": [{"type": "text", "text": json.dumps(agents, ensure_ascii=False, indent=2)}]
                    }
                elif tool_name == "a2a_send_task":
                    target = args.get("targetAgentId")
                    msg = args.get("message")
                    res = hub_client.send_task(target, msg)
                    resp["result"] = {
                        "content": [{"type": "text", "text": json.dumps(res, ensure_ascii=False, indent=2)}]
                    }
                elif tool_name == "a2a_broadcast_group":
                    gid = args.get("groupId")
                    msg = args.get("message")
                    res = hub_client.send_group_message(gid, msg)
                    resp["result"] = {
                        "content": [{"type": "text", "text": json.dumps(res, ensure_ascii=False, indent=2)}]
                    }
                elif tool_name == "a2a_poll_inbox":
                    after = args.get("afterSequence", 0)
                    limit = args.get("limit", 20)
                    items = hub_client.poll_inbox(after=after, limit=limit)
                    resp["result"] = {
                        "content": [{"type": "text", "text": json.dumps(items, ensure_ascii=False, indent=2)}]
                    }
                elif tool_name == "a2a_status":
                    st = hub_client.status()
                    resp["result"] = {
                        "content": [{"type": "text", "text": json.dumps(st, ensure_ascii=False, indent=2)}]
                    }
                else:
                    resp["error"] = {"code": -32601, "message": f"Tool '{tool_name}' not found"}
            except Exception as e:
                resp["result"] = {
                    "content": [{"type": "text", "text": f"Error executing {tool_name}: {str(e)}"}],
                    "isError": True
                }
        elif method == "ping":
            resp["result"] = {}
        else:
            resp["error"] = {"code": -32601, "message": f"Method '{method}' not found"}

        out = json.dumps(resp, ensure_ascii=False)
        out_pipe.write(out + "\n")
        out_pipe.flush()


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

    plist = {
        "Label": label,
        "ProgramArguments": [python_bin, script_path, *clean_args],
        "RunAtLoad": True, "KeepAlive": True,
        "EnvironmentVariables": {"PATH": get_enhanced_env()["PATH"], "PYTHONUNBUFFERED": "1"},
        "StandardOutPath": f"{log_dir}/{slug}.log",
        "StandardErrorPath": f"{log_dir}/{slug}.error.log",
    }
    with open(plist_path, "wb") as f:
        plistlib.dump(plist, f, fmt=plistlib.FMT_XML)
    os.chmod(plist_path, 0o600)

    print(f"[✓] LaunchAgent plist created at: {plist_path}")
    # Unload if already loaded, then load
    subprocess.run(["launchctl", "unload", plist_path], capture_output=True, check=False)
    res = subprocess.run(["launchctl", "load", "-w", plist_path], capture_output=True, text=True)
    if res.returncode == 0:
        print(f"[✓] Service '{label}' loaded and running in background!")
        print(f"[*] Check logs with: tail -f {log_dir}/{slug}.log")
    else:
        raise RuntimeError(f"launchctl load failed: {res.stderr.strip()}")


def install_systemd_service(agent_name, script_args, service_name=None):
    """Generate and enable Linux user systemd service."""
    slug = service_name or sanitize_slug(agent_name)
    service_unit = f"a2a-agent-{slug}.service"
    service_dir = os.path.expanduser("~/.config/systemd/user")
    os.makedirs(service_dir, exist_ok=True)
    service_path = os.path.join(service_dir, service_unit)

    python_bin = sys.executable
    script_path = os.path.abspath(__file__)
    clean_args = " ".join(systemd_unit_quote(a) for a in filter_service_args(script_args))
    systemd_path = systemd_unit_quote("PATH=" + get_enhanced_env()["PATH"], exec_arg=False)

    content = f"""[Unit]
Description=888a2a-lite Universal Agent Bridge ({slug})
After=network.target

[Service]
Type=simple
Environment=PYTHONUNBUFFERED=1
Environment={systemd_path}
ExecStart={systemd_unit_quote(python_bin)} {systemd_unit_quote(script_path)} {clean_args}
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
"""
    with open(service_path, "w", encoding="utf-8") as f:
        f.write(content)
    os.chmod(service_path, 0o600)

    print(f"[✓] systemd user service created at: {service_path}")
    reload_res = subprocess.run(["systemctl", "--user", "daemon-reload"], capture_output=True, text=True)
    if reload_res.returncode != 0:
        raise RuntimeError(f"systemd daemon-reload failed: {reload_res.stderr.strip()}")
    enable_res = subprocess.run(["systemctl", "--user", "enable", "--now", service_unit], capture_output=True, text=True)
    if enable_res.returncode != 0:
        raise RuntimeError(f"systemd enable failed: {enable_res.stderr.strip()}")
    res = subprocess.run(["systemctl", "--user", "restart", service_unit], capture_output=True, text=True)
    if res.returncode == 0:
        print(f"[✓] Service '{service_unit}' enabled and restarted!")
        print(f"[*] Check logs with: journalctl --user -u {service_unit} -f")
    else:
        raise RuntimeError(f"systemd restart failed: {res.stderr.strip()}")


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
    parser.add_argument("--queue-db", help="Durable local work queue SQLite path")

    # Backend selection
    parser.add_argument("--backend", choices=["openclaw", "hermes", "openai", "claudecode", "codex", "command", "echo"], default="openclaw",
                        help="AI Execution Backend (default: openclaw)")
    parser.add_argument("--backend-agent", default="default", help="Agent profile for OpenClaw (default: default)")
    parser.add_argument("--backend-cmd", help="Command string or template for command backend")
    parser.add_argument("--system-prompt", default="", help="Persona or system prompt instructions")

    # OpenAI / Ollama compatible settings
    parser.add_argument("--api-base", default="http://localhost:11434/v1", help="API Base for OpenAI backend")
    parser.add_argument("--api-key", default="sk-dummy", help="API Key for OpenAI backend")
    parser.add_argument("--model", default="llama3", help="Model name for OpenAI backend")

    # Model Context Protocol (MCP) mode
    parser.add_argument("--mcp", action="store_true", help="Run as Model Context Protocol (MCP) stdio JSON-RPC server")

    # Service installation
    parser.add_argument("--install-service", choices=["launchd", "systemd"],
                        help="Install and start as background OS service (launchd on macOS, systemd on Linux)")
    parser.add_argument("--service-name", help="Custom ASCII service name for launchd/systemd")

    args = parser.parse_args()
    if bool(args.agent_id) != bool(args.token):
        parser.error("--agent-id and --token must be provided together")

    # Handle service installation if requested
    if args.install_service:
        if args.install_service == "launchd":
            install_launchd_service(args.name, sys.argv[1:], service_name=args.service_name)
        elif args.install_service == "systemd":
            install_systemd_service(args.name, sys.argv[1:], service_name=args.service_name)
        sys.exit(0)

    mcp_out = None
    if args.mcp:
        mcp_out = sys.stdout
        sys.stdout = sys.stderr

    # Resolve or auto-register credentials
    cred_file = args.credentials or os.path.expanduser(f"~/.a2a/credentials_{sanitize_slug(args.name)}.json")
    agent_id = args.agent_id
    token = args.token
    registration_key = None
    credential_data = {}

    if os.path.isfile(cred_file):
        print(f"[*] Loading existing credentials from: {cred_file}")
        try:
            with open(cred_file, "r", encoding="utf-8") as f:
                data = json.load(f)
                credential_data = data if isinstance(data, dict) else {}
                ident = data.get("identity", data)
                if not args.agent_id and not args.token:
                    agent_id = ident.get("agentId")
                    token = ident.get("agentToken")
                registration_key = data.get("registrationIdempotencyKey") or ident.get("registrationIdempotencyKey")
        except Exception as e:
            print(f"[!] Warning: Could not read {cred_file}: {e}", file=sys.stderr)

    needs_registration = not agent_id or not token
    if needs_registration and not registration_key:
        registration_key = "bridge-" + uuid.uuid4().hex
        os.makedirs(os.path.dirname(os.path.abspath(cred_file)), exist_ok=True)
        bootstrap = dict(credential_data)
        bootstrap["registrationIdempotencyKey"] = registration_key
        save_credentials(cred_file, bootstrap)

    hub_client = HubClient(args.hub, agent_id, token, args.shared_key)

    if not hub_client.agent_id or not hub_client.token:
        print(f"[*] No credentials found for '{args.name}'. Auto-registering with Hub...")
        try:
            reg_data = hub_client.register(args.name, registration_key, provider_family=args.backend)
            reg_data["registrationIdempotencyKey"] = registration_key
            print(f"[✓] Registration successful! Agent ID: {hub_client.agent_id}")
            os.makedirs(os.path.dirname(os.path.abspath(cred_file)), exist_ok=True)
            save_credentials(cred_file, reg_data)
            print(f"[✓] Credentials saved to: {cred_file}")
        except Exception as e:
            print(f"[!] Auto-registration failed: {e}", file=sys.stderr)
            sys.exit(1)

    # If MCP mode requested, run the MCP stdio server and exit
    if args.mcp:
        try:
            run_mcp_server(hub_client, args.name, out_stream=mcp_out)
        except KeyboardInterrupt:
            pass
        sys.exit(0)

    # Initialize AI Backend
    if args.backend == "openclaw":
        backend = OpenClawBackend(agent_profile=args.backend_agent, system_prompt=args.system_prompt)
    elif args.backend == "hermes":
        backend = HermesBackend(system_prompt=args.system_prompt)
    elif args.backend == "claudecode":
        backend = ClaudeCodeBackend(system_prompt=args.system_prompt)
    elif args.backend == "codex":
        backend = CodexBackend(system_prompt=args.system_prompt)
    elif args.backend == "command":
        backend = CommandBackend(command_cmd=args.backend_cmd, system_prompt=args.system_prompt)
    elif args.backend == "openai":
        backend = OpenAIBackend(args.api_base, args.api_key, args.model, system_prompt=args.system_prompt)
    else:
        backend = EchoBackend(agent_name=args.name)

    queue_scope = hashlib.sha256(f"{args.hub.rstrip('/')}/{hub_client.agent_id}".encode()).hexdigest()[:16]
    queue_default = os.path.expanduser(f"~/.a2a/queue_{queue_scope}.db")
    queue_path = args.queue_db or queue_default
    process_lock = acquire_process_lock(queue_path)
    queue = DurableWorkQueue(queue_path, scope=f"{args.hub.rstrip('/')}/{hub_client.agent_id}")

    # Run the resilient listener
    try:
        run_bridge_listener(hub_client, backend, args.name, queue)
    except KeyboardInterrupt:
        print("\n[*] Universal Agent Bridge shutdown gracefully.")
    finally:
        process_lock.close()


if __name__ == "__main__":
    main()
