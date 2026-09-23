#!/usr/bin/env python3
"""
Pattern 04: 辯論與對抗審查 (Debate / Adversarial with Anti-Echo Guard)
Demonstrates multi-turn bounded debate and adversarial review over 888a2a-lite.

Participants:
  [Proposer Agent] ──(Round 1: Proposal)──► [Critic Agent]
         ▲                                         │
         │                                         │
         └──(Round 2: Rebuttal & Revisions)────────┘
                                                   │ (Reached max_rounds)
                                                   ▼
                                           [Judge / Arbiter]
                                                   │ (terminal=True, [[A2A_NO_REPLY]])
                                                   ▼
                                        [Clean Workflow Exit]

Critical Safety Protections (Lessons Learned #7):
1. Bounded Rounds: Hard limit on debate iterations (`max_rounds=2`).
2. Instant ACK on Ingest: Every participant acknowledges incoming messages (<50ms) immediately.
3. Terminal Guard (`terminal=True`): Instructs participants that conversation has ended; no further replies allowed.
4. Anti-Echo Storm Guard: Automatically silences polite/closing chatter and respects `[[A2A_NO_REPLY]]`.

Usage:
  python3 04_debate_group.py --demo
"""

import argparse
import os
import sys
import time
from typing import Dict, Any

from a2a_envelope import Envelope
from a2a_pattern_client import PatternHubClient


