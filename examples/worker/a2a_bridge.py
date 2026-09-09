#!/usr/bin/env python3
"""
888a2a-lite Universal Agent Bridge (a2a_bridge.py)
Official Production-Ready Agent Daemon for 888a2a-lite Hub.

Features:
- Zero external dependencies (pure Python standard library).
- Multi-backend AI support (OpenClaw, Hermes, OpenAI/Ollama compatible API, custom cmd).
- Durable local enqueue before receipt ACK; independent inference and retry workers.
- Built-in Anti-Echo Storm Guard & [[A2A_NO_REPLY]] conversation termination.
- Auto-Registration & Credential Persistence (~/.a2a/credentials_<name>[_scope].json).
- Auto-Accept Group Invitations & Group broadcast spam suppression.
- Resilient SSE Outbound Streaming with auto-reconnect & keepalive handling.
- One-Click Service Installer for macOS (LaunchAgent) & Linux (systemd user unit).
- Complete PATH and unbuffered environment isolation.

Usage:
  # Quick start with OpenClaw:
  python3 a2a_bridge.py --name "MyAgent" --backend openclaw --backend-agent default

  # Quick start with Hermes:
  python3 a2a_bridge.py --name "MyAgent" --backend hermes

  # Install as auto-starting daemon:
  python3 a2a_bridge.py --name "MyAgent" --backend openclaw --install-service launchd
"""

import argparse
import contextlib
from datetime import datetime, timezone
import fcntl
import hashlib
import hmac
import http.server
import json
import math
import os
import plistlib
import queue
import re
import secrets
import shlex
import shutil
import socket
import sqlite3
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import webbrowser

MAX_LOCAL_REQUEST_BYTES = 1 << 20

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


def credential_scope(hub_url, shared_key):
    """Return a non-secret scope for a Hub and registration circle key."""
    normalized_key = (shared_key or "").strip() or "public"
    scope_input = f"{hub_url.rstrip('/')}\x00{normalized_key}"
    return hashlib.sha256(scope_input.encode("utf-8")).hexdigest()[:16]


def default_credential_path(hub_url, agent_name, shared_key=None, ui=False):
    """Choose a credential file that cannot silently cross Hub circles."""
    directory = os.path.expanduser("~/.a2a")
    if not (shared_key or "").strip():
        # Preserve the original public-mode paths for existing installations.
        filename = "user_credentials.json" if ui else f"credentials_{sanitize_slug(agent_name)}.json"
    else:
        prefix = "user_credentials" if ui else f"credentials_{sanitize_slug(agent_name)}"
        filename = f"{prefix}_{credential_scope(hub_url, shared_key)}.json"
    return os.path.join(directory, filename)


def default_chat_db_path(hub_url, agent_id):
    """Keep local conversation history isolated by the issued Agent identity."""
    scope = credential_scope(hub_url, agent_id)
    return os.path.expanduser(f"~/.a2a/chat_{scope}.db")


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
    def __init__(self, hub_url, agent_id=None, token=None, shared_key=None, circle_id=None):
        self.hub_url = hub_url.rstrip("/")
        self.agent_id = agent_id
        self.token = token
        self.shared_key = (shared_key or "").strip() or None
        self.circle_id = circle_id

    def _headers(self, auth=True):
        h = {"Content-Type": "application/json"}
        if auth and self.token:
            h["Authorization"] = f"Bearer {self.token}"
        if auth and self.agent_id:
            h["X-Agent-ID"] = self.agent_id
        # Once registration returned a persisted circle identity, the shared
        # key is bootstrap-only. Keep it for registration and for legacy
        # credentials that predate circleId support.
        if self.shared_key and (not auth or not self.circle_id):
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
                self.circle_id = ident.get("circleId")
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
            body = e.read().decode("utf-8", errors="replace")
            if e.code == 409 and "TASK_CANCELED" in body:
                return {"state": "CANCELED"}
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

    def submit_standard_update(self, task_id, update):
        """Persist an executor result against the originating A2A task."""
        url = f"{self.hub_url}/hub/v1/a2a/tasks/{urllib.parse.quote(task_id, safe='')}/updates"
        payload = json.dumps(update, ensure_ascii=False).encode("utf-8")
        headers = self._headers()
        headers["Content-Type"] = "application/a2a+json"
        headers.pop("X-Agent-ID", None)
        headers.pop("X-Hub-Key", None)
        req = urllib.request.Request(url, data=payload, headers=headers)
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                return json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            print(f"[!] Error updating standard task {task_id}: HTTP {e.code}", file=sys.stderr)
            return None
        except Exception as e:
            print(f"[!] Network error updating standard task {task_id}: {e}", file=sys.stderr)
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

    def _standard_headers(self, content_type="application/a2a+json"):
        headers = self._headers()
        headers["A2A-Extensions"] = "https://a2a.david888.com/extensions/groups/v1"
        headers["A2A-Version"] = "1.0"
        headers["Content-Type"] = content_type
        return headers

    def list_standard_groups(self, page_size=100):
        """List groups visible to this existing Hub Agent principal."""
        url = f"{self.hub_url}/a2a/v1/groups?pageSize={max(1, min(int(page_size), 100))}"
        req = urllib.request.Request(url, headers=self._standard_headers())
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read().decode("utf-8"))
            return data.get("groups", [])

    def get_standard_group_card(self, group_id):
        url = f"{self.hub_url}/a2a/v1/groups/{urllib.parse.quote(group_id, safe='')}/card"
        req = urllib.request.Request(url, headers=self._standard_headers())
        with urllib.request.urlopen(req, timeout=10) as resp:
            return json.loads(resp.read().decode("utf-8"))

    def get_group_roster(self, group_id):
        """Read the membership roster through the authenticated Hub principal."""
        url = f"{self.hub_url}/hub/v1/groups/{urllib.parse.quote(group_id, safe='')}/roster"
        req = urllib.request.Request(url, headers=self._headers())
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read().decode("utf-8"))
            return data.get("members", data.get("roster", []))

    def list_standard_group_tasks(self, group_id, page_size=100):
        tenant = urllib.parse.quote(f"group:{group_id}", safe=":")
        url = f"{self.hub_url}/a2a/v1/{tenant}/tasks?pageSize={max(1, min(int(page_size), 100))}"
        req = urllib.request.Request(url, headers=self._standard_headers())
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read().decode("utf-8"))
            return data.get("tasks", [])

    def send_standard_group_message(self, group_id, message, mentions=None, context_id=None, message_id=None, idempotency_key=None):
        """Send a Human message through the standard Group Gateway only."""
        mentions = list(mentions or [])
        policy = "MENTIONED_ONLY" if mentions else "ACK_ONLY"
        message_id = message_id or f"human-{uuid.uuid4()}"
        context_id = context_id or f"group-context-{uuid.uuid4()}"
        extension_metadata = {"replyPolicy": policy}
        if mentions:
            extension_metadata["mentions"] = mentions
        body = {
            "tenant": f"group:{group_id}",
            "message": {
                "messageId": message_id,
                "contextId": context_id,
                "role": "ROLE_USER",
                "parts": [{"text": message}],
                "metadata": {"https://a2a.david888.com/extensions/groups/v1": extension_metadata},
            },
            "configuration": {"returnImmediately": True},
        }
        headers = self._standard_headers()
        headers["X-Idempotency-Key"] = idempotency_key or f"a2a-ui:{message_id}"
        req = urllib.request.Request(
            f"{self.hub_url}/a2a/v1/message:send",
            data=json.dumps(body, ensure_ascii=False).encode("utf-8"),
            headers=headers,
        )
        with urllib.request.urlopen(req, timeout=15) as resp:
            return json.loads(resp.read().decode("utf-8"))

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
    ack_result = hub_client.ack_task(seq)
    if ack_result:
        queue.set_acked(seq)
        if isinstance(ack_result, dict) and ack_result.get("state") == "CANCELED":
            queue.finish(seq)
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
        ack_result = hub_client.ack_task(seq)
        if ack_result:
            queue.set_acked(seq)
            if isinstance(ack_result, dict) and ack_result.get("state") == "CANCELED":
                queue.finish(seq)
                return
        else:
            queue.retry(seq)
            return

    if item.get("protocol") == "A2A/1.0":
        process_standard_queued_task(hub_client, backend, queue, row, agent_name)
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


def process_standard_queued_task(hub_client, backend, queue, row, agent_name):
    """Report a standard task outcome without creating an unlinked reply task."""
    item = json.loads(row["item_json"])
    seq = item.get("sequence")
    task_id = item.get("taskId")
    turn_id = item.get("turnId")
    saved_reply = json.loads(row["reply_json"]) if row.get("reply_json") else None
    if saved_reply and saved_reply.get("standard_update"):
        update = saved_reply["standard_update"]
    else:
        update_id = f"update-{task_id}-{turn_id}-completed"
        expected_revision = int(item.get("taskRevision") or 1) + 1
        reply_policy = item.get("replyPolicy") or "ALL"
        mentions = item.get("mentions") or []
        mention_set = {m.strip().lower() for m in mentions if isinstance(m, str)}
        if reply_policy == "ACK_ONLY" or (reply_policy == "MENTIONED_ONLY" and hub_client.agent_id.strip().lower() not in mention_set):
            update = {"updateId": update_id, "turnId": turn_id, "expectedRevision": expected_revision, "state": "TASK_STATE_COMPLETED", "artifacts": []}
        elif is_pure_closing_statement(item.get("message", "")):
            update = {"updateId": update_id, "turnId": turn_id, "expectedRevision": expected_revision, "state": "TASK_STATE_COMPLETED", "artifacts": []}
        else:
            reply_text = backend.execute(item.get("message", ""), item.get("requesterAgentId", ""), {"contextId": item.get("contextId")})
            if not reply_text:
                if int(row.get("attempts", 0)) >= 3:
                    update = {"updateId": f"update-{task_id}-{turn_id}-failed", "turnId": turn_id, "expectedRevision": expected_revision, "state": "TASK_STATE_FAILED", "artifacts": []}
                else:
                    queue.retry(seq)
                    return
            elif "[[A2A_NO_REPLY]]" in reply_text:
                update = {"updateId": update_id, "turnId": turn_id, "expectedRevision": expected_revision, "state": "TASK_STATE_COMPLETED", "artifacts": []}
            elif "[[A2A_REJECTED]]" in reply_text:
                update = {"updateId": f"update-{task_id}-{turn_id}-rejected", "turnId": turn_id, "expectedRevision": expected_revision, "state": "TASK_STATE_REJECTED", "artifacts": []}
            elif "[[A2A_INPUT_REQUIRED]]" in reply_text:
                update = {"updateId": f"update-{task_id}-{turn_id}-input", "turnId": turn_id, "expectedRevision": expected_revision, "state": "TASK_STATE_INPUT_REQUIRED", "artifacts": []}
            elif "[[A2A_AUTH_REQUIRED]]" in reply_text:
                update = {"updateId": f"update-{task_id}-{turn_id}-auth", "turnId": turn_id, "expectedRevision": expected_revision, "state": "TASK_STATE_AUTH_REQUIRED", "artifacts": []}
            else:
                update = {"updateId": update_id, "turnId": turn_id, "expectedRevision": expected_revision, "state": "TASK_STATE_COMPLETED", "message": {"messageId": f"reply-{task_id}-{turn_id}", "contextId": item.get("contextId", ""), "taskId": task_id, "role": "ROLE_AGENT", "parts": [{"text": reply_text.strip()}]}, "artifacts": []}
        queue.save_reply(seq, {"standard_update": update})

    if hub_client.submit_standard_update(task_id, update):
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
# Client Web UI (a2a ui: Local User Chat Web Console)
# ---------------------------------------------------------------------------

