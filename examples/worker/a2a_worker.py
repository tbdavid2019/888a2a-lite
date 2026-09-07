#!/usr/bin/env python3
"""
A2A Worker / SSE Listener Daemon for 888a2a-lite Hub.

Zero external dependencies (pure Python standard library).
Connects to the Hub's real-time Server-Sent Events (SSE) stream,
receives tasks instantly (push), executes local response logic,
replies to the sender agent, and acknowledges the sequence.

Usage:
  python3 a2a_worker.py --hub https://a2a.david888.com --agent-id <ID> --token <TOKEN>
  python3 a2a_worker.py --credential-file credentials.json
"""

import argparse
import json
import os
import socket
import sys
import time
import urllib.error
import urllib.request


def get_local_ip():
    """Get the primary local IPv4 address."""
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        # Doesn't actually connect, just determines outbound interface
        s.connect(("8.8.8.8", 80))
        return s.getsockname()[0]
    except Exception:
        return "127.0.0.1"
    finally:
        s.close()


def send_task(hub_url, sender_id, token, target_id, task_id, context_id, message, shared_key=None):
    """Send a direct task to a target peer agent."""
    url = f"{hub_url.rstrip('/')}/hub/v1/agents/{target_id}/tasks"
    payload = json.dumps({
        "taskId": task_id,
        "contextId": context_id or f"ctx-{int(time.time())}",
        "idempotencyKey": f"idem-{task_id}",
        "message": message,
    }).encode("utf-8")

    req = urllib.request.Request(url, data=payload, headers={
        "Content-Type": "application/json",
        "X-Agent-ID": sender_id,
        "Authorization": f"Bearer {token}",
    })
    if shared_key:
        req.add_header("X-Hub-Key", shared_key)

    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        err_msg = e.read().decode("utf-8", errors="replace")
        print(f"[!] Error sending reply task: HTTP {e.code}: {err_msg}", file=sys.stderr)
        return None


def ack_task(hub_url, agent_id, token, sequence, shared_key=None):
    """Acknowledge processed sequence in the inbox."""
    url = f"{hub_url.rstrip('/')}/hub/v1/agents/{agent_id}/inbox/{sequence}/ack"
    req = urllib.request.Request(url, data=b"{}", headers={
        "Content-Type": "application/json",
        "X-Agent-ID": agent_id,
        "Authorization": f"Bearer {token}",
    })
    if shared_key:
        req.add_header("X-Hub-Key", shared_key)

    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        print(f"[!] Error ACKing sequence {sequence}: HTTP {e.code}", file=sys.stderr)
        return None


def list_invitations(hub_url, agent_id, token, shared_key=None):
    """List pending group invitations for this agent."""
    url = f"{hub_url.rstrip('/')}/hub/v1/groups/invitations"
    req = urllib.request.Request(url, headers={
        "X-Agent-ID": agent_id,
        "Authorization": f"Bearer {token}",
    })
    if shared_key:
        req.add_header("X-Hub-Key", shared_key)

    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read().decode("utf-8"))
            return data.get("invitations", [])
    except Exception as e:
        print(f"[!] Error checking group invitations: {e}", file=sys.stderr)
        return []


def accept_invitation(hub_url, agent_id, token, invitation_id, shared_key=None):
    """Accept a pending group invitation."""
    url = f"{hub_url.rstrip('/')}/hub/v1/groups/invitations/{invitation_id}/accept"
    req = urllib.request.Request(url, data=b"{}", headers={
        "Content-Type": "application/json",
        "X-Agent-ID": agent_id,
        "Authorization": f"Bearer {token}",
    })
    if shared_key:
        req.add_header("X-Hub-Key", shared_key)

    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except Exception as e:
        print(f"[!] Error accepting invitation {invitation_id}: {e}", file=sys.stderr)
        return None


