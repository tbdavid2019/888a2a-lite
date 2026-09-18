#!/usr/bin/env python3
"""
Pattern 02: 並行派工與聚合 (Parallel Fan-out / Fan-in Aggregator)
Demonstrates broadcasting subtasks to multiple agents in parallel,
and collecting/synthesizing results with quorum and deadline handling.

Architecture:
                      [Aggregator / Coordinator]
                                   │
              ┌────────────────────┼────────────────────┐
              ▼ (Task 1)           ▼ (Task 2)           ▼ (Task 3)
         [Worker A: WAL]     [Worker B: PG]       [Worker C: Append]
              │                    │                    │
              └────────────────────┼────────────────────┘
                                   ▼ (Fan-in Collection)
                      [Aggregator: Synthesis Report]

Key Features:
- Fan-out with shared workflow_id and correlation_id.
- Independent asynchronous workers with Instant ACK on ingest.
- Aggregator deadline & quorum tolerance (tolerates slow or offline nodes).
- Final consolidated summary marked with terminal: True.

Usage:
  python3 02_parallel_fanout.py --demo
"""

import argparse
import os
import sys
import time
from typing import Dict, List, Any

from a2a_envelope import Envelope
from a2a_pattern_client import PatternHubClient


def simulate_parallel_fanout(hub_url: str, shared_key: str = None):
    print("=" * 65)
    print(" 888a2a-lite Pattern 02: Parallel Fan-out / Fan-in Aggregator")
    print(f" Hub: {hub_url}")
    print("=" * 65)

    # 1. Register Aggregator and 3 Specialized Worker Agents
    print("\n[*] Initializing Aggregator & Worker Agents...")
    aggregator = PatternHubClient(hub_url=hub_url, shared_key=shared_key)
    aggregator.register("Fanout-Aggregator", capabilities=["aggregate/synthesis"])
    print(f"  [✓] Aggregator registered: {aggregator.agent_id}")

    subtopics = [
        ("Worker-WAL", "評估 SQLite WAL 的並發與讀寫鎖特性", "worker/wal"),
        ("Worker-Postgres", "評估 PostgreSQL 的網路開銷與外掛相依性", "worker/postgres"),
        ("Worker-AppendLog", "評估本地 Append-only Log 的極限寫入輸送量", "worker/appendlog"),
    ]

    workers: List[Dict[str, Any]] = []
    for name, prompt, cap in subtopics:
        w_client = PatternHubClient(hub_url=hub_url, shared_key=shared_key)
        w_client.register(f"Fanout-{name}", capabilities=[cap])
        workers.append({
            "name": name,
            "prompt": prompt,
            "client": w_client,
            "agent_id": w_client.agent_id,
        })
        print(f"  [✓] Worker '{name}' registered: {w_client.agent_id}")

    # 2. Aggregator Fans Out Tasks to All Workers in Parallel
    workflow_id = f"wf-fanout-{int(time.time())}"
    correlation_id = f"corr-{int(time.time())}"
    print(f"\n[Step 1] Aggregator fanning out subtasks (Workflow: {workflow_id}, Correlation: {correlation_id})...")

    for w in workers:
        env = Envelope(
            workflow_id=workflow_id,
            correlation_id=correlation_id,
            flow_type="parallel",
            step_id=f"analyze_{w['name']}",
            role="aggregator",
            terminal=False,
            payload={
                "topic": w["prompt"],
                "reply_to": aggregator.agent_id,
            }
        )
        aggregator.send_envelope(w["agent_id"], env)
        print(f"  [➔] Dispatched subtask to {w['name']} ({w['agent_id']})")

    # 3. Workers Process Simultaneously in Parallel Threads
    print("\n[Step 2] Workers processing in parallel (concurrent.futures.ThreadPoolExecutor)...")

    simulated_results = {
        "Worker-WAL": "SQLite WAL 支援單寫多讀無阻塞，磁碟耐久性優異，非常適配單節點邊緣 Agent Hub。",
        "Worker-Postgres": "PostgreSQL 功能完整但需維運連線池與網路 TCP 往返，對輕量獨立 Hub 存在過重依賴。",
        "Worker-AppendLog": "Append-only Log 寫入吞吐量破百萬 IOPS，但缺乏查詢索引，需要手動維護壓縮快照。",
    }

    def worker_thread_job(w_entry: Dict[str, Any]) -> bool:
        client = w_entry["client"]
        w_name = w_entry["name"]
        for _ in range(5):
            items = client.poll_inbox()
            if items:
                item = items[0]
                seq = item.get("sequence")
                # Instant ACK on Ingest (<50ms)
                client.ack_task(seq)

                in_env = Envelope.from_json(item.get("message", ""))
                analysis_text = simulated_results.get(w_name, "分析完成")

                reply_env = in_env.next_step(
                    next_step_id=f"result_{w_name}",
                    role="worker",
                    payload={"worker": w_name, "analysis": analysis_text},
                    terminal=False  # Still part of collection phase
                )
                client.send_envelope(aggregator.agent_id, reply_env)
                print(f"  [✓] {w_name} completed concurrently and replied to Aggregator.")
                return True
            time.sleep(0.5)
        print(f"  [!] {w_name} timed out waiting for task.")
        return False

    import concurrent.futures
    with concurrent.futures.ThreadPoolExecutor(max_workers=len(workers)) as executor:
        futures = [executor.submit(worker_thread_job, w) for w in workers]
        concurrent.futures.wait(futures)

    # 4. Aggregator Fans In Results with Quorum and Timeout
    print(f"\n[Step 3] Aggregator collecting fan-in responses (Quorum: 2/3, Timeout: 10s)...")
    collected, _ = aggregator.collect_envelopes(
        correlation_id=correlation_id,
        expected_count=len(workers),
        timeout_seconds=10.0,
        min_quorum=2
    )

    print(f"\n[Step 4] Aggregation Summary:")
    print(f"  Received {len(collected)} of {len(workers)} expected worker responses.")

    synthesis = []
    for c in collected:
        w_data = c.payload
        if isinstance(w_data, dict):
            synthesis.append(f"- [{w_data.get('worker')}]: {w_data.get('analysis')}")

    synthesis_report = "\n".join(synthesis)
    print("\n" + "=" * 50)
    print("📊 Final Consolidated Architecture Decision Report:")
    print("=" * 50)
    print(synthesis_report)
    print("=" * 50)

    # 5. Final Terminal Envelope (Completed)
    final_env = Envelope(
        workflow_id=workflow_id,
        correlation_id=correlation_id,
        flow_type="parallel",
        step_id="synthesis_complete",
        role="aggregator",
        terminal=True,  # Terminates workflow and activates Anti-Echo Guard
        payload={"report": synthesis_report, "total_workers_responded": len(collected)}
    )
    print(f"\n[✓] Workflow successfully concluded. Terminal flag set to: {final_env.terminal}")


def main():
    parser = argparse.ArgumentParser(description="A2A Pattern 02: Parallel Fan-out / Fan-in")
    parser.add_argument("--hub", default=os.getenv("A2A888_HUB_URL", "https://a2a.david888.com"), help="Hub URL")
    parser.add_argument("--key", default=os.getenv("A2A888_HUB_SHARED_KEY"), help="Shared Hub Key (if private circle)")
    parser.add_argument("--demo", action="store_true", help="Run end-to-end fan-out/fan-in demonstration")
    args = parser.parse_args()

    simulate_parallel_fanout(hub_url=args.hub, shared_key=args.key)


if __name__ == "__main__":
    main()
