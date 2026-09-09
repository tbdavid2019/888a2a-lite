# local-runtime-hub Specification

## Purpose
提供本機 Runtime 的唯讀診斷、受保護設定與使用者層級服務管理，讓使用者知道 CLI 是否存在並能安全切換本機 Bridge 執行引擎。

## Requirements

### Requirement: Local UI reports runtime availability without claiming provider health

Local UI SHALL 檢查 PATH 和已知 user installation directories 中的 OpenClaw、Claude Code、Goose、Hermes、Codex、OpenCode。`GET /api/runtimes` SHALL 回傳固定 runtime ID、display name、resolved executable path、version probe 結果、`ready`/`cli_needed`/`unavailable` status 和目前 Bridge backend。`ready` 只表示 executable 可安全探測，不代表 provider login、model access 或任務成功。

#### Scenario: Runtime inspection returns bounded metadata

- **WHEN** UI 查詢 `GET /api/runtimes`
- **THEN** server 回傳固定 schema 和 bounded metadata，不回傳環境變數值、Token、API key 或完整 secret configuration

#### Scenario: Missing runtime is distinguishable

- **WHEN** binary 不存在或 version probe 失敗
- **THEN** response 明確標示 `cli_needed` 或 `unavailable`，不把缺少 binary 顯示為 ready

### Requirement: Custom Runtime uses argv and safe environment references

`POST /api/runtimes/custom` SHALL 只接受 bounded name、固定 ID、absolute executable 或安全解析後路徑、argv array 與環境變數名稱引用。不得接受 shell command string、pipe、semicolon、redirect、command substitution 或任意 service path。設定檔 SHALL 使用 schema version、0600 權限與 atomic replace；執行 SHALL 使用 `shell=false`、timeout、output limit 和 process cleanup。

#### Scenario: Shell syntax is rejected

- **WHEN** user 提交含 shell operator 或 command substitution 的 custom runtime
- **THEN** server 回 400，不保存設定，也不執行任何 command

#### Scenario: Runtime configuration does not expose secrets

- **WHEN** custom runtime 使用 environment name reference
- **THEN** API 和 UI 只回傳名稱與 metadata，不回傳 environment value 或 credential

### Requirement: Runtime selection has an explicit persisted source of truth

UI SHALL 顯示目前 Bridge process backend 與 persisted desired backend。切換操作 SHALL 先原子保存 desired configuration，再由 managed user service restart 讓新 Bridge 載入；UI 不得宣稱尚未重啟的 process 已切換。新 backend 啟動失敗 SHALL 保留舊設定並 rollback。

#### Scenario: Runtime switch requires restart

- **WHEN** user 選擇另一個 ready runtime
- **THEN** UI 顯示 requested/pending 狀態，service restart 成功後才顯示 active

#### Scenario: Runtime start fails

- **WHEN** 新 runtime service 啟動失敗
- **THEN** server 回傳 failure，保留可用的舊設定並提供恢復狀態，不刪除舊 credential 或 queue

### Requirement: Service controls are local, scoped, and protected

Local UI SHALL 提供 macOS LaunchAgent 或 Linux systemd user service 的 status/install/restart endpoint。所有 mutation SHALL 要求 local session/CSRF token、合法 loopback Host/Origin、bounded body 與固定 managed service identity；不得接受 browser 提供的任意 unit、plist 或 shell arguments。操作 SHALL idempotent、可觀測並回傳 requested/observed/error status。

#### Scenario: Cross-origin service mutation is rejected

- **WHEN** 非 local UI origin 的 request 呼叫 service install/restart
- **THEN** server 拒絕 mutation，且不觸發 OS service command

#### Scenario: Repeated install is safe

- **WHEN** user 重複按 Install Service
- **THEN** server 不建立重複 unit，回傳目前 managed service state
