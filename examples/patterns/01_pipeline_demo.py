#!/usr/bin/env python3
"""
Pattern 01: 串行流水線接力 (Sequential / Pipeline)
Demonstrates multi-stage sequential agent handoff over 888a2a-lite.

Workflow Stages:
  [Initiator]
      │ (Envelope step_id: "draft")
      ▼
  [Agent 1: Drafter] -> Instant ACK -> Generates draft
      │ (Envelope step_id: "audit")
      ▼
  [Agent 2: Auditor] -> Instant ACK -> Adds security & quality check
      │ (Envelope step_id: "publish", terminal: True)
      ▼
  [Initiator / User] -> Receives final result (Workflow Complete)

Usage:
  # Run end-to-end simulated 3-agent pipeline on Hub:
  python3 01_pipeline_demo.py --demo

  # Connect to specific Hub:
  python3 01_pipeline_demo.py --demo --hub https://a2a.david888.com --key my-team-key
"""

import argparse
import os
import sys
import time
from typing import Dict, Any

from a2a_envelope import Envelope
from a2a_pattern_client import PatternHubClient


def simulate_pipeline(hub_url: str, shared_key: str = None):
    print("=" * 65)
    print(" 888a2a-lite Pattern 01: Sequential Pipeline Demonstration")
    print(f" Hub: {hub_url}")
    print("=" * 65)

    # 1. Register 3 distinct pipeline participants
    print("\n[*] Initializing Pipeline Agents...")
    initiator = PatternHubClient(hub_url=hub_url, shared_key=shared_key)
    initiator.register("Pipeline-Initiator", capabilities=["pipeline/initiator"])
    print(f"  [✓] Initiator registered: {initiator.agent_id}")

    drafter = PatternHubClient(hub_url=hub_url, shared_key=shared_key)
    drafter.register("Pipeline-Drafter", capabilities=["code/generate", "pipeline/drafter"])
    print(f"  [✓] Drafter registered:   {drafter.agent_id}")

    auditor = PatternHubClient(hub_url=hub_url, shared_key=shared_key)
    auditor.register("Pipeline-Auditor", capabilities=["code/audit", "pipeline/auditor"])
    print(f"  [✓] Auditor registered:   {auditor.agent_id}")

    # 2. Initiator creates workflow envelope and dispatches to Drafter
    workflow_id = f"wf-pipeline-{time.time_ns()}"
    correlation_id = f"corr-{time.time_ns()}"
    initiator.create_workflow(
        workflow_id=workflow_id,
        flow_type="pipeline",
        expected_steps=2,
        join_policy="ALL_SUCCESS",
        retry_limit=1,
    )

    initial_envelope = Envelope(
        workflow_id=workflow_id,
        correlation_id=correlation_id,
        flow_type="pipeline",
        step_id="draft",
        role="drafter",
        terminal=False,
        payload={
            "instruction": "編寫一個 Go 語言的 LRU Cache 結構體，包含 Get 與 Put 方法。",
            "target_auditor": auditor.agent_id,
            "reply_to_agent": initiator.agent_id,
        }
    )

    print(f"\n[Step 1] Initiator dispatching initial task to Drafter ({drafter.agent_id})...")
    draft_task = initiator.send_envelope(drafter.agent_id, initial_envelope)
    initiator.register_workflow_attempt(workflow_id, "draft", drafter.agent_id, draft_task["taskId"])

    # 3. Drafter polls inbox, ACKs immediately, processes, and hands off to Auditor
    print("\n[Step 2] Drafter receiving task...")
    time.sleep(1.0)
    items = drafter.poll_inbox()
    if not items:
        print("[!] Drafter received no items. Retrying...")
        time.sleep(1.5)
        items = drafter.poll_inbox()

    if not items:
        print("[!] Pipeline failed: Drafter inbox empty")
        return

    item = items[0]
    seq = item.get("sequence")
    drafter.ack_task(seq)
    drafter.report_workflow_outcome(workflow_id, "draft", 1, "WORKING")
    print(f"  [✓] Drafter instant ACK sent for sequence #{seq}")

    req_env = Envelope.from_json(item.get("message", ""))
    print(f"  [*] Drafter working on: {req_env.payload.get('instruction')}")

    # Simulate LLM drafting
    draft_code = (
        "type LRUCache struct {\n"
        "    capacity int\n"
        "    cache    map[int]*list.Element\n"
        "    list     *list.List\n"
        "}\n"
        "// Implements Get and Put with O(1) complexity"
    )

    audit_envelope = req_env.next_step(
        next_step_id="audit",
        role="auditor",
        payload={
            "draft_code": draft_code,
            "reply_to_agent": initiator.agent_id,
        }
    )
    print(f"  [✓] Drafter completed draft. Forwarding to Auditor ({auditor.agent_id})...")
    drafter.report_workflow_outcome(workflow_id, "draft", 1, "COMPLETED", result="draft generated")
    audit_task = drafter.send_envelope(auditor.agent_id, audit_envelope)
    drafter.register_workflow_attempt(workflow_id, "audit", auditor.agent_id, audit_task["taskId"])

    # 4. Auditor polls inbox, ACKs immediately, reviews, sets terminal=True, returns to Initiator
    print("\n[Step 3] Auditor receiving draft...")
    time.sleep(1.0)
    audit_items = auditor.poll_inbox()
    if not audit_items:
        time.sleep(1.5)
        audit_items = auditor.poll_inbox()

    if not audit_items:
        print("[!] Pipeline failed: Auditor inbox empty")
        return

    audit_item = audit_items[0]
    audit_seq = audit_item.get("sequence")
    auditor.ack_task(audit_seq)
    auditor.report_workflow_outcome(workflow_id, "audit", 1, "WORKING")
    print(f"  [✓] Auditor instant ACK sent for sequence #{audit_seq}")

    auditor_env = Envelope.from_json(audit_item.get("message", ""))
    print(f"  [*] Auditor reviewing code draft...")

    # Simulate security & concurrency review
    audit_report = {
        "status": "APPROVED",
        "concurrency_warning": "建議加入 sync.RWMutex 以防範並發競態 (Race Condition)",
        "final_code": (
            "type LRUCache struct {\n"
            "    mu       sync.RWMutex\n"
            "    capacity int\n"
            "    cache    map[int]*list.Element\n"
            "    list     *list.List\n"
            "}\n"
        )
    }

    final_envelope = auditor_env.next_step(
        next_step_id="completed",
        role="publisher",
        payload=audit_report,
        terminal=True  # Terminal flag triggers Anti-Echo Guard
    )
    print(f"  [✓] Auditor review passed. Returning final result to Initiator ({initiator.agent_id})...")
    auditor.report_workflow_outcome(workflow_id, "audit", 1, "COMPLETED", result="audit approved")
    auditor.send_envelope(initiator.agent_id, final_envelope)

    # 5. Initiator collects final result
    print("\n[Step 4] Initiator awaiting final pipeline delivery...")
    final_envs, _ = initiator.collect_envelopes(correlation_id=correlation_id, expected_count=1, timeout_seconds=10.0)

    if final_envs:
        final_res = final_envs[0]
        print(f"\n🎉 [Pipeline Succeeded!]")
        print(f"  Workflow ID:    {final_res.workflow_id}")
        print(f"  Correlation ID: {final_res.correlation_id}")
        print(f"  Step ID:        {final_res.step_id}")
        print(f"  Terminal State: {final_res.terminal} (Anti-Echo Guard Activated)")
        print(f"  Final Payload:  {final_res.payload}")
    else:
        print("[!] Initiator did not receive final envelope within timeout.")

    workflow = initiator.get_workflow(workflow_id)
    print(f"  Hub Workflow:   {workflow['state']} ({workflow['counts']['completed']}/{workflow['counts']['expected']} steps)")


def main():
    parser = argparse.ArgumentParser(description="A2A Pattern 01: Sequential Pipeline")
    parser.add_argument("--hub", default=os.getenv("A2A888_HUB_URL", "http://127.0.0.1:8080"), help="Hub URL")
    parser.add_argument("--key", default=os.getenv("A2A888_HUB_SHARED_KEY"), help="Shared Hub Key (if private circle)")
    parser.add_argument("--demo", action="store_true", help="Run end-to-end 3-agent pipeline demonstration")
    args = parser.parse_args()

    if not args.demo:
        parser.error("--demo is required to register example Agents and send tasks")

    simulate_pipeline(hub_url=args.hub, shared_key=args.key)


if __name__ == "__main__":
    main()
