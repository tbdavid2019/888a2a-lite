#!/usr/bin/env python3
"""Dependency-free A2A 1.0 HTTP+JSON smoke fixture.

The fixture only uses the public Card and an issued Agent Token. Set
A2A_FIXTURE_URL, A2A_FIXTURE_TOKEN, and A2A_FIXTURE_TENANT to run it.
"""

from __future__ import annotations

import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request


def request(url: str, token: str | None = None, payload: dict | None = None):
    data = None if payload is None else json.dumps(payload).encode("utf-8")
    headers = {"Accept": "application/json"}
    if data is not None:
        headers["Content-Type"] = "application/a2a+json"
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(url, data=data, headers=headers)
    with urllib.request.urlopen(req, timeout=15) as response:
        content = response.read().decode("utf-8")
        return response.status, json.loads(content) if content else None


def main() -> int:
    base = os.environ.get("A2A_FIXTURE_URL", "").rstrip("/")
    token = os.environ.get("A2A_FIXTURE_TOKEN", "")
    tenant = os.environ.get("A2A_FIXTURE_TENANT", "")
    if not base or not token or not tenant:
        print("set A2A_FIXTURE_URL, A2A_FIXTURE_TOKEN, and A2A_FIXTURE_TENANT", file=sys.stderr)
        return 2

    _, root_card = request(f"{base}/.well-known/agent-card.json")
    assert root_card["supportedInterfaces"][0]["protocolBinding"] == "HTTP+JSON"
    card_status, card = request(f"{base}/a2a/v1/agents/{urllib.parse.quote(tenant, safe='')}/card", token)
    assert card_status == 200 and card["supportedInterfaces"][0]["tenant"] == tenant
    interface = card["supportedInterfaces"][0]["url"].rstrip("/")
    message = {"tenant": tenant, "message": {"messageId": "fixture-message-1", "role": "ROLE_USER", "parts": [{"text": "A2A fixture ping"}]}, "configuration": {"returnImmediately": True}}
    status, sent = request(f"{interface}/message:send", token, message)
    assert status == 200 and sent.get("task", {}).get("id")
    task_id = sent["task"]["id"]
    attachment_url = "https://box.david888.com/storage/a2a-fixture.pdf"
    attachment_message = {"tenant": tenant, "message": {"messageId": "fixture-message-attachment-1", "role": "ROLE_USER", "parts": [{"text": "A2A attachment fixture"}, {"url": attachment_url, "filename": "a2a-fixture.pdf", "mediaType": "application/pdf"}]}, "configuration": {"returnImmediately": True}}
    status, attachment_sent = request(f"{interface}/message:send", token, attachment_message)
    assert status == 200 and attachment_sent.get("task", {}).get("id")
    attachment_task = request(f"{interface}/tasks/{urllib.parse.quote(attachment_sent['task']['id'], safe='')}?includeArtifacts=true", token)[1]
    assert attachment_task["history"][0]["parts"][1]["url"] == attachment_url

    invalid_message = {"tenant": tenant, "message": {"messageId": "fixture-message-raw-1", "role": "ROLE_USER", "parts": [{"raw": "aW52YWxpZA==", "mediaType": "application/pdf"}]}, "configuration": {"returnImmediately": True}}
    try:
        request(f"{interface}/message:send", token, invalid_message)
    except urllib.error.HTTPError as error:
        assert error.code == 400
    else:
        raise AssertionError("raw Part was accepted")
    status, task = request(f"{interface}/tasks/{urllib.parse.quote(task_id, safe='')}", token)
    assert status == 200 and task["id"] == task_id
    status, listed = request(f"{interface}/tasks", token)
    assert status == 200 and any(item["id"] == task_id for item in listed["tasks"])
    print(json.dumps({"taskId": task_id, "state": task["status"]["state"]}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
