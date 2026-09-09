## Context

Local UI 目前由 `examples/worker/a2a_bridge.py` 與 embedded `internal/service/a2a_bridge.py` 提供，使用 loopback `http.server`、本機 SQLite WAL、Hub client 和 P2P chat。第二期已提供 standard Group Gateway、`group:<groupId>` tenant、reply policy、mentions、Parent/Member Task 與結果回報。

第三期的核心邊界是：Human UI 是使用者工作台，不是第二套 Hub 身分系統；Runtime Console 是本機控制面，不是遠端執行 API。所有群組訊息必須走第二期 standard Group Gateway，所有本機高權限操作必須經過 local security boundary。

## Goals / Non-Goals

**Goals:**

- 使用既有 Human Agent identity、membership 與 Agent Token 進入同圈群組。
- 以 standard Group Card、`group:<groupId>`、`A2A-Extensions` 和 Parent Task stream 支援人類群聊。
- 讓無 mention 訊息通知所有 eligible bots 並靜默完成，讓有 mention 訊息仍傳送給所有 eligible bots，但只有被指定者執行 LLM。
- 提供 Runtime 的唯讀檢測、受保護設定、active backend 顯示與 user service 狀態。
- embedded 與 distributed Bridge 使用同一套 UI、group metadata、durable history 和安全規則。

**Non-Goals:**

- 不建立 Human-only principal，也不允許 UI 繞過 Hub group membership。
- 不讓 Local UI 直接呼叫 legacy `/hub/v1/groups/{id}/messages` 來傳送帶有 standard reply policy 的訊息。
- 不執行任意 shell command、root service、遠端 command 或未經 UI 授權的本機副作用。
- 不宣稱達到 Buzz 的完整 workspace；channel、thread、reaction、search、workflow、git forge、voice 與 signed event 留待其他 change。

## Decisions

### 1. 施工依賴與分層

第二期 `a2a-group-coordination-extension` 必須完成，尤其是 Group Gateway、`MENTIONED_ONLY` fan-out、Parent/Member Task update 和 cancellation contract；第三期不得自行重做這些 Hub 行為。第三期分成四個順序：runtime read-only、local security/config、human group read/write、service control。

### 2. Human UI 使用既有 Agent identity

`a2a ui` 啟動時已透過既有 registration 流程取得 Human Agent ID/Token。UI 查詢 `/hub/v1/groups` 或 `/a2a/v1/groups` 只顯示該 identity 所屬的 active groups；roster 中 human 使用 `agentType: human` 或等價本機 metadata，bot 使用 `agentType: agent`。Human 必須是 active accepted member，退出、移除、過期或 circle 停用後，UI 禁止發言並清除 active group view。

UI 不信任 display name 作為授權或 routing key。mention picker 綁定唯一 Agent ID，顯示名稱重複時追加末 6 碼 ID；mentions 必須由 Hub 再次驗證為同圈 active member。

### 3. Group UI 走 standard Gateway

流程固定為：

```text
Local UI
  ├─ GET /a2a/v1/groups
  ├─ GET /a2a/v1/groups/{groupId}/card
  └─ POST /a2a/v1/message:send|message:stream
       tenant = group:<groupId>
       A2A-Extensions = group extension URI
       message.metadata[group-extension-uri] = {replyPolicy, mentions}
              │
              ▼
       Parent Task / Member Delivery / standard Task stream
```

`POST /api/groups/{id}/messages` 是 Local UI facade；它 SHALL 將輸入轉成 standard `SendMessageRequest`，不能直接使用 legacy group endpoint。`returnImmediately=true` 適合 UI 先取得 Parent Task 再 subscribe；blocking request 的 HTTP deadline 回 504 時，本機保留 task reference，不能顯示為已完成。

### 4. `MENTIONED_ONLY` 的 delivery 語義

Human message 有 mention 時仍 fan-out 給所有 eligible bot。每個 bot 都收到同一 Parent/Member correlation：

- 未被 mention：durable commit → ACK → `COMPLETED` 空結果，不啟動 Runtime。
- 被 mention：durable commit → ACK → 取得 execution lease → 執行 Runtime → 回報 Member result。
- 無 mention：使用 `ACK_ONLY`，所有 eligible bot 都走靜默完成。

這樣 UI 才能顯示「全員已讀、指定者回答」。Hub 只驗證 extension metadata，不使用自然語言判斷是否被指名；Bridge 只依 `replyPolicy` 與 Agent ID 執行。

### 5. Local UI session 與 CSRF 防護

Local UI 維持 loopback bind，但 loopback 不視為授權。每次 server process 啟動產生高熵 local session token，注入 HTML bootstrap 或以同源 httpOnly cookie 建立 session。所有 POST/PATCH/DELETE facade，尤其 `/api/groups/*/messages`、`/api/runtimes/custom`、`/api/service/install`、`/api/service/restart`，都必須帶 `X-Local-UI-Token` 或等價 CSRF proof。

Server 同時檢查：

- `Host` 僅允許 loopback host/port，拒絕 DNS rebinding host。
- `Origin`/`Referer` 若存在必須是本機 UI origin。
- token 使用 constant-time comparison、不可寫入 log、不可放入 URL。
- mutation body bounded、`Content-Type` 嚴格、unknown fields 拒絕。
- 回應設 `Cache-Control: no-store`、`X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`。