CLIENT_HTML = """<!DOCTYPE html>
<html lang="zh-TW">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>888a2a Client Workstation</title>
  <style>
    :root {
      --bg: #f8fafc;
      --panel: #ffffff;
      --ink: #0f172a;
      --muted: #64748b;
      --line: #e2e8f0;
      --accent: #2563eb;
      --accent-light: #eff6ff;
      --online: #10b981;
      --offline: #94a3b8;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif; background: var(--bg); color: var(--ink); height: 100vh; display: flex; flex-direction: column; overflow: hidden; }
    header { height: 56px; background: #0f172a; color: #fff; display: flex; align-items: center; justify-content: space-between; padding: 0 20px; flex-shrink: 0; }
    .brand { display: flex; align-items: center; gap: 10px; font-weight: 700; font-size: 16px; }
    .brand-badge { background: var(--accent); color: #fff; padding: 3px 8px; border-radius: 6px; font-size: 11px; letter-spacing: 0.5px; text-transform: uppercase; }
    .nav-status { display: flex; align-items: center; gap: 16px; font-size: 13px; }
    .hub-badge { color: #94a3b8; font-size: 12px; }
    .hub-badge a { color: #38bdf8; text-decoration: none; }
    .user-chip { background: #1e293b; padding: 4px 12px; border-radius: 20px; display: flex; align-items: center; gap: 8px; border: 1px solid #334155; }
    .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--online); box-shadow: 0 0 6px var(--online); }
    .workspace { display: flex; flex: 1 1 auto; height: calc(100vh - 56px); overflow: hidden; }
    aside { width: 320px; background: var(--panel); border-right: 1px solid var(--line); display: flex; flex-direction: column; flex-shrink: 0; }
    .sidebar-header { padding: 14px 16px; border-bottom: 1px solid var(--line); display: flex; justify-content: space-between; align-items: center; }
    .sidebar-header h2 { font-size: 14px; font-weight: 700; color: var(--ink); }
    .refresh-btn { background: none; border: 1px solid var(--line); border-radius: 6px; padding: 4px 8px; cursor: pointer; font-size: 12px; color: var(--muted); transition: all .15s; }
    .refresh-btn:hover { background: var(--accent-light); color: var(--accent); border-color: var(--accent); }
    .search-bar { padding: 10px 16px; border-bottom: 1px solid var(--line); }
    .search-bar input { width: 100%; padding: 8px 12px; border: 1px solid var(--line); border-radius: 8px; font-size: 13px; outline: none; transition: border-color .15s; }
    .search-bar input:focus { border-color: var(--accent); }
    .peer-list { flex: 1 1 auto; overflow-y: auto; padding: 8px; display: flex; flex-direction: column; gap: 6px; }
    .groups-panel { border-top: 1px solid var(--line); padding: 10px 14px; background: #fff; max-height: 190px; overflow-y: auto; }
    .groups-panel-header { display:flex; align-items:center; justify-content:space-between; margin-bottom:6px; }
    .groups-panel h3 { font-size: 12px; color: var(--ink); }
    .group-card { padding: 7px 8px; margin-top: 5px; border: 1px solid var(--line); border-radius: 7px; cursor: pointer; background: #fff; }
    .group-card:hover, .group-card.active { border-color: var(--accent); background: var(--accent-light); }
    .group-card-name { font-size: 12px; font-weight: 700; }
    .group-card-meta { font-size: 10px; color: var(--muted); margin-top: 3px; }
    .mention-list { position: absolute; left: 20px; right: 110px; bottom: 75px; max-height: 150px; overflow-y: auto; background: #fff; border: 1px solid var(--line); border-radius: 8px; box-shadow: 0 8px 20px rgba(15,23,42,.12); z-index: 3; }
    .mention-option { display:block; width:100%; text-align:left; border:0; background:#fff; padding:8px 10px; cursor:pointer; font-size:12px; }
    .mention-option:hover { background: var(--accent-light); }
    .runtime-panel { border-top: 1px solid var(--line); padding: 12px 14px; background: #f8fafc; max-height: 230px; overflow-y: auto; }
    .runtime-panel-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
    .runtime-panel h3 { font-size: 12px; color: var(--ink); }
    .runtime-refresh { border: 1px solid var(--line); background: #fff; border-radius: 6px; padding: 3px 7px; cursor: pointer; color: var(--muted); font-size: 11px; }
    .runtime-card { display: grid; grid-template-columns: 1fr auto; gap: 2px 8px; padding: 7px 8px; margin-top: 5px; border: 1px solid var(--line); border-radius: 7px; background: #fff; }
    .runtime-name { font-size: 12px; font-weight: 700; }
    .runtime-version { font-size: 10px; color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .runtime-state { grid-row: 1 / span 2; align-self: center; font-size: 10px; font-weight: 700; padding: 3px 6px; border-radius: 10px; }
    .runtime-state.ready { color: #166534; background: #dcfce7; }
    .runtime-state.cli_needed { color: #92400e; background: #fef3c7; }
    .runtime-state.unavailable { color: #991b1b; background: #fee2e2; }
    .peer-card { padding: 10px 12px; border: 1px solid var(--line); border-radius: 8px; cursor: pointer; background: #fff; transition: all .15s; }
    .peer-card:hover { border-color: var(--accent); background: var(--accent-light); }
    .peer-card.active { border-color: var(--accent); background: var(--accent-light); box-shadow: 0 1px 3px rgba(37,99,235,.15); }
    .peer-card-top { display: flex; justify-content: space-between; align-items: center; }
    .peer-name { font-weight: 700; font-size: 14px; }
    .status-pill { font-size: 11px; font-weight: 600; padding: 2px 6px; border-radius: 10px; }
    .status-pill.ONLINE { background: #dcfce7; color: #166534; }
    .status-pill.OFFLINE { background: #f1f5f9; color: #64748b; }
    .peer-id { font-family: ui-monospace, monospace; font-size: 11px; color: var(--muted); margin-top: 4px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    main { flex: 1 1 auto; display: flex; flex-direction: column; background: #f8fafc; overflow: hidden; }
    .chat-header { height: 56px; background: var(--panel); border-bottom: 1px solid var(--line); display: flex; align-items: center; justify-content: space-between; padding: 0 20px; flex-shrink: 0; }
    .chat-target-info { display: flex; align-items: center; gap: 12px; }
    .chat-target-name { font-size: 15px; font-weight: 700; }
    .chat-target-meta { font-size: 12px; color: var(--muted); font-family: ui-monospace, monospace; }
    .chat-stream { flex: 1 1 auto; overflow-y: auto; padding: 20px; display: flex; flex-direction: column; gap: 14px; }
    .chat-empty { margin: auto; text-align: center; color: var(--muted); max-width: 380px; line-height: 1.6; }
    .chat-empty-icon { font-size: 40px; margin-bottom: 12px; }
    .bubble { max-width: 75%; padding: 12px 16px; border-radius: 12px; font-size: 14px; line-height: 1.6; word-break: break-word; white-space: pre-wrap; }
    .bubble.user { align-self: flex-end; background: var(--accent); color: #fff; border-bottom-right-radius: 2px; }
    .bubble.agent { align-self: flex-start; background: #fff; border: 1px solid var(--line); color: var(--ink); border-bottom-left-radius: 2px; box-shadow: 0 1px 3px rgba(0,0,0,.04); }
    .bubble-meta { font-size: 11px; opacity: 0.75; margin-top: 6px; display: flex; justify-content: space-between; gap: 12px; }
    .quick-prompts { padding: 8px 20px; display: flex; gap: 8px; overflow-x: auto; flex-shrink: 0; background: #f1f5f9; border-top: 1px solid var(--line); }
    .quick-btn { background: #fff; border: 1px solid var(--line); border-radius: 16px; padding: 5px 12px; font-size: 12px; color: var(--ink); cursor: pointer; white-space: nowrap; transition: all .15s; }
    .quick-btn:hover { background: var(--accent-light); border-color: var(--accent); color: var(--accent); }
    .chat-input-area { padding: 14px 20px; background: var(--panel); border-top: 1px solid var(--line); display: flex; gap: 10px; flex-shrink: 0; }
    .chat-input-area textarea { flex: 1 1 auto; height: 50px; padding: 10px 14px; border: 1px solid var(--line); border-radius: 10px; resize: none; font-family: inherit; font-size: 14px; outline: none; transition: border-color .15s; }
    .chat-input-area textarea:focus { border-color: var(--accent); }
    .send-btn { background: var(--accent); color: #fff; border: none; border-radius: 10px; padding: 0 24px; font-size: 14px; font-weight: 700; cursor: pointer; transition: opacity .15s; }
    .send-btn:hover { opacity: 0.9; }
    .send-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  </style>
</head>
<body>
  <header>
    <div class="brand">
      <span>888a2a</span>
      <span class="brand-badge">Client Workstation</span>
    </div>
    <div class="nav-status">
      <div class="hub-badge">Hub: <a id="hub-link" href="#" target="_blank">-</a></div>
      <div class="user-chip">
        <span class="dot"></span>
        <span id="my-name">連線中...</span>
      </div>
    </div>
  </header>

  <div class="workspace">
    <aside>
      <div class="sidebar-header">
        <h2>在線 Agent 通訊錄</h2>
        <button id="refresh-peers" class="refresh-btn" type="button">↻ 重新整理</button>
      </div>
      <div class="search-bar">
        <input id="peer-search" type="text" placeholder="搜尋 Agent 名稱或 ID...">
      </div>
      <div id="peer-list" class="peer-list">
        <div style="padding:20px;text-align:center;color:var(--muted);font-size:13px">正在獲取 Agent 名單...</div>
      </div>
      <section class="groups-panel" aria-labelledby="groups-title">
        <div class="groups-panel-header">
          <h3 id="groups-title">Groups 人機群組</h3>
          <button id="refresh-groups" class="runtime-refresh" type="button">↻</button>
        </div>
        <div id="group-list" aria-live="polite"><div style="font-size:11px;color:var(--muted)">正在取得同圈群組...</div></div>
      </section>
      <section class="runtime-panel" aria-labelledby="runtime-title">
        <div class="runtime-panel-header">
          <h3 id="runtime-title">本機 Agent Runtimes</h3>
          <button id="refresh-runtimes" class="runtime-refresh" type="button">↻</button>
        </div>
        <div id="runtime-list" aria-live="polite"><div style="font-size:11px;color:var(--muted)">正在檢測本機 CLI...</div></div>
      </section>
    </aside>

    <main>
      <div class="chat-header">
        <div class="chat-target-info">
          <span id="target-name" class="chat-target-name">未選擇 Agent</span>
          <span id="target-badge" class="status-pill OFFLINE" style="display:none">OFFLINE</span>
          <span id="target-id" class="chat-target-meta"></span>
        </div>
        <div id="target-caps" style="font-size:12px;color:var(--muted)"></div>
      </div>

      <div id="chat-stream" class="chat-stream">
        <div class="chat-empty">
          <div class="chat-empty-icon">💬</div>
          <h3>歡迎使用 888a2a Client</h3>
          <p style="margin-top:6px">請從左側通訊錄點選一位在線的 Agent，即可在此發送任務指令並進行即時雙向交談！</p>
        </div>
      </div>

      <div class="quick-prompts">
        <button class="quick-btn" data-text="你好！請自我介紹一下你的專長與支援的能力">👋 自我介紹</button>
        <button class="quick-btn" data-text="請回報你目前的系統狀態與工作隊列">⚡️ 狀態回報</button>
        <button class="quick-btn" data-text="請簡要說明多 Agent 協作的最佳實踐是什麼？">💡 多 Agent 協作</button>
      </div>

      <form id="chat-form" class="chat-input-area">
        <textarea id="chat-input" placeholder="輸入訊息或任務指令... (Enter 發送，Shift+Enter 換行)" disabled></textarea>
        <button id="chat-send" class="send-btn" type="submit" disabled>發送任務</button>
      </form>
      <div id="mention-list" class="mention-list" hidden></div>
    </main>
  </div>

  <script>
    const $ = (id) => document.getElementById(id);
    const LOCAL_UI_TOKEN = "{{LOCAL_UI_TOKEN}}";
    let myInfo = null;
    let peers = [];
    let conversations = [];
    let activePeer = null;
    let groups = [];
    let activeGroup = null;
    let groupHistory = {};
    let groupMentions = [];
    let conversationHistory = {};

    function escapeHtml(str) {
      return String(str || "").replace(/[&<>'\"]/g, tag => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '\"': "&quot;" }[tag]));
    }

    function formatTime(iso) {
      if (!iso) return "";
      try {
        const d = new Date(iso);
        return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
      } catch {
        return "";
      }
    }

    async function loadMe() {
      try {
        const res = await fetch("/api/me");
        if (res.ok) {
          myInfo = await res.json();
          $("my-name").textContent = `${myInfo.displayName} (${myInfo.agentId})`;
          $("hub-link").textContent = myInfo.hubUrl;
          $("hub-link").href = myInfo.hubUrl;
        }
      } catch (err) {
        console.error("Failed to load user info", err);
      }
    }

    async function loadConversations() {
      try {
        const res = await fetch("/api/conversations");
        if (res.ok) {
          const data = await res.json();
          conversations = data.conversations || [];
        }
      } catch (err) {
        console.error("Failed to load conversations", err);
      }
    }

    async function loadGroups() {
      try {
        const res = await fetch("/api/groups", { cache: "no-store" });
        if (res.ok) {
          const data = await res.json();
          groups = data.groups || [];
          renderGroups();
        }
      } catch (err) {
        console.error("Failed to load groups", err);
      }
    }

    function renderGroups() {
      const list = $("group-list");
      if (!groups.length) {
        list.innerHTML = '<div style="font-size:11px;color:var(--muted);padding:4px 0">尚無可用的同圈群組</div>';
        return;
      }
      list.innerHTML = groups.map(group => `
        <div class="group-card ${activeGroup?.groupId === group.groupId ? 'active' : ''}" data-group-id="${escapeHtml(group.groupId)}">
          <div class="group-card-name">▦ ${escapeHtml(group.name || group.groupId)}</div>
          <div class="group-card-meta">${Number(group.memberCount || (group.roster || []).length)} 位成員 · standard Gateway</div>
        </div>
      `).join("");
      list.querySelectorAll(".group-card").forEach(el => el.addEventListener("click", () => {
        const group = groups.find(item => item.groupId === el.dataset.groupId);
        if (group) selectGroup(group);
      }));
    }

    async function selectGroup(group) {
      activeGroup = group;
      activePeer = null;
      groupMentions = [];
      $("target-name").textContent = `▦ ${group.name || group.groupId}`;
      $("target-id").textContent = `(group:${group.groupId})`;
      $("target-badge").textContent = "GROUP";
      $("target-badge").className = "status-pill ONLINE";
      $("target-badge").style.display = "inline-block";
      $("target-caps").textContent = "standard Group Gateway · mentions 支援";
      $("chat-input").disabled = false;
      $("chat-send").disabled = false;
      $("chat-send").textContent = "發送群組訊息";
      renderGroups();
      await loadGroupHistory(group.groupId);
      $("chat-input").focus();
    }

    async function loadGroupHistory(groupId) {
      try {
        const res = await fetch(`/api/groups/${encodeURIComponent(groupId)}/messages`, { cache: "no-store" });
        if (res.ok) {
          const data = await res.json();
          groupHistory[groupId] = data.messages || [];
        }
      } catch (err) {
        console.error("Failed to load group history", err);
      }
      renderGroupChat();
    }

    function renderGroupChat() {
      if (!activeGroup) return;
      const history = groupHistory[activeGroup.groupId] || [];
      const stream = $("chat-stream");
      if (!history.length) {
        stream.innerHTML = `<div class="chat-empty"><div class="chat-empty-icon">▦</div><h3>${escapeHtml(activeGroup.name || activeGroup.groupId)}</h3><p style="margin-top:6px">群組尚無本機同步紀錄。輸入訊息即可透過 standard Gateway 發送。</p></div>`;
        return;
      }
      stream.innerHTML = history.map(item => `
        <div class="bubble ${item.senderType === 'HUMAN' ? 'user' : 'agent'}">
          <div style="font-weight:700;font-size:12px;margin-bottom:4px">${escapeHtml(item.senderName || item.senderId || 'Agent')}</div>
          <div>${escapeHtml(item.message)}</div>
          <div class="bubble-meta"><span>${formatTime(item.timestamp)}</span><span>${escapeHtml(item.state || '')} · ${escapeHtml(item.replyPolicy || '')}</span></div>
        </div>
      `).join("");
      stream.scrollTop = stream.scrollHeight;
    }

    function renderMentionSuggestions() {
      const list = $("mention-list");
      if (!activeGroup) { list.hidden = true; return; }
      const value = $("chat-input").value;
      const match = value.match(/(?:^|\s)@([A-Za-z0-9_-]{0,64})$/);
      if (!match) { list.hidden = true; return; }
      const query = match[1].toLowerCase();
      const roster = (activeGroup.roster || []).filter(member => member.state === "ACTIVE" || !member.state);
      const candidates = roster.filter(member => {
        const agent = member.agent || {};
        return String(member.agentId || '').toLowerCase().includes(query) || String(agent.displayName || '').toLowerCase().includes(query);
      }).slice(0, 8);
      if (!candidates.length) { list.hidden = true; return; }
      list.innerHTML = candidates.map(member => {
        const agent = member.agent || {};
        const name = agent.displayName || member.agentId;
        const suffix = name === member.agentId ? '' : ` · ${String(member.agentId).slice(-8)}`;
        return `<button type="button" class="mention-option" data-agent-id="${escapeHtml(member.agentId)}" data-display-name="${escapeHtml(name)}">@${escapeHtml(name)}${escapeHtml(suffix)}</button>`;
      }).join("");
      list.hidden = false;
      list.querySelectorAll(".mention-option").forEach(button => button.addEventListener("click", () => {
        const current = $("chat-input").value;
        $("chat-input").value = current.replace(/(?:^|\s)@[A-Za-z0-9_-]{0,64}$/, matchText => `${matchText.slice(0, 1)}@${button.dataset.displayName} `);
        groupMentions = [...new Set([...groupMentions, button.dataset.agentId])].slice(0, 16);
        list.hidden = true;
        $("chat-input").focus();
      }));
    }

    async function loadPeers() {
      try {
        const res = await fetch("/api/peers");
        if (!res.ok) return;
        const data = await res.json();
        peers = (data.agents || []).filter(a => a.agentId !== myInfo?.agentId);
        await loadConversations();
        renderPeers();
      } catch (err) {
        console.error("Failed to load peers", err);
      }
    }

    async function loadRuntimes() {
      try {
        const res = await fetch("/api/runtimes", { cache: "no-store" });
        if (!res.ok) throw new Error("runtime API unavailable");
        const data = await res.json();
        const runtimes = Array.isArray(data.runtimes) ? data.runtimes : [];
        $("runtime-list").innerHTML = runtimes.map(runtime => {
          const state = ["ready", "cli_needed", "unavailable"].includes(runtime.status) ? runtime.status : "unavailable";
          const label = state === "ready" ? "Ready" : state === "cli_needed" ? "CLI needed" : "Unavailable";
          const marker = runtime.active ? " · active" : runtime.desired ? " · desired" : "";
          return `<div class="runtime-card"><div class="runtime-name">${escapeHtml(runtime.name)}${marker}</div><span class="runtime-state ${state}">${label}</span><div class="runtime-version">${escapeHtml(runtime.version || runtime.executable || "未找到可執行檔")}</div></div>`;
        }).join("") || `<div style="font-size:11px;color:var(--muted)">沒有可用 Runtime</div>`;
      } catch (err) {
        $("runtime-list").innerHTML = `<div style="font-size:11px;color:#991b1b">無法取得 Runtime 狀態</div>`;
      }
    }

    function renderPeers() {
      const q = $("peer-search").value.toLowerCase().trim();
      const filtered = peers.filter(p => 
        (p.displayName || "").toLowerCase().includes(q) || 
        (p.agentId || "").toLowerCase().includes(q)
      );

      const list = $("peer-list");
      if (filtered.length === 0) {
        list.innerHTML = `<div style="padding:20px;text-align:center;color:var(--muted);font-size:13px">無相符的 Agent</div>`;
        return;
      }

      list.innerHTML = filtered.map(p => {
        const conv = conversations.find(c => c.peerId === p.agentId);
        const lastMsgPreview = conv?.lastMessage ? 
          `<div style="font-size:11px;color:var(--muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;margin-top:3px">${escapeHtml(conv.lastMessage)}</div>` : '';
        return `
        <div class="peer-card ${activePeer?.agentId === p.agentId ? 'active' : ''}" data-id="${escapeHtml(p.agentId)}">
          <div class="peer-card-top">
            <span class="peer-name">${escapeHtml(p.displayName || p.agentId)}</span>
            <span class="status-pill ${escapeHtml(p.state)}">${escapeHtml(p.state)}</span>
          </div>
          <div class="peer-id">${escapeHtml(p.agentId)}</div>
          ${lastMsgPreview}
        </div>
      `;
      }).join("");

      list.querySelectorAll(".peer-card").forEach(el => {
        el.addEventListener("click", async () => {
          const id = el.getAttribute("data-id");
          const target = peers.find(p => p.agentId === id);
          if (target) await selectPeer(target);
        });
      });
    }

    async function selectPeer(peer) {
      activePeer = peer;
      activeGroup = null;
      groupMentions = [];
      $("target-name").textContent = peer.displayName || peer.agentId;
      $("target-id").textContent = `(${peer.agentId})`;
      $("target-badge").textContent = peer.state;
      $("target-badge").className = `status-pill ${peer.state}`;
      $("target-badge").style.display = "inline-block";
      $("target-caps").textContent = (peer.capabilities || []).join(", ");
      $("chat-input").disabled = false;
      $("chat-send").disabled = false;
      $("chat-send").textContent = "發送任務";
      renderPeers();

      // Hydrate conversation history from SQLite WAL via /api/history
      await loadHistory(peer.agentId);
      $("chat-input").focus();
    }

    async function loadHistory(peerId) {
      try {
        const res = await fetch(`/api/history?peer=${encodeURIComponent(peerId)}`);
        if (res.ok) {
          const data = await res.json();
          conversationHistory[peerId] = data.messages || [];
        }
      } catch (err) {
        console.error("Failed to load history for " + peerId, err);
      }
      renderChat();
    }

    function renderChat() {
      if (!activePeer) return;
      const history = conversationHistory[activePeer.agentId] || [];
      const stream = $("chat-stream");

      if (history.length === 0) {
        stream.innerHTML = `
          <div class="chat-empty">
            <div class="chat-empty-icon">✨</div>
            <h3>與 ${escapeHtml(activePeer.displayName || activePeer.agentId)} 的對話</h3>
            <p style="margin-top:6px">尚未有任何通訊紀錄。在下方輸入任務訊息開始互動。</p>
          </div>
        `;
        return;
      }

      stream.innerHTML = history.map((m, index) => {
        let statusBadge = '';
        if (m.isOutgoing) {
          if (m.state === 'FAILED') {
            statusBadge = `<span style="color:#ef4444">✕ 發送失敗</span> <button class="retry-btn" data-retry-index="${index}" style="background:#fee2e2;color:#991b1b;border:none;border-radius:4px;padding:2px 8px;cursor:pointer;font-size:11px;font-weight:600">↻ 重試</button>`;
          } else if (m.state === 'PENDING' || m.state === 'SENDING') {
            statusBadge = '<span style="color:#f59e0b">⏳ 傳送中</span>';
          } else {
            statusBadge = '<span>✓ 已送達</span>';
          }
        } else {
          statusBadge = '<span>✓ 簽收</span>';
        }
        return `
        <div class="bubble ${m.isOutgoing ? 'user' : 'agent'}">
          <div style="font-weight:700;font-size:12px;margin-bottom:4px">
            ${m.isOutgoing ? '我 (You)' : escapeHtml(activePeer.displayName || m.senderName || 'Agent')}
          </div>
          <div>${escapeHtml(m.message)}</div>
          <div class="bubble-meta">
            <span>${formatTime(m.timestamp)}</span>
            ${statusBadge}
          </div>
        </div>
      `;
      }).join("");

      stream.querySelectorAll(".retry-btn").forEach(button => {
        const index = Number(button.dataset.retryIndex);
        const message = history[index];
        if (message) button.addEventListener("click", () => retryMessage(message.id));
      });

      stream.scrollTop = stream.scrollHeight;
    }

    window.retryMessage = async function(msgId) {
      if (!activePeer) return;
      const history = conversationHistory[activePeer.agentId] || [];
      const m = history.find(item => item.id === msgId);
      if (!m) return;
      m.state = "PENDING";
      renderChat();
      try {
        const res = await fetch("/api/send", {
          method: "POST",
          headers: { "Content-Type": "application/json", "X-Local-UI-Token": LOCAL_UI_TOKEN },
          body: JSON.stringify({
            targetAgentId: activePeer.agentId,
            message: m.message,
            taskId: m.id
          })
        });
        const data = await res.json().catch(() => ({}));
        if (!res.ok || !data.ok) {
          m.state = "FAILED";
          renderChat();
          alert(data.error || "重試發送失敗，請確認 Hub 連線狀態。");
        } else {
          m.state = "SENT";
          if (data.taskId) m.id = data.taskId;
          renderChat();
          loadConversations().then(renderPeers);
        }
      } catch (err) {
        m.state = "FAILED";
        renderChat();
        alert("重試發生例外錯誤：" + err.message);
      }
    };

    async function sendMessage(text) {
      if (!activePeer || !text.trim()) return;
      const msgText = text.trim();
      $("chat-input").value = "";
      $("chat-send").disabled = true;

      const tempId = "out-" + Date.now() + "-" + Math.random().toString(36).substring(2, 8);
      const userMsg = {
        id: tempId,
        peerId: activePeer.agentId,
        senderName: myInfo?.displayName || "User",
        message: msgText,
        timestamp: new Date().toISOString(),
        isOutgoing: true,
        state: "PENDING"
      };

      if (!conversationHistory[activePeer.agentId]) conversationHistory[activePeer.agentId] = [];
      conversationHistory[activePeer.agentId].push(userMsg);
      renderChat();

      try {
        const res = await fetch("/api/send", {
          method: "POST",
          headers: { "Content-Type": "application/json", "X-Local-UI-Token": LOCAL_UI_TOKEN },
          body: JSON.stringify({
            targetAgentId: activePeer.agentId,
            message: msgText,
            taskId: tempId
          })
        });
        const data = await res.json().catch(() => ({}));
        if (!res.ok || !data.ok) {
          userMsg.state = "FAILED";
          renderChat();
          alert(data.error || "發送失敗，請確認 Hub 連線狀態。");
        } else {
          userMsg.state = "SENT";
          if (data.taskId) userMsg.id = data.taskId;
          renderChat();
          loadConversations().then(renderPeers);
        }
      } catch (err) {
        userMsg.state = "FAILED";
        renderChat();
        alert("發送發生例外錯誤：" + err.message);
      } finally {
        $("chat-send").disabled = false;
        $("chat-input").focus();
      }
    }

    async function sendGroupMessage(text) {
      if (!activeGroup || !text.trim()) return;
      const group = activeGroup;
      const message = text.trim();
      const mentions = groupMentions.slice(0, 16);
      groupMentions = [];
      $("chat-input").value = "";
      $("chat-send").disabled = true;
      const tempId = "human-" + Date.now() + "-" + Math.random().toString(36).substring(2, 8);
      const pending = { id: tempId, groupId: group.groupId, senderId: myInfo?.agentId || "", senderName: myInfo?.displayName || "Human", senderType: "HUMAN", message, timestamp: new Date().toISOString(), state: "PENDING", replyPolicy: mentions.length ? "MENTIONED_ONLY" : "ACK_ONLY", mentions };
      if (!groupHistory[group.groupId]) groupHistory[group.groupId] = [];
      groupHistory[group.groupId].push(pending);
      renderGroupChat();
      try {
        const res = await fetch(`/api/groups/${encodeURIComponent(group.groupId)}/messages`, {
          method: "POST",
          headers: { "Content-Type": "application/json", "X-Local-UI-Token": LOCAL_UI_TOKEN },
          body: JSON.stringify({ message, mentions, messageId: tempId })
        });
        const data = await res.json().catch(() => ({}));
        if (!res.ok || !data.ok) {
          pending.state = "FAILED";
          renderGroupChat();
          alert(data.error || "群組訊息發送失敗");
        } else {
          await loadGroupHistory(group.groupId);
        }
      } catch (err) {
        pending.state = "FAILED";
        renderGroupChat();
        alert("群組訊息發送發生例外錯誤：" + err.message);
      } finally {
        $("chat-send").disabled = false;
        $("chat-input").focus();
      }
    }

    $("chat-form").addEventListener("submit", (e) => {
      e.preventDefault();
      if (activeGroup) sendGroupMessage($("chat-input").value);
      else sendMessage($("chat-input").value);
    });

    $("chat-input").addEventListener("keydown", (e) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        if (activeGroup) sendGroupMessage($("chat-input").value);
        else sendMessage($("chat-input").value);
      }
    });

    $("chat-input").addEventListener("input", renderMentionSuggestions);

    $("refresh-peers").addEventListener("click", loadPeers);
    $("peer-search").addEventListener("input", renderPeers);

    document.querySelectorAll(".quick-btn").forEach(btn => {
      btn.addEventListener("click", () => {
        if (!activePeer && !activeGroup) {
          alert("請先從左側選擇一位對話 Agent 或 Group！");
          return;
        }
        if (activeGroup) sendGroupMessage(btn.getAttribute("data-text"));
        else sendMessage(btn.getAttribute("data-text"));
      });
    });

    function connectEvents() {
      const ev = new EventSource("/api/events");
      ev.onmessage = (e) => {
        try {
          const evt = JSON.parse(e.data);
          if (evt.type === "group_message" && evt.groupId) {
            if (!groupHistory[evt.groupId]) groupHistory[evt.groupId] = [];
            const idx = groupHistory[evt.groupId].findIndex(item => item.id === evt.id);
            if (idx >= 0) groupHistory[evt.groupId][idx] = { ...groupHistory[evt.groupId][idx], ...evt };
            else if (evt.message) groupHistory[evt.groupId].push(evt);
            if (activeGroup?.groupId === evt.groupId) renderGroupChat();
            return;
          }
          const peer = evt.peerId;
          if (!conversationHistory[peer]) conversationHistory[peer] = [];
          const idx = conversationHistory[peer].findIndex(m => m.id === evt.id || (evt.previousId && m.id === evt.previousId));
          if (idx >= 0) {
            conversationHistory[peer][idx] = { ...conversationHistory[peer][idx], ...evt, id: evt.id };
          } else if (evt.message) {
            conversationHistory[peer].push(evt);
          }
          if (activePeer && activePeer.agentId === peer) {
            renderChat();
          }
          loadConversations().then(renderPeers);
        } catch (err) {
          console.error("SSE parse error", err);
        }
      };
    }

    async function init() {
      await loadMe();
      await loadConversations();
      await loadPeers();
      await loadGroups();
      await loadRuntimes();
      connectEvents();
      setInterval(loadPeers, 4000);
      setInterval(loadRuntimes, 10000);
      const defaultPeer = peers.find(p => p.agentId === conversations[0]?.peerId) || 
                          peers.find(p => p.state === "ONLINE") || 
                          peers[0];
      if (defaultPeer) await selectPeer(defaultPeer);
    }

    $("refresh-runtimes").addEventListener("click", loadRuntimes);
    $("refresh-groups").addEventListener("click", loadGroups);

    init();
  </script>
</body>
</html>"""


