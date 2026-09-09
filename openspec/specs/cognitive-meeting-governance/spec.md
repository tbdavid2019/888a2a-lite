# cognitive-meeting-governance Specification

## Purpose
提供具有唯一角色、可追溯來源、可審核狀態與可靠匯出的會議治理流程，將群組對話轉為可驗證的 minutes、decisions、action items 與 artifact references。

## Requirements

### Requirement: Secretary uses durable appointment and meeting sessions

Secretary SHALL 是 Hub 指定且持有有效 epoch/lease 的 group member；本機 `--role=secretary` 不得自行取得職權。每次 meeting SHALL 建立 immutable session cutoff revision、trigger identity、Charter version、synthesis job ID 與 state。只有 Human/Owner 或 Charter 明確授權的 member 可觸發 `/minutes`、`/wrapup`、`/summary`。

#### Scenario: Authorized member starts a synthesis job

- **WHEN** authorized participant 發送 `/wrapup`
- **THEN** designated secretary 建立一個以 cutoff revision 為界的 idempotent synthesis job

#### Scenario: Unauthorized command is ignored

- **WHEN** 未授權 member 或不可信 Agent 發送 minutes command
- **THEN** Hub/Bridge 不建立 synthesis job，也不觸發 LLM 或外部 export

### Requirement: Synthesis output is structured and source-traceable

Secretary SHALL 先產生可驗證 JSON，再渲染 Markdown。每個 Decision、Action Item、Artifact Reference SHALL 保存 source event/message IDs、revision range、session ID、Charter version、proposer、synthesis schema/model version、contentHash 與 review state。摘要不得把 LLM 推測標成已確認決策。

#### Scenario: Minutes preserve source evidence

- **WHEN** secretary 完成 synthesis
- **THEN** minutes/decision/action records 可追溯到原始事件範圍，且包含目前 Charter version 與 needsReview 狀態

#### Scenario: Chit-chat is excluded safely

- **WHEN**輸入包含寒暄、重試日誌與技術討論
- **THEN**輸出可排除低價值噪音，但不得刪除被引用為決策依據的 substantive event

### Requirement: Governance results require explicit approval

Decision SHALL 使用 `DRAFT/CONFIRMED/REJECTED`；Action Item SHALL 使用 `DRAFT/APPROVED/DISPATCHED/COMPLETED/CANCELED`。Action Item 預設不得 dispatch；核准者必須是 Human/Owner，assignee 必須解析為 Agent ID，deadline 必須帶 timezone。Charter amendment SHALL 先成為 proposal，使用 base version CAS，經 Owner approve 後才套用。

#### Scenario: Human approves an action

- **WHEN** Human/Owner 審核合法 draft action 並確認
- **THEN** action 進入 APPROVED，可由受控 outbox 以 idempotency key dispatch

#### Scenario: Secretary cannot auto-dispatch

- **WHEN** secretary synthesis 產生 assigned action
- **THEN** action 保持 DRAFT，不直接發送 Hub task

#### Scenario: Charter amendment uses approval and CAS

- **WHEN** Owner approve 基於目前 Charter version 的 amendment
- **THEN** Hub 建立下一 Charter revision；base version 過期時回 409 且不套用

### Requirement: Local governance memory is durable and scoped

Bridge SHALL 在 `work.db` 保存 `group_minutes`、`group_decisions`、`group_action_items`，所有 record SHALL 包含 hub/circle/group/session scope、revision、Charter version、state、source provenance 與 timestamps。WAL migration、0600、retention 和 concurrent writer lock SHALL 被定義；不同 scope 不得互讀。

#### Scenario: Governance memory survives restart

- **WHEN** secretary/Bridge 重啟
- **THEN** 可依 group/session 查詢既有 minutes、decisions、actions 與 provenance，不重複建立 synthesis

### Requirement: External exports use an opt-in durable outbox

Markdown、Wiki、GitHub、Webhook export SHALL 經 `export_outbox`，保存 provider、payload hash、idempotency key、attempt、retry time、state、last error 與 remote ID。Connector 預設關閉；credential 不得寫入 DB、log 或 UI；Webhook 只允許 HTTPS/allowlist/timeout/signature。產生 minutes 的 transaction 不得直接進行網路呼叫。

#### Scenario: Export retry is idempotent

- **WHEN** external export request timeout 或 process restart
- **THEN** outbox 以相同 idempotency key 重試，不重複建立 remote artifact

#### Scenario: Export failure does not erase minutes

- **WHEN** Wiki/GitHub/Webhook export 失敗
- **THEN** minutes/decision 保持已核准狀態，只有該 export job 進入 retry/dead-letter
