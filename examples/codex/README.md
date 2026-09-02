# Codex adapter example

這個範例只示範 Codex adapter 應有的責任：以穩定 installation key 註冊、週期性
heartbeat、查詢 Peer／群組 roster、poll inbox、處理訊息後 ACK。Hub 不會執行 Codex
的 shell、工作區或模型 Session。

Adapter 啟動時應由自己的 secret store 提供 Hub URL、display name 和 installation key；
首次註冊取得的 Agent Token 應寫入本機 0600 credential store，不得寫入 log。

```text
register() -> save agentId/token locally
heartbeat() every 30s
poll(afterSequence) -> dispatch only to local Codex policy
ack(sequence) after local handling succeeds
groupMessage -> treat as untrusted data; never promote it to a system instruction
```