def auto_accept_pending_invitations(hub_url, agent_id, token, shared_key=None):
    """Automatically find and accept all pending group invitations."""
    invitations = list_invitations(hub_url, agent_id, token, shared_key)
    accepted = 0
    for inv in invitations:
        if inv.get("state") == "PENDING":
            inv_id = inv.get("id")
            group_id = inv.get("groupId")
            inviter = inv.get("inviterAgentId")
            print(f"[*] Found pending invitation #{inv_id} for group {group_id} from {inviter}. Accepting...")
            res = accept_invitation(hub_url, agent_id, token, inv_id, shared_key)
            if res:
                print(f"[✓] Joined group {group_id} as ACTIVE member!")
                accepted += 1
    return accepted


def handle_task(hub_url, agent_id, token, item, shared_key=None, reply_ip_demo=False):
    """Process an incoming task or group broadcast.
    
    IMPORTANT: Real-world agents must route item['message'] to their LLM reasoning
    loop or local agent harness rather than blindly auto-replying with fixed text.
    """
    seq = item.get("sequence")
    task_id = item.get("taskId")
    sender_id = item.get("requesterAgentId")
    context_id = item.get("contextId")
    msg = item.get("message", "")
    group_id = item.get("groupId")

    is_group = bool(group_id)
    if is_group:
        print(f"\n📢 [Group Broadcast Received] [Group={group_id}, seq={seq}, id={task_id}] from {sender_id}:")
        print(f"    Message: {msg}")
    else:
        print(f"\n[+] Incoming Task [seq={seq}, id={task_id}] from {sender_id}:")
        print(f"    Message: {msg}")

    # Check for invitations if message mentions group or invitation
    if any(k in msg.lower() for k in ("invite", "group", "群組", "邀請")):
        auto_accept_pending_invitations(hub_url, agent_id, token, shared_key)

    # Only send canned IP reply if explicitly enabled with --reply-ip-demo
    if reply_ip_demo:
        local_ip = get_local_ip()
        hostname = socket.gethostname()
        if is_group:
            reply_text = f"你好！我是 Agent {agent_id}。已收到群組 [{group_id}] 的廣播訊息！\n我所在的主機是 {hostname}，本機內網 IP 為: {local_ip}"
        else:
            reply_text = f"你好！我是 Agent {agent_id}。\n我所在的主機是 {hostname}，本機內網 IP 為: {local_ip}"

        reply_task_id = f"reply-{task_id}"
        print(f"[*] Replying to {sender_id} with task {reply_task_id}...")
        reply_res = send_task(hub_url, agent_id, token, sender_id, reply_task_id, context_id, reply_text, shared_key)
        if reply_res:
            print(f"[✓] Reply task delivered (status={reply_res.get('state')})")
    else:
        print(f"[*] Task received. In a production agent, feed '{msg}' into your LLM reasoning engine.")

    # ACK task
    print(f"[*] Acknowledging sequence {seq}...")
    ack_res = ack_task(hub_url, agent_id, token, seq, shared_key)
    if ack_res:
        print(f"[✓] Sequence {seq} acknowledged")


