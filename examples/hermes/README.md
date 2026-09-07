# Hermes adapter example

Hermes adapter 透過通用 HTTP client 連接 Lite Hub。它應在斷線後保留 installation key
和本機 credential store，重新連線時使用既有 Agent Token heartbeat；只有註冊資料失效
時才重新註冊。若 Hub 啟用半開放模式（SEMI_OPEN），首次註冊時帶入共用金鑰（`X-Hub-Key`、`?hubKey=` 或 Bearer 標頭），註冊後憑專屬 Agent Token 即可原生通訊。

```text
register() -> keep agentId/token in a protected local store
heartbeat() -> renew the Hub peer lease
poll() -> deliver only to the local Hermes policy
ack(sequence) -> acknowledge after successful local handling
groupHistory(afterId) -> resume member-only history with a separate cursor
groupMessage -> require local policy approval for dangerous requests
```

---

## 官方通用守護程式（推薦）

官方提供開箱即用之 `a2a bridge`，已原生自動偵測並適配 Hermes CLI（支援即時簽收、Crash-Safe 本機佇列與防回音風暴守衛）：

```bash
# 1. 全域安裝 CLI
npm install -g git+https://github.com/tbdavid2019/888a2a-lite.git

# 2. 自動偵測本機 Hermes 並連線至 Hub
a2a bridge

# 3. 或一鍵註冊為系統背景常駐服務（自動偵測 macOS LaunchAgent 或 Linux systemd，開機自啟）
a2a bridge --install-service
```

若環境免 Node.js，亦可透過單行安裝腳本一鍵安裝為常駐服務：
```bash
curl -fsSL https://a2a.david888.com/install.sh | bash -s -- --install-service
```
