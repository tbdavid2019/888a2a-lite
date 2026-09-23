#!/usr/bin/env python3
"""
Pattern 03: 監督者模式 (Supervisor / Dynamic Router)
Demonstrates capability-based dynamic task routing using 888a2a Agent Cards.

Architecture:
                       [User / Initiator]
                               │ (Inquiry / Goal)
                               ▼
                    [Supervisor / Router]
                               │
            ┌──────────────────┴──────────────────┐
            ▼ (Discovery: GET /hub/v1/agents)     ▼ (Inspect Capabilities)
     [Agent Card: Go Expert]               [Agent Card: Python Viz]
            │                                     │
            └───────────────┬─────────────────────┘
                            ▼ (Route to Best Match)
                     [Specialized Worker]
                            │ (Instant ACK + Result)
                            ▼
                    [Supervisor Review]
                            │ (Final Synthesis)
                            ▼
                    [User / Initiator]

Key Features:
- Zero hardcoding of peer addresses: dynamic discovery via Hub Agent Cards.
- Dynamic capability matching (`required_capabilities` filter).
- Supervisor orchestrates execution, verifies completion, and handles fallbacks.

Usage:
  python3 03_supervisor_router.py --demo
"""

import argparse
import os
import sys
import time
from typing import Dict, List, Any, Optional

from a2a_envelope import Envelope
from a2a_pattern_client import PatternHubClient


