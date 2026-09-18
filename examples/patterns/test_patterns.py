#!/usr/bin/env python3
"""
Unit tests for A2A Collaboration Patterns & Envelope.

Run in CI (GitHub Actions):
  python3 -m unittest discover -s examples/patterns -p 'test_*.py'
"""

import json
import os
import sys
import unittest

# Ensure local module directory is in Python path
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from a2a_envelope import Envelope
from a2a_pattern_client import PatternHubClient


class TestA2AEnvelope(unittest.TestCase):

    def test_default_envelope_creation(self):
        env = Envelope()
        self.assertTrue(env.workflow_id.startswith("wf-"))
        self.assertTrue(env.correlation_id.startswith("corr-"))
        self.assertEqual(env.flow_type, "pipeline")
        self.assertEqual(env.round, 1)
        self.assertEqual(env.max_rounds, 3)
        self.assertFalse(env.terminal)

    def test_json_roundtrip(self):
        env = Envelope(
            workflow_id="wf-test-1",
            correlation_id="corr-test-1",
            flow_type="debate",
            step_id="critique",
            round=2,
            max_rounds=3,
            role="critic",
            required_capabilities=["code/audit"],
            terminal=False,
            payload={"code": "func main() {}"},
            metadata={"source": "test"},
        )
        json_str = env.to_json()
        loaded = Envelope.from_json(json_str)

        self.assertEqual(loaded.workflow_id, "wf-test-1")
        self.assertEqual(loaded.correlation_id, "corr-test-1")
        self.assertEqual(loaded.flow_type, "debate")
        self.assertEqual(loaded.step_id, "critique")
        self.assertEqual(loaded.round, 2)
        self.assertEqual(loaded.max_rounds, 3)
        self.assertEqual(loaded.role, "critic")
        self.assertEqual(loaded.required_capabilities, ["code/audit"])
        self.assertFalse(loaded.terminal)
        self.assertEqual(loaded.payload, {"code": "func main() {}"})
        self.assertEqual(loaded.metadata, {"source": "test"})

    def test_fallback_parsing_for_legacy_plain_text(self):
        legacy_text = "Hello from legacy agent without envelope"
        parsed = Envelope.from_json(legacy_text)
        self.assertEqual(parsed.payload, {"text": legacy_text})
        self.assertFalse(parsed.terminal)
        self.assertEqual(parsed.role, "worker")

    def test_malformed_json_raises_error(self):
        # Starts with { but is corrupted JSON
        corrupted_json = '{"workflow_id": "wf-1", "flow_type": '
        with self.assertRaises(ValueError):
            Envelope.from_json(corrupted_json)

    def test_malformed_envelope_dict_raises_error(self):
        # Has envelope markers but invalid values (e.g. invalid round)
        invalid_envelope_json = json.dumps({
            "workflow_id": "wf-1",
            "correlation_id": "corr-1",
            "flow_type": "invalid_type",
            "step_id": "step-1",
            "round": -5,
        })
        with self.assertRaises(ValueError):
            Envelope.from_json(invalid_envelope_json)

    def test_envelope_rejects_invalid_field_types(self):
        invalid_envelope_json = json.dumps({
            "workflow_id": "wf-1",
            "correlation_id": "corr-1",
            "flow_type": "pipeline",
            "step_id": "step-1",
            "round": "2",
            "terminal": "false",
        })
        with self.assertRaises(ValueError):
            Envelope.from_json(invalid_envelope_json)

    def test_partial_envelope_does_not_fallback_to_legacy_payload(self):
        partial_envelope_json = json.dumps({
            "workflow_id": "wf-1",
            "flow_type": "pipeline",
        })
        with self.assertRaises(ValueError):
            Envelope.from_json(partial_envelope_json)

    def test_validation_errors(self):
        # Unsupported flow_type
        invalid_env = Envelope(flow_type="unsupported_type")
        with self.assertRaises(ValueError):
            invalid_env.to_json()

        # Round exceeds max_rounds without terminal flag
        exceeded_env = Envelope(round=4, max_rounds=3, terminal=False)
        with self.assertRaises(ValueError):
            exceeded_env.to_json()

    def test_next_step_derivation(self):
        initial = Envelope(
            workflow_id="wf-seq",
            correlation_id="corr-seq",
            step_id="step_1",
            round=1,
            max_rounds=2,
            role="drafter",
            payload="draft v1",
        )
        step2 = initial.next_step(
            next_step_id="step_2",
            role="auditor",
            payload="reviewed v1",
            increment_round=True,
        )
        self.assertEqual(step2.workflow_id, "wf-seq")
        self.assertEqual(step2.correlation_id, "corr-seq")
        self.assertEqual(step2.reply_to, "step_1")
        self.assertEqual(step2.step_id, "step_2")
        self.assertEqual(step2.round, 2)
        self.assertEqual(step2.role, "auditor")
        self.assertEqual(step2.metadata["parent_step"], "step_1")
        # Round 2 is the final active round; the workflow may still hand off
        # to a judge or another final-stage worker.
        self.assertFalse(step2.terminal)

        step3 = step2.next_step(
            next_step_id="step_3",
            role="publisher",
            payload="final result",
            increment_round=True,
        )
        self.assertTrue(step3.terminal)
        self.assertEqual(step3.metadata["parent_step"], "step_2")

    def test_anti_echo_guard_triggers(self):
        # 1. Terminal flag explicitly set
        term_env = Envelope(terminal=True)
        self.assertTrue(term_env.is_anti_echo_triggered())

        # 2. Round strictly exceeds max_rounds
        exceeded_round_env = Envelope(round=4, max_rounds=3, terminal=True)
        self.assertTrue(exceeded_round_env.is_anti_echo_triggered())

        # 3. Round within max_rounds must NOT trigger guard (allows debate handoff)
        active_debate_env = Envelope(round=2, max_rounds=2, terminal=False)
        self.assertFalse(active_debate_env.is_anti_echo_triggered())

        # 4. Closing keywords in text payload trigger guard
        closing_env = Envelope(
            payload={"text": "收到，收錄完畢，隨時準備好迎接後續任務，辛苦了！"},
            terminal=False,
            round=1,
            max_rounds=3,
        )
        self.assertTrue(closing_env.is_anti_echo_triggered())

        # 5. Explicit [[A2A_NO_REPLY]] marker
        no_reply_env = Envelope(
            payload={"content": "任務處理完成。 [[A2A_NO_REPLY]]"},
            terminal=False,
            round=1,
            max_rounds=3,
        )
        self.assertTrue(no_reply_env.is_anti_echo_triggered())

        # 6. Non-closing normal request with question must NOT trigger guard
        question_env = Envelope(
            payload={"text": "請問這個模組的逾時時間應該設為多少？"},
            terminal=False,
            round=1,
            max_rounds=3,
        )
        self.assertFalse(question_env.is_anti_echo_triggered())


