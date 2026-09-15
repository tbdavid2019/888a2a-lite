## MODIFIED Requirements

### Requirement: Executor reports durable outcomes explicitly

Hub SHALL 提供自訂 `POST /hub/v1/a2a/tasks/{taskId}/updates`，只接受指定 target 的 Agent credential。update SHALL 含 updateId、turnId、expectedRevision、state 和可選 message/artifacts。同 updateId 同內容 SHALL 冪等；改內容、過期 turn 或不符 revision SHALL 拒絕。Task/result/event SHALL 同交易提交，終態不可覆寫。
Message 與 Artifact 的 Parts SHALL 使用與 standard Gateway 相同的 URL attachment validation；允許 text 與合法 HTTPS URL reference，拒絕 raw、data、非法 URL、未宣告 MIME type 與超過 collection limits 的內容。
Standard delivery SHALL preserve the validated input Message Parts in the durable inbox item so the target adapter receives URL attachment metadata rather than only the flattened message text.

#### Scenario: Executor finishes without a reply
- **WHEN** Anti-Echo 或 NO_REPLY 結束本機工作
- **THEN** Bridge 先持久化 COMPLETED 空結果回報，重試至 Hub 確認才完成本機工作，不另發回信

#### Scenario: Executor reports a file reference artifact
- **WHEN** 指定 target 回報一個包含合法 HTTPS URL Part、filename 與 mediaType 的 Artifact
- **THEN** Hub 以同一 transaction 保存 Artifact，Task GET 與 standard stream 可取得該 Artifact，且 Hub 不向 URL 發出請求

#### Scenario: Standard delivery carries the original Parts
- **WHEN** a standard Task is enqueued for a target Agent
- **THEN** the durable inbox item contains the original validated Message Parts and the legacy flattened message, with no outbound request to any Part URL

#### Scenario: Invalid artifact content is rejected
- **WHEN** Executor 回報的 Artifact 含 raw、data、非法 URL 或過大 metadata
- **THEN** Hub 回傳 bounded 4xx error，Task revision、state、Artifact 與 event 均保持不變

#### Scenario: Unauthorized executor submits result
- **WHEN** 非指定 target 嘗試回報，即使同圈且知道 taskId
- **THEN** Hub 拒絕且不改動 Task