GET 可讓 browser `EventSource` 使用，不以 GET 觸發本機副作用。Server 關閉時 token 失效。

### 6. Runtime 設定與執行安全

唯讀 detection 回傳固定 runtime ID、display name、resolved executable path、version probe 結果與狀態 `ready`/`cli_needed`/`unavailable`。Ready 只代表 binary 可被安全探測，不代表 provider login 或模型可用。

Custom Runtime 只接受：

```json
{
  "id": "custom-reviewer",
  "name": "Reviewer",
  "executable": "/absolute/path/reviewer",
  "args": ["--json"],
  "envNames": ["REVIEWER_PROFILE"]
}
```

不得接受 shell string、pipe、semicolon、redirect、command substitution 或任意 service path。envNames 只能引用既有 process environment；UI 不保存或回傳 secret value。設定檔存在 `~/.a2a`、權限 0600、atomic replace、schema version 與 bounded limits。註冊 custom runtime 不立即執行；執行時使用 `subprocess.run([...], shell=False)`，並沿用 timeout、output limit、enhanced PATH 和 process group cleanup。

### 7. Active backend 與 service lifecycle

UI 顯示的 active backend 必須有單一來源：目前 Bridge process 的 immutable startup config 加上 local persisted desired config。UI 修改後寫入 desired config，透過 managed user service restart 讓新 Bridge 讀取；不能假裝已切換正在執行的 Python process。

Install/restart 只允許目前使用者自己的 macOS LaunchAgent 或 Linux systemd user unit，unit/label 從固定 Agent scope 產生。操作必須 idempotent，回傳 `requested`、`observed`、`error` 狀態；新設定啟動失敗時保留舊設定並 rollback。不要把 `launchctl` 或 `systemctl` 的任意參數交給 browser。

### 8. Group history、events 與本機資料 scope

新增或 migration local group tables 時保存 `hubUrl/hubId`、`circleId`、`groupId`、parent/member task ID、message ID、sequence、revision、sender ID/type、reply policy、mentions、state、idempotency key 與 timestamps。唯一鍵至少涵蓋 hub/circle/group/message/turn；SSE redelivery 不得重複資料。

UI 的 group history 使用 Parent/Member revision cursor；Hub Task stream 事件與 custom local `/api/events` 事件分開 serializer，但最後都寫入同一個 scoped local store。Local UI 重啟後先以 durable cursor 補資料，再接 SSE；不同 circle/Agent identity 使用不同 local scope。收到跨圈或已被撤銷的 event 不落盤。

### 9. Buzz 對標邊界

Buzz 提供 relay event log、channel membership、Agent presence、search、workflow、git 與人類 workspace。本期只提供人類參與群組、mention/reply policy、可回放群組歷史、Agent runtime 可見性與受保護 user service control，不把視覺風格當作協定或驗收標準。

## Risks / Trade-offs

- **[Risk]** Human Agent 未加入群組卻能從 UI 發言。→ **Mitigation:** 所有 group list/card/send/history 由 Hub 用 persisted principal 授權；UI session 只作 CSRF，不作 Hub authorization。
- **[Risk]** `MENTIONED_ONLY` 只派給被提及者，無法顯示全員已讀。→ **Mitigation:** standard fan-out 保留所有 eligible member delivery，未提及者以 ACK-only 完成。
- **[Risk]** Localhost UI 被其他本機網頁觸發 service restart。→ **Mitigation:** local token、Origin/Host 檢查、no-store、mutation body bounds 和固定 managed unit。
- **[Risk]** Custom Runtime 透過 shell injection 執行任意指令。→ **Mitigation:** absolute executable + argv、shell=false、env name references、schema validation、0600 atomic config。
- **[Risk]** embedded script 與分發 script 行為分歧。→ **Mitigation:** source parity check、相同 fixture、CI 下載 embedded asset 後比對 hash。
- **[Risk]** Group stream 或 history 和 P2P cursor 混用。→ **Mitigation:** separate event envelope、scoped tables、parent/member revision cursor 與 restart replay tests。
- **[Risk]** 一次引入 UI、Runtime、service control 導致 rollback 困難。→ **Mitigation:** read-only → config → group send → privileged service 的垂直切片與 feature flags。

## Migration Plan

1. 先確認第二期 Group Extension 的 `MENTIONED_ONLY` all-member delivery、Parent Task stream 和 result update 已在 CI 通過。
2. 先交付 read-only runtime detection 與 read-only group discovery/history，確認 embedded/distributed Bridge parity。
3. 加入 local token/CSRF、Origin/Host checks 與 scoped local group schema migration。
4. 接 standard Group Gateway，完成 Human Agent membership、mentions、reply policy、Parent Task stream 與 durable replay。
5. 最後加入 custom Runtime config 與 managed user service install/restart；以失敗 rollback fixture 驗證。
6. 透過 GitHub Actions 做 Python unit、Go integration、browser、macOS/Linux service matrix，再執行指定遠端 smoke test。所有 gate 通過後才開啟 production UI controls。

## Open Questions

沒有會阻塞本期邊界的問題。Runtime provider login、Windows service、群組 thread/reaction/search、Buzz workspace parity 另立 change。