def message_timestamp_order(value):
    """Return a comparable UTC timestamp for conversation ordering."""
    try:
        parsed = datetime.fromisoformat(str(value).replace("Z", "+00:00"))
        if parsed.tzinfo is None:
            parsed = parsed.replace(tzinfo=timezone.utc)
        return parsed.astimezone(timezone.utc).timestamp()
    except (TypeError, ValueError, OverflowError):
        return 0.0


def a2a_message_text(message):
    if not isinstance(message, dict):
        return ""
    texts = []
    for part in message.get("parts", []):
        if isinstance(part, dict) and isinstance(part.get("text"), str):
            texts.append(part["text"])
    return "\n".join(texts).strip()


def standard_task_group_messages(group_id, task, human_agent_id, human_name):
    """Project a standard parent Task into safe, deduplicated local timeline rows."""
    if not isinstance(task, dict) or not task.get("id"):
        return []
    task_id = str(task["id"])
    status = task.get("status") if isinstance(task.get("status"), dict) else {}
    state = status.get("state", "TASK_STATE_SUBMITTED")
    timestamp = status.get("timestamp") or datetime.now(timezone.utc).isoformat()
    history = task.get("history") if isinstance(task.get("history"), list) else []
    first = history[0] if history and isinstance(history[0], dict) else task.get("message", {})
    metadata = first.get("metadata", {}) if isinstance(first, dict) and isinstance(first.get("metadata"), dict) else {}
    extension = metadata.get("https://a2a.david888.com/extensions/groups/v1", {})
    extension = extension if isinstance(extension, dict) else {}
    mentions = extension.get("mentions", [])
    mentions = mentions if isinstance(mentions, list) else []
    result = [{
        "id": (first.get("messageId") if isinstance(first, dict) and first.get("messageId") else f"{task_id}:human"),
        "parentTaskId": task_id,
        "senderId": human_agent_id,
        "senderName": human_name,
        "senderType": "HUMAN",
        "message": a2a_message_text(first),
        "timestamp": first.get("timestamp", timestamp) if isinstance(first, dict) else timestamp,
        "state": state,
        "replyPolicy": extension.get("replyPolicy", "ACK_ONLY"),
        "mentions": mentions,
        "revision": 1,
    }]
    status_message = status.get("message")
    result_text = a2a_message_text(status_message)
    if result_text:
        result.append({
            "id": f"{task_id}:result",
            "parentTaskId": task_id,
            "senderId": "",
            "senderName": "Agent result",
            "senderType": "AGENT",
            "message": result_text,
            "timestamp": timestamp,
            "state": state,
            "replyPolicy": result[0]["replyPolicy"],
            "mentions": result[0]["mentions"],
            "revision": 2,
        })
    return [item for item in result if item["message"]]