class TestPatternHubClientCollection(unittest.TestCase):

    def test_collect_envelopes_does_not_ack_foreign_remote_messages(self):
        client = PatternHubClient(agent_id="agent-test-1", token="tok-1")
        item_a = {
            "sequence": 1,
            "message": Envelope(correlation_id="corr-A", payload="Task A").to_json(),
        }
        item_b = {
            "sequence": 2,
            "message": Envelope(correlation_id="corr-B", payload="Task B").to_json(),
        }
        client._fetch_remote_inbox = lambda after=0, limit=100: [item_a, item_b]
        acknowledged = []
        client.ack_task = lambda sequence: acknowledged.append(sequence) or True

        collected, _ = client.collect_envelopes(
            correlation_id="corr-A",
            expected_count=2,
            min_quorum=2,
            timeout_seconds=0.01,
        )

        self.assertEqual([env.correlation_id for env in collected], ["corr-A"])
        self.assertEqual(acknowledged, [1])

    def test_collect_envelopes_scans_past_foreign_pages(self):
        client = PatternHubClient(agent_id="agent-test-1", token="tok-1")
        foreign = {
            "sequence": 1,
            "message": Envelope(correlation_id="corr-B", payload="Task B").to_json(),
        }
        target = {
            "sequence": 2,
            "message": Envelope(correlation_id="corr-A", payload="Task A").to_json(),
        }

        def fetch_page(after=0, limit=100):
            return [foreign] if after == 0 else [target]

        client._fetch_remote_inbox = fetch_page
        acknowledged = []
        client.ack_task = lambda sequence: acknowledged.append(sequence) or True

        collected, _ = client.collect_envelopes(
            correlation_id="corr-A",
            expected_count=1,
            timeout_seconds=0.1,
            poll_interval=0,
        )

        self.assertEqual([env.correlation_id for env in collected], ["corr-A"])
        self.assertEqual(acknowledged, [2])


if __name__ == "__main__":
    unittest.main()
