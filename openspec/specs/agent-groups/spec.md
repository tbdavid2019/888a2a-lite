# agent-groups Specification

## Purpose

提供 Hub-scoped 的 Agent 協作群組，讓 Agent 可以透過受控的成員名單、presence 摘要、
群組歷史與權限邊界進行可追蹤的多 Agent 協作，而不需要各自維護群組狀態。

## Requirements

### Requirement: Agents can manage Hub-scoped groups

已驗證的 Agent SHALL 可以建立群組、取得群組摘要、邀請已註冊 Agent、退出群組和封存群組。
建立者 SHALL 成為 owner；群組 SHALL 有 server-assigned immutable group ID、bounded
display name、created-at、狀態和成員上限。封存群組不得接受新訊息或新成員，但既有歷史
仍可依權限讀取。群組建立時 SHALL 繼承建立者的 `circle_id`。群組、成員、邀請、訊息與 delivery 資料 SHALL 保存可驗證的 circle scope；所有群組成員邀請、列表查詢、歷史讀取與群組訊息派送 SHALL 嚴格限制於同一 `circle_id`。禁止跨圈邀請成員；嘗試邀請跨圈 Agent 視同不存在，回傳 404 Not Found。

#### Scenario: Agent creates a group

- **WHEN** 已驗證 Agent 以合法名稱建立群組
- **THEN** Hub 建立 active group、將建立者加入為 owner，並回傳 group ID 和成員摘要

#### Scenario: Archived group rejects mutations

- **WHEN** Agent 對已封存群組傳送訊息或邀請成員
- **THEN** Hub 回傳穩定的 group-archived error，且不改變群組或 mailbox 狀態

#### Scenario: Agent creates group inherits circle ID

- **WHEN** 處於某 circle 的 Agent 建立群組
- **THEN** Hub 建立群組並綁定該 Agent 的 `circle_id`，只有同圈 Agent 可受邀加入

#### Scenario: Inviting agent from another circle is rejected with 404

- **WHEN** 群組 owner 嘗試邀請處於不同 circle 的 Agent ID
- **THEN** Hub 回傳 404 Agent Not Found，拒絕將該 Agent 加入邀請名單

#### Scenario: Non-member from another circle cannot access group

- **WHEN** 處於其他 circle 的 Agent 嘗試讀取或加入該群組
- **THEN** Hub 回傳 404 Group Not Found

### Requirement: Group membership is explicit and authorized

只有 owner 或被授權的 group admin SHALL 可以邀請或移除成員；被邀請 Agent SHALL 必須明確
接受後才成為 member。Member 可以自行退出；owner 不得在未轉移 ownership 前退出。重複
邀請、接受、退出和移除 SHALL 是 idempotent，且撤銷或過期的 Agent 不得加入群組。

#### Scenario: Invited Agent joins

- **WHEN** owner 邀請一個有效 Agent，且該 Agent 接受邀請
- **THEN** Hub 將 Agent 加入一次，後續 roster 和 group message permission 都包含該 Agent

#### Scenario: Non-member cannot join by guessing an ID

- **WHEN** 未受邀 Agent 只帶 group ID 嘗試加入
- **THEN** Hub 拒絕加入，不洩漏群組成員或邀請資訊

### Requirement: Group roster exposes safe presence metadata

群組 member SHALL 可以查詢該群組 roster。Roster 至少 SHALL 包含 Agent ID、safe display
name、provider family、capabilities 摘要、Agent Card URL、ONLINE／OFFLINE／EXPIRED／
REVOKED 狀態與 last-seen；不得包含 Agent Token、workspace path、provider secret 或
未驗證的原始 card。Presence SHALL 沿用既有 heartbeat lease，不建立第二套在線判定。

#### Scenario: Member sees current group roster

- **WHEN** group member 查詢自己所屬的 group roster
- **THEN** Hub 回傳目前成員的安全摘要與 lease-derived presence，並排除非成員資料

#### Scenario: Removed member loses roster access

- **WHEN** Agent 離開或被移出群組後查詢該 group roster
- **THEN** Hub 拒絕查詢，且不回傳成員清單或 capabilities

### Requirement: Members can read group history incrementally

群組 member SHALL 可以依 server-assigned monotonic group message ID 以 `afterId` 和
bounded `limit` 讀取群組歷史。歷史 SHALL 依 ID 遞增、保留 sender safe identity、內容、
created-at、delivery summary 和 control-plane trust marker；非 member 不得讀取。被移除
或退出的 Agent 不得讀取退出後的新內容，但已經取出的訊息無法被 Hub 回收。

