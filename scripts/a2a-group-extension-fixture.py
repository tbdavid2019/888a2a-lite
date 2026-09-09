#!/usr/bin/env python3
"""Dependency-free fixture for the optional Group Coordination Extension."""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.parse
import urllib.request


EXTENSION_URI = "https://a2a.david888.com/extensions/groups/v1"


def request(url, token, payload=None, extension=False, timeout=15):
    data = None if payload is None else json.dumps(payload).encode("utf-8")
    headers = {"Accept": "application/json", "Authorization": f"Bearer {token}"}
    if data is not None:
        headers["Content-Type"] = "application/a2a+json"
    if extension:
        headers["A2A-Extensions"] = EXTENSION_URI
    req = urllib.request.Request(url, data=data, headers=headers)
    with urllib.request.urlopen(req, timeout=timeout) as response:
        return response.status, json.loads(response.read().decode("utf-8"))


def read_sse_progress(url, token):
    req = urllib.request.Request(url, headers={"Accept": "text/event-stream", "Authorization": f"Bearer {token}", "A2A-Extensions": EXTENSION_URI})
    with urllib.request.urlopen(req, timeout=10) as response:
        events = []
        data = []
        for raw_line in response:
            line = raw_line.decode("utf-8").rstrip("\r\n")
            if line.startswith("data: "):
                data.append(line[6:])
            if not line and data:
                events.append(json.loads("\n".join(data)))
                data = []
                if any("statusUpdate" in event or "artifactUpdate" in event for event in events):
                    return events
    return events


def main():
    base = os.environ["A2A_GROUP_FIXTURE_URL"].rstrip("/")
    token = os.environ["A2A_GROUP_FIXTURE_TOKEN"]
    group_id = os.environ["A2A_GROUP_FIXTURE_GROUP_ID"]
    status, listing = request(f"{base}/a2a/v1/groups", token)
    assert status == 200 and any(group["groupId"] == group_id for group in listing["groups"])
    status, card = request(f"{base}/a2a/v1/groups/{urllib.parse.quote(group_id, safe='')}/card", token)
    assert status == 200
    interface = card["supportedInterfaces"][0]
    assert interface["tenant"] == f"group:{group_id}"
    assert any(extension["uri"] == EXTENSION_URI for extension in card["capabilities"]["extensions"])
    payload = {"tenant": f"group:{group_id}", "message": {"messageId": "group-extension-fixture", "role": "ROLE_USER", "parts": [{"text": "fixture group ping"}], "metadata": {EXTENSION_URI: {"replyPolicy": "ACK_ONLY"}}}, "configuration": {"returnImmediately": True}}
    status, sent = request(f"{base}/a2a/v1/message:send", token, payload, extension=True)
    assert status == 200 and sent["task"]["id"]
    task_id = sent["task"]["id"]
    stream_events = read_sse_progress(f"{interface['url'].rstrip('/')}/tasks/{urllib.parse.quote(task_id, safe='')}:subscribe", token)
    assert stream_events and stream_events[0].get("task", {}).get("id") == task_id
    for event in stream_events[1:]:
        assert event.get("statusUpdate", {}).get("taskId") == task_id or event.get("artifactUpdate", {}).get("taskId") == task_id
    status, canceled = request(f"{interface['url'].rstrip('/')}/tasks/{urllib.parse.quote(task_id, safe='')}:cancel", token, {}, extension=True)
    assert status in (200, 400)
    print(json.dumps({"parentTaskId": task_id, "streamKeys": [sorted(event.keys()) for event in stream_events]}))


if __name__ == "__main__":
    main()
