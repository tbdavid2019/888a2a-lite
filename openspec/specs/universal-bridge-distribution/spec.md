# universal-bridge-distribution Specification

## Purpose
Provides zero-friction single-line cross-host distribution mechanisms for 888a2a-lite, including a Hub-hosted POSIX bootstrap installer script and npm global package tooling.

## Requirements

### Requirement: Hub serves standalone POSIX bootstrap installer script
The Hub SHALL serve an executable POSIX shell script at `GET /install.sh` and the raw bridge Python script at `GET /a2a_bridge.py`. The installer script SHALL support execution via `curl -fsSL https://<hub>/install.sh | bash -s -- <args>`.

#### Scenario: Single-line installer execution
- **WHEN** user executes `curl -fsSL https://<hub>/install.sh | bash -s -- --name "MyAgent" --backend openclaw` on a Linux or macOS host with Python 3.10+
- **THEN** the installer verifies Python 3 availability, downloads `a2a_bridge.py` to `~/.a2a/bin/` or `/usr/local/bin/`, marks it executable, and initiates agent bridge execution

#### Scenario: Single-line service installation
- **WHEN** user provides `--install-service` to the piped installer script
- **THEN** the installer sets up and starts the corresponding systemd user unit on Linux or launchd plist on macOS, reporting service status and logs directory

### Requirement: NPM package distribution exposes global a2a binaries
The repository SHALL provide an npm package definition (`package.json`) defining the `888a2a` package with `bin` entries for `a2a` and `a2a-bridge`.

#### Scenario: NPM global installation
- **WHEN** user executes `npm install -g git+https://github.com/tbdavid2019/888a2a-lite.git` or `npm install -g 888a2a`
- **THEN** the `a2a` and `a2a-bridge` command-line utilities become globally available in the user's terminal environment

### Requirement: Embedded and distributed Bridge assets remain behaviorally identical

`examples/worker/a2a_bridge.py` 與 embedded `internal/service/a2a_bridge.py` SHALL 使用相同的 Local UI API、Group Gateway mapping、reply policy、runtime validation、local security token 與 durable history behavior。CI SHALL 比對來源/embedded asset parity 或等價產物。

#### Scenario: Downloaded bridge matches embedded bridge

- **WHEN** CI 取得 Hub 的 `/a2a_bridge.py` 與 repository distributed script
- **THEN** 版本、security behavior、group metadata mapping 和 standard update handling 一致

### Requirement: Local APIs provide scoped group history and standard dispatch

Local UI SHALL 提供 bounded `GET /api/groups`、`GET /api/groups/{id}/messages` 和 `POST /api/groups/{id}/messages`。POST facade SHALL 使用 existing Human Agent Token 經 standard Group Gateway 發送，帶 `tenant`、`A2A-Extensions`、reply policy、mentions、Parent Task reference 和 idempotency；local store SHALL 保存 hub/circle/group/task/revision scope。

#### Scenario: Local group send uses standard contract

- **WHEN** user 透過 local facade 發送帶 `text`、`replyPolicy` 和 `mentions` 的訊息
- **THEN** server 轉成 standard Group Message、保存本地 pending record、回傳 Parent Task reference，並可透過 stream/subscribe 更新狀態

#### Scenario: Local history pagination is bounded

- **WHEN** client 查詢 group history
- **THEN** server 驗證 group/circle scope、bounded cursor/limit，並以 idempotent event hydration 回傳結果

### Requirement: Local mutation APIs have a browser security boundary

Local UI SHALL 為 process session 產生高熵 token，所有 POST/PATCH/DELETE facade SHALL 驗證 token、Origin/Host、Content-Type、body limit 和 unknown fields。GET history/event APIs SHALL 不觸發 OS mutation；錯誤不得回傳 stack trace、command output secret 或 credentials。

#### Scenario: Missing local token is rejected

- **WHEN** browser 呼叫 local mutation API 但沒有有效 local token
- **THEN** server 回 401/403，不寫入 group/runtime config，也不啟動 service

### Requirement: Group events hydrate after local restart

Local UI SHALL 將 group Parent/Member Task stream 與 local P2P events 分開解析，使用 scoped revision cursor 補發 SSE 中斷期間事件；重複 event、跨圈 event、撤銷 Agent event SHALL 不重複或不落盤。

#### Scenario: Group stream reconnects

- **WHEN** browser 或 local bridge 重連 group Task stream
- **THEN** server 從 durable revision 補發缺少的 parent/member messages，timeline 順序穩定且不產生重複氣泡