class LocalChatStore:
    """Crash-safe local SQLite store for human conversation history in a2a ui."""
    def __init__(self, db_path):
        self.path = db_path
        os.makedirs(os.path.dirname(self.path), exist_ok=True)
        self.lock = threading.Lock()
        with self._connect() as db:
            db.execute("PRAGMA journal_mode=WAL")
            db.execute("""
                CREATE TABLE IF NOT EXISTS conversations (
                    peer_id TEXT PRIMARY KEY,
                    display_name TEXT,
                    last_message TEXT,
                    last_timestamp TEXT,
                    last_order REAL NOT NULL DEFAULT 0,
                    updated_at REAL NOT NULL
                )
            """)
            db.execute("""
                CREATE TABLE IF NOT EXISTS messages (
                    id TEXT PRIMARY KEY,
                    peer_id TEXT NOT NULL,
                    sender_id TEXT NOT NULL,
                    sender_name TEXT,
                    message TEXT NOT NULL,
                    timestamp TEXT NOT NULL,
                    is_outgoing INTEGER NOT NULL,
                    sequence INTEGER,
                    state TEXT NOT NULL DEFAULT 'SENT',
                    sort_timestamp REAL NOT NULL DEFAULT 0,
                    attempts INTEGER NOT NULL DEFAULT 0,
                    next_retry_at REAL NOT NULL DEFAULT 0,
                    created_at REAL NOT NULL
                )
            """)
            message_cols = [r[1] for r in db.execute("PRAGMA table_info(messages)").fetchall()]
            if "state" not in message_cols:
                db.execute("ALTER TABLE messages ADD COLUMN state TEXT NOT NULL DEFAULT 'SENT'")
            added_sort_timestamp = "sort_timestamp" not in message_cols
            if added_sort_timestamp:
                db.execute("ALTER TABLE messages ADD COLUMN sort_timestamp REAL NOT NULL DEFAULT 0")
            if "attempts" not in message_cols:
                db.execute("ALTER TABLE messages ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0")
            if "next_retry_at" not in message_cols:
                db.execute("ALTER TABLE messages ADD COLUMN next_retry_at REAL NOT NULL DEFAULT 0")
            conversation_cols = [r[1] for r in db.execute("PRAGMA table_info(conversations)").fetchall()]
            added_last_order = "last_order" not in conversation_cols
            if added_last_order:
                db.execute("ALTER TABLE conversations ADD COLUMN last_order REAL NOT NULL DEFAULT 0")
            if added_sort_timestamp:
                for row in db.execute("SELECT id, timestamp FROM messages WHERE sort_timestamp = 0").fetchall():
                    db.execute("UPDATE messages SET sort_timestamp = ? WHERE id = ?", (message_timestamp_order(row[1]), row[0]))
            if added_last_order:
                for row in db.execute("SELECT peer_id, last_timestamp FROM conversations WHERE last_order = 0").fetchall():
                    db.execute("UPDATE conversations SET last_order = ? WHERE peer_id = ?", (message_timestamp_order(row[1]), row[0]))
            db.execute("CREATE INDEX IF NOT EXISTS idx_messages_peer_created ON messages(peer_id, created_at, sequence)")
            db.execute("CREATE INDEX IF NOT EXISTS idx_messages_outbox ON messages(is_outgoing, state, next_retry_at)")
            db.execute("""
                CREATE TABLE IF NOT EXISTS local_groups (
                    group_id TEXT PRIMARY KEY,
                    circle_id TEXT NOT NULL DEFAULT '',
                    name TEXT NOT NULL,
                    member_count INTEGER NOT NULL DEFAULT 0,
                    state TEXT NOT NULL DEFAULT 'ACTIVE',
                    updated_at REAL NOT NULL
                )
            """)
            db.execute("""
                CREATE TABLE IF NOT EXISTS group_messages (
                    group_id TEXT NOT NULL,
                    message_id TEXT NOT NULL,
                    circle_id TEXT NOT NULL DEFAULT '',
                    parent_task_id TEXT NOT NULL DEFAULT '',
                    sender_id TEXT NOT NULL DEFAULT '',
                    sender_name TEXT NOT NULL DEFAULT '',
                    sender_type TEXT NOT NULL DEFAULT 'AGENT',
                    message TEXT NOT NULL,
                    timestamp TEXT NOT NULL,
                    state TEXT NOT NULL DEFAULT 'SENT',
                    reply_policy TEXT NOT NULL DEFAULT 'ACK_ONLY',
                    mentions TEXT NOT NULL DEFAULT '[]',
                    sequence INTEGER,
                    revision INTEGER NOT NULL DEFAULT 0,
                    idempotency_key TEXT NOT NULL DEFAULT '',
                    sort_timestamp REAL NOT NULL DEFAULT 0,
                    created_at REAL NOT NULL,
                    PRIMARY KEY (group_id, message_id)
                )
            """)
            db.execute("CREATE INDEX IF NOT EXISTS idx_group_messages_scope ON group_messages(group_id, sort_timestamp, created_at)")
        os.chmod(self.path, 0o600)

    def save_group(self, group):
        with self.lock, self._db() as db:
            db.execute("""
                INSERT INTO local_groups(group_id, circle_id, name, member_count, state, updated_at)
                VALUES (?, ?, ?, ?, ?, ?)
                ON CONFLICT(group_id) DO UPDATE SET
                    circle_id=excluded.circle_id, name=excluded.name,
                    member_count=excluded.member_count, state=excluded.state,
                    updated_at=excluded.updated_at
            """, (group.get("groupId", ""), group.get("circleId", ""), group.get("name", ""), int(group.get("memberCount", 0)), group.get("state", "ACTIVE"), time.time()))

    def get_groups(self):
        with self.lock, self._db() as db:
            rows = db.execute("SELECT group_id, circle_id, name, member_count, state, updated_at FROM local_groups ORDER BY updated_at DESC").fetchall()
            return [{"groupId": row["group_id"], "circleId": row["circle_id"], "name": row["name"], "memberCount": row["member_count"], "state": row["state"], "updatedAt": row["updated_at"]} for row in rows]

    def save_group_message(self, group_id, item):
        message_id = str(item.get("id") or item.get("messageId") or uuid.uuid4())
        timestamp = item.get("timestamp") or datetime.now(timezone.utc).isoformat()
        mentions = item.get("mentions", [])
        with self.lock, self._db() as db:
            db.execute("""
                INSERT INTO group_messages(group_id, message_id, circle_id, parent_task_id, sender_id, sender_name, sender_type, message, timestamp, state, reply_policy, mentions, sequence, revision, idempotency_key, sort_timestamp, created_at)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(group_id, message_id) DO UPDATE SET
                    parent_task_id=excluded.parent_task_id, sender_id=excluded.sender_id,
                    sender_name=excluded.sender_name, sender_type=excluded.sender_type,
                    message=excluded.message, timestamp=excluded.timestamp,
                    state=excluded.state, reply_policy=excluded.reply_policy,
                    mentions=excluded.mentions, sequence=COALESCE(excluded.sequence, group_messages.sequence),
                    revision=excluded.revision, idempotency_key=excluded.idempotency_key
            """, (group_id, message_id, item.get("circleId", ""), item.get("parentTaskId", ""), item.get("senderId", ""), item.get("senderName", ""), item.get("senderType", "AGENT"), item.get("message", ""), timestamp, item.get("state", "SENT"), item.get("replyPolicy", "ACK_ONLY"), json.dumps(mentions, ensure_ascii=False), item.get("sequence"), int(item.get("revision", 0)), item.get("idempotencyKey", ""), message_timestamp_order(timestamp), time.time()))

    def get_group_messages(self, group_id, limit=200):
        limit = max(1, min(int(limit), 200))
        with self.lock, self._db() as db:
            rows = db.execute("SELECT * FROM group_messages WHERE group_id = ? ORDER BY sort_timestamp ASC, created_at ASC, message_id ASC LIMIT ?", (group_id, limit)).fetchall()
            result = []
            for row in rows:
                result.append({"id": row["message_id"], "groupId": row["group_id"], "circleId": row["circle_id"], "parentTaskId": row["parent_task_id"], "senderId": row["sender_id"], "senderName": row["sender_name"], "senderType": row["sender_type"], "message": row["message"], "timestamp": row["timestamp"], "state": row["state"], "replyPolicy": row["reply_policy"], "mentions": json.loads(row["mentions"] or "[]"), "sequence": row["sequence"], "revision": row["revision"], "idempotencyKey": row["idempotency_key"]})
            return result

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

    def save_message(self, peer_id, msg, sequence=None, display_name=None, state="SENT"):
        msg_id = msg.get("id") or str(uuid.uuid4())
        sender_id = msg.get("senderId", "")
        sender_name = msg.get("senderName", "")
        content = msg.get("message", "")
        ts = msg.get("timestamp") or datetime.now(timezone.utc).isoformat()
        is_out = 1 if msg.get("isOutgoing") else 0
        msg_state = msg.get("state") or state
        sort_timestamp = message_timestamp_order(ts)
        now = time.time()

        with self.lock, self._db() as db:
            # ON CONFLICT(id): Preserve original created_at to avoid re-ordering on SSE re-delivery!
            db.execute("""
                INSERT INTO messages(id, peer_id, sender_id, sender_name, message, timestamp, is_outgoing, sequence, state, sort_timestamp, attempts, next_retry_at, created_at)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, ?)
                ON CONFLICT(id) DO UPDATE SET
                    sequence = COALESCE(excluded.sequence, messages.sequence),
                    state = CASE
                        WHEN messages.state = 'SENT' THEN 'SENT'
                        ELSE COALESCE(excluded.state, messages.state)
                    END,
                    sender_name = COALESCE(excluded.sender_name, messages.sender_name)
            """, (msg_id, peer_id, sender_id, sender_name, content, ts, is_out, sequence, msg_state, sort_timestamp, now))

            db.execute("""
                INSERT INTO conversations(peer_id, display_name, last_message, last_timestamp, last_order, updated_at)
                VALUES (?, ?, ?, ?, ?, ?)
                ON CONFLICT(peer_id) DO UPDATE SET
                    display_name=COALESCE(excluded.display_name, conversations.display_name),
                    last_message=CASE 
                        WHEN excluded.last_order >= conversations.last_order
                        THEN excluded.last_message 
                        ELSE conversations.last_message 
                    END,
                    updated_at=CASE 
                        WHEN excluded.last_order >= conversations.last_order
                        THEN excluded.updated_at 
                        ELSE conversations.updated_at 
                    END,
                    last_timestamp=CASE 
                        WHEN excluded.last_order >= conversations.last_order
                        THEN excluded.last_timestamp 
                        ELSE conversations.last_timestamp 
                    END,
                    last_order=CASE
                        WHEN excluded.last_order >= conversations.last_order
                        THEN excluded.last_order
                        ELSE conversations.last_order
                    END
            """, (peer_id, display_name or sender_name or peer_id, content, ts, sort_timestamp, now))

    def update_message_state(self, msg_id, state, retry_after=0):
        with self.lock, self._db() as db:
            next_retry_at = time.time() + retry_after if state == "FAILED" and retry_after > 0 else 0
            db.execute("UPDATE messages SET state = ?, next_retry_at = ? WHERE id = ?", (state, next_retry_at, msg_id))

    def get_message(self, msg_id):
        with self.lock, self._db() as db:
            row = db.execute("SELECT * FROM messages WHERE id = ?", (msg_id,)).fetchone()
            return dict(row) if row else None

    def claim_outbox(self, msg_id, now=None, force=False):
        now = time.time() if now is None else now
        with self.lock, self._db() as db:
            if force:
                row = db.execute("""
                    SELECT id, peer_id, message, attempts, state
                    FROM messages
                    WHERE id = ? AND is_outgoing = 1 AND state != 'SENT'
                """, (msg_id,)).fetchone()
            else:
                row = db.execute("""
                    SELECT id, peer_id, message, attempts, state
                    FROM messages
                    WHERE id = ? AND is_outgoing = 1
                      AND state IN ('PENDING', 'FAILED', 'SENDING')
                      AND next_retry_at <= ?
                """, (msg_id, now)).fetchone()
            if not row:
                return None
            db.execute("""
                UPDATE messages
                SET state = 'SENDING', attempts = attempts + 1, next_retry_at = ?
                WHERE id = ?
            """, (now + 60, msg_id))
            claimed = dict(row)
            claimed["attempts"] = int(row["attempts"]) + 1
            claimed["state"] = "SENDING"
            return claimed

    def due_outbox_ids(self, now=None, limit=10):
        now = time.time() if now is None else now
        with self.lock, self._db() as db:
            rows = db.execute("""
                SELECT id
                FROM messages
                WHERE is_outgoing = 1
                  AND state IN ('PENDING', 'FAILED', 'SENDING')
                  AND next_retry_at <= ?
                ORDER BY created_at ASC
                LIMIT ?
            """, (now, limit)).fetchall()
            return [row["id"] for row in rows]

    def get_history(self, peer_id, limit=100, before=None, offset=0):
        with self.lock, self._db() as db:
            if before is not None:
                rows = db.execute("""
                    SELECT id, peer_id, sender_id, sender_name, message, timestamp, is_outgoing, sequence, state, sort_timestamp, created_at
                    FROM messages
                    WHERE peer_id = ? AND sort_timestamp < ?
                    ORDER BY sort_timestamp ASC, created_at ASC, id ASC
                    LIMIT ? OFFSET ?
                """, (peer_id, before, limit, offset)).fetchall()
            else:
                rows = db.execute("""
                    SELECT id, peer_id, sender_id, sender_name, message, timestamp, is_outgoing, sequence, state, sort_timestamp, created_at
                    FROM messages
                    WHERE peer_id = ?
                    ORDER BY sort_timestamp ASC, created_at ASC, id ASC
                    LIMIT ? OFFSET ?
                """, (peer_id, limit, offset)).fetchall()
            return [
                {
                    "id": r["id"],
                    "peerId": r["peer_id"],
                    "senderId": r["sender_id"],
                    "senderName": r["sender_name"],
                    "message": r["message"],
                    "timestamp": r["timestamp"],
                    "isOutgoing": bool(r["is_outgoing"]),
                    "sequence": r["sequence"],
                    "state": r["state"] if "state" in r.keys() else "SENT",
                    "sortTimestamp": r["sort_timestamp"] if "sort_timestamp" in r.keys() else r["created_at"],
                    "createdAt": r["created_at"],
                }
                for r in rows
            ]

    def get_conversations(self):
        with self.lock, self._db() as db:
            rows = db.execute("""
                SELECT peer_id, display_name, last_message, last_timestamp, updated_at
                FROM conversations
                ORDER BY updated_at DESC
            """).fetchall()
            return [
                {
                    "peerId": r["peer_id"],
                    "displayName": r["display_name"],
                    "lastMessage": r["last_message"],
                    "lastTimestamp": r["last_timestamp"],
                    "updatedAt": r["updated_at"],
                }
                for r in rows
            ]


