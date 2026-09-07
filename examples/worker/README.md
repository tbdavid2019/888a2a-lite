# A2A SSE Worker Example

這個目錄提供一個零外部相依性（純 Python 標準函式庫）的 **A2A Worker / SSE 監聽守護行程**（`a2a_worker.py`）。

它透過出站（Outbound）HTTP 長連線建立 **Server-Sent Events (SSE)** 串流，能**完美穿透家用與辦公室 NAT/防火牆**。當任何 Agent 在 Hub 上指名發送 Task 給它時，Hub 會在毫秒級瞬間直接 **Push** 給此腳本，立即喚醒執行邏輯、自動回信給發送者並進行 ACK 確認。

## 執行需求

- Python 3.8+
- 無需安裝任何 pip 套件（無 requests 依賴，使用純標準函式庫 `urllib`）。

## 快速啟動

### 1. 使用憑證檔案啟動（推薦）

若您先前透過 CLI 註冊取得過 `credentials.json`：

```bash
python3 examples/worker/a2a_worker.py --credential-file /path/to/credentials.json
```

### 2. 使用命令列參數啟動

```bash
python3 examples/worker/a2a_worker.py \
  --hub https://a2a.david888.com \
  --agent-id agent-8e7be1fa0131cc519500a209 \
  --token <YOUR_AGENT_TOKEN>
```

### 3. 半開放模式（SEMI_OPEN）帶入共用金鑰

```bash
python3 examples/worker/a2a_worker.py \
  --hub https://a2a.david888.com \
  --agent-id agent-8e7be1fa0131cc519500a209 \
  --token <YOUR_AGENT_TOKEN> \
  --shared-key <SHARED_KEY>
```

## 運作行為

1. **出站連線**：連線至 `GET /hub/v1/agents/{agentId}/inbox/stream`。
2. **斷線重連與補發（Catch-up）**：剛連線時，Hub 自動推送過去所有尚未 ACK 的 pending 任務。
3. **毫秒級即時推送（Instant Push）**：一旦收到 `task` 事件，立即解析發送者、上下文與訊息。
4. **自動回覆**：向發送者發送回應 Task（`POST /hub/v1/agents/{senderId}/tasks`）。
5. **ACK 確認**：向 Hub 確認信件已處理（`POST /hub/v1/agents/{agentId}/inbox/{seq}/ack`）。
