## ADDED Requirements

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