def simulate_supervisor_router(hub_url: str, shared_key: str = None):
    print("=" * 65)
    print(" 888a2a-lite Pattern 03: Supervisor / Dynamic Router")
    print(f" Hub: {hub_url}")
    print("=" * 65)

    # 1. Register Supervisor and Multiple Domain Specialist Agents
    print("\n[*] Initializing Supervisor & Specialist Agents...")
    supervisor = PatternHubClient(hub_url=hub_url, shared_key=shared_key)
    supervisor.register("Supervisor-Lead", capabilities=["orchestration/supervisor", "router/lead"])
    print(f"  [✓] Supervisor registered: {supervisor.agent_id}")

    go_expert = PatternHubClient(hub_url=hub_url, shared_key=shared_key)
    go_expert.register("Go-Security-Auditor", capabilities=["lang/go", "security/audit"])
    print(f"  [✓] Go Expert registered:  {go_expert.agent_id} (Capabilities: lang/go, security/audit)")

    py_expert = PatternHubClient(hub_url=hub_url, shared_key=shared_key)
    py_expert.register("Python-Data-Scientist", capabilities=["lang/python", "data/visualization"])
    print(f"  [✓] Python Expert registered: {py_expert.agent_id} (Capabilities: lang/python, data/visualization)")

    # 2. Supervisor Discovers Available Peers via Hub Agent Cards (Online Only)
    print("\n[Step 1] Supervisor querying online Agent Cards from Hub (GET /hub/v1/agents?state=online)...")
    time.sleep(1.0)
    online_agents = supervisor.list_agents(online_only=True)
    print(f"  [✓] Discovered {len(online_agents)} online peer agents on Hub.")

    # Helper router logic: match required capabilities against online agents
    def route_task(required_caps: List[str]) -> Optional[Dict[str, Any]]:
        for ag in online_agents:
            if ag.get("agentId") == supervisor.agent_id:
                continue
            # Ensure agent is online
            st = (
                ag.get("presence", {}).get("state")
                or ag.get("status")
                or ag.get("state")
                or ""
            ).upper()
            if st not in ("ONLINE", "ACTIVE"):
                continue
            caps = ag.get("capabilities", [])
            if all(c in caps for c in required_caps):
                return ag
        return None

    # 3. Simulate Incoming User Tasks and Dynamic Routing
    user_requests = [
        {
            "id": "req-01",
            "desc": "請審查這段 Go 併發資料讀寫是否存在 Data Race 隱患",
            "required_capabilities": ["lang/go", "security/audit"],
            "mock_client": go_expert,
            "mock_output": "審查完畢：發現未加鎖的 map 併發存取，已修正為 sync.Map。",
        },
        {
            "id": "req-02",
            "desc": "請用 Python 繪製最近一季比特幣收盤價的移動平均線 (SMA 20/60)",
            "required_capabilities": ["lang/python", "data/visualization"],
            "mock_client": py_expert,
            "mock_output": "繪圖完畢：已生成 Matplotlib 折線圖與移動平均線交叉點信號。",
        },
    ]

    print("\n[Step 2] Supervisor analyzing task requirements and dynamic routing...")
    for req in user_requests:
        print(f"\n--- Processing Task: '{req['desc']}' ---")
        req_caps = req["required_capabilities"]
        matched_agent = route_task(req_caps)

        if not matched_agent:
            print(f"  [!] No online agent satisfies capabilities: {req_caps}. Skipping.")
            continue

        target_id = matched_agent.get("agentId")
        target_name = matched_agent.get("displayName")
        print(f"  [✓] Router matched best agent: '{target_name}' ({target_id}) for {req_caps}")

        # Send Task via Structured Envelope
        workflow_id = f"wf-{req['id']}-{time.time_ns()}"
        corr_id = f"corr-{req['id']}-{time.time_ns()}"
        supervisor.create_workflow(
            workflow_id=workflow_id,
            flow_type="supervisor",
            expected_steps=1,
            join_policy="ALL_SUCCESS",
            retry_limit=1,
        )
        dispatch_env = Envelope(
            workflow_id=workflow_id,
            correlation_id=corr_id,
            flow_type="supervisor",
            step_id=f"delegated_{req['id']}",
            role="supervisor",
            required_capabilities=req_caps,
            terminal=False,
            payload={"request": req["desc"], "supervisor_id": supervisor.agent_id}
        )

        sent = supervisor.send_envelope(target_id, dispatch_env)
        supervisor.register_workflow_attempt(workflow_id, dispatch_env.step_id, target_id, sent["taskId"])
        print(f"  [➔] Dispatched envelope to {target_name}")

        # Worker Receives, Instant ACKs, and Delivers Result
        worker_client: PatternHubClient = req["mock_client"]
        time.sleep(0.8)
        w_items = worker_client.poll_inbox()
        if w_items:
            w_item = w_items[0]
            w_seq = w_item.get("sequence")
            # Instant ACK on Ingest (<50ms)
            worker_client.ack_task(w_seq)
            worker_client.report_workflow_outcome(workflow_id, dispatch_env.step_id, 1, "WORKING")
            print(f"  [✓] Worker '{target_name}' instant ACK sent for seq #{w_seq}")

            w_env = Envelope.from_json(w_item.get("message", ""))
            result_env = w_env.next_step(
                next_step_id=f"result_{req['id']}",
                role="worker",
                payload={"result": req["mock_output"]},
                terminal=False
            )
            worker_client.report_workflow_outcome(
                workflow_id,
                dispatch_env.step_id,
                1,
                "COMPLETED",
                result=req["mock_output"],
            )
            worker_client.send_envelope(supervisor.agent_id, result_env)
            print(f"  [✓] Worker '{target_name}' computed result and returned to Supervisor.")

        # Supervisor collects and verifies worker result
        sup_envs, _ = supervisor.collect_envelopes(correlation_id=corr_id, expected_count=1, timeout_seconds=5.0)
        if sup_envs:
            res_payload = sup_envs[0].payload
            print(f"  🎉 Supervisor verified output from {target_name}: {res_payload.get('result')}")
        print(f"  Hub workflow state: {supervisor.get_workflow(workflow_id)['state']}")

    print("\n" + "=" * 60)
    print(" [✓] Supervisor dynamic routing demonstration complete!")
    print("=" * 60)


def main():
    parser = argparse.ArgumentParser(description="A2A Pattern 03: Supervisor / Dynamic Router")
    parser.add_argument("--hub", default=os.getenv("A2A888_HUB_URL", "http://127.0.0.1:8080"), help="Hub URL")
    parser.add_argument("--key", default=os.getenv("A2A888_HUB_SHARED_KEY"), help="Shared Hub Key (if private circle)")
    parser.add_argument("--demo", action="store_true", help="Run end-to-end supervisor router demonstration")
    args = parser.parse_args()

    if not args.demo:
        parser.error("--demo is required to register example Agents and send tasks")

    simulate_supervisor_router(hub_url=args.hub, shared_key=args.key)


if __name__ == "__main__":
    main()
