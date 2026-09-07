import importlib.util
import json
import os
import tempfile
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location("a2a_bridge", os.path.join(os.path.dirname(__file__), "a2a_bridge.py"))
bridge = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(bridge)


class DurableBridgeTests(unittest.TestCase):
    def test_closing_filter_only_matches_exact_statement(self):
        self.assertTrue(bridge.is_pure_closing_statement("辛苦了！"))
        self.assertFalse(bridge.is_pure_closing_statement("辛苦了，整理今天的錯誤紀錄"))

    def test_inflight_work_recovers_after_restart(self):
        with tempfile.TemporaryDirectory() as directory:
            path = os.path.join(directory, "queue.db")
            item = {"sequence": 7, "message": "hello", "taskId": "t-7"}
            queue = bridge.DurableWorkQueue(path, scope="hub/agent")
            queue.enqueue(item)
            row = queue.next()
            self.assertEqual(row["state"], "pending")
            restarted = bridge.DurableWorkQueue(path, scope="hub/agent")
            recovered = restarted.next()
            self.assertEqual(json_item(recovered)["sequence"], 7)

    def test_queue_scope_prevents_cross_agent_reuse(self):
        with tempfile.TemporaryDirectory() as directory:
            path = os.path.join(directory, "queue.db")
            bridge.DurableWorkQueue(path, scope="hub/a")
            with self.assertRaises(RuntimeError):
                bridge.DurableWorkQueue(path, scope="hub/b")

    def test_enqueue_happens_before_ack(self):
        class Hub:
            agent_id = "agent-a"
            def ack_task(self, sequence):
                with bridge.sqlite3.connect(queue.path) as db:
                    self.seen = db.execute("SELECT 1 FROM work WHERE sequence=?", (sequence,)).fetchone() is not None
                return True
        with tempfile.TemporaryDirectory() as directory:
            queue = bridge.DurableWorkQueue(os.path.join(directory, "q.db"), scope="hub/agent-a")
            hub = Hub()
            bridge.process_incoming_task(hub, object(), queue, {"sequence": 1, "taskId": "t", "message": "收到"}, "A")
            self.assertTrue(hub.seen)

    def test_ack_failure_keeps_work_without_success_log(self):
        class Hub:
            agent_id = "agent-a"
            def ack_task(self, sequence):
                return None
        with tempfile.TemporaryDirectory() as directory:
            queue = bridge.DurableWorkQueue(os.path.join(directory, "q.db"), scope="hub/agent-a")
            with mock.patch("builtins.print") as printer:
                bridge.process_incoming_task(Hub(), object(), queue,
                                              {"sequence": 3, "taskId": "t", "message": "hello"}, "A")
            self.assertFalse(any("successfully acknowledged" in str(call) for call in printer.call_args_list))
            self.assertFalse(any("ACK confirmed" in str(call) for call in printer.call_args_list))
            with queue._db() as db:
                self.assertEqual(db.execute("SELECT acked FROM work WHERE sequence=3").fetchone()[0], 0)
            self.assertIsNotNone(queue.next())

    def test_queued_send_retry_reuses_reply_without_second_inference(self):
        class Hub:
            agent_id = "agent-a"
            def ack_task(self, sequence): return True
            def send_task(self, *args, **kwargs):
                self.calls = getattr(self, "calls", 0) + 1
                self.last = (args, kwargs)
                self.history = getattr(self, "history", []) + [(args, kwargs)]
                return None if self.calls == 1 else {"state": "PENDING"}
            def auto_accept_pending_invitations(self): return 0
        class Backend:
            calls = 0
            def execute(self, *args):
                self.calls += 1
                return "stable reply"
        with tempfile.TemporaryDirectory() as directory:
            queue = bridge.DurableWorkQueue(os.path.join(directory, "q.db"), scope="hub/agent-a")
            queue.enqueue({"sequence": 4, "taskId": "t", "requesterAgentId": "b", "contextId": "c", "message": "do"})
            hub, backend = Hub(), Backend()
            row = queue.next()
            bridge.process_queued_task(hub, backend, queue, row, "A")
            with queue._db() as db:
                db.execute("UPDATE work SET retry_after=0 WHERE sequence=4")
            row = queue.next()
            bridge.process_queued_task(hub, backend, queue, row, "A")
            self.assertEqual(backend.calls, 1)
            self.assertEqual(hub.calls, 2)
            self.assertEqual(hub.history[0], hub.history[1])
            self.assertEqual(hub.last[0][1], "stable reply")
            self.assertEqual(hub.last[1]["task_id"], "reply-agent-a-4")
            self.assertEqual(hub.calls, 2)
            with queue._db() as db:
                self.assertEqual(db.execute("SELECT state FROM work WHERE sequence=4").fetchone()[0], "done")

    def test_backend_failure_does_not_block_next_work(self):
        class Hub:
            agent_id = "a"
            def ack_task(self, sequence): return True
            def auto_accept_pending_invitations(self): return 0
            def send_task(self, *args, **kwargs): return {"state": "PENDING"}
        class Backend:
            def execute(self, message, *args): return None if message == "bad" else "ok"
        with tempfile.TemporaryDirectory() as directory:
            queue = bridge.DurableWorkQueue(os.path.join(directory, "q.db"), scope="hub/a")
            queue.enqueue({"sequence": 5, "taskId": "bad", "requesterAgentId": "b", "contextId": "c", "message": "bad"})
            queue.enqueue({"sequence": 6, "taskId": "good", "requesterAgentId": "b", "contextId": "c", "message": "good"})
            row = queue.next(); bridge.process_queued_task(Hub(), Backend(), queue, row, "A")
            row = queue.next(); self.assertEqual(row["sequence"], 6)
            bridge.process_queued_task(Hub(), Backend(), queue, row, "A")
            with queue._db() as db:
                self.assertEqual(db.execute("SELECT state FROM work WHERE sequence=5").fetchone()[0], "pending")
                self.assertEqual(db.execute("SELECT state FROM work WHERE sequence=6").fetchone()[0], "done")

    def test_register_payload_and_duplicate_without_token_fails(self):
        class Response:
            status = 200
            def __enter__(self): return self
            def __exit__(self, *args): pass
            def read(self): return json.dumps({"identity": {"agentId": "a", "agentToken": "tok"}}).encode()
        captured = {}
        def open_url(req, timeout):
            captured["request"] = req
            return Response()
        client = bridge.HubClient("https://hub", shared_key="secret")
        with mock.patch.object(bridge.urllib.request, "urlopen", open_url):
            client.register("Name", "install-key", "openclaw")
        body = json.loads(captured["request"].data)
        self.assertEqual(body["displayName"], "Name")
        self.assertEqual(body["providerFamily"], "openclaw")
        self.assertEqual(body["registrationIdempotencyKey"], "install-key")
        self.assertEqual(captured["request"].get_header("X-hub-key"), "secret")

        class Duplicate(Response):
            def read(self): return json.dumps({"identity": {"agentId": "a"}}).encode()
        with mock.patch.object(bridge.urllib.request, "urlopen", lambda *args, **kwargs: Duplicate()):
            with self.assertRaises(RuntimeError): bridge.HubClient("https://hub").register("N", "k")

    def test_main_preserves_legacy_and_explicit_credentials(self):
        with tempfile.TemporaryDirectory() as directory:
            path = os.path.join(directory, "credentials.json")
            original = {"identity": {"agentId": "a", "agentToken": "secret"}}
            with open(path, "w") as f: json.dump(original, f)
            queue = os.path.join(directory, "q.db")
            with mock.patch.object(bridge, "run_bridge_listener"), mock.patch.object(bridge.sys, "argv", ["bridge", "--credentials", path, "--queue-db", queue, "--backend", "echo", "--name", "A"]): bridge.main()
            with open(path) as f: self.assertEqual(json.load(f), original)
            explicit_queue = os.path.join(directory, "explicit.db")
            with mock.patch.object(bridge, "run_bridge_listener") as listener, mock.patch.object(bridge.sys, "argv", ["bridge", "--credentials", path, "--agent-id", "explicit", "--token", "explicit-secret", "--queue-db", explicit_queue, "--backend", "echo"]): bridge.main()
            self.assertEqual(listener.call_args.args[0].agent_id, "explicit")
            self.assertEqual(listener.call_args.args[0].token, "explicit-secret")
            with open(path) as f: self.assertEqual(json.load(f), original)

    def test_systemd_failures_raise(self):
        with tempfile.TemporaryDirectory() as directory:
            expand = lambda p: p.replace("~", directory)
            fail = mock.Mock(returncode=1, stderr="failed")
            ok = mock.Mock(returncode=0, stderr="")
            with mock.patch.object(bridge.os.path, "expanduser", side_effect=expand), mock.patch.object(bridge.subprocess, "run", return_value=fail):
                with self.assertRaises(RuntimeError): bridge.install_systemd_service("A", [])
            with mock.patch.object(bridge.os.path, "expanduser", side_effect=expand), mock.patch.object(bridge.subprocess, "run", side_effect=[ok, fail]):
                with self.assertRaises(RuntimeError): bridge.install_systemd_service("A", [])
            with mock.patch.object(bridge.os.path, "expanduser", side_effect=expand), mock.patch.object(bridge.subprocess, "run", side_effect=[ok, ok, fail]):
                with self.assertRaises(RuntimeError): bridge.install_systemd_service("A", [])

    def test_systemd_quote_escapes_exec_expansions_but_not_environment(self):
        self.assertEqual(bridge.systemd_unit_quote("a $HOME %x 'b"), '"a $$HOME %%x \'b"')
        self.assertEqual(bridge.systemd_unit_quote("PATH=a $HOME %x", exec_arg=False), '"PATH=a $HOME %x"')

    def test_saved_reply_is_reusable_after_send_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            queue = bridge.DurableWorkQueue(os.path.join(directory, "q.db"), scope="hub/a")
            queue.enqueue({"sequence": 2, "taskId": "t", "requesterAgentId": "b", "message": "do"})
            row = queue.next()
            reply = {"target": "b", "message": "done", "context_id": "c", "task_id": "reply-a-2"}
            queue.save_reply(2, reply)
            row = queue.next()
            self.assertIsNone(row)
            queue.retry(2)
            with queue._db() as db:
                db.execute("UPDATE work SET retry_after=0 WHERE sequence=2")
            row = queue.next()
            self.assertEqual(json.loads(row["reply_json"]), reply)

    def test_claudecode_backend_execution(self):
        backend = bridge.ClaudeCodeBackend(system_prompt="Test Prompt")
        with mock.patch("subprocess.run") as mock_run:
            mock_run.return_value = mock.Mock(returncode=0, stdout="Claude Reasoning Result", stderr="")
            res = backend.execute("hello", "peer-1", {})
            self.assertEqual(res, "Claude Reasoning Result")
            cmd = mock_run.call_args[0][0]
            self.assertEqual(cmd[0], "claude")
            self.assertEqual(cmd[1], "-p")
            self.assertIn("Test Prompt", cmd[2])
            self.assertIn("[[A2A_NO_REPLY]]", cmd[2])

    def test_codex_backend_execution(self):
        backend = bridge.CodexBackend(system_prompt="Codex Persona")
        with mock.patch("subprocess.run") as mock_run:
            mock_run.return_value = mock.Mock(returncode=0, stdout="Codex Result", stderr="")
            res = backend.execute("build", "peer-2", {})
            self.assertEqual(res, "Codex Result")
            cmd = mock_run.call_args[0][0]
            self.assertEqual(cmd[0], "codex")
            self.assertEqual(cmd[1], "exec")
            self.assertIn("Codex Persona", cmd[2])

    def test_command_backend_execution(self):
        backend = bridge.CommandBackend(command_cmd="echo 'custom result'")
        with mock.patch("subprocess.run") as mock_run:
            mock_run.return_value = mock.Mock(returncode=0, stdout="custom result", stderr="")
            res = backend.execute("task", "peer-3", {})
            self.assertEqual(res, "custom result")

    def test_mcp_server_initialize_and_tools_list(self):
        import io
        class MockHub:
            agent_id = "agent-test"
            def list_agents(self): return [{"agentId": "a1", "displayName": "Agent1"}]
            def status(self): return {"hubId": "test", "mode": "PUBLIC"}
        hub = MockHub()
        in_stream = io.StringIO(
            json.dumps({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {}}) + "\n" +
            json.dumps({"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}}) + "\n" +
            json.dumps({"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "a2a_list_agents", "arguments": {}}}) + "\n"
        )
        out_stream = io.StringIO()
        with mock.patch("sys.stdin", in_stream):
            bridge.run_mcp_server(hub, "TestAgent", out_stream=out_stream)
        lines = [json.loads(l) for l in out_stream.getvalue().strip().split("\n") if l]
        self.assertEqual(len(lines), 3)
        self.assertEqual(lines[0]["result"]["serverInfo"]["name"], "888a2a-lite-mcp")
        self.assertTrue(any(t["name"] == "a2a_list_agents" for t in lines[1]["result"]["tools"]))
        self.assertIn("Agent1", lines[2]["result"]["content"][0]["text"])


def json_item(row):
    import json
    return json.loads(row["item_json"])


if __name__ == "__main__":
    unittest.main()
