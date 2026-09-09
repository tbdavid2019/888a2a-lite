## ADDED Requirements

### Requirement: Existing groups expose a standard virtual tenant without changing legacy behavior

每個 active existing group SHALL 可映射至唯一 `group:<groupId>` virtual A2A tenant，並保留 `/hub/v1/groups` 的 owner/member/invitation/roster/history/message 語義。標準廣播 SHALL 建立 Parent Task、Member Delivery、group ID、circle ID、message ID 與 revision correlation；既有 group message SHALL 不被轉換成標準 Task。

#### Scenario: Legacy group API remains stable

- **WHEN** existing client 使用 `/hub/v1/groups` 發送或讀取 group message
- **THEN** response、sender exclusion、membership、ACK 與 history 語義維持不變

#### Scenario: Group has stable virtual tenant

- **WHEN** member 取得同一 active group 的 standard routing target
- **THEN** tenant 固定為 `group:<groupId>`，重啟或重試不改變，且不可與其他 group 混淆

### Requirement: Group delivery uses a membership snapshot and atomic capacity checks

Standard group fan-out SHALL 在同一 transaction 固定 active accepted eligible member snapshot，預先檢查 circle、group size、fan-out、target pending capacity 與 idempotency。任一檢查失敗 SHALL rollback 全部 parent/member/mailbox writes；後續加入者不追溯收到已建立的 broadcast，退出或移除者依 cancellation policy 處理未 ACK delivery。

#### Scenario: Membership changes during broadcast

- **WHEN** member 在 broadcast transaction 前後加入或離開
- **THEN** 該 broadcast 只使用 transaction snapshot，沒有 partial 或追溯 fan-out

#### Scenario: Fan-out capacity is exceeded

- **WHEN** 任一 eligible recipient 會超過 policy capacity 或 fan-out limit
- **THEN** Hub 回 bounded error，Parent、Member Delivery、mailbox 與 event 都不留下部分資料

### Requirement: Group results aggregate by member and preserve revisions

每個 Member Delivery SHALL 有自己的 status、turn、update id 與 revision；Parent SHALL 以 deterministic member ordering 聚合結果，保留已完成、失敗、拒絕、逾時與空結果成員摘要。重複 update 同內容冪等，改內容或過期 revision SHALL 被拒絕。

#### Scenario: Concurrent member updates

- **WHEN** 多個成員同時回報同一 Parent Task
- **THEN** Hub 以 CAS/revision 接受各自合法 update，結果順序可重建且不覆寫其他成員

#### Scenario: Late member update

- **WHEN** canceled 或 terminal Parent 收到遲到 Member update
- **THEN** Hub 拒絕 update，Parent 與既有結果保持 terminal
