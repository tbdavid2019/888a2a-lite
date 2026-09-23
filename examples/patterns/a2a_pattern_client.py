#!/usr/bin/env python3
"""
A2A Pattern Client Helper (a2a_pattern_client.py)
Minimal zero-dependency Python client for multi-agent pattern coordination.

Supports:
- Hub registration with optional preloaded credentials
- Sending tasks with structured Envelope
- Instant ACK on Ingest (network latency dependent)
- Agent discovery and capability matching
- Resilient polling and timeout-bounded aggregation
"""

import json
import sys
import time
from datetime import datetime, timedelta, timezone
import urllib.error
import urllib.request
from typing import Any, Dict, List, Optional, Tuple
from urllib.parse import quote

from a2a_envelope import Envelope


class PatternHubClient:
    """Client for 888a2a-lite Hub with Envelope and Pattern support."""

    def __init__(
        self,
        hub_url: str = "http://127.0.0.1:8080",
        agent_id: Optional[str] = None,
        token: Optional[str] = None,
        shared_key: Optional[str] = None,
        circle_id: Optional[str] = None,
    ):
        self.hub_url = hub_url.rstrip("/")
        self.agent_id = agent_id
        self.token = token
        self.shared_key = shared_key
        self.circle_id = circle_id
        self._workflow_create_requests: Dict[str, Dict[str, Any]] = {}

    def _request_json(self, url: str, method: str = "GET", body: Optional[Dict[str, Any]] = None, timeout: float = 15.0) -> Dict[str, Any]:
        payload = None if body is None else json.dumps(body, ensure_ascii=False).encode("utf-8")
        req = urllib.request.Request(url, data=payload, headers=self._headers(), method=method)
        try:
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                return json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as err:
            detail = err.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"Hub request failed: HTTP {err.code}: {detail}") from err
        except Exception as err:
            raise RuntimeError(f"Hub request failed: {err}") from err

    def _headers(self, auth: bool = True) -> Dict[str, str]:
        h = {
            "Content-Type": "application/json",
            "User-Agent": "888a2a-Pattern-Client/1.0",
        }
        if auth and self.token:
            h["Authorization"] = f"Bearer {self.token}"
        if auth and self.agent_id:
            h["X-Agent-ID"] = self.agent_id
        if self.shared_key and (not auth or not self.circle_id):
            h["X-Hub-Key"] = self.shared_key
        return h

    def register(
        self,
        display_name: str,
        capabilities: Optional[List[str]] = None,
        provider_family: str = "pattern-worker",
    ) -> Dict[str, Any]:
        """Register agent with Hub and receive agentId and token."""
        url = f"{self.hub_url}/hub/v1/agents/register"
        reg_key = f"reg-{int(time.time() * 1000)}-{display_name.lower().replace(' ', '-')}"
        payload = json.dumps({
            "displayName": display_name,
            "providerFamily": provider_family,
            "transportId": "http-json",
            "capabilities": capabilities or ["text/plain", "pattern/envelope"],
            "registrationIdempotencyKey": reg_key,
        }).encode("utf-8")

        req = urllib.request.Request(url, data=payload, headers=self._headers(auth=False))
        try:
            with urllib.request.urlopen(req, timeout=15) as resp:
                data = json.loads(resp.read().decode("utf-8"))
                ident = data.get("identity", {})
                self.agent_id = ident.get("agentId")
                self.token = ident.get("agentToken")
                self.circle_id = ident.get("circleId")
                return data
        except urllib.error.HTTPError as e:
            err = e.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"Hub registration failed: HTTP {e.code}: {err}")
        except Exception as e:
            raise RuntimeError(f"Hub registration network error: {e}")

    def ack_task(self, sequence: int) -> bool:
        """Acknowledge an accepted sequence; latency depends on the network."""
        if not self.agent_id or not self.token:
            return False
        url = f"{self.hub_url}/hub/v1/agents/{self.agent_id}/inbox/{sequence}/ack"
        req = urllib.request.Request(url, data=b"{}", headers=self._headers())
        try:
            with urllib.request.urlopen(req, timeout=5) as resp:
                return resp.status == 200
        except Exception as e:
            print(f"[!] ACK failed for sequence {sequence}: {e}", file=sys.stderr)
            return False

    def send_task(
        self,
        target_agent_id: str,
        message: str,
        context_id: Optional[str] = None,
        task_id: Optional[str] = None,
    ) -> Optional[Dict[str, Any]]:
        """Send a direct task message to target peer agent."""
        url = f"{self.hub_url}/hub/v1/agents/{quote(target_agent_id, safe='')}/tasks"
        t_id = task_id or f"task-{time.time_ns()}"
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

    def send_envelope(
        self,
        target_agent_id: str,
        envelope: Envelope,
        task_id: Optional[str] = None,
    ) -> Optional[Dict[str, Any]]:
        """Serialize and send structured Envelope as direct task."""
        envelope_json = envelope.to_json()
        return self.send_task(
            target_agent_id=target_agent_id,
            message=envelope_json,
            context_id=envelope.correlation_id,
            task_id=task_id,
        )

    def create_workflow(
        self,
        workflow_id: str,
        flow_type: str,
        expected_steps: int,
        join_policy: str = "ALL_SUCCESS",
        minimum_successes: Optional[int] = None,
        retry_limit: int = 0,
        deadline_seconds: float = 300.0,
    ) -> Dict[str, Any]:
        """Create a durable Hub workflow with bounded lifetime and retries."""
        if not self.agent_id or not self.token:
            raise RuntimeError("register an Agent before creating a workflow")
        if workflow_id in self._workflow_create_requests:
            previous = self._workflow_create_requests[workflow_id]
            min_success = expected_steps if minimum_successes is None else minimum_successes
            if (
                previous["flowType"] != flow_type
                or previous["joinPolicy"] != join_policy
                or previous["expectedSteps"] != expected_steps
                or previous["minimumSuccesses"] != min_success
                or previous["retryLimit"] != retry_limit
            ):
                raise ValueError("workflow_id is already bound to a different create request")
            return self._request_json(
                f"{self.hub_url}/hub/v1/workflows",
                method="POST",
                body=self._workflow_create_requests[workflow_id],
            )
        min_success = expected_steps if minimum_successes is None else minimum_successes
        body = {
            "workflowId": workflow_id,
            "flowType": flow_type,
            "joinPolicy": join_policy,
            "expectedSteps": expected_steps,
            "minimumSuccesses": min_success,
            "retryLimit": retry_limit,
            "deadline": (datetime.now(timezone.utc) + timedelta(seconds=deadline_seconds)).isoformat(),
            "idempotencyKey": f"create-{workflow_id}",
        }
        self._workflow_create_requests[workflow_id] = body
        return self._request_json(f"{self.hub_url}/hub/v1/workflows", method="POST", body=body)

    def register_workflow_attempt(
        self,
        workflow_id: str,
        step_id: str,
        target_agent_id: str,
        task_id: str,
        attempt: int = 1,
    ) -> Dict[str, Any]:
        """Link an existing durable inbox task to a workflow step attempt."""
        body = {
            "stepId": step_id,
            "targetAgentId": target_agent_id,
            "taskId": task_id,
            "attempt": attempt,
            "idempotencyKey": f"{workflow_id}-{step_id}-{attempt}",
        }
        url = f"{self.hub_url}/hub/v1/workflows/{quote(workflow_id, safe='')}/steps"
        return self._request_json(url, method="POST", body=body)

    def report_workflow_outcome(
        self,
        workflow_id: str,
        step_id: str,
        attempt: int,
        state: str,
        result: str = "",
        error: str = "",
    ) -> Dict[str, Any]:
        """Report WORKING or a terminal attempt outcome to the Hub."""
        body = {"state": state, "result": result, "error": error}
        url = f"{self.hub_url}/hub/v1/workflows/{quote(workflow_id, safe='')}/steps/{quote(step_id, safe='')}/attempts/{attempt}/outcome"
        return self._request_json(url, method="POST", body=body)

    def get_workflow(self, workflow_id: str) -> Dict[str, Any]:
        url = f"{self.hub_url}/hub/v1/workflows/{quote(workflow_id, safe='')}"
        return self._request_json(url)

    def list_workflows(self, limit: int = 50, offset: int = 0) -> Dict[str, Any]:
        """List workflows owned by this Agent for restart recovery and inspection."""
        url = f"{self.hub_url}/hub/v1/workflows?limit={max(1, min(int(limit), 100))}&offset={max(0, int(offset))}"
        return self._request_json(url)

    def cancel_workflow(self, workflow_id: str) -> Dict[str, Any]:
        url = f"{self.hub_url}/hub/v1/workflows/{quote(workflow_id, safe='')}/cancel"
        return self._request_json(url, method="POST", body={})

    def list_agents(self, online_only: bool = True) -> List[Dict[str, Any]]:
        """List peer agents on Hub, optionally filtering for online presence."""
        url = f"{self.hub_url}/hub/v1/agents"
        if online_only:
            url += "?state=online"
        req = urllib.request.Request(url, headers=self._headers())
        try:
            with urllib.request.urlopen(req, timeout=10) as resp:
                data = json.loads(resp.read().decode("utf-8"))
                agents = data.get("agents", [])
                if online_only:
                    filtered = []
                    for ag in agents:
                        st = (
                            ag.get("presence", {}).get("state")
                            or ag.get("status")
                            or ag.get("state")
                            or ""
                        ).upper()
                        if st in ("ONLINE", "ACTIVE"):
                            filtered.append(ag)
                    return filtered
                return agents
        except Exception as e:
            print(f"[!] Error fetching agents: {e}", file=sys.stderr)
            return []

    def find_agents_by_capability(
        self, required_caps: List[str], online_only: bool = True
    ) -> List[Dict[str, Any]]:
        """Discover online agents possessing all specified capabilities."""
        agents = self.list_agents(online_only=online_only)
        matched = []
        for ag in agents:
            if ag.get("agentId") == self.agent_id:
                continue
            caps = ag.get("capabilities") or []
            if all(c in caps for c in required_caps):
                matched.append(ag)
        return matched

    def _fetch_remote_inbox(self, after: int = 0, limit: int = 100) -> List[Dict[str, Any]]:
        """Fetch raw inbox items directly from the Hub."""
        if not self.agent_id:
            return []
        url = f"{self.hub_url}/hub/v1/agents/{self.agent_id}/inbox?afterSequence={after}&limit={limit}"
        req = urllib.request.Request(url, headers=self._headers())
        try:
            with urllib.request.urlopen(req, timeout=10) as resp:
                data = json.loads(resp.read().decode("utf-8"))
                return data.get("items", [])
        except Exception as e:
            print(f"[!] Error fetching remote inbox: {e}", file=sys.stderr)
            return []

    def poll_inbox(self, after: int = 0, limit: int = 50) -> List[Dict[str, Any]]:
        """Poll pending inbox items from the Hub."""
        return self._fetch_remote_inbox(after=after, limit=limit)

    def collect_envelopes(
        self,
        correlation_id: str,
        expected_count: int,
        timeout_seconds: float = 15.0,
        min_quorum: int = 1,
        poll_interval: float = 0.5,
    ) -> Tuple[List[Envelope], List[Dict[str, Any]]]:
        """Poll and collect matching Envelopes.

        Preserves at-least-once inbox semantics: only matching items are ACKed;
        foreign or malformed items remain PENDING on the Hub.
        """
        if expected_count < 1:
            raise ValueError("expected_count must be >= 1")
        if min_quorum < 1 or min_quorum > expected_count:
            raise ValueError("min_quorum must be between 1 and expected_count")
        if timeout_seconds <= 0:
            raise ValueError("timeout_seconds must be > 0")

        start_time = time.time()
        collected_envelopes: List[Envelope] = []
        raw_items: List[Dict[str, Any]] = []
        seen_sequences = set()
        scan_after = 0

        # Poll the remote Hub. Foreign messages are deliberately left pending
        # so another workflow or a restarted process can recover them.
        while (time.time() - start_time) < timeout_seconds:
            remote_items = self._fetch_remote_inbox(after=scan_after, limit=100)
            for item in remote_items:
                seq = item.get("sequence")
                if seq in seen_sequences:
                    continue
                seen_sequences.add(seq)
                if isinstance(seq, int) and seq > scan_after:
                    scan_after = seq

                raw_msg = item.get("message", "")
                try:
                    env = Envelope.from_json(raw_msg)
                    if env.correlation_id == correlation_id:
                        # ACK only after this workflow accepts the message.
                        # Foreign or malformed messages stay PENDING so they
                        # remain recoverable after a process crash.
                        self.ack_task(seq)
                        collected_envelopes.append(env)
                        raw_items.append(item)
                except Exception:
                    # Leave malformed items PENDING for inspection or a
                    # dedicated dead-letter handler.
                    continue

                if len(collected_envelopes) >= expected_count:
                    return collected_envelopes, raw_items

            if len(collected_envelopes) >= min_quorum and (time.time() - start_time) > (timeout_seconds / 2):
                break

            time.sleep(poll_interval)

        return collected_envelopes, raw_items
