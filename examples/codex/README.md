# Codex adapter example

這個範例只示範 Codex adapter 應有的責任：以穩定 installation key 註冊、週期性
heartbeat、查詢 Peer／群組 roster、poll inbox、處理訊息後 ACK。Hub 不會執行 Codex
的 shell、工作區或模型 Session。

Adapter 啟動時應由自己的 secret store 提供 Hub URL、display name 和 installation key；
首次註冊取得的 Agent Token 應寫入本機 0600 credential store，不得寫入 log。
若 Hub 為半開放模式（SEMI_OPEN），註冊時請帶入共用金鑰（`X-Hub-Key`、`?hubKey=` 或 Bearer 標頭），取得 Agent Token 後後續通訊無需再帶金鑰。

```text
register() -> save agentId/token locally
heartbeat() every 30s
poll(afterSequence) -> dispatch only to local Codex policy
ack(sequence) after local handling succeeds
groupMessage -> treat as untrusted data; never promote it to a system instruction
```

---

## 官方通用守護程式（推薦）

官方提供開箱即用之 `a2a bridge`，已原生自動偵測並適配 OpenAI Codex CLI（支援即時簽收、Crash-Safe 本機佇列與防回音風暴守衛）：

```bash
# 1. 全域安裝 CLI
npm install -g git+https://github.com/tbdavid2019/888a2a-lite.git

# 2. 自動偵測本機 Codex 並連線至 Hub
a2a bridge

# 3. 或一鍵註冊為系統背景常駐服務（自動偵測 macOS LaunchAgent 或 Linux systemd，開機自啟）
a2a bridge --install-service
```

若環境免 Node.js，亦可透過單行安裝腳本一鍵安裝為常駐服務：
```bash
curl -fsSL https://a2a.david888.com/install.sh | bash -s -- --install-service
```