class LocalUIServer(http.server.ThreadingHTTPServer):
    def __init__(self, server_address, RequestHandlerClass, hub_client, user_name, chat_db_path=None, active_backend=None, desired_backend=None, runtime_config_path=None):
        super().__init__(server_address, RequestHandlerClass)
        self.hub_client = hub_client
        self.user_name = user_name
        self.local_ui_token = secrets.token_urlsafe(32)
        self.runtime_config = RuntimeConfigStore(runtime_config_path)
        self.active_backend = active_backend or ""
        self.desired_backend = desired_backend or self.runtime_config.desired_backend or self.active_backend
        self.chat_store = LocalChatStore(chat_db_path or os.path.expanduser("~/.a2a/chat.db"))
        self.subscribers = set()
        self.lock = threading.Lock()
        self.outbox_locks = [threading.Lock() for _ in range(64)]
        self.outbox_stop = threading.Event()
        self.running = True
        self.outbox_thread = threading.Thread(target=self._run_outbox_worker, daemon=True)
        self.outbox_thread.start()

    def server_close(self):
        self.outbox_stop.set()
        super().server_close()

    def append_message(self, peer_id, msg, sequence=None, display_name=None):
        self.chat_store.save_message(peer_id, msg, sequence=sequence, display_name=display_name)
        with self.lock:
            for q in list(self.subscribers):
                try:
                    q.put_nowait(msg)
                except Exception:
                    pass

    def append_group_message(self, group_id, msg):
        self.chat_store.save_group_message(group_id, msg)
        event = dict(msg)
        event["groupId"] = group_id
        event["type"] = "group_message"
        with self.lock:
            for q in list(self.subscribers):
                try:
                    q.put_nowait(event)
                except Exception:
                    pass

    def update_message_state(self, peer_id, msg_id, state, previous_id=None):
        retry_after = 2 if state == "FAILED" else 0
        self.chat_store.update_message_state(msg_id, state, retry_after=retry_after)
        evt = {"id": msg_id, "peerId": peer_id, "state": state, "type": "state_change"}
        if previous_id:
            evt["previousId"] = previous_id
        with self.lock:
            for q in list(self.subscribers):
                try:
                    q.put_nowait(evt)
                except Exception:
                    pass

    def deliver_outbox(self, msg_id, force=False):
        """Deliver one persisted outgoing message without concurrent duplicate sends."""
        lock = self.outbox_locks[hash(msg_id) % len(self.outbox_locks)]
        with lock:
            claimed = self.chat_store.claim_outbox(msg_id, force=force)
            if not claimed:
                current = self.chat_store.get_message(msg_id)
                if current and current.get("state") == "SENT":
                    return current.get("id") or msg_id
                return None
            return self._deliver_claimed(claimed)

    def _deliver_claimed(self, claimed):
        try:
            result = self.hub_client.send_task(claimed["peer_id"], claimed["message"], task_id=claimed["id"])
        except Exception as exc:
            print(f"[!] Outbox delivery error for {claimed['id']}: {exc}", file=sys.stderr)
            result = None
        if result and isinstance(result, dict) and result.get("taskId"):
            final_task_id = result.get("taskId")
            if final_task_id != claimed["id"]:
                with self.chat_store.lock, self.chat_store._db() as db:
                    db.execute("UPDATE messages SET id = ?, state = 'SENT' WHERE id = ?", (final_task_id, claimed["id"]))
                self.update_message_state(claimed["peer_id"], final_task_id, "SENT", previous_id=claimed["id"])
            else:
                self.update_message_state(claimed["peer_id"], claimed["id"], "SENT")
            return final_task_id

        retry_after = min(300, 2 ** min(int(claimed["attempts"]), 8))
        self.chat_store.update_message_state(claimed["id"], "FAILED", retry_after=retry_after)
        self._publish_state_change(claimed["peer_id"], claimed["id"], "FAILED")
        return None

    def _publish_state_change(self, peer_id, msg_id, state):
        with self.lock:
            for q in list(self.subscribers):
                try:
                    q.put_nowait({"id": msg_id, "peerId": peer_id, "state": state, "type": "state_change"})
                except Exception:
                    pass

    def _run_outbox_worker(self):
        while not self.outbox_stop.wait(2):
            try:
                due_ids = self.chat_store.due_outbox_ids(limit=10)
                for msg_id in due_ids:
                    if self.outbox_stop.is_set():
                        break
                    self.deliver_outbox(msg_id)
            except Exception as exc:
                print(f"[!] Outbox worker loop error: {exc}", file=sys.stderr)

    def add_subscriber(self, q):
        with self.lock:
            self.subscribers.add(q)

    def remove_subscriber(self, q):
        with self.lock:
            self.subscribers.discard(q)


