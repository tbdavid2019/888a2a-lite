## Context

目前群組資料在 `agent_group`、`group_member`、`group_message` 等 SQLite table，由 `internal/service` 和 `internal/store/sqlite` 管理。Standard Group Extension 已有 Parent/Member Task、reply policy、mentions 與事件 revision；第三期 Local UI/Bridge 已能承載 Human Agent 與本機 durable queue。第四期增加 governance data，不能把 Charter 當成 authentication 或 runtime permission。

第一期 A2A Protocol 是 1.0，Python SDK 的版本另行固定；本文件不把 SDK 版本寫成 Protocol 版本。第四期必須等前三期的官方 SDK、Group Extension、Human UI 與 Runtime security gate 通過。

## Goals / Non-Goals

**Goals:**

- 提供可讀、可版本化、可審計、可回滾的群組 Charter。
- 讓 Bridge 在有限、可驗證的政策 context 下使用 Charter，不改變本機安全權限。
- 讓指定 Secretary 以 durable lease 產出可追溯的 minutes、decisions 和 action items。
- 讓人類審核治理結果，再以 outbox 方式執行檔案或外部匯出。

**Non-Goals:**

- Charter 不授予 shell、檔案、網路、provider 或其他本機工具權限。
- 不自動派發未經人類/Owner 確認的 Action Item。
- 不把摘要宣稱為真實決策；所有決策先是 draft 並帶來源證據。
- 不實作中心端向量搜尋、Buzz 完整 workspace、thread/reaction/search/workflow/git/voice/signed-event 或拜占庭投票。

## Decisions

### 1. 先完成 4A，再做 4B，最後做 4C

施工切片固定為：

```text
4A Charter Core
  schema / ACL / CAS / history / event / cache / safe context
        ↓
4B Secretary & Memory
  secretary lease / session cutoff / minutes draft / provenance / approval
        ↓
4C Export
  Markdown / Wiki / GitHub / Webhook outbox and retry
```

4A 的 CI gate 未通過時，4B 不啟用；4B 未完成 approval/provenance 時，4C 不啟用。

### 2. Charter lifecycle 與資料模型

在 `agent_group` 加入 `charter_version`、`has_charter`、`charter_content_hash`、`charter_updated_at`；完整內容與歷史放在 `group_charter_revision`，欄位至少包含 `hub_id`、`circle_id`、`group_id`、version、content、content_hash、updated_by、created_at、superseded_at。API JSON 使用 camelCase：`charterVersion`、`hasCharter`、`contentHash`、`updatedAt`。

沒有明確 Charter 時 version=0、hasCharter=false，Bridge 不注入 Charter。建立 Charter 由 version 0 變 1；更新必須帶 `expectedVersion` 與 idempotency key，使用 transaction/CAS。版本衝突回 409；相同 key/相同 content 回原結果；相同 key/不同 content 回 409。保留歷史，Owner 可提出 rollback，但 rollback 仍建立新版本。

Charter body 限制大小（建議 32 KiB）、UTF-8、Markdown subset，禁止 raw script/event handler、credential-like content 和未受控外部資源。UI 顯示必須 escape/sanitize，不把 Markdown 直接塞入 `innerHTML`。

### 3. ACL 與 Charter 的權限邊界

GET Charter 只允許 active group member；PUT、amendment approve 和 rollback 只允許 Owner 或明確指定的 Admin role。Operator 可以依既有 operator policy 審計或停用，不自動成為群組居民。Charter 只能描述 RACI、發言政策、決策門檻與輸出格式；不得改變 group membership、Agent authentication、tool permission、circle boundary 或 operator policy。

### 4. CHARTER_UPDATED event 與快取

更新 transaction 同時寫 charter revision、durable group event 和 audit record；broker 只負責即時喚醒。事件只包含 group/circle/version/hash/updatedBy/revision，不包含完整 Charter。Bridge 收到事件後依版本 GET Charter，驗證 hash 後寫入 scoped cache：`~/.a2a/groups/<hubScope>/<circleId>/<groupId>/charter.md` 與 metadata file。cache 使用 0600、atomic replace、symlink protection、size limit。

事件遺失時，Bridge 在 accept、啟動、連線或執行 group task 前以版本/ETag 校準。拉取失敗保留舊快取並標記 stale；若 Charter 是 optional，仍可依 local policy 執行，並將 stale 狀態記錄；若群組明確宣告 Charter required，則停止需要 governance context 的執行並回報原因。

### 5. Safe Prompt context

Prompt 組裝順序固定為 provider/system safety、local policy、Human-approved Charter snapshot、untrusted task/message。Charter snapshot 使用清楚的 data delimiter 和版本/hash，不得使用「你現在的 system instruction」等可提升權限的文字。LLM 輸出需經 structured schema validation；Charter 不可要求讀取 credential、執行 command、改寫 ACL 或自動外傳資料。

### 6. Secretary appointment、lease 與 session

