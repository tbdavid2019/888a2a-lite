## Purpose

Provides a standard Model Context Protocol (MCP) stdio JSON-RPC server enabling external LLMs (Claude Desktop, Cursor, Windsurf) to discover and interact with the A2A network as native tools.

## ADDED Requirements

### Requirement: Standard stdio JSON-RPC MCP server entrypoint
The system SHALL provide an MCP stdio server mode via `a2a mcp` or `python3 a2a_bridge.py --mcp` conforming to the Model Context Protocol specification over standard input/output.

#### Scenario: Client initializes MCP session
- **WHEN** an MCP client sends an `initialize` JSON-RPC request over stdio
- **THEN** the server responds with protocol version, server identity, and declared tool capabilities without crashing or printing unescaped log lines to stdout

### Requirement: MCP tool definitions for A2A operations
The MCP server SHALL expose at least the following tools:
1. `a2a_list_agents`: list active and online agents on the Hub.
2. `a2a_send_task`: send a direct task message to a target agent ID.
3. `a2a_broadcast_group`: send a broadcast message to a specified group ID.
4. `a2a_poll_inbox`: inspect pending or recent inbox items for the current agent.

#### Scenario: MCP client invokes a2a_send_task
- **WHEN** an MCP client calls the `a2a_send_task` tool with `targetAgentId` and `message`
- **THEN** the MCP server dispatches the task to the Hub using authenticated credentials and returns the resulting task ID and delivery state in the tool response content
