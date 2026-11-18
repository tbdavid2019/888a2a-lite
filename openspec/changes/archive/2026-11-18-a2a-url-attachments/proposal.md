## Why

888a2a-lite 的 A2A model 已保留 `Part.url` 與 `Artifact` 欄位，但標準輸入目前只允許文字，且 Executor 回報路徑沒有對 Artifact Part 套用同一套驗證。這讓 888box 或其他物件儲存只能透過非正式文字 URL 慣例整合，也使 Message 與 Artifact 的內容邊界不一致。

現在先加入受限的外部 URL attachment profile，讓 Hub 能可靠保存與轉送檔案引用，同時維持 Hub 不下載檔案、不儲存 binary、不持有 888box credential 的架構。

## What Changes

- 新增 URL attachment 的共用 Part／Artifact 驗證規則。
- 允許標準 Message 與 Executor Artifact 使用 `Part.url`，並要求合法 HTTPS URL、MIME type、filename 與 bounded collection 大小。
- 保留 `text/plain` 支援，允許 text 與 URL Part 混合；仍拒絕 inline `raw` 與 structured `data`。
- 對 URL query 中的 credential-like 預簽名參數套用安全政策，不在 Hub log 或 audit event 中記錄完整 URL。
- 讓 Agent Card 宣告實際支援的 URL attachment MIME modes。
- 維持 Hub 不 fetch、proxy、redirect 或解析 `Part.url`；888box 上傳由 Agent adapter 或外部流程負責。
- 補充 URL attachment 的 payload、Artifact、restart、idempotency、錯誤與安全測試。

## Capabilities

### New Capabilities

- `a2a-url-attachments`: 定義外部 URL attachment 的內容驗證、安全政策、持久化與 Agent Card 宣告。

### Modified Capabilities

- `a2a-standard-transport`: Standard Gateway 從 text-only 擴充為 text plus URL-reference Part profile。
- `durable-agent-mailbox`: Standard Task 的 Message／Artifact result 支援受限 URL Part，並保持 durable correlation 與 idempotency。
- `lite-hub-http-contract`: payload、collection、錯誤與 audit 邊界涵蓋 URL attachment metadata。

## Impact

- Affected code: `internal/a2a`, `internal/hub`, `internal/service`, `internal/store/sqlite`。
- Affected APIs: `/a2a/v1/message:send`、`/a2a/v1/message:stream`、`/hub/v1/a2a/tasks/{taskId}/updates`、Task GET／subscribe responses 與 Agent Cards。
- Affected persistence: existing JSON task/artifact storage continues to store bounded metadata and URLs; the durable inbox gains optional standard Part JSON so target adapters receive the same URL references; no binary column or object-storage credential is added.
- Affected clients: adapters can upload to 888box and submit the returned HTTPS URL. Existing text-only clients remain compatible.
- No new runtime dependency or direct 888box server-side integration is introduced in this change.
