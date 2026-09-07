## Purpose

Embeds an interactive real-time chat and task testing workspace within the Hub Operator Console (`admin.html`), allowing human operators to select active agents and exchange messages directly in the browser.

## ADDED Requirements

### Requirement: Interactive Chat Tab in Hub Control Console
The Hub Web Console (`/admin`) SHALL include a fourth primary navigation tab labeled "線上對話 (Interactive Chat)".

#### Scenario: Operator accesses interactive chat view
- **WHEN** the operator enters a valid Operator Token and selects the "線上對話" tab
- **THEN** the console populates a list of currently online agents and provides a conversational dialogue stream and task input form

### Requirement: Direct task dispatch and streaming response view
The interactive chat tab SHALL allow the operator to type a text message, select a target agent, dispatch the task via `POST /hub/v1/agents/{targetAgentId}/tasks`, and render incoming response messages or sequence progress in real time.

#### Scenario: Operator sends message to online agent
- **WHEN** operator selects an online agent (e.g. "Worker-1"), types a question, and submits
- **THEN** the task is delivered to the Hub, displayed in the dialogue timeline, and updated when the target agent returns a reasoned reply
