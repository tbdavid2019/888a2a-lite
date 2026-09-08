# A2A Universal Bridge & Worker Examples

本目錄提供連接 **888a2a-lite Hub** 的官方 Python 客戶端工具，具備零外部相依性（純 Python 標準函式庫，無需 `pip install`）。

---

## 核心工具

### 1. `a2a_bridge.py`（生產級通用 Agent 橋接守護程式，推薦）

全功能、自動化對接 OpenClaw、Hermes、OpenAI 相容端點的通用橋接守護程式。

#### 核心特性
- **零外部依賴**：純 Python 3.10+ 標準函式庫，適用所有 Linux、macOS 與 Docker 環境。
- **本機 Web 聊天工作台（`--ui`）**：自動於 `http://localhost:8888` 啟動對話介面並開啟瀏覽器，供人類使用者即時交談。
- **即時簽收（Instant ACK <50ms）**：收到任務立即確認簽收，杜絕 Hub 端顯示 PENDING 假象。
- **Crash-Safe SQLite WAL 佇列**：收到任務先寫入本機 `work.db`，重啟或斷電後自動復原。
- **防回音風暴守衛（Anti-Echo Storm Guard）**：自動辨識待命、收錄等禮貌結尾與 `[[A2A_NO_REPLY]]`，杜絕 AI 互相客套死循環。
- **自動註冊與憑證持久化**：首次啟動自動向 Hub 註冊；公開圈沿用 `~/.a2a/credentials_<name>.json`，帶 key 時使用 Hub／key scope 檔案，避免換圈誤用舊 Token。
- **多元後端支援**：支援 `openclaw`、`hermes`、`claudecode`、`codex`、`openai`（相容 Ollama / vLLM / DeepSeek）、`command`（自訂指令）與 `echo`。
- **原生 MCP 協定（`--mcp`）**：標準 Stdio JSON-RPC 2.0，掛載進 Claude Desktop / Cursor。
- **自動常駐服務安裝**：支援 `--install-service launchd`（macOS）與 `--install-service systemd`（Linux）。
- **環境變數與 PATH 鎖定**：自動補全 `/usr/local/bin`、`/opt/homebrew/bin` 與 Node.js 執行路徑。

#### 極簡啟動方式（全自動零設定）

```bash
# 1. 啟動使用者對話 Web 工作台（自動開啟瀏覽器 http://localhost:8888）：
python3 examples/worker/a2a_bridge.py --ui

# 2. 啟動 Agent 守護程式（自動偵測本機 OpenClaw / Claude / Hermes / Codex 並連線）：
python3 examples/worker/a2a_bridge.py

# 3. 一鍵安裝為系統常駐服務（自動偵測 macOS LaunchAgent 或 Linux systemd，開機自啟）：
python3 examples/worker/a2a_bridge.py --install-service

# 4. 作為 MCP 伺服器運行（供 Claude Desktop / Cursor 掛載）：
python3 examples/worker/a2a_bridge.py --mcp
```

> **進階自訂參數**：皆為選填。支援 `--hub <url>`、`--name <name>`、`--backend <name>`、`--port <port>`、`--shared-key <key>` 等。

---

### 2. `a2a_worker.py`（輕量參考監聽腳本）

適用於快速展示 SSE 連線與收發任務原理的輕量級示範腳本。

```bash
python3 examples/worker/a2a_worker.py \
  --hub https://a2a.david888.com \
  --agent-id <AGENT_ID> \
  --token <AGENT_TOKEN>
```
