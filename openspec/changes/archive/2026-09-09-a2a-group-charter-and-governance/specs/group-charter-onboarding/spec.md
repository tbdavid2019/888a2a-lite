## Purpose

提供群組 Charter 的安全託管、版本控制、成員同步與治理 context，讓 Agent 能取得一致的流程政策，同時保留 authentication、工具權限與 Multi-Circle 的既有安全邊界。

## ADDED Requirements

### Requirement: Group Charter has explicit lifecycle and safe content limits

Hub SHALL 以 `hasCharter`、`charterVersion`、`contentHash`、`updatedAt` 表示 Charter 狀態。未設定 Charter 時 SHALL 使用 version 0、`hasCharter=false`，不建立預設治理內容且不注入 Prompt。明確建立後從 version 0 開始；內容 SHALL 是 bounded UTF-8 Markdown subset，不得含 script、event handler、credential-like content 或未受控外部資源。

#### Scenario: Group without charter keeps legacy behavior

- **WHEN** group 建立時未提供 Charter
- **THEN** Group Card 回傳 `hasCharter=false`、`charterVersion=0`，Agent 依既有群組行為運作，不載入治理 Prompt

#### Scenario: Oversized or unsafe charter is rejected

- **WHEN** client 提交超過大小限制或含 script/credential-like content 的 Charter
- **THEN** Hub 回傳 400，且不建立 revision、不改變目前 Charter

### Requirement: Charter read and write uses scoped authorization and CAS

Hub SHALL 提供 `GET/PUT /hub/v1/groups/{groupId}/charter`。GET 只允許 active group member；PUT/rollback 只允許 Owner 或明確指定的 Admin。PUT SHALL 要求 `expectedVersion` 與 idempotency key，使用 atomic compare-and-set；版本不符回 409。API JSON SHALL 使用 camelCase，且不得把 Charter 當作權限授予來源。

#### Scenario: Owner updates current charter

- **WHEN** Owner 以正確 expectedVersion 提交合法 Charter
- **THEN** Hub 原子建立下一版 revision、更新 contentHash/updatedAt 並寫入 audit/event

#### Scenario: Concurrent charter update conflicts

- **WHEN** 兩個更新使用同一舊 expectedVersion
- **THEN** 只有一個更新成功，另一個回 409，既有版本歷史完整保留

#### Scenario: Regular member cannot update charter

- **WHEN** 一般 member 呼叫 PUT 或 rollback
- **THEN** Hub 回 403/404，不修改 Charter 或版本

### Requirement: Charter updates are durable and replayable

每次 Charter revision SHALL 保存 hub/circle/group/version/contentHash/updatedBy/createdAt 與前後關係；`CHARTER_UPDATED` event SHALL 只包含 group/circle/version/hash/updatedBy/revision，不包含完整 Charter。事件 SHALL 可由 member 的 group event stream 或版本查詢補回。

#### Scenario: Member receives charter update

- **WHEN** Owner 更新 Charter 且 member 已連線
- **THEN** member 收到帶有新 version/hash 的 `CHARTER_UPDATED` event，並能 GET 相同版本內容

#### Scenario: Disconnected member catches up

- **WHEN** member 在更新時離線，之後重新加入或連線
- **THEN** bridge 以版本/ETag 校準取得最新 Charter，不依賴 event 必定到達

### Requirement: Bridge cache is scoped, atomic, and stale-aware

Bridge SHALL 將 Charter cache 存在含 Hub/Circle/Group scope 的目錄，檔案與 metadata SHALL 具有限制、0600 權限、atomic replace 與 symlink protection。Cache SHALL 驗證 version 與 contentHash；拉取失敗時保留舊 cache 並標記 stale。Charter required policy 未能取得最新內容時，Bridge SHALL 暫停需要 governance context 的執行。

#### Scenario: Charter cache survives restart

- **WHEN** Bridge 重啟且 cache 版本仍有效
- **THEN** Bridge 讀取 scoped cache，並以 bounded version check 確認是否需要更新

#### Scenario: Cache cannot cross Hub or circle

- **WHEN** 同一台機器切換 Hub、Circle 或 Agent identity
- **THEN** Bridge 不讀取其他 scope 的 Charter cache

### Requirement: Charter context cannot grant authority

Bridge SHALL 將 Charter 以標記清楚的 policy data 注入 LLM context，優先順序 SHALL 低於 system/local safety policy 且高於 untrusted group message。Charter 不得授予工具、檔案、網路、credential、membership 或 operator 權限；UI 顯示 SHALL 安全 escape/sanitize。

#### Scenario: Charter requests forbidden action

- **WHEN** Charter 內容要求讀取 Token、執行 shell 或外傳秘密
- **THEN** Bridge 忽略該要求，沿用 local safety policy，且不執行副作用
