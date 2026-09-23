# 2026-09-23 Live Hub 驗收紀錄

## 範圍

- 驗收 commit：`f44ea50` (`fix(workflows): check SQLite cursor close errors`)
- Hub：`https://a2a.david888.com`
- 執行環境：`david@10.9.0.11`
- 本機驗收：未執行測試或建置；依專案規範由 GitHub Actions 與遠端 smoke script 驗證。

## GitHub Actions

| 流程 | 結果 | 執行紀錄 |
|---|---|---|
| CI | 通過：Go tests、Go static checks、Python bridge/pattern checks、A2A source/SDK gate、container build | [Run 35826299140](https://github.com/tbdavid2019/888a2a-lite/actions/runs/35826299140) |
| Docker image publish | 通過：multi-arch image 推送完成 | [Run 35826299098](https://github.com/tbdavid2019/888a2a-lite/actions/runs/35826299098) |

發佈過程中，CI 曾回報 Workflow API 測試的 `int`／`uint64` 型別不一致，以及一處未檢查的 SQLite `rows.Close()`；兩處修正後，最終 CI 與 image publish 均通過。

官方 SDK source/REST/tenant checks 通過。需要外部 fixture credentials 的 SDK interoperability 與 Group Extension fixture 在本次 CI 中略過。

## 正式 Hub 部署與 Smoke Test

- Watchtower 偵測到新 image `tbdavid2019/888a2a-lite:latest`，更新 `888a2a-lite-hub-1` 容器。
- 更新後容器健康狀態為 `healthy`。Smoke test 完成後容器再次啟動，健康狀態維持正常。
- 在 `david@10.9.0.11` 執行 `scripts/smoke-test.sh`，結果為 `888a2a-lite smoke test passed`。
- Smoke 覆蓋 Agent 註冊與探索、Hub 與 Per-Agent Card、direct task 投遞及冪等、inbox ACK、群組邀請／廣播／冪等、Hub restart 後未 ACK direct/group delivery 恢復、成員移除、群組封存、Agent revoke，以及 registration policy 關閉後的拒絕行為。

Smoke 起始時，從遠端 checkout `.env` 取得的 operator credential 呼叫 registration control 得到 HTTP 401。檢查確認 checkout `.env` 與執行中容器的 operator credential 不一致。後續由執行中容器環境提供 credential，值未輸出，Smoke test 通過。

## 三 Agent 私有圈互動測試

線上 `/hub/v1/status` 回傳 `MULTI_CIRCLE`、dynamic circles enabled。依線上 `/llms.txt` 指示，三個 subagent 使用新建的私有測試圈；沒有把測試訊息送進 public circle。

| Agent | 驗收項目 | 結果 |
|---|---|---|
| A (`agent-a0585d9425349dd21c61e027`) | 建立 Workflow，送 task 給 B，收 B 回覆，查詢 Workflow | 通過；Workflow 狀態為 `COMPLETED`，1 個 step 完成 |
| B (`agent-d55025d6164ef51284335583`) | 收 A task、ACK、回報 `WORKING`／`COMPLETED`，再回覆 A 與 C | 通過 |
| C (`agent-b72ea26fd337a55948003a6e`) | 同圈 peer discovery，送 task 給 B，收 B 回覆並 ACK | 通過 |

本機私有圈 key 與三份 Agent credential 已刪除。測試 Agent 使用的 offline 狀態由執行結果無法確認。針對 `10.9.0.11` 管理 API 的刪除請求回 HTTP 404；以該容器 operator credential 呼叫 public hostname 的刪除請求回 HTTP 401。因此 Hub 上的私有測試 Agent／Workflow 紀錄清理尚未確認，註冊租約到期前可能仍保留在其所連線的 Hub。測試 Agent ID 已列於上表，方便後續管理查核。

Smoke test 新建立的三個 public-circle Agent 已透過 `DELETE /hub/v1/admin/agents/{agentId}` 清除，三次回應皆為 HTTP 200。更早期的歷史 smoke Agent 紀錄未處理。

## Code Review

獨立 subagent 完成唯讀 review。它最初提出的 A2A Task ID 驗證問題經複核後撤回；程式會以路由 Task ID 查詢，並比對 Message 的 `taskId` 與 `contextId`。Workflow 到期清理的逐 attempt 更新已改為批次 SQL；其餘權限、冪等、retry/dead-letter、cancel、quorum 與文件契約檢視未發現其他可確認缺陷。

## 後續查核

- 確認 public hostname 與 `10.9.0.11` 的 operator credential／後端路由配置關係。
- 確認私有測試圈的三筆 Agent 與已完成 Workflow 紀錄；本機 key 與 credential 已刪除，無法再以測試 Agent Token 查詢。
