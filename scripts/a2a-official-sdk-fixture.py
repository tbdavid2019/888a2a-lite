#!/usr/bin/env python3
"""Run the A2A 1.0 flow with the unmodified official Python SDK.

The CI job runs this fixture when A2A_OFFICIAL_BASE_URL, A2A_OFFICIAL_CARD_URL,
and A2A_OFFICIAL_TOKEN are supplied by the deployment environment.
"""

from __future__ import annotations

import asyncio
import os

import httpx
from a2a.client import ClientConfig, ClientFactory
from a2a.client.card_resolver import parse_agent_card
from a2a.types import CancelTaskRequest, GetTaskRequest, ListTasksRequest, Message, Part, Role, SendMessageConfiguration, SendMessageRequest
from a2a.utils.constants import TransportProtocol


async def main() -> None:
    card_url = os.environ["A2A_OFFICIAL_CARD_URL"]
    token = os.environ["A2A_OFFICIAL_TOKEN"]
    async with httpx.AsyncClient(headers={"Authorization": f"Bearer {token}"}) as http:
        card_response = await http.get(card_url)
        card_response.raise_for_status()
        card = parse_agent_card(card_response.json())
        config = ClientConfig(httpx_client=http, supported_protocol_bindings=[TransportProtocol.HTTP_JSON], polling=True)
        client = ClientFactory(config).create(card)
        request = SendMessageRequest(message=Message(message_id="official-sdk-fixture", role=Role.ROLE_USER, parts=[Part(text="official SDK ping")]), configuration=SendMessageConfiguration(return_immediately=True))
        task_id = None
        async for event in client.send_message(request):
            if event.HasField("task"):
                task_id = event.task.id
                break
        assert task_id, "task_id not received from stream"
        task = await client.get_task(GetTaskRequest(id=task_id))
        assert task.id == task_id
        list_resp = await client.list_tasks(ListTasksRequest(page_size=10))
        assert any(t.id == task_id for t in list_resp.tasks)
        canceled = await client.cancel_task(CancelTaskRequest(id=task_id))
        assert canceled.id == task_id
        await client.close()
    print(f"official-sdk-task={task_id}")


if __name__ == "__main__":
    asyncio.run(main())