新增 `group_secretary`：`hub_id`、`circle_id`、`group_id`、`agent_id`、epoch、state、lease_expires_at、appointed_by、updated_at。Owner/Admin 透過受保護 API 指定、撤換或停用；`--role=secretary` 只有在 Agent 已被指定且成功取得 lease 時才生效。每個 group/epoch 最多一個 active lease；接管必須 CAS/epoch 遞增，舊秘書的 synthesis/approve request 全部失效。

新增 `meeting_session`：`session_id`、group/circle、start_revision、cutoff_revision、trigger_message_id、triggered_by、charter_version、state、synthesis_job_id。`/minutes`、`/wrapup`、`/summary` 只接受 Human/Owner 或 Charter 明確授權的 member；同一 command/message/job idempotent。摘要輸入以 immutable cutoff revision 固定，避免新訊息混入舊會議。

### 7. Structured governance output 與 approval

Secretary 先產生 JSON，再渲染 Markdown。每個 Decision、Action、Artifact 至少保存 source event/message IDs、revision range、session、Charter version、proposer、synthesis model/schema version、content hash 和 `needsReview`。

Action state 為 `DRAFT → APPROVED → DISPATCHED → COMPLETED/CANCELED`；Decision 為 `DRAFT → CONFIRMED/REJECTED`。Assignee 用 Agent ID，不用 display name；deadline 包含 timezone。預設不 dispatch，只有 Human/Owner 明確核准且通過 idempotency/authorization 才可使用 standard task。Artifact path/URL 只當不可信 reference，不自動開啟或讀取。

Charter amendment 使用 `charter_amendment` 保存 baseVersion、proposed content/diff、proposer、status、approvedBy、appliedVersion；Owner approve 時以 expectedVersion CAS 寫入新 Charter version。Secretary 無法直接改 Charter。

### 8. Local memory 與外部 Export Outbox

`work.db` 的 `group_minutes`、`group_decisions`、`group_action_items`、`export_outbox` 都以 hub/circle/group/session scope。資料庫 migration、0600、WAL lock、retention 和刪除規則要明確。Markdown export 先寫 temporary file 再 atomic rename。

Wiki/GitHub/Webhook 都經 `export_outbox`，包含 provider、payload hash、idempotency key、attempts、next_retry_at、state、last_error、remote_id。外部 connector 預設關閉，credential 只讀 process secret store/env，不寫 DB/log/UI；Webhook 使用 HTTPS、allowlist、timeout、重試與簽名。產生 minutes 的 transaction 不直接做網路呼叫。

### 9. Buzz 對標範圍

Buzz 的 relay/event log、membership、audit、search、workflow、git 與 workspace 是更大的平台。本期對標「群組章程、成員治理、會議結果與事件可追溯」，不把單純 Markdown summary 宣稱為 Buzz parity。

## Risks / Trade-offs

- **[Risk]** Charter 被用作 Prompt Injection。→ **Mitigation:** policy data delimiter、固定優先順序、schema/size validation、禁止授權語句與工具權限。
- **[Risk]** Concurrent PUT 造成遺失更新。→ **Mitigation:** expectedVersion CAS、revision history、idempotency 和 409 conflict。
- **[Risk]** 多個秘書同時產生 minutes。→ **Mitigation:** owner appointment、epoch、single lease、job idempotency。
- **[Risk]** Secretary 遺漏事件或重啟後重複摘要。→ **Mitigation:** immutable cutoff revision、durable event replay、synthesis job outbox。
- **[Risk]** Action Item 誤派或重複派發。→ **Mitigation:** draft-by-default、Human approval、Agent ID resolution、idempotency 和 dispatch audit。
- **[Risk]** 外部匯出洩漏企業資料。→ **Mitigation:** opt-in connector、secret separation、host allowlist、redaction、signed HTTPS、outbox retry。
- **[Risk]** 本機多 Hub/Circle 共用記憶。→ **Mitigation:** every table/cache/export key includes hub/circle/group scope and migration tests。

## Migration Plan

1. 先完成前三階段固定版本與 CI/遠端驗收 gate。
2. 4A 先加入 Charter schema/history/ACL/CAS/event/cache/safe context；未設定 Charter 維持舊行為。
3. 4A 通過後啟用 4B：Secretary appointment/lease、session cutoff、minutes draft、provenance 與 Human approval。
4. 4B 通過後啟用 4C：Markdown，再逐一加入 Wiki/GitHub/Webhook outbox connector。
5. 任一 connector 失敗只影響該 export job，不回滾已核准的 minutes/decision，也不直接重送 group message。
6. 回滾只關閉對應 feature flag；保留版本歷史、draft、approval 與 outbox，避免破壞既有群組資料。

## Open Questions

沒有會阻塞本期邊界的問題。Quorum/投票決策、向量搜尋、跨群組治理、Buzz workspace parity 與 Windows service 另立 change。
