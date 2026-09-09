import importlib.util
import json
import os
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request
from unittest import mock


SPEC = importlib.util.spec_from_file_location("a2a_bridge", os.path.join(os.path.dirname(__file__), "a2a_bridge.py"))
bridge = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(bridge)


class DurableBridgeTests(unittest.TestCase):
    def test_default_credentials_are_separated_by_hub_and_circle_key(self):
        public_path = bridge.default_credential_path("https://hub-a", "Agent")
        private_a = bridge.default_credential_path("https://hub-a", "Agent", "key-a")
        private_b = bridge.default_credential_path("https://hub-a", "Agent", "key-b")
        other_hub = bridge.default_credential_path("https://hub-b", "Agent", "key-a")

        self.assertNotEqual(public_path, private_a)
        self.assertNotEqual(private_a, private_b)
        self.assertNotEqual(private_a, other_hub)
        self.assertNotIn("key-a", private_a)
        self.assertEqual(private_a, bridge.default_credential_path("https://hub-a", "Agent", " key-a "))

    def test_default_chat_database_is_separated_by_agent_identity(self):
        self.assertNotEqual(
            bridge.default_chat_db_path("https://hub", "agent-public"),
            bridge.default_chat_db_path("https://hub", "agent-private"),
        )

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

    def test_shared_key_is_not_sent_after_circle_registration(self):
        client = bridge.HubClient("https://hub", agent_id="agent", token="token", shared_key="secret", circle_id="circle-a")
        self.assertNotIn("X-hub-key", {key.lower() for key in client._headers()})

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

    def test_standard_task_reports_correlated_completion_without_reply_task(self):
        class Hub:
            agent_id = "executor"
            def ack_task(self, sequence): return True
            def submit_standard_update(self, task_id, update):
                self.task_id = task_id
                self.update = update
                return {"task": {"id": task_id}}

        class Backend:
            def execute(self, *args): return "standard result"

        with tempfile.TemporaryDirectory() as directory:
            queue = bridge.DurableWorkQueue(os.path.join(directory, "queue.db"), scope="hub/executor")
            queue.enqueue({"sequence": 9, "taskId": "a2a-task-9", "protocol": "A2A/1.0", "turnId": "turn-1", "taskRevision": 1, "requesterAgentId": "requester", "contextId": "context-1", "message": "do work"})
            hub = Hub()
            row = queue.next()
            bridge.process_queued_task(hub, Backend(), queue, row, "Executor")
            self.assertEqual(hub.task_id, "a2a-task-9")
            self.assertEqual(hub.update["state"], "TASK_STATE_COMPLETED")
            self.assertEqual(hub.update["expectedRevision"], 2)
            self.assertIn("message", hub.update)
            with queue._db() as db:
                self.assertEqual(db.execute("SELECT state FROM work WHERE sequence=9").fetchone()[0], "done")

    def test_standard_no_reply_reports_empty_completion(self):
        class Hub:
            agent_id = "executor"
            def ack_task(self, sequence): return True
            def submit_standard_update(self, task_id, update):
                self.update = update
                return {"task": {"id": task_id}}

        with tempfile.TemporaryDirectory() as directory:
            queue = bridge.DurableWorkQueue(os.path.join(directory, "queue.db"), scope="hub/executor")
            queue.enqueue({"sequence": 10, "taskId": "a2a-task-10", "protocol": "A2A/1.0", "turnId": "turn-1", "taskRevision": 1, "requesterAgentId": "requester", "contextId": "context-1", "message": "收到"})
            hub = Hub()
            row = queue.next()
            bridge.process_queued_task(hub, object(), queue, row, "Executor")
            self.assertEqual(hub.update["state"], "TASK_STATE_COMPLETED")
            self.assertNotIn("message", hub.update)

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

    def test_local_ui_server_endpoints(self):
        class MockHub:
            agent_id = "agent-ui-user"
            hub_url = "https://a2a.test.com"
            def list_agents(self): return [{"agentId": "peer-1", "displayName": "PeerAgent", "state": "ONLINE"}]
            def send_task(self, target, msg, *args, **kwargs): return {"taskId": "task-ui-123"}
        hub = MockHub()
        server = bridge.LocalUIServer(("127.0.0.1", 0), bridge.LocalUIHandler, hub, "TestUser (Web)")
        server_thread = bridge.threading.Thread(target=server.serve_forever, daemon=True)
        server_thread.start()
        port = server.server_address[1]
        base_url = f"http://127.0.0.1:{port}"

        try:
            # 1. GET /
            with bridge.urllib.request.urlopen(base_url) as resp:
                self.assertEqual(resp.status, 200)
                html = resp.read().decode("utf-8")
                self.assertIn("888a2a Client Workstation", html)

            # 2. GET /api/me
            with bridge.urllib.request.urlopen(f"{base_url}/api/me") as resp:
                self.assertEqual(resp.status, 200)
                me = json.loads(resp.read().decode("utf-8"))
                self.assertEqual(me["agentId"], "agent-ui-user")
                self.assertEqual(me["displayName"], "TestUser (Web)")

            # 3. GET /api/peers
            with bridge.urllib.request.urlopen(f"{base_url}/api/peers") as resp:
                self.assertEqual(resp.status, 200)
                data = json.loads(resp.read().decode("utf-8"))
                self.assertEqual(len(data["agents"]), 1)
                self.assertEqual(data["agents"][0]["displayName"], "PeerAgent")

            # 4. POST /api/send
            req = bridge.urllib.request.Request(
                f"{base_url}/api/send",
                data=json.dumps({"targetAgentId": "peer-1", "message": "Hello from UI"}).encode("utf-8"),
                headers={"Content-Type": "application/json"}
            )
            with bridge.urllib.request.urlopen(req) as resp:
                self.assertEqual(resp.status, 200)
                send_res = json.loads(resp.read().decode("utf-8"))
                self.assertTrue(send_res["ok"])
                self.assertEqual(send_res["taskId"], "task-ui-123")

            # 5. GET /api/history?peer=peer-1
            with bridge.urllib.request.urlopen(f"{base_url}/api/history?peer=peer-1") as resp:
                self.assertEqual(resp.status, 200)
                hist = json.loads(resp.read().decode("utf-8"))
                self.assertEqual(len(hist["messages"]), 1)
                self.assertEqual(hist["messages"][0]["message"], "Hello from UI")
                self.assertTrue(hist["messages"][0]["isOutgoing"])
        finally:
            server.shutdown()
            server.server_close()

    def test_service_argument_filtering_and_detection(self):
        # test filter_service_args with and without value
        args1 = ["--install-service", "systemd", "--hub", "https://hub.test", "--name", "A"]
        self.assertEqual(bridge.filter_service_args(args1), ["--hub", "https://hub.test", "--name", "A"])

        args2 = ["--install-service", "--hub", "https://hub.test", "--name", "A"]
        self.assertEqual(bridge.filter_service_args(args2), ["--hub", "https://hub.test", "--name", "A"])

        args3 = ["--install-service=launchd", "--backend", "openclaw"]
        self.assertEqual(bridge.filter_service_args(args3), ["--backend", "openclaw"])

        # test get_default_service_type
        with mock.patch("sys.platform", "darwin"):
            self.assertEqual(bridge.get_default_service_type(), "launchd")
        with mock.patch("sys.platform", "linux"):
            self.assertEqual(bridge.get_default_service_type(), "systemd")

        # test detect_backend fallback
        with mock.patch("shutil.which", return_value=None):
            self.assertEqual(bridge.detect_backend(), "openclaw")
        with mock.patch("shutil.which", side_effect=lambda c, path=None: "/bin/claude" if c == "claude" else None):
            self.assertEqual(bridge.detect_backend(), "claudecode")

    def test_local_chat_store_persistence(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            db_path = os.path.join(temp_dir, "test_chat.db")
            store1 = bridge.LocalChatStore(db_path)
            store1.save_message("peer-A", {
                "id": "msg-1",
                "senderId": "peer-A",
                "senderName": "Peer A",
                "message": "Hello from A",
                "isOutgoing": False
            }, sequence=101)
            store1.save_message("peer-A", {
                "id": "msg-2",
                "senderId": "me",
                "senderName": "Me",
                "message": "Hello back from Me",
                "isOutgoing": True
            })

            # Re-open in a second instance (simulating app restart)
            store2 = bridge.LocalChatStore(db_path)
            history = store2.get_history("peer-A")
            self.assertEqual(len(history), 2)
            self.assertEqual(history[0]["id"], "msg-1")
            self.assertEqual(history[0]["message"], "Hello from A")
            self.assertFalse(history[0]["isOutgoing"])
            self.assertEqual(history[1]["id"], "msg-2")
            self.assertEqual(history[1]["message"], "Hello back from Me")
            self.assertTrue(history[1]["isOutgoing"])

    def test_duplicate_redelivery_preserves_order(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            db_path = os.path.join(temp_dir, "test_order.db")
            store = bridge.LocalChatStore(db_path)
            store.save_message("peer-A", {"id": "msg-1", "message": "First", "isOutgoing": False}, sequence=1)
            time.sleep(0.01)
            store.save_message("peer-A", {"id": "msg-2", "message": "Second", "isOutgoing": True}, sequence=2)
            time.sleep(0.01)

            # Redelivery of msg-1 via SSE retry later in time
            store.save_message("peer-A", {"id": "msg-1", "message": "First", "isOutgoing": False}, sequence=1)

            history = store.get_history("peer-A")
            self.assertEqual(len(history), 2)
            # msg-1 must stay FIRST, must not bump to end!
            self.assertEqual(history[0]["id"], "msg-1")
            self.assertEqual(history[1]["id"], "msg-2")

    def test_conversations_and_pagination(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            db_path = os.path.join(temp_dir, "test_conv.db")
            store = bridge.LocalChatStore(db_path)
            store.save_message("peer-A", {"id": "msg-A1", "message": "Hello A", "isOutgoing": False}, display_name="Agent Alpha")
            time.sleep(0.01)
            store.save_message("peer-B", {"id": "msg-B1", "message": "Hello B", "isOutgoing": False}, display_name="Agent Beta")

            convs = store.get_conversations()
            self.assertEqual(len(convs), 2)
            # Beta updated most recently, should be first
            self.assertEqual(convs[0]["peerId"], "peer-B")
            self.assertEqual(convs[0]["displayName"], "Agent Beta")
            self.assertEqual(convs[1]["peerId"], "peer-A")

            # Test pagination
            store.save_message("peer-A", {"id": "msg-A2", "message": "Msg 2", "isOutgoing": True})
            store.save_message("peer-A", {"id": "msg-A3", "message": "Msg 3", "isOutgoing": True})

            page1 = store.get_history("peer-A", limit=2, offset=0)
            self.assertEqual(len(page1), 2)
            self.assertEqual(page1[0]["id"], "msg-A1")
            self.assertEqual(page1[1]["id"], "msg-A2")

            page2 = store.get_history("peer-A", limit=2, offset=2)
            self.assertEqual(len(page2), 1)
            self.assertEqual(page2[0]["id"], "msg-A3")

    def test_conversation_summary_preserves_latest_on_redelivery(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            db_path = os.path.join(temp_dir, "test_summary.db")
            store = bridge.LocalChatStore(db_path)
            # Save newer message B
            store.save_message("peer-A", {
                "id": "msg-new",
                "message": "Newer Message",
                "timestamp": "2026-09-07T12:05:00Z",
                "isOutgoing": False
            }, display_name="Peer A")

            convs1 = store.get_conversations()
            self.assertEqual(convs1[0]["lastMessage"], "Newer Message")
            self.assertEqual(convs1[0]["lastTimestamp"], "2026-09-07T12:05:00Z")
            updated_at1 = convs1[0]["updatedAt"]

            time.sleep(0.02)
            # Redeliver older message A (e.g. from SSE backlog / retry)
            store.save_message("peer-A", {
                "id": "msg-old",
                "message": "Older Redelivered Message",
                "timestamp": "2026-09-07T12:01:00Z",
                "isOutgoing": False
            }, display_name="Peer A")

            convs2 = store.get_conversations()
            # Summary MUST still be the newer message! Must NOT regress to old message!
            self.assertEqual(convs2[0]["lastMessage"], "Newer Message")
            self.assertEqual(convs2[0]["lastTimestamp"], "2026-09-07T12:05:00Z")
            self.assertEqual(convs2[0]["updatedAt"], updated_at1)

    def test_conversation_summary_compares_absolute_timestamps(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            store = bridge.LocalChatStore(os.path.join(temp_dir, "test_timezone.db"))
            store.save_message("peer-A", {
                "id": "msg-new",
                "message": "Newer in UTC",
                "timestamp": "2026-09-07T12:05:00Z",
                "isOutgoing": False,
            })
            # Lexically this appears later (13:00), but +02:00 is 11:00 UTC.
            store.save_message("peer-A", {
                "id": "msg-old",
                "message": "Older with offset",
                "timestamp": "2026-09-07T13:00:00+02:00",
                "isOutgoing": False,
            })
            conversation = store.get_conversations()[0]
            self.assertEqual(conversation["lastMessage"], "Newer in UTC")

    def test_history_api_validation(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            db_path = os.path.join(temp_dir, "test_api.db")
            mock_hub = mock.MagicMock()
            mock_hub.agent_id = "test-agent"
            mock_hub.hub_url = "http://test"
            server = bridge.LocalUIServer(("127.0.0.1", 0), bridge.LocalUIHandler, mock_hub, "Test User", chat_db_path=db_path)
            port = server.server_port
            t = threading.Thread(target=server.serve_forever, daemon=True)
            t.start()
            try:
                # Missing peer
                with self.assertRaises(urllib.error.HTTPError) as ctx:
                    urllib.request.urlopen(f"http://127.0.0.1:{port}/api/history")
                self.assertEqual(ctx.exception.code, 400)

                # Invalid limit
                with self.assertRaises(urllib.error.HTTPError) as ctx:
                    urllib.request.urlopen(f"http://127.0.0.1:{port}/api/history?peer=agent-1&limit=-1")
                self.assertEqual(ctx.exception.code, 400)

                with self.assertRaises(urllib.error.HTTPError) as ctx:
                    urllib.request.urlopen(f"http://127.0.0.1:{port}/api/history?peer=agent-1&limit=abc")
                self.assertEqual(ctx.exception.code, 400)

                # Invalid offset
                with self.assertRaises(urllib.error.HTTPError) as ctx:
                    urllib.request.urlopen(f"http://127.0.0.1:{port}/api/history?peer=agent-1&offset=-5")
                self.assertEqual(ctx.exception.code, 400)

                # Invalid before
                with self.assertRaises(urllib.error.HTTPError) as ctx:
                    urllib.request.urlopen(f"http://127.0.0.1:{port}/api/history?peer=agent-1&before=invalid")
                self.assertEqual(ctx.exception.code, 400)

                # Valid request
                with urllib.request.urlopen(f"http://127.0.0.1:{port}/api/history?peer=agent-1&limit=50") as resp:
                    self.assertEqual(resp.status, 200)
                    data = json.loads(resp.read().decode("utf-8"))
                    self.assertEqual(data["messages"], [])

                for value in ("nan", "inf", "-inf"):
                    with self.assertRaises(urllib.error.HTTPError) as ctx:
                        urllib.request.urlopen(f"http://127.0.0.1:{port}/api/history?peer=agent-1&before={value}")
                    self.assertEqual(ctx.exception.code, 400)
            finally:
                server.shutdown()
                server.server_close()

    def test_outbound_idempotent_retry(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            db_path = os.path.join(temp_dir, "test_outbox.db")
            mock_hub = mock.MagicMock()
            mock_hub.agent_id = "test-agent"
            mock_hub.hub_url = "http://test"
            # First attempt fails
            mock_hub.send_task.return_value = None

            server = bridge.LocalUIServer(("127.0.0.1", 0), bridge.LocalUIHandler, mock_hub, "Test User", chat_db_path=db_path)
            port = server.server_port
            t = threading.Thread(target=server.serve_forever, daemon=True)
            t.start()
            try:
                task_id = "out-fixed-key-123"
                payload = json.dumps({"targetAgentId": "peer-A", "message": "hello outbox", "taskId": task_id}).encode("utf-8")

                # Attempt 1: Hub fails -> returns 502, state becomes FAILED
                req = urllib.request.Request(f"http://127.0.0.1:{port}/api/send", data=payload, headers={"Content-Type": "application/json"})
                with self.assertRaises(urllib.error.HTTPError) as ctx:
                    urllib.request.urlopen(req)
                self.assertEqual(ctx.exception.code, 502)

                msgs = server.chat_store.get_history("peer-A")
                self.assertEqual(len(msgs), 1)
                self.assertEqual(msgs[0]["id"], task_id)
                self.assertEqual(msgs[0]["state"], "FAILED")

                # Attempt 2 (Retry): Hub succeeds -> returns 200, state becomes SENT
                mock_hub.send_task.return_value = {"taskId": task_id, "state": "PENDING"}
                req2 = urllib.request.Request(f"http://127.0.0.1:{port}/api/send", data=payload, headers={"Content-Type": "application/json"})
                with urllib.request.urlopen(req2) as resp:
                    self.assertEqual(resp.status, 200)
                    res_data = json.loads(resp.read().decode("utf-8"))
                    self.assertTrue(res_data["ok"])
                    self.assertEqual(res_data["state"], "SENT")

                msgs_after = server.chat_store.get_history("peer-A")
                # Exactly 1 row in DB (idempotent, no duplicates!) and state is SENT!
                self.assertEqual(len(msgs_after), 1)
                self.assertEqual(msgs_after[0]["id"], task_id)
                self.assertEqual(msgs_after[0]["state"], "SENT")
            finally:
                server.shutdown()
                server.server_close()

    def test_retry_markup_does_not_embed_task_id_in_javascript(self):
        self.assertNotIn("onclick=\"retryMessage('", bridge.CLIENT_HTML)
        self.assertIn('button.addEventListener("click"', bridge.CLIENT_HTML)
        self.assertIn("m.state === 'PENDING' || m.state === 'SENDING'", bridge.CLIENT_HTML)

    def test_sent_message_state_is_terminal_on_duplicate_save(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            store = bridge.LocalChatStore(os.path.join(temp_dir, "test_terminal.db"))
            store.save_message("peer-A", {
                "id": "msg-sent",
                "message": "already sent",
                "isOutgoing": True,
                "state": "SENT",
            })
            store.save_message("peer-A", {
                "id": "msg-sent",
                "message": "already sent",
                "isOutgoing": True,
                "state": "PENDING",
            })
            self.assertEqual(store.get_history("peer-A")[0]["state"], "SENT")

    def test_outbox_delivery_exception_is_retriable(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            hub = mock.MagicMock()
            hub.send_task.side_effect = RuntimeError("temporary network error")
            server = bridge.LocalUIServer(("127.0.0.1", 0), bridge.LocalUIHandler, hub, "Test User", chat_db_path=os.path.join(temp_dir, "test_worker.db"))
            try:
                task_id = "out-worker-retry"
                server.append_message("peer-A", {
                    "id": task_id,
                    "senderId": "test-agent",
                    "message": "retry me",
                    "isOutgoing": True,
                    "state": "PENDING",
                })
                self.assertFalse(server.deliver_outbox(task_id, force=True))
                self.assertEqual(server.chat_store.get_history("peer-A")[0]["state"], "FAILED")

                hub.send_task.side_effect = None
                hub.send_task.return_value = {"taskId": task_id, "state": "PENDING"}
                self.assertTrue(server.deliver_outbox(task_id, force=True))
                self.assertEqual(server.chat_store.get_history("peer-A")[0]["state"], "SENT")
            finally:
                server.server_close()

    def test_outbox_worker_delivers_due_message(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            hub = mock.MagicMock()
            hub.send_task.return_value = {"taskId": "out-worker", "state": "PENDING"}
            server = bridge.LocalUIServer(("127.0.0.1", 0), bridge.LocalUIHandler, hub, "Test User", chat_db_path=os.path.join(temp_dir, "test_worker_due.db"))
            try:
                server.append_message("peer-A", {
                    "id": "out-worker",
                    "senderId": "test-agent",
                    "message": "deliver in background",
                    "isOutgoing": True,
                    "state": "PENDING",
                })
                wait_calls = [0]
                def wait_once(_timeout):
                    wait_calls[0] += 1
                    return wait_calls[0] > 1
                server.outbox_stop.wait = wait_once
                worker = threading.Thread(target=server._run_outbox_worker)
                worker.start()
                worker.join(timeout=2)
                self.assertFalse(worker.is_alive())
                self.assertEqual(server.chat_store.get_history("peer-A")[0]["state"], "SENT")
                hub.send_task.assert_called_once_with("peer-A", "deliver in background", task_id="out-worker")
            finally:
                server.server_close()

    def test_hub_assigned_task_id_is_reconciled(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            hub = mock.MagicMock()
            hub.agent_id = "test-agent"
            hub.hub_url = "http://test"
            hub.send_task.return_value = {"taskId": "hub-task-42", "state": "PENDING"}
            server = bridge.LocalUIServer(("127.0.0.1", 0), bridge.LocalUIHandler, hub, "Test User", chat_db_path=os.path.join(temp_dir, "test_reconcile.db"))
            port = server.server_port
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            try:
                payload = json.dumps({
                    "targetAgentId": "peer-A",
                    "message": "reconcile task id",
                    "taskId": "local-task-1",
                }).encode("utf-8")
                request = urllib.request.Request(
                    f"http://127.0.0.1:{port}/api/send",
                    data=payload,
                    headers={"Content-Type": "application/json"},
                )
                with urllib.request.urlopen(request) as response:
                    result = json.loads(response.read().decode("utf-8"))

                self.assertEqual(result["taskId"], "hub-task-42")
                history = server.chat_store.get_history("peer-A")
                self.assertEqual(len(history), 1)
                self.assertEqual(history[0]["id"], "hub-task-42")
                self.assertEqual(history[0]["state"], "SENT")
                hub.send_task.assert_called_once_with("peer-A", "reconcile task id", task_id="local-task-1")
            finally:
                server.shutdown()
                server.server_close()

    def test_hub_client_reuses_idempotency_key_for_fixed_task(self):
        class Response:
            def __init__(self, body):
                self.body = body
                self.status = 202
            def __enter__(self):
                return self
            def __exit__(self, *args):
                return None
            def read(self):
                return self.body

        requests = []
        response_body = json.dumps({"taskId": "out-fixed", "state": "PENDING"}).encode()

        def open_url(request, timeout):
            requests.append(json.loads(request.data.decode()))
            return Response(response_body)

        client = bridge.HubClient("https://hub", agent_id="agent-a", token="token")
        with mock.patch.object(bridge.urllib.request, "urlopen", open_url):
            client.send_task("agent-b", "hello", task_id="out-fixed")
            client.send_task("agent-b", "hello", task_id="out-fixed")

        self.assertEqual(len(requests), 2)
        self.assertEqual(requests[0]["taskId"], "out-fixed")
        self.assertEqual(requests[0]["idempotencyKey"], "idem-out-fixed")
        self.assertEqual(requests[0]["idempotencyKey"], requests[1]["idempotencyKey"])


def json_item(row):
    import json
    return json.loads(row["item_json"])


if __name__ == "__main__":
    unittest.main()
