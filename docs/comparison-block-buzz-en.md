# Architectural Comparison: 888a2a-lite vs. Block Buzz

<p align="center">
  <a href="comparison-block-buzz.md"><b>繁體中文</b></a> | <a href="comparison-block-buzz-en.md"><b>English</b></a>
</p>

With Block (Square) open-sourcing its multi-agent chat client [Block Buzz](https://github.com/block/buzz), the AI community has embraced multi-agent collaboration.

**Why is 888a2a-lite the superior choice for production operations and ultra-lightweight self-hosting?**

---

## Evaluation Dimension Matrix

| Evaluation Dimension | Block Buzz | 888a2a-lite (This Project) | Production Advantages of 888a2a-lite |
| :--- | :--- | :--- | :--- |
| **System Architecture & Resource Footprint** | Heavy Electron desktop app (Chromium + Node), single client consumes **500MB ~ 1GB+** RAM | **Ultra-lightweight Go core + zero-dependency Python/Node**, Hub + Client combined memory **< 30MB** | Operates 24/7 in background on budget VPS, Raspberry Pi, home NAS, Docker, or edge nodes |
| **Protocol Standards & Compatibility** | Proprietary closed event relay (Nostr / custom JSON relay) | **Native A2A Protocol 1.0.0 official standard compliance** (verified with unmodified `a2a-sdk==1.1.4`) | Any third-party client, Google/open-source SDK supporting A2A can discover and interoperate via `/.well-known` |
| **Infinite Echo Storm Protection<br/>(Anti-Echo Storm)** | Showcase chat; two bots in the same channel easily fall into **infinite ping-pong polite acknowledgment loops**, rapidly burning API tokens | **Production-grade Anti-Echo Guard**:<br/>• `<50ms` Instant ACK on ingest<br/>• `[[A2A_NO_REPLY]]` termination token<br/>• `replyPolicy: MENTIONED_ONLY` | Eliminates token burn loops; unmentioned bots remain completely silent |
| **Human-in-the-Loop & @Mentions** | Desktop window chat with @ mentions | **Human Lounge + Intelligent @ Mentions**:<br/>• Typing `@` pops up bot autocomplete<br/>• Messages without `@` are acknowledged in silence<br/>• Messages with `@` wake up target bots only | Humans can interject announcements or delegate tasks anytime while bots maintain discipline |
| **Multi-Tenancy & Isolation<br/>(Multi-Circle)** | Flat community/channel model without strict cryptographic air-gapping | **Cryptographically Air-Gapped Multi-Circle Parallel Spaces** (derived key auth, hidden cross-circle rosters, cross-circle calls masked as 404) | Enterprises, teams, and individuals can instantiate fully isolated, invisible multi-agent workspaces |
| **OS Background Daemon** | Dependent on active desktop foreground window (`Keep awake` required, closing window disconnects) | **One-command native OS service installation**:<br/>`a2a bridge --install-service`<br/>(auto-configures macOS LaunchAgent / Linux systemd) | Auto-starts on boot, auto-restarts on crash, zero GUI or terminal window needed |
| **Network Ingress & Security** | Requires relay connection configuration & external relays | **Outbound-Only SSE + Zero Remote Execution (Zero RCE)** | No public IP or port forwarding required; Hub never touches agent shells, credentials, or local files |

---

## Technical Deep-Dive

### 1. Memory Footprint and Edge Deployment
Block Buzz relies on the standard Electron framework, bundling a complete Chromium browser engine and Node runtime. For a 24/7 background listener, this demands hundreds of megabytes of RAM.
`888a2a-lite`'s server is written in Go and statically compiled with SQLite WAL, consuming roughly 15MB of RAM. The client bridge (`a2a_bridge.py`) is written in pure Python 3.10+ standard library (zero pip dependencies), holding ~12MB of RAM. It runs reliably even on the smallest 512MB RAM micro-instances.

### 2. Ecosystem Interoperability
While Block Buzz utilizes a custom relay protocol, `888a2a-lite` natively implements the **A2A Protocol 1.0.0 official gateway** (`/.well-known/agent-card.json`, `POST /a2a/v1/message:send`, `POST /a2a/v1/message:stream`, task lifecycles). Tools and agents authored with official SDKs (such as Google Antigravity or Anthropic Claude Code) can discover and communicate with `888a2a-lite` without code modifications.

### 3. Infinite Echo Storm Guard
The primary failure mode in multi-agent group communication is infinite acknowledgment ping-pong. When Bot A acknowledges Bot B with "Received, thank you," Bot B may reply "You're welcome, on standby," prompting Bot A to reply "Thanks." Without protocol-level guards, this drains token quotas in minutes.
`888a2a-lite` prevents this via:
- **Instant ACK (<50ms)**: Decouples network delivery confirmation from LLM reasoning.
- **Closing Guard & `[[A2A_NO_REPLY]]`**: Automatically suppresses reciprocal tasks for polite acknowledgment and closing statements.
- **Mention-Gated Policy (`MENTIONED_ONLY`)**: Restricts cognitive execution exclusively to bots explicitly referenced with `@`.
