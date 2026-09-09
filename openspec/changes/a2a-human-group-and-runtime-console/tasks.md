## 1. Visual Agent Runtimes Management

- [ ] 1.1 在 `examples/worker/a2a_bridge.py` 實作 CLI 探測器（掃描 `openclaw`、`claude`、`hermes`、`codex`、`goose` 等），並在 `GET /api/runtimes` 回傳探測結果與狀態
- [ ] 1.2 在 Local Web UI (`http://localhost:8888`) 新增「Agent Runtimes」分頁，呈現 Ready 綠燈徽章與琥珀色 `CLI needed` 提示卡片
- [ ] 1.3 實作客製化 Runtime 新增介面（`POST /api/runtimes`）並持久化至 `~/.a2a/runtimes.json`
- [ ] 1.4 在 UI 提供「安裝為系統背景服務」按鈕，呼叫既有之 launchd/systemd 服務產生邏輯

## 2. Human Group Chat & Mention Routing

- [ ] 2.1 在 Local Web UI 導覽列新增「協作群組（Groups）」分頁，支援載入本機已加入群組與對話歷史
- [ ] 2.2 實作群聊對話氣泡流與 SSE 即時推播監聽，區分人類與不同 Bot 之頭像與訊息
- [ ] 2.3 實作輸入框 `@` mentions 智慧補全清單，並在送出時解析提及之 `agentId`
- [ ] 2.4 實作無提及時自動附帶 `replyPolicy: ACK_ONLY`，有提及時附帶 `replyPolicy: MENTIONED_ONLY` 與 `mentions` 清單
- [ ] 2.5 驗證多 Bot 群組下，未被指名之 Bot 僅秒級 ACK 已讀不回覆，僅被 @ 的 Bot 啟動推理回信

## 3. End-to-End Verification

- [ ] 3.1 執行本地端驗證：開啟 Web UI，切換 Runtimes 狀態，進行群聊輸入測試
- [ ] 3.2 驗證 Buzz 對標各項指標在 Web UI 運作順暢
