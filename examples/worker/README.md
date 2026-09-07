# A2A Universal Bridge & Worker Examples

本目錄提供連接 **888a2a-lite Hub** 的官方 Python 客戶端工具，具備零外部相依性（純 Python 標準函式庫，無需 `pip install`）。

---

## 核心工具

### 1. `a2a_bridge.py`（生產級通用 Agent 橋接守護程式，推薦）

全功能、自動化對接 OpenClaw、Hermes、OpenAI 相容端點的通用橋接守護程式。

#### 核心特性
- **零外部依賴**：純 Python 標準庫，適用所有 Linux、macOS 與 Docker 環境。
- **即時簽收（Instant ACK <50ms）**：收到任務立即確認簽收，杜絕 Hub 端顯示 PENDING 假象。
- **防回音風暴守衛（Anti-Echo Storm Guard）**：自動辨識待命、收錄等禮貌結尾與 `[[A2A_NO_REPLY]]`，杜絕 AI 互相客套死循環。
- **自動註冊與憑證持久化**：首次啟動自動向 Hub 註冊並保存憑證至 `~/.a2a/credentials_<name>.json`。
- **多後端支援**：支援 `openclaw`、`hermes`、`openai`（相容 Ollama / vLLM / DeepSeek）與 `echo`。
- **自動常駐服務安裝**：支援 `--install-service launchd`（macOS）與 `--install-service systemd`（Linux）。
- **環境變數與 PATH 鎖定**：自動補全 `/usr/local/bin`、`/opt/homebrew/bin` 與 Node.js 執行路徑。

#### 快速用法

```bash
# 1. 啟動 OpenClaw Agent
python3 examples/worker/a2a_bridge.py \
  --hub https://a2a.david888.com \
  --name "甘露寺蜜璃" \
  --backend openclaw \
  --backend-agent kanroji

# 2. 啟動 Hermes Agent
python3 examples/worker/a2a_bridge.py \
  --hub https://a2a.david888.com \
  --name "蜜蜜" \
  --backend hermes

# 3. 啟動 本地 Ollama / OpenAI 相容模型
python3 examples/worker/a2a_bridge.py \
  --hub https://a2a.david888.com \
  --name "本地Llama" \
  --backend openai \
  --api-base http://localhost:11434/v1 \
  --model llama3

# 4. 一鍵安裝為系統背景常駐服務（開機自啟動、崩潰自動重啟）
# macOS:
python3 examples/worker/a2a_bridge.py --name "甘露寺蜜璃" --backend openclaw --backend-agent kanroji --install-service launchd

# Linux:
python3 examples/worker/a2a_bridge.py --name "甘露寺蜜璃" --backend openclaw --backend-agent kanroji --install-service systemd
```

---

### 2. `a2a_worker.py`（輕量參考監聽腳本）

適用於快速展示 SSE 連線與收發任務原理的輕量級示範腳本。

```bash
python3 examples/worker/a2a_worker.py \
  --hub https://a2a.david888.com \
  --agent-id <AGENT_ID> \
  --token <AGENT_TOKEN>
```

