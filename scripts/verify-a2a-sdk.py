#!/usr/bin/env python3
"""CI gate for the exact, unmodified official A2A Python SDK."""

from __future__ import annotations

import inspect

from a2a.client.client import ClientConfig
from a2a.client.client_factory import ClientFactory
from a2a.client.transports.rest import RestTransport
from a2a.client.transports.tenant_decorator import TenantTransportDecorator
from a2a.types import AgentInterface
from a2a.utils.constants import TransportProtocol


def main() -> None:
    assert "create_from_url" in dir(ClientFactory)
    assert "create" in dir(ClientFactory)
    assert inspect.isclass(RestTransport)
    assert inspect.isclass(TenantTransportDecorator)
    assert "tenant" in AgentInterface.DESCRIPTOR.fields_by_name
    assert TransportProtocol.HTTP_JSON.value == "HTTP+JSON"
    assert ClientConfig(supported_protocol_bindings=[TransportProtocol.HTTP_JSON]).supported_protocol_bindings == [TransportProtocol.HTTP_JSON]

    # This is deliberately an introspection gate. The live interoperability
    # fixture is run separately once the Gateway exists; no serializer patch or
    # local replacement client is accepted here.
    print("official a2a-sdk supports REST HTTP+JSON and AgentInterface.tenant")


if __name__ == "__main__":
    main()