class LocalUIHandler(http.server.BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        pass

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        if parsed.path in ("/", "/index.html"):
            content = CLIENT_HTML.replace("{{LOCAL_UI_TOKEN}}", self.server.local_ui_token).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Cache-Control", "no-store")
            self.send_header("X-Content-Type-Options", "nosniff")
            self.send_header("Content-Length", str(len(content)))
            self.end_headers()
            self.wfile.write(content)
        elif parsed.path == "/api/me":
            data = {
                "agentId": self.server.hub_client.agent_id,
                "displayName": self.server.user_name,
                "hubUrl": self.server.hub_client.hub_url,
            }
            body = json.dumps(data, ensure_ascii=False).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        elif parsed.path == "/api/runtimes":
            data = {"runtimes": detect_runtimes(self.server.active_backend, self.server.desired_backend, self.server.runtime_config.custom), "configStatus": "invalid" if self.server.runtime_config.error else "ok"}
            body = json.dumps(data, ensure_ascii=False).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Cache-Control", "no-store")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        elif parsed.path == "/api/groups":
            try:
                groups = self._refresh_groups()
                self._write_json(200, {"groups": groups, "source": "hub"})
            except Exception:
                circle_id = getattr(self.server.hub_client, "circle_id", None)
                cached = self.server.chat_store.get_groups()
                if circle_id:
                    cached = [group for group in cached if group.get("circleId") == circle_id]
                else:
                    cached = []
                self._write_json(200, {"groups": cached, "source": "local", "offline": True})
        elif parsed.path.startswith("/api/groups/") and parsed.path.endswith("/messages"):
            group_id = urllib.parse.unquote(parsed.path[len("/api/groups/"):-len("/messages")]).strip("/")
            if not group_id or "/" in group_id:
                self._write_local_error(404, "group not found")
                return
            try:
                groups = self._refresh_groups()
                if not any(group.get("groupId") == group_id for group in groups):
                    self._write_local_error(404, "group not found")
                    return
                tasks = self.server.hub_client.list_standard_group_tasks(group_id)
                for task in tasks:
                    for item in standard_task_group_messages(group_id, task, self.server.hub_client.agent_id, self.server.user_name):
                        self.server.chat_store.save_group_message(group_id, item)
                self._write_json(200, {"groupId": group_id, "messages": self.server.chat_store.get_group_messages(group_id, limit=200)})
            except Exception:
                self._write_local_error(503, "group history is unavailable")
        elif parsed.path == "/api/peers":
            try:
                agents = self.server.hub_client.list_agents()
                body = json.dumps({"agents": agents}, ensure_ascii=False).encode("utf-8")
                self.send_response(200)
                self.send_header("Content-Type", "application/json; charset=utf-8")
                self.send_header("Cache-Control", "no-store")
                self.send_header("X-Content-Type-Options", "nosniff")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            except Exception:
                self._write_local_error(500, "peer discovery failed")
        elif parsed.path == "/api/conversations":
            convs = self.server.chat_store.get_conversations()
            body = json.dumps({"conversations": convs}, ensure_ascii=False).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        elif parsed.path == "/api/history":
            qs = urllib.parse.parse_qs(parsed.query)
            peer = qs.get("peer", [""])[0].strip()
            if not peer:
                body = json.dumps({"error": "Missing required query parameter: peer"}, ensure_ascii=False).encode("utf-8")
                self.send_response(400)
                self.send_header("Content-Type", "application/json; charset=utf-8")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
                return

            try:
                raw_limit = qs.get("limit", ["100"])[0]
                limit = int(raw_limit)
                if limit < 1 or limit > 500:
                    raise ValueError("limit must be an integer between 1 and 500")
            except (ValueError, TypeError) as e:
                body = json.dumps({"error": f"Invalid limit: {e}"}, ensure_ascii=False).encode("utf-8")
                self.send_response(400)
                self.send_header("Content-Type", "application/json; charset=utf-8")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
                return

            try:
                raw_offset = qs.get("offset", ["0"])[0]
                offset = int(raw_offset)
                if offset < 0 or offset > 100000:
                    raise ValueError("offset must be an integer between 0 and 100000")
            except (ValueError, TypeError) as e:
                body = json.dumps({"error": f"Invalid offset: {e}"}, ensure_ascii=False).encode("utf-8")
                self.send_response(400)
                self.send_header("Content-Type", "application/json; charset=utf-8")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
                return

            before = None
            if "before" in qs:
                raw_before = qs.get("before", [""])[0]
                try:
                    before = float(raw_before)
                    if before < 0 or not math.isfinite(before):
                        raise ValueError("before must be a non-negative timestamp")
                except (ValueError, TypeError) as e:
                    body = json.dumps({"error": f"Invalid before cursor: {e}"}, ensure_ascii=False).encode("utf-8")
                    self.send_response(400)
                    self.send_header("Content-Type", "application/json; charset=utf-8")
                    self.send_header("Content-Length", str(len(body)))
                    self.end_headers()
                    self.wfile.write(body)
                    return

            msgs = self.server.chat_store.get_history(peer, limit=limit, before=before, offset=offset)
            body = json.dumps({"messages": msgs}, ensure_ascii=False).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        elif parsed.path == "/api/events":
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-cache")
            self.send_header("Connection", "keep-alive")
            self.end_headers()
            sub_q = queue.Queue()
            self.server.add_subscriber(sub_q)
            try:
                while getattr(self.server, "running", True):
                    try:
                        event = sub_q.get(timeout=15)
                        data_line = f"data: {json.dumps(event, ensure_ascii=False)}\n\n"
                        self.wfile.write(data_line.encode("utf-8"))
                        self.wfile.flush()
                    except queue.Empty:
                        self.wfile.write(b": keepalive\n\n")
                        self.wfile.flush()
            except Exception:
                pass
            finally:
                self.server.remove_subscriber(sub_q)
        else:
            self.send_response(404)
            self.end_headers()

    def _write_json(self, status, value):
        body = json.dumps(value, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _refresh_groups(self):
        groups = []
        for reference in self.server.hub_client.list_standard_groups(page_size=100):
            group_id = reference.get("groupId") if isinstance(reference, dict) else None
            if not isinstance(group_id, str) or not group_id or "/" in group_id:
                continue
            group = dict(reference)
            group["state"] = "ACTIVE"
            group["circleId"] = getattr(self.server.hub_client, "circle_id", "") or ""
            try:
                group["roster"] = self.server.hub_client.get_group_roster(group_id)
            except Exception:
                group["roster"] = []
            try:
                card = self.server.hub_client.get_standard_group_card(group_id)
                interfaces = card.get("supportedInterfaces", []) if isinstance(card, dict) else []
                if not any(isinstance(item, dict) and item.get("tenant") == f"group:{group_id}" for item in interfaces):
                    continue
            except Exception:
                continue
            self.server.chat_store.save_group(group)
            groups.append(group)
        return groups

    def do_POST(self):
        parsed = urllib.parse.urlparse(self.path)
        if not self._validate_local_mutation():
            return
        if parsed.path.startswith("/api/groups/") and parsed.path.endswith("/messages"):
            group_id = urllib.parse.unquote(parsed.path[len("/api/groups/"):-len("/messages")]).strip("/")
            payload = self._read_json_body()
            if payload is None:
                return
            allowed = {"message", "mentions", "messageId", "contextId"}
            if not group_id or "/" in group_id or not isinstance(payload, dict) or set(payload) - allowed:
                self._write_local_error(400, "group message is invalid")
                return
            message = payload.get("message")
            mentions = payload.get("mentions", [])
            if not isinstance(message, str) or not message.strip() or len(message.encode("utf-8")) > MAX_LOCAL_REQUEST_BYTES:
                self._write_local_error(400, "group message is invalid")
                return
            if not isinstance(mentions, list) or len(mentions) > 16 or any(not isinstance(value, str) or not value.strip() or len(value) > 128 or any(char in value for char in "\r\n") for value in mentions):
                self._write_local_error(400, "group mentions are invalid")
                return
            if any(field in payload and not isinstance(payload[field], str) for field in ("messageId", "contextId")):
                self._write_local_error(400, "group message identifiers are invalid")
                return
            try:
                groups = self._refresh_groups()
                group = next((item for item in groups if item.get("groupId") == group_id), None)
                if group is None:
                    self._write_local_error(404, "group not found")
                    return
                message_id = payload.get("messageId", "").strip() or f"human-{uuid.uuid4()}"
                pending = {"id": message_id, "parentTaskId": "", "senderId": self.server.hub_client.agent_id, "senderName": self.server.user_name, "senderType": "HUMAN", "message": message.strip(), "timestamp": datetime.now(timezone.utc).isoformat(), "state": "PENDING", "replyPolicy": "MENTIONED_ONLY" if mentions else "ACK_ONLY", "mentions": mentions, "idempotencyKey": f"a2a-ui:{message_id}", "revision": 0}
                self.server.append_group_message(group_id, pending)
                response = self.server.hub_client.send_standard_group_message(group_id, pending["message"], mentions=mentions, context_id=payload.get("contextId") or None, message_id=message_id, idempotency_key=pending["idempotencyKey"])
                task = response.get("task", response) if isinstance(response, dict) else {}
                for item in standard_task_group_messages(group_id, task, self.server.hub_client.agent_id, self.server.user_name):
                    self.server.append_group_message(group_id, item)
                self._write_json(200, {"ok": True, "parentTaskId": task.get("id", ""), "task": task, "state": (task.get("status") or {}).get("state", "TASK_STATE_SUBMITTED")})
            except Exception:
                failed = dict(pending) if "pending" in locals() else {"id": f"failed-{uuid.uuid4()}", "message": message.strip(), "senderId": self.server.hub_client.agent_id, "senderName": self.server.user_name, "senderType": "HUMAN", "timestamp": datetime.now(timezone.utc).isoformat()}
                failed["state"] = "FAILED"
                self.server.append_group_message(group_id, failed)
                self._write_local_error(502, "group delivery failed; local message remains pending or failed")
            return
        if parsed.path == "/api/runtimes/custom":
            payload = self._read_json_body()
            if payload is None: return
            try:
                runtime = validate_custom_runtime(payload)
                self.server.runtime_config.add_custom(runtime)
            except (ValueError, OSError):
                self._write_local_error(400, "invalid Runtime configuration")
                return
            self.send_response(201)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Cache-Control", "no-store")
            body = json.dumps({"runtime": {"id": runtime["id"], "name": runtime["name"], "executable": runtime["executable"], "args": runtime["args"], "envNames": runtime["envNames"]}}, ensure_ascii=False).encode("utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        if parsed.path == "/api/runtimes/select":
            payload = self._read_json_body()
            if payload is None: return
            if not isinstance(payload, dict) or set(payload) != {"id"} or not isinstance(payload["id"], str):
                self._write_local_error(400, "runtime selection is invalid")
                return
            try:
                self.server.runtime_config.select(payload["id"].strip())
            except (ValueError, OSError):
                self._write_local_error(400, "runtime id does not exist")
                return
            self.server.desired_backend = payload["id"].strip()
            self.send_response(200)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Cache-Control", "no-store")
            body = json.dumps({"requested": self.server.desired_backend, "active": self.server.active_backend, "state": "active" if self.server.desired_backend == self.server.active_backend else "pending"}).encode("utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        if parsed.path == "/api/send":
            try:
                length = int(self.headers.get("Content-Length", 0))
            except (TypeError, ValueError):
                length = -1
            if length < 0:
                body = json.dumps({"error": "Content-Length must be a valid non-negative integer"}).encode("utf-8")
                self.send_response(400)
                self.send_header("Content-Type", "application/json; charset=utf-8")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
                return
            if length > MAX_LOCAL_REQUEST_BYTES:
                body = json.dumps({"error": "request body is too large"}).encode("utf-8")
                self.send_response(413)
                self.send_header("Content-Type", "application/json; charset=utf-8")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
                return
            raw = self.rfile.read(length).decode("utf-8")
            try:
                payload = json.loads(raw)
                allowed_fields = {"targetAgentId", "message", "taskId"}
                if not isinstance(payload, dict) or set(payload) - allowed_fields:
                    raise ValueError("unsupported request field")
                if not all(isinstance(payload.get(field, ""), str) for field in ("targetAgentId", "message", "taskId")):
                    raise ValueError("request fields must be strings")
                target_id = payload.get("targetAgentId", "").strip()
                msg_text = payload.get("message", "").strip()
                task_id = payload.get("taskId", "").strip() or f"out-{uuid.uuid4()}"
                if not target_id or not msg_text:
                    body = json.dumps({"error": "targetAgentId and message are required"}, ensure_ascii=False).encode("utf-8")
                    self.send_response(400)
                    self.send_header("Content-Type", "application/json; charset=utf-8")
                    self.send_header("Content-Length", str(len(body)))
                    self.end_headers()
                    self.wfile.write(body)
                    return

                ts = datetime.now(timezone.utc).isoformat()
                # 1. OUTBOX: Persist as PENDING with deterministic task_id first
                out_msg = {
                    "id": task_id,
                    "peerId": target_id,
                    "senderId": self.server.hub_client.agent_id,
                    "senderName": self.server.user_name,
                    "message": msg_text,
                    "timestamp": ts,
                    "isOutgoing": True,
                    "state": "PENDING"
                }
                self.server.append_message(target_id, out_msg, display_name=target_id)

                # 2. Dispatch through the serialized local outbox worker.
                final_task_id = self.server.deliver_outbox(task_id, force=True)

                # 3. Handle Hub outcome
                if not final_task_id:
                    body = json.dumps({
                        "ok": False,
                        "taskId": task_id,
                        "state": "FAILED",
                        "error": "Hub 任務投遞失敗或連線逾時，已寫入本機佇列並標記為 FAILED，可安全重試"
                    }, ensure_ascii=False).encode("utf-8")
                    self.send_response(502)
                    self.send_header("Content-Type", "application/json; charset=utf-8")
                    self.send_header("Cache-Control", "no-store")
                    self.send_header("Content-Length", str(len(body)))
                    self.end_headers()
                    self.wfile.write(body)
                    return

                # Hub acknowledged/accepted task
                body = json.dumps({"ok": True, "taskId": final_task_id, "state": "SENT"}, ensure_ascii=False).encode("utf-8")
                self.send_response(200)
                self.send_header("Content-Type", "application/json; charset=utf-8")
                self.send_header("Cache-Control", "no-store")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            except ValueError:
                self._write_local_error(400, "request body is invalid")
            except Exception:
                self._write_local_error(500, "local delivery failed")
        else:
            self.send_response(404)
            self.end_headers()

    def _validate_local_mutation(self):
        host = self.headers.get("Host", "")
        try:
            host_url = urllib.parse.urlsplit("//" + host)
            valid_host = host_url.hostname in {"127.0.0.1", "localhost", "::1"} and host_url.port == self.server.server_port
        except ValueError:
            valid_host = False
        if not valid_host:
            self._write_local_error(400, "invalid local host")
            return False
        for header_name in ("Origin", "Referer"):
            value = self.headers.get(header_name)
            if not value:
                continue
            try:
                origin = urllib.parse.urlsplit(value)
                valid_origin = origin.scheme == "http" and origin.hostname in {"127.0.0.1", "localhost", "::1"} and origin.port == self.server.server_port
            except ValueError:
                valid_origin = False
            if not valid_origin:
                self._write_local_error(403, "origin is not allowed")
                return False
        if not hmac.compare_digest(self.headers.get("X-Local-UI-Token", ""), self.server.local_ui_token):
            self._write_local_error(403, "local session is invalid")
            return False
        content_type = self.headers.get("Content-Type", "").split(";", 1)[0].strip().lower()
        if content_type != "application/json":
            self._write_local_error(415, "Content-Type must be application/json")
            return False
        return True

    def _write_local_error(self, status, message):
        body = json.dumps({"error": message}, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _read_json_body(self):
        try:
            length = int(self.headers.get("Content-Length", 0))
        except (TypeError, ValueError):
            length = -1
        if length < 0 or length > MAX_LOCAL_REQUEST_BYTES:
            self._write_local_error(413 if length > MAX_LOCAL_REQUEST_BYTES else 400, "request body is invalid")
            return None
        try:
            value = json.loads(self.rfile.read(length).decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError):
            self._write_local_error(400, "request body is invalid")
            return None
        return value


def run_local_ui(hub_client, user_name, port=8888, open_browser=True, chat_db_path=None, active_backend=None, desired_backend=None):
    """Serve local User Chat Web UI and stream live events with agents."""
    actual_port = port
    server = None
    for p in range(port, port + 20):
        try:
            server = LocalUIServer(("127.0.0.1", p), LocalUIHandler, hub_client, user_name, chat_db_path=chat_db_path, active_backend=active_backend, desired_backend=desired_backend)
            actual_port = p
            break
        except OSError:
            continue
    if not server:
        print(f"[!] Error: Could not bind to any port in range {port}-{port+20}", file=sys.stderr)
        sys.exit(1)

    url = f"http://127.0.0.1:{actual_port}"
    print("=" * 64)
    print("🌐 888a2a Client Web UI (User Workstation)")
    print("-" * 64)
    print(f"Hub URL:      {hub_client.hub_url}")
    print(f"User Agent:   {user_name} ({hub_client.agent_id})")
    print(f"Web Console:  {url}")
    print("-" * 64)
    print("Serving client console. Press Ctrl+C to exit.")
    print("=" * 64)

    def hub_inbox_worker():
        while getattr(server, "running", True):
            try:
                stream_url = f"{hub_client.hub_url}/hub/v1/agents/{hub_client.agent_id}/inbox/stream"
                req = urllib.request.Request(stream_url, headers={
                    "Accept": "text/event-stream",
                    "X-Agent-ID": hub_client.agent_id,
                    "Authorization": f"Bearer {hub_client.token}",
                })
                if hub_client.shared_key:
                    req.add_header("X-Hub-Key", hub_client.shared_key)
                with urllib.request.urlopen(req, timeout=60) as resp:
                    current_data = []
                    for raw_line in resp:
                        if not getattr(server, "running", True):
                            break
                        line = raw_line.decode("utf-8", errors="replace").rstrip("\r\n")
                        if not line:
                            if current_data:
                                raw_json = "\n".join(current_data)
                                try:
                                    item = json.loads(raw_json)
                                    seq = item.get("sequence")
                                    sender_id = item.get("requesterAgentId", "unknown")
                                    msg = {
                                        "id": item.get("taskId", str(uuid.uuid4())),
                                        "peerId": sender_id,
                                        "senderId": sender_id,
                                        "senderName": sender_id,
                                        "message": item.get("message", ""),
                                        "timestamp": item.get("createdAt", datetime.now(timezone.utc).isoformat()),
                                        "isOutgoing": False,
                                    }
                                    # 1. Commit to local durable store first
                                    server.append_message(sender_id, msg, sequence=seq)
                                    # 2. ACK Hub after durable commit
                                    if seq:
                                        hub_client.ack_task(seq)
                                except Exception as exc:
                                    print(f"[!] Error parsing incoming task: {exc}", file=sys.stderr)
                            current_data = []
                            continue
                        if line.startswith(":"):
                            continue
                        if line.startswith("data: "):
                            current_data.append(line[6:])
            except Exception:
                time.sleep(3)

    t = threading.Thread(target=hub_inbox_worker, daemon=True)
    t.start()

    if open_browser:
        threading.Timer(0.6, lambda: webbrowser.open(url)).start()

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.running = False
        server.shutdown()
        server.server_close()
        print("\n[*] Local UI server stopped.")


# ---------------------------------------------------------------------------
# Service Installation (LaunchAgent & systemd)
# ---------------------------------------------------------------------------

RUNTIME_DEFINITIONS = (
    {"id": "openclaw", "name": "OpenClaw", "command": "openclaw"},
    {"id": "claudecode", "name": "Claude Code", "command": "claude"},
    {"id": "goose", "name": "Goose", "command": "goose"},
    {"id": "hermes", "name": "Hermes", "command": "hermes"},
    {"id": "codex", "name": "Codex", "command": "codex"},
    {"id": "opencode", "name": "OpenCode", "command": "opencode"},
)


def probe_runtime(executable, env):
    try:
        probe = subprocess.run(
            [executable, "--version"],
            capture_output=True,
            text=True,
            timeout=2,
            env=env,
            shell=False,
        )
        if probe.returncode == 0:
            version = (probe.stdout or "").strip().splitlines()[0:1]
            return "ready", version[0][:128] if version else "unknown"
    except (OSError, subprocess.SubprocessError):
        pass
    return "unavailable", None


def detect_runtimes(active_backend=None, desired_backend=None, custom_runtimes=None):
    """Inspect known local binaries without claiming provider/login health."""
    enhanced_env = get_enhanced_env()
    active_backend = active_backend or ""
    desired_backend = desired_backend or active_backend
    runtimes = []
    definitions = list(RUNTIME_DEFINITIONS) + list(custom_runtimes or [])
    for definition in definitions:
        executable = definition.get("executable") or shutil.which(definition["command"], path=enhanced_env.get("PATH"))
        runtime = {
            "id": definition["id"],
            "name": definition["name"],
            "executable": executable,
            "version": None,
            "status": "cli_needed" if not executable else "unavailable",
            "active": definition["id"] == active_backend,
            "desired": definition["id"] == desired_backend,
        }
        if executable:
            try:
                runtime["status"], runtime["version"] = probe_runtime(executable, enhanced_env)
        runtimes.append(runtime)
    return runtimes


def detect_backend():
    """Auto-detect available local AI CLI backend."""
    for runtime in detect_runtimes():
        if runtime["status"] == "ready":
            return runtime["id"]
    return "openclaw"


RUNTIME_ID_PATTERN = re.compile(r"^[a-z][a-z0-9_-]{0,31}$")
ENV_NAME_PATTERN = re.compile(r"^[A-Z_][A-Z0-9_]{0,63}$")


def validate_custom_runtime(payload):
    if not isinstance(payload, dict):
        raise ValueError("runtime must be an object")
    allowed = {"id", "name", "executable", "args", "envNames"}
    if set(payload) - allowed:
        raise ValueError("unsupported runtime field")
    if not all(isinstance(payload.get(field), str) for field in ("id", "name", "executable")):
        raise ValueError("id, name, and executable must be strings")
    runtime_id = payload["id"].strip()
    name = payload["name"].strip()
    executable = payload["executable"].strip()
    if not RUNTIME_ID_PATTERN.fullmatch(runtime_id):
        raise ValueError("id must be a bounded lower-case runtime identifier")
    if not name or len(name) > 80:
        raise ValueError("name must contain 1 to 80 characters")
    if not os.path.isabs(executable) or not os.path.isfile(executable) or not os.access(executable, os.X_OK):
        raise ValueError("executable must be an existing executable absolute path")
    args = payload.get("args", [])
    env_names = payload.get("envNames", [])
    if not isinstance(args, list) or len(args) > 16 or any(not isinstance(value, str) or len(value) > 128 or any(marker in value for marker in (";", "|", "&", "$", "`", "\n", "\r")) for value in args):
        raise ValueError("args must be a bounded argv array without shell operators")
    if not isinstance(env_names, list) or len(env_names) > 16 or any(not isinstance(value, str) or not ENV_NAME_PATTERN.fullmatch(value) for value in env_names):
        raise ValueError("envNames must contain environment variable names only")
    return {"id": runtime_id, "name": name, "command": runtime_id, "executable": executable, "args": list(args), "envNames": list(env_names)}


class RuntimeConfigStore:
    """0600, schema-versioned local desired Runtime configuration."""
    def __init__(self, path=None):
        self.path = os.path.abspath(os.path.expanduser(path or "~/.a2a/runtime_config.json"))
        self.lock = threading.Lock()
        self.desired_backend = ""
        self.custom = []
        self.error = None
        self._load()

    def _load(self):
        try:
            with open(self.path, "r", encoding="utf-8") as handle:
                data = json.load(handle)
            if not isinstance(data, dict) or data.get("schemaVersion") != 1:
                raise ValueError("unsupported runtime config schema")
            self.desired_backend = data.get("desiredBackend", "") if isinstance(data.get("desiredBackend", ""), str) else ""
            custom = data.get("custom", [])
            if not isinstance(custom, list):
                raise ValueError("custom runtimes must be an array")
            self.custom = [validate_custom_runtime(item) for item in custom]
            custom_ids = [item["id"] for item in self.custom]
            if len(custom_ids) != len(set(custom_ids)):
                raise ValueError("custom runtime IDs must be unique")
            available = {item["id"] for item in RUNTIME_DEFINITIONS} | set(custom_ids)
            if self.desired_backend and self.desired_backend not in available:
                raise ValueError("desired runtime does not exist")
        except FileNotFoundError:
            return
        except (OSError, ValueError, TypeError, json.JSONDecodeError):
            self.error = "runtime configuration is invalid"

    def _save(self):
        parent = os.path.dirname(self.path)
        os.makedirs(parent, mode=0o700, exist_ok=True)
        fd, temporary = tempfile.mkstemp(prefix=".runtime-config.", dir=parent)
        try:
            os.fchmod(fd, 0o600)
            with os.fdopen(fd, "w", encoding="utf-8") as handle:
                json.dump({"schemaVersion": 1, "desiredBackend": self.desired_backend, "custom": self.custom}, handle, ensure_ascii=False, indent=2)
                handle.flush()
                os.fsync(handle.fileno())
            os.replace(temporary, self.path)
            os.chmod(self.path, 0o600)
        except Exception:
            try: os.unlink(temporary)
            except FileNotFoundError: pass
            raise

    def add_custom(self, runtime):
        with self.lock:
            if any(item["id"] == runtime["id"] for item in self.custom) or any(item["id"] == runtime["id"] for item in RUNTIME_DEFINITIONS):
                raise ValueError("runtime id already exists")
            self.custom.append(runtime)
            self._save()

    def select(self, runtime_id):
        with self.lock:
            available = {item["id"] for item in RUNTIME_DEFINITIONS} | {item["id"] for item in self.custom}
            if runtime_id not in available:
                raise ValueError("runtime id does not exist")
            self.desired_backend = runtime_id
            self._save()


def get_default_service_type():
    """Auto-detect background service manager based on host platform."""
    if sys.platform == "darwin":
        return "launchd"
    if sys.platform.startswith("linux"):
        return "systemd"
    return None


def filter_service_args(args):
    """Cleanly strip --install-service and its optional option value from args."""
    clean = []
    i = 0
    while i < len(args):
        a = args[i]
        if a == "--install-service":
            if i + 1 < len(args) and args[i + 1] in ("auto", "launchd", "systemd"):
                i += 2
                continue
            i += 1
            continue
        if a.startswith("--install-service="):
            i += 1
            continue
        clean.append(a)
        i += 1
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
    parser.add_argument("--name", default=None, help="Human-readable Agent Name (default: auto-detected)")
    parser.add_argument("--agent-id", help="Explicit Agent ID (optional, auto-loaded/registered)")
    parser.add_argument("--token", help="Explicit Agent Token (optional, auto-loaded/registered)")
    parser.add_argument("--shared-key", default=os.getenv("A2A888_HUB_SHARED_KEY"),
                        help="Hub registration key (for SEMI_OPEN or MULTI_CIRCLE mode)")
    parser.add_argument("--credentials", help="Path to credentials JSON file")
    parser.add_argument("--queue-db", help="Durable local work queue SQLite path")

    # Backend selection
    parser.add_argument("--backend", choices=["openclaw", "hermes", "openai", "claudecode", "codex", "command", "echo"], default=None,
                        help="AI Execution Backend (default: auto-detected)")
    parser.add_argument("--backend-agent", default="default", help="Agent profile for OpenClaw (default: default)")
    parser.add_argument("--backend-cmd", help="Command string or template for command backend")
    parser.add_argument("--system-prompt", default="", help="Persona or system prompt instructions")
    parser.add_argument("--standard-executor", action="store_true", help="Advertise the versioned A2A standard executor capability")

    # OpenAI / Ollama compatible settings
    parser.add_argument("--api-base", default="http://localhost:11434/v1", help="API Base for OpenAI backend")
    parser.add_argument("--api-key", default="sk-dummy", help="API Key for OpenAI backend")
    parser.add_argument("--model", default="llama3", help="Model name for OpenAI backend")

    # Model Context Protocol (MCP) mode
    parser.add_argument("--mcp", action="store_true", help="Run as Model Context Protocol (MCP) stdio JSON-RPC server")

    # Client Web UI mode
    parser.add_argument("--ui", action="store_true", help="Launch local User Chat Web UI in browser")
    parser.add_argument("--port", type=int, default=8888, help="Port for local User Chat Web UI (default: 8888)")

    # Service installation
    parser.add_argument("--install-service", nargs="?", const="auto",
                        choices=["auto", "launchd", "systemd"],
                        help="Install and start as background OS service (auto-detects macOS launchd / Linux systemd)")
    parser.add_argument("--service-name", help="Custom ASCII service name for launchd/systemd")

    args = parser.parse_args()
    if bool(args.agent_id) != bool(args.token):
        parser.error("--agent-id and --token must be provided together")

    # Auto-detect backend if not specified
    if not args.backend:
        args.backend = detect_backend()

    # Auto-detect name if not specified
    if not args.name:
        if args.ui:
            args.name = os.getenv("USER") or "User"
        elif args.mcp:
            user = os.getenv("USER") or "User"
            args.name = f"{user}-MCP"
        else:
            host_name = socket.gethostname().split(".")[0]
            backend_title = {
                "openclaw": "OpenClaw",
                "claudecode": "Claude",
                "hermes": "Hermes",
                "codex": "Codex",
                "openai": "OpenAI",
                "command": "Command",
                "echo": "Echo",
            }.get(args.backend, args.backend.capitalize())
            args.name = f"{backend_title}-{host_name}"

    # Handle service installation if requested
    if args.install_service:
        srv_type = args.install_service
        if srv_type == "auto":
            srv_type = get_default_service_type()
            if not srv_type:
                print("[!] Service installation is only supported on macOS (launchd) and Linux (systemd).", file=sys.stderr)
                sys.exit(1)
        if srv_type == "launchd":
            install_launchd_service(args.name, sys.argv[1:], service_name=args.service_name)
        elif srv_type == "systemd":
            install_systemd_service(args.name, sys.argv[1:], service_name=args.service_name)
        sys.exit(0)

    mcp_out = None
    if args.mcp:
        mcp_out = sys.stdout
        sys.stdout = sys.stderr

    # Resolve or auto-register credentials
    if args.ui and not args.credentials:
        args.credentials = default_credential_path(args.hub, args.name, args.shared_key, ui=True)

    cred_file = args.credentials or default_credential_path(args.hub, args.name, args.shared_key)
    agent_id = args.agent_id
    token = args.token
    circle_id = None
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
                circle_id = ident.get("circleId")
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

    hub_client = HubClient(args.hub, agent_id, token, args.shared_key, circle_id=circle_id)

    if not hub_client.agent_id or not hub_client.token:
        print(f"[*] No credentials found for '{args.name}'. Auto-registering with Hub...")
        try:
            capabilities = ["text/plain"]
            if args.standard_executor:
                capabilities.append("a2a-executor/v1")
            reg_data = hub_client.register(args.name, registration_key, provider_family=args.backend, capabilities=capabilities)
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

    # If Client Web UI mode requested, run local workstation server and exit
    if args.ui:
        try:
            run_local_ui(hub_client, args.name, port=args.port, chat_db_path=default_chat_db_path(args.hub, hub_client.agent_id), active_backend=args.backend)
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