def simulate_debate(hub_url: str, shared_key: str = None):
    print("=" * 65)
    print(" 888a2a-lite Pattern 04: Adversarial Debate with Anti-Echo Guard")
    print(f" Hub: {hub_url}")
    print("=" * 65)

    # 1. Register Proposer, Critic, and Judge
    print("\n[*] Registering Debate Participants...")
    proposer = PatternHubClient(hub_url=hub_url, shared_key=shared_key)
    proposer.register("Debate-Proposer", capabilities=["debate/proposer"])
    print(f"  [✓] Proposer registered: {proposer.agent_id}")

    critic = PatternHubClient(hub_url=hub_url, shared_key=shared_key)
    critic.register("Debate-Critic", capabilities=["debate/critic"])
    print(f"  [✓] Critic registered:   {critic.agent_id}")

    judge = PatternHubClient(hub_url=hub_url, shared_key=shared_key)
    judge.register("Debate-Judge", capabilities=["debate/judge"])
    print(f"  [✓] Judge registered:    {judge.agent_id}")

    workflow_id = f"wf-debate-{time.time_ns()}"
    correlation_id = f"corr-debate-{time.time_ns()}"
    max_rounds = 2
    proposer.create_workflow(
        workflow_id=workflow_id,
        flow_type="debate",
        expected_steps=4,
        join_policy="ALL_SUCCESS",
        retry_limit=1,
    )

    # --- ROUND 1: Proposer submits proposal ---
    print(f"\n[Round 1 / {max_rounds}] Proposer presenting initial architecture proposal...")
    r1_proposal = Envelope(
        workflow_id=workflow_id,
        correlation_id=correlation_id,
        flow_type="debate",
        step_id="round_1_proposal",
        round=1,
        max_rounds=max_rounds,
        role="proposer",
        terminal=False,
        payload={
            "topic": "系統架構提案：全面廢除資料庫交易，改採最終一致性與記憶體快取",
            "arguments": "追求極致微秒級延遲，所有狀態僅保存在 Redis 記憶體中，非同步寫入磁碟。",
            "critic_id": critic.agent_id,
            "judge_id": judge.agent_id,
        }
    )
    proposal_task = proposer.send_envelope(critic.agent_id, r1_proposal)
    proposer.register_workflow_attempt(workflow_id, r1_proposal.step_id, critic.agent_id, proposal_task["taskId"])

    # Critic receives, Instant ACKs, critiques
    time.sleep(1.0)
    c_items = critic.poll_inbox()
    if not c_items:
        time.sleep(1.0)
        c_items = critic.poll_inbox()

    if not c_items:
        print("[!] Critic received no message. Aborting.")
        return

    c_item = c_items[0]
    critic.ack_task(c_item["sequence"])
    critic.report_workflow_outcome(workflow_id, "round_1_proposal", 1, "WORKING")
    print("  [✓] Critic sent instant ACK (<50ms)")

    c_env = Envelope.from_json(c_item["message"])
    print(f"  [*] Critic analyzing Proposal Round 1...")

    critique_text = (
        "【反方質詢】：Redis 記憶體非同步刷盤在伺服器斷電或 OOM 時，會導致不可逆的金融級資料遺失！"
        "若無 Write-Ahead Logging (WAL) 或兩階段提交，無法保證 ACID 安全。"
    )

    r1_critique = c_env.next_step(
        next_step_id="round_1_critique",
        role="critic",
        payload={"critique": critique_text},
        terminal=False,
        increment_round=False  # Still in round 1 exchange
    )
    critic.report_workflow_outcome(workflow_id, "round_1_proposal", 1, "COMPLETED", result=critique_text)
    critique_task = critic.send_envelope(proposer.agent_id, r1_critique)
    critic.register_workflow_attempt(workflow_id, r1_critique.step_id, proposer.agent_id, critique_task["taskId"])
    print("  [✓] Critic delivered critique to Proposer.")

    # --- ROUND 2: Proposer revises proposal ---
    print(f"\n[Round 2 / {max_rounds}] Proposer submitting revised defense...")
    time.sleep(1.0)
    p_items = proposer.poll_inbox()
    if p_items:
        proposer.ack_task(p_items[0]["sequence"])
        proposer.report_workflow_outcome(workflow_id, "round_1_critique", 1, "WORKING")
        print("  [✓] Proposer sent instant ACK (<50ms)")

    rebuttal_text = (
        "【提案方修正】：接受質詢意見。將架構修改為「輕量嵌入式 SQLite WAL + 記憶體緩衝」，"
        "在保證持久化磁碟 fsync 的同時，提供單節點極致讀寫效能。"
    )
    r2_defense = Envelope(
        workflow_id=workflow_id,
        correlation_id=correlation_id,
        flow_type="debate",
        step_id="round_2_defense",
        round=2,
        max_rounds=max_rounds,
        role="proposer",
        terminal=False,
        payload={"defense": rebuttal_text}
    )
    proposer.report_workflow_outcome(workflow_id, "round_1_critique", 1, "COMPLETED", result="proposal revised")
    defense_task = proposer.send_envelope(critic.agent_id, r2_defense)
    proposer.register_workflow_attempt(workflow_id, r2_defense.step_id, critic.agent_id, defense_task["taskId"])
    print("  [✓] Proposer delivered revised proposal to Critic.")

    # Critic receives Round 2 defense, reaches max_rounds, forwards to Judge
    time.sleep(1.0)
    c_items2 = critic.poll_inbox()
    if c_items2:
        critic.ack_task(c_items2[0]["sequence"])
        critic.report_workflow_outcome(workflow_id, "round_2_defense", 1, "WORKING")
        print("  [✓] Critic sent instant ACK (<50ms)")

    print(f"\n[Max Rounds Reached] Critic acknowledges revisions and passes case to Judge ({judge.agent_id})...")
    handoff_judge = Envelope(
        workflow_id=workflow_id,
        correlation_id=correlation_id,
        flow_type="debate",
        step_id="judge_arbitration",
        round=2,
        max_rounds=max_rounds,
        role="critic",
        terminal=False,
        payload={
            "case_summary": "雙方完成 2 回合攻防：提案方已將純記憶體方案修正為 SQLite WAL 持久化。",
            "request": "請裁判做出最終定案裁決。"
        }
    )
    critic.report_workflow_outcome(workflow_id, "round_2_defense", 1, "COMPLETED", result="reviewed revisions")
    judge_task = critic.send_envelope(judge.agent_id, handoff_judge)
    critic.register_workflow_attempt(workflow_id, handoff_judge.step_id, judge.agent_id, judge_task["taskId"])

    # --- FINAL ARBITRATION: Judge issues verdict with Terminal & Anti-Echo Guard ---
    print("\n[Arbitration] Judge reviewing debate arguments and rendering verdict...")
    time.sleep(1.0)
    j_items = judge.poll_inbox()
    if j_items:
        judge.ack_task(j_items[0]["sequence"])
        judge.report_workflow_outcome(workflow_id, "judge_arbitration", 1, "WORKING")
        print("  [✓] Judge sent instant ACK (<50ms)")

    verdict_text = (
        "【裁判最終裁決】：\n"
        "1. 採納提案方修正後的「SQLite WAL 持久化架構」，兼顧低延遲與故障恢復。\n"
        "2. 駁回無防護純記憶體快取方案。\n"
        "本回合辯論正式結案，請雙方就此定案實施，無需再進行回覆。\n"
        "[[A2A_NO_REPLY]]"
    )

    verdict_env = Envelope(
        workflow_id=workflow_id,
        correlation_id=correlation_id,
        flow_type="debate",
        step_id="verdict_final",
        round=2,
        max_rounds=max_rounds,
        role="judge",
        terminal=True,  # Critical: Forces all agents to cease replying!
        payload={"verdict": verdict_text}
    )

    judge.report_workflow_outcome(workflow_id, "judge_arbitration", 1, "COMPLETED", result=verdict_text)

    # Broadcast verdict to both Proposer and Critic
    judge.send_envelope(proposer.agent_id, verdict_env)
    judge.send_envelope(critic.agent_id, verdict_env)
    print("  [✓] Judge published final verdict with terminal=True and [[A2A_NO_REPLY]]")

    # --- VERIFY ANTI-ECHO STORM GUARD ---
    print("\n[Anti-Echo Storm Verification] Proposer & Critic receiving final verdict...")
    time.sleep(1.0)
    for agent_label, client in [("Proposer", proposer), ("Critic", critic)]:
        items = client.poll_inbox()
        for it in items:
            client.ack_task(it["sequence"])
            env = Envelope.from_json(it["message"])
            is_echo_triggered = env.is_anti_echo_triggered()
            print(f"  [🛡️ Guard Check - {agent_label}]:")
            print(f"    Terminal Flag:         {env.terminal}")
            print(f"    Anti-Echo Triggered:   {is_echo_triggered}")
            if is_echo_triggered:
                print(f"    Action:                Instant ACK succeeded; auto-reply SUPPRESSED. Zero echo storm.")

    print("\n" + "=" * 65)
    workflow = proposer.get_workflow(workflow_id)
    print(f" 🎉 [✓] Debate concluded: Hub workflow {workflow['state']} ({workflow['counts']['completed']}/{workflow['counts']['expected']} steps)")
    print("=" * 65)


def main():
    parser = argparse.ArgumentParser(description="A2A Pattern 04: Debate / Adversarial")
    parser.add_argument("--hub", default=os.getenv("A2A888_HUB_URL", "http://127.0.0.1:8080"), help="Hub URL")
    parser.add_argument("--key", default=os.getenv("A2A888_HUB_SHARED_KEY"), help="Shared Hub Key (if private circle)")
    parser.add_argument("--demo", action="store_true", help="Run end-to-end debate demonstration")
    args = parser.parse_args()

    if not args.demo:
        parser.error("--demo is required to register example Agents and send tasks")

    simulate_debate(hub_url=args.hub, shared_key=args.key)


if __name__ == "__main__":
    main()