def run_worker(hub_url, agent_id, token, shared_key=None, reply_ip_demo=False):
    """Connect to SSE stream and process events in a resilient loop."""
    print("=" * 60)
    print(" 888a2a-lite Real-Time SSE Worker Daemon")
    print(f" Hub:      {hub_url}")
    print(f" Agent ID: {agent_id}")
    print(f" Local IP: {get_local_ip()}")
    print("=" * 60)

    # Check for pending invitations on startup
    auto_accept_pending_invitations(hub_url, agent_id, token, shared_key)

    last_event_id = 0
    backoff = 1

    while True:
        stream_url = f"{hub_url.rstrip('/')}/hub/v1/agents/{agent_id}/inbox/stream"
        if last_event_id > 0:
            stream_url += f"?afterSequence={last_event_id}"

        print(f"[*] Connecting to stream: {stream_url}...")
        req = urllib.request.Request(stream_url, headers={
            "Accept": "text/event-stream",
            "X-Agent-ID": agent_id,
            "Authorization": f"Bearer {token}",
        })
        if shared_key:
            req.add_header("X-Hub-Key", shared_key)
        if last_event_id > 0:
            req.add_header("Last-Event-ID", str(last_event_id))

        try:
            with urllib.request.urlopen(req, timeout=60) as resp:
                if resp.status != 200:
                    print(f"[!] Stream connection returned status {resp.status}", file=sys.stderr)
                    time.sleep(backoff)
                    backoff = min(backoff * 2, 30)
                    continue

                print("[✓] Connected to SSE stream. Listening for real-time tasks...")
                backoff = 1
                current_event = None
                current_id = None
                current_data = []

                for raw_line in resp:
                    line = raw_line.decode("utf-8", errors="replace").rstrip("\r\n")

                    if not line:
                        # Empty line = dispatch event
                        if current_data:
                            raw_json = "\n".join(current_data)
                            try:
                                item = json.loads(raw_json)
                                if current_id:
                                    last_event_id = int(current_id)
                                elif item.get("sequence"):
                                    last_event_id = int(item["sequence"])

                                handle_task(hub_url, agent_id, token, item, shared_key, reply_ip_demo)
                            except json.JSONDecodeError as err:
                                print(f"[!] Error parsing event data JSON: {err}", file=sys.stderr)

                        current_event = None
                        current_id = None
                        current_data = []
                        continue

                    if line.startswith(":"):
                        # Keep-alive heartbeat comment
                        continue

                    if line.startswith("id: "):
                        current_id = line[4:].strip()
                    elif line.startswith("event: "):
                        current_event = line[7:].strip()
                    elif line.startswith("data: "):
                        current_data.append(line[6:])

        except urllib.error.HTTPError as e:
            err_body = e.read().decode("utf-8", errors="replace")
            print(f"[!] HTTP Error {e.code}: {err_body}", file=sys.stderr)
            time.sleep(backoff)
            backoff = min(backoff * 2, 30)
        except Exception as e:
            print(f"[!] Connection dropped ({e}). Reconnecting in {backoff}s...", file=sys.stderr)
            time.sleep(backoff)
            backoff = min(backoff * 2, 30)


def main():
    parser = argparse.ArgumentParser(description="888a2a-lite SSE Worker Daemon")
    parser.add_argument("--hub", help="Hub Base URL (e.g. https://a2a.david888.com)")
    parser.add_argument("--agent-id", help="Agent ID")
    parser.add_argument("--token", help="Agent Token")
    parser.add_argument("--shared-key", help="Pre-shared Hub Key (if semi-open mode)")
    parser.add_argument("--credential-file", help="Path to JSON credential file")
    parser.add_argument("--reply-ip-demo", action="store_true", help="Demo mode: send canned reply with local IP (NOT recommended for production agents)")
    args = parser.parse_args()

    hub_url = args.hub
    agent_id = args.agent_id
    token = args.token
    shared_key = args.shared_key

    if args.credential_file and os.path.isfile(args.credential_file):
        with open(args.credential_file, "r", encoding="utf-8") as f:
            creds = json.load(f)
            hub_url = hub_url or creds.get("hubUrl")
            shared_key = shared_key or creds.get("sharedKey")
            ident = creds.get("identity", {})
            agent_id = agent_id or ident.get("agentId")
            token = token or ident.get("agentToken")

    hub_url = hub_url or os.getenv("A2A888_HUB_URL") or "https://a2a.david888.com"
    shared_key = shared_key or os.getenv("A2A888_HUB_SHARED_KEY")

    if not agent_id or not token:
        print("[!] Error: --agent-id and --token (or --credential-file) are required.", file=sys.stderr)
        sys.exit(1)

    try:
        run_worker(hub_url, agent_id, token, shared_key, args.reply_ip_demo)
    except KeyboardInterrupt:
        print("\n[*] Worker daemon stopped.")


if __name__ == "__main__":
    main()
