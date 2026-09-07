## ADDED Requirements

### Requirement: Recipient can subscribe to real-time inbox SSE stream

已驗證的 Agent SHALL 可以透過 `GET /hub/v1/agents/{agentId}/inbox/stream` 建立 `text/event-stream` 持續連線。Hub SHALL 在建立連線時自動發送尚未 ACK 的 pending inbox items，並在有新的 direct task 或 group delivery 抵達時，立即將該事件主動推送到 target Agent 的串流中。

#### Scenario: Stream pending tasks on connect
- **WHEN** 已註冊且已驗證的 Agent 建立 SSE stream 連線且收件匣存在未 ACK 的任務
- **THEN** Hub 立即向 stream 輸出尚未 ACK 的 inbox items，每個 event 包含 `id: <sequence>`、`event: task` 與包含 taskId、senderAgentId、contextId、body、createdAt 的 JSON payload

#### Scenario: Real-time task push upon creation
- **WHEN** requester 發送 direct task 或 group message 給已維持 SSE 連線的 target Agent
- **THEN** Hub 將任務寫入 SQLite durable inbox 後，立即將該 task 推送至 target Agent 的活躍 SSE stream，無需等待 target 輪詢

#### Scenario: Stream keep-alive and lease extension
- **WHEN** SSE stream 保持開啟但暫無新訊息
- **THEN** Hub 定期發送 `: keepalive` 註釋或 ping 事件，避免代理伺服器連線逾時，並自動展延該 Agent 的在線租約

#### Scenario: Resume stream from Last-Event-ID or query parameter
- **WHEN** Agent 斷線重連並提供 `Last-Event-ID` 標頭或 `?afterSequence=N` 參數
- **THEN** Hub 自該 sequence 之後的第一筆未 ACK 項目開始串流推送，不遺漏任何新訊息，亦不重複發送已過濾的舊序號
