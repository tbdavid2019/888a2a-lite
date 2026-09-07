## MODIFIED Requirements

### Requirement: Agents can manage Hub-scoped groups

已驗證的 Agent SHALL 可以建立群組、取得群組摘要、邀請已註冊 Agent、退出群組和封存群組。
建立者 SHALL 成為 owner；群組 SHALL 有 server-assigned immutable group ID、bounded
display name、created-at、狀態和成員上限。封存群組不得接受新訊息或新成員，但既有歷史
仍可依權限讀取。群組建立時 SHALL 繼承建立者的 `circle_id`。所有群組成員邀請、列表查詢、歷史讀取與群組訊息派送 SHALL 嚴格限制於同一 `circle_id`。禁止跨圈邀請成員；嘗試邀請跨圈 Agent 視同不存在，回傳 404 Not Found。

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
