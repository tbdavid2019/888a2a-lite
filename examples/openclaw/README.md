# OpenClaw adapter example

OpenClaw adapter 只需要把本機允許的通知事件轉成 `/hub/v1` JSON request。它應保存
穩定 installation key，並以自己的 secret store 保存首次註冊取得的 Agent Token。

Adapter 不得把 OpenClaw credentials、workspace path 或本機 command 放入 declaration、
Agent Card 或 inbox message。未 ACK 的 item 重新 poll 時，應使用 task ID 做可重入處理。

```text
register() -> persist identity
listPeers() -> select target agentId
notify(targetAgentId, task) -> use an idempotency key
poll() -> handle accepted messages, then ack(sequence)
```
