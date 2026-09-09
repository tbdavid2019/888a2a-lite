## Purpose

讓本機人類使用者以既有 Hub Agent 身分參與同圈群組，透過 standard Group Gateway、mentions、reply policy、Parent Task stream 與 durable history 進行可控的人機群聊。

## ADDED Requirements

### Requirement: Human UI uses an existing Agent identity and group membership

Local UI SHALL 使用已核發的 Human Agent ID/Token 查詢與操作群組，不得建立 Human-only authorization。只有 active accepted group member 可以讀取 group roster/history、取得 Group Card 或發送訊息；退出、移除、過期或 circle 停用後 SHALL 禁止發言。

#### Scenario: Human member opens a group

- **WHEN** Human Agent 查詢同圈 active group
- **THEN** UI 顯示群組、roster 和 history，且 Hub 以 persisted Agent principal 驗證 membership

#### Scenario: Non-member attempts to post

- **WHEN** 已認證但未加入群組的 Human Agent 呼叫 local group facade
- **THEN** facade 回傳 masked/forbidden error，不建立 standard task 或 mailbox delivery

### Requirement: Local UI supports safe mention autocomplete

群組 composer SHALL 由同圈 active roster 產生 mention suggestions。候選項 SHALL 綁定唯一 Agent ID；同 display name 必須顯示可區分的 Agent ID 尾碼。前端不得將 display name 當作授權或 routing key，mentions SHALL 在 Hub 再次驗證。

#### Scenario: Typing @ triggers member autocomplete

- **WHEN** user 在群組 composer 輸入 `@`
- **THEN** UI 顯示該 active group roster 中的安全候選，並以唯一 Agent ID 產生選取結果

#### Scenario: Duplicate display names remain unambiguous

- **WHEN** 多個成員有相同 display name
- **THEN** picker 顯示區分資訊，送出的 mentions 仍只包含正確 Agent IDs

### Requirement: Human messages use standard Group Gateway and explicit reply policy

Local facade `POST /api/groups/{groupId}/messages` SHALL 將請求轉成 standard `SendMessageRequest`，使用 `tenant: "group:<groupId>"`、Group Extension `A2A-Extensions` header 和 extension metadata。無 mention SHALL 使用 `ACK_ONLY`；有 mention SHALL 使用 `MENTIONED_ONLY` 並包含驗證後 Agent IDs。所有 eligible bots 都收到 delivery；未被 mention 者靜默完成。

#### Scenario: Human sends an announcement without mentions

- **WHEN** user 發送不含 mention 的文字
- **THEN** UI 使用 standard Gateway 發送 ACK_ONLY，所有 eligible bots 可讀取並完成空結果，不執行 LLM

#### Scenario: Human mentions one or more bots

- **WHEN** user 發送含一個或多個有效 mention 的文字
- **THEN** UI 使用 MENTIONED_ONLY 與 mentions IDs 發送給所有 eligible bots，只有被指定 bots 取得執行資格

#### Scenario: Legacy group endpoint is not used for policy messages

- **WHEN** UI 發送帶 reply policy 或 mentions 的訊息
- **THEN** request 不直接呼叫 legacy `/hub/v1/groups/{id}/messages`，而使用 standard Group Gateway

### Requirement: Group Task results are visible in the human timeline

UI SHALL 保存 parent/member task correlation、reply policy、mentions、sender type、sequence/revision 和 aggregated result。發送後 SHALL 以 standard Task response/stream 或 subscribe 取得 bot 回覆；不得只等待 custom P2P `/api/events`。

#### Scenario: Mentioned bot reply appears in timeline

- **WHEN** mentioned bot 回報 correlated completed result
- **THEN** UI 將結果以該 bot sender identity 加入正確 group timeline，不建立新的 reciprocal group broadcast

#### Scenario: Group task survives UI restart

- **WHEN** Local UI 重啟或 SSE 中斷
- **THEN** UI 以 scoped durable revision cursor 補回 parent/member events，且同一事件不重複顯示

### Requirement: Group history is scoped and bounded

Local group history SHALL 以 hub/circle/group/parent task/revision scope 查詢，使用 bounded cursor、idempotency key 和 safe rendering。P2P history 與 group history 的 cursor 不得混用；不同 circle/Agent identity 不得讀取舊資料。

#### Scenario: Cross-circle history is unavailable

- **WHEN** local client 嘗試以不同 circle context 查詢 group history
- **THEN** server 回傳空或 masked error，不返回任何舊圈訊息
