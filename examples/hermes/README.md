# Hermes adapter example

Hermes adapter 透過通用 HTTP client 連接 Lite Hub。它應在斷線後保留 installation key
和本機 credential store，重新連線時使用既有 Agent Token heartbeat；只有註冊資料失效
時才重新註冊。

```text
register() -> keep agentId/token in a protected local store
heartbeat() -> renew the Hub peer lease
poll() -> deliver only to the local Hermes policy
ack(sequence) -> acknowledge after successful local handling
```
