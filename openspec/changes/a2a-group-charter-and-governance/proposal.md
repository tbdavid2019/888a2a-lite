## Why

第一至第三階段已處理 Agent 認證、Multi-Circle、群組協作、Human UI 與本機 Runtime。群組開始運作後，新的問題是規範如何被保存、更新、同步與審核，以及會議結果如何從大量對話沉澱為可追溯的決策與待辦。

第四階段建立 Group Charter 與認知治理層。Charter 是群組的「流程政策資料」，Secretary 是被 Owner 指定且持有 lease 的會議角色，Minutes/Decision/Action Item 都先產生可追溯 draft，再經人類或 Owner 確認。Hub 只託管版本與事件，不執行 LLM 或遠端工具。

## What Changes

- 新增 Group Charter CRUD、版本 CAS、版本歷史、內容 hash、Owner/Admin 權限與 durable `CHARTER_UPDATED` 事件。
- 明確定義無 Charter 的相容行為：`hasCharter=false`、version 0、不注入治理 Prompt。
- Bridge 以 Hub/Circle/Group scope 快取 Charter，原子更新、驗證 hash、處理 stale/offline 狀態。
- Charter 以不可信群組政策資料注入 LLM context，不得覆寫 system/developer/local safety policy 或授予工具權限。
- 新增 Secretary appointment、epoch/lease、接管與唯一 active secretary 規則；`--role=secretary` 只能表達本機意願，不能自行取得群組職權。
- 新增 meeting session、cutoff revision、minutes job、Decision/Action/Artifact provenance 與冪等 synthesis。
- Action Items 預設為 DRAFT，須經人類/Owner approval 後才能派發；Charter amendment 同樣經 proposal、CAS 與 Owner approval。
- 新增本機 scoped `work.db` 記憶、Markdown export 與可重試 Export Outbox；Wiki/GitHub/Webhook 整合預設關閉。
- 保留既有 `/hub/v1/groups`、A2A Group Extension、Human UI、Multi-Circle 與 Bridge 行為。

## Capabilities

### New Capabilities

- `group-charter-onboarding`: Charter schema、權限、版本歷史、更新事件、Bridge cache 與安全 Prompt context。
- `cognitive-meeting-governance`: Secretary lease、meeting session、minutes/decision/action/artifact provenance、approval 與 durable memory/export。

### Modified Capabilities

- `agent-groups`: Group Card metadata、Charter lifecycle、Secretary role 與治理事件。
- `a2a-group-coordination-extension`: Group Task 與 group event stream 能承載 Charter version、meeting command 和治理 correlation。
- `universal-bridge-distribution`: Bridge Charter cache、Secretary mode、local work DB、structured synthesis 與 export outbox。
- `human-group-interaction`: Human UI 提供 Charter 編輯/審核、minutes review 和 Action approval 的受保護入口。

## Impact

- Hub 實作應修改目前實際使用的 `internal/hub/group.go`、`internal/service/groups.go`、`internal/service/http.go`、`internal/store/sqlite/sqlite.go`、`internal/store/sqlite/group_repository.go` 與相關標準 group service；資料表目前是 `agent_group`，不是 `groups`。
- Client 實作修改 `examples/worker/a2a_bridge.py` 與 embedded `internal/service/a2a_bridge.py`，並同步 `bin/a2a.js`、README、`llms.txt` 與 client Skill。
- 不新增 Hub 向量資料庫、遠端執行、未經核准的 Action dispatch、必需的外部 Wiki/GitHub service 或拜占庭共識。
- 本階段依賴前三階段 CI/遠端驗收完成；任何未完成的群組標準 Task/結果回報不可由本階段繞過。