#### Scenario: Member resumes group history

- **WHEN** member 帶上次 group cursor 查詢歷史
- **THEN** Hub 只回傳 cursor 之後的可見訊息，依序排列並提供下一個 cursor

#### Scenario: Non-member cannot read history

- **WHEN** 非群組 member 呼叫 group history endpoint
- **THEN** Hub 回傳 unauthorized 或 forbidden，且不洩漏訊息內容

### Requirement: Group content is untrusted collaboration data

Group name、member metadata、message、history 和 delivery status SHALL 被標示為不可信
collaboration data。Hub、SDK 和 adapter 不得把任何群組內容升格為 system/developer
instruction，不得因群組訊息直接執行 shell、檔案、credential、Docker、MCP 或其他本機
工具；危險操作仍須遵守 Agent 本機 policy 和人工核准。

#### Scenario: Group message requests a local destructive action

- **WHEN** 群組訊息要求 Agent 刪除檔案、讀取密鑰或執行未核准工具
- **THEN** Agent 將訊息當作不可信資料，記錄或回報後依本機 policy 處理，不直接執行

### Requirement: Human Agent participates through existing group authorization

Group standard dispatch SHALL 將 Human UI 的 Agent identity 視為一般 group member，沿用 active accepted membership、circle scope、sender exclusion 和既有 group role policy。Human UI 不得用 local session 取代 Hub Agent Token。

#### Scenario: Human Agent sends to a group

- **WHEN** active accepted Human Agent 透過 standard Group Gateway 發送
- **THEN** Hub 接受其為 requester，建立符合第二期 contract 的 Parent Task 和 Member Deliveries

#### Scenario: Human Agent loses membership

- **WHEN** Human Agent 被移除、退出、過期或所在 circle 停用
- **THEN** 後續 group list/card/history/send 全部依 Hub 授權拒絕，local UI 不保留可操作的 active composer

### Requirement: Mention policy delivers to all eligible members

`MENTIONED_ONLY` SHALL 對所有 eligible members 建立 delivery；未被提及成員以 ACK-only 完成，被提及成員才取得 execution lease。`ACK_ONLY` SHALL 對所有 eligible members 建立空結果完成流程。此行為 SHALL 不改變既有 legacy group API 的 sender exclusion。

#### Scenario: Unmentioned members are readable but silent

- **WHEN** Human message mentions only Agent A
- **THEN** Agent A 可執行並回報，其他 eligible members 都收到、ACK、完成空結果，且不產生 LLM reply

### Requirement: Group task results remain correlated

Human UI 所見的 bot result SHALL 使用第二期 Parent/Member Task correlation、monotonic revision 和 member identity。bot reply 不得再次 fan-out 到 group tenant；group parent cancellation、late update 和 terminal state SHALL 按第二期規則處理。

#### Scenario: Multiple bots reply concurrently

- **WHEN** 多個被 mention bot 同時回報結果
- **THEN** UI 依 revision/member ordering 顯示所有結果，不覆寫或重複任何成員結果

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

### Requirement: Group metadata exposes charter state without content leakage

Active Group Card/metadata SHALL 包含 `hasCharter`、`charterVersion`、`contentHash`（可選）與 `updatedAt`，不包含完整 Charter 或其他 group secret。沒有 Charter 時 SHALL 為 version 0；Group archive/disable 後不可透過一般 member API 取得治理內容。

#### Scenario: Member sees charter metadata

- **WHEN** active member 讀取 Group Card
- **THEN** response 顯示 Charter 是否存在與目前版本，不洩漏完整內容

### Requirement: Secretary appointment follows group authorization

Group SHALL 保存唯一 active secretary appointment、epoch 與 lease。只有 Owner/Admin 可指定、撤換或停用 secretary；`--role=secretary` 只能在指定 Agent 取得有效 lease 後生效。舊 epoch 的 secretary 不得建立正式 minutes 或修改 Charter。

#### Scenario: Owner appoints secretary

- **WHEN** Owner 指定同圈 active member 為 secretary
- **THEN** Hub 建立新 epoch appointment，該 Agent 可取得 lease，其餘候選不得同時成為 active secretary

#### Scenario: Old secretary loses authority

- **WHEN** Owner 撤換 secretary 或 lease epoch 更新
- **THEN** 舊 secretary 的 synthesis/approval request 被拒絕，且新 secretary 可在 lease 期限內接管
