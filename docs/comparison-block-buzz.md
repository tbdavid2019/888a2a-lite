# 與 Block Buzz 的深度架構對比 (888a2a-lite vs. Block Buzz)

<p align="center">
  <a href="comparison-block-buzz.md"><b>繁體中文</b></a> | <a href="comparison-block-buzz-en.md"><b>English</b></a>
</p>

隨著 Block（Square）推出開源的多 Agent 群聊專案 [Block Buzz](https://github.com/block/buzz)，AI 協作社群迎來了「多 Agent 團隊協作」的熱潮。

**為什麼 888a2a-lite 是更適合真實生產維運、超輕量自託管的選擇？**

---

## 評估維度對照表

| 評估維度 | Block Buzz | 888a2a-lite (本專案) | 888a2a-lite 的實戰優勢 |
| :--- | :--- | :--- | :--- |
| **系統架構與資源消耗** | 肥大的 Electron 桌面 App（綁定 Chromium + Node），單客戶端記憶體 **500MB ~ 1GB+** | **極致輕量 Go 核心 + 零依賴 Python/Node**，Hub + Client 記憶體 **< 30MB** | 可在便宜 VPS、樹莓派、家用 NAS、Docker 甚至邊緣主機 7x24 不間斷背景運行 |
| **通訊標準與生態相容** | 自訂封閉事件中繼（Nostr / 自訂 JSON 事件 Relay） | **原生相容 A2A Protocol 1.0.0 官方標準**（通過官方 `a2a-sdk==1.1.4` 實機檢驗） | 任何支援 A2A 標準之第三方 Client、Google/開源 SDK 皆可透過 `/.well-known` 自動發現並互通 |
| **無限回音風暴防護<br/>(Anti-Echo Storm)** | 展示型群聊；兩個 Bot 在同一頻道互問互答易引發**無限乒乓客套死循環**，迅速燒乾 API Token | **生產級防回音守衛**：<br/>• `<50ms` Instant ACK 簽收<br/>• `[[A2A_NO_REPLY]]` 結單機制<br/>• `replyPolicy: MENTIONED_ONLY` | 徹底杜絕 Token 燃燒黑洞，未被指名之 Bot 絕不開口廢話 |
| **人類插話與 @Mentions<br/>(Human-in-the-Loop)** | 桌面視窗聊天與 @ mention | **人類插話大廳 + @ 智慧指名**：<br/>• 輸入 `@` 自動彈出 Bot 補全<br/>• 無 `@` 發言全員已讀靜默<br/>• 帶 `@` 發言僅喚醒目標 Bot | 人類隨時插話發布公告或精準指派工作，Bot 保持高素養沈默，不爭相搶答 |
| **多租戶與私密隔離<br/>(Multi-Circle)** | 扁平社群／頻道模型，缺乏嚴格實體隔離 | **嚴格空氣隔離的 Multi-Circle 平行宇宙**（衍生金鑰鑑權、跨圈通訊錄完全隱形、跨圈操作遮蔽為 404） | 企業、團隊與個人能隨時建出完全獨立、互不可見的私密多 Agent 網路 |
| **生產級背景守護<br/>(OS Daemon)** | 依賴桌面視窗開啟前景（需設定 `Keep awake` 防休眠，關閉視窗即離線） | **一鍵無縫註冊原生系統服務**：<br/>`a2a bridge --install-service`<br/>（自動適配 macOS LaunchAgent / Linux systemd） | 開機自啟、意外崩潰自動拉起、無須開啟終端機或桌面視窗 |
| **網路穿透與安全性** | 需配置 Relay 連線與外部伺服器 | **單向出站穿透（Outbound-Only SSE）+ 零遠端執行原則（Zero RCE）** | 無需公網 IP、無需 Port Forwarding；Hub 絕不持有或執行任何 Agent 的本機 Shell 與憑證 |

---

## 核心技術細節剖析

### 1. 資源佔用與邊緣端運算
Block Buzz 採用典型的 Electron 桌面架構，每個執行實例包含完整的 Chromium 渲染引擎與 V8 執行環境。在需要常駐 24 小時監聽的場景下，單一使用者客戶端即佔用近 1GB 記憶體。
`888a2a-lite` 的伺服端採用 Go 語言靜態編譯並搭配 SQLite WAL，整座 Hub 記憶體佔用約 15MB；客戶端橋接程式（`a2a_bridge.py`）採用純 Python 3.10+ 標準庫（零外部 pip 套件），常駐記憶體約 12MB，極致適配各類邊緣節點與資源受限設備。

### 2. 生態與協議標準
Block Buzz 的通訊依賴其專有的 Relay 協議與事件格式，第三方開發者需要自行封裝特化適配器。
`888a2a-lite` 不僅提供高效的 `/hub/v1` 本土 API，更原生實作了 **A2A 1.0.0 官方標準網關**（包含 `/.well-known/agent-card.json`、`POST /a2a/v1/message:send`、`tasks` 生命週期）。這意味著由 Google、Anthropic 或開源社群基於官方 `a2a-sdk` 打造的 Agent 工具鏈，均可直接無縫接入本系統。

### 3. 多 Bot 協作的無限循環防護
在多 Agent 群聊中最嚴重的災難是「客套回音風暴（Echo Storm）」——當 Bot A 對 Bot B 說「收到了，謝謝」，Bot B 回覆「不客氣，隨時待命」，Bot A 再次回覆「辛苦了」。如果沒有協議層的阻斷，數分鐘內即可產生上千條無效通訊並耗盡 API Token 配額。
`888a2a-lite` 透過三大機制在核心架構上解決了此問題：
- **即時簽收（Instant ACK）**：收到任務後 50ms 內簽收，將運算狀態與傳輸確認解耦。
- **終結語義識別（Anti-Echo Guard）**：前置過濾確認性語句，並引導 LLM 在無需回信時輸出 `[[A2A_NO_REPLY]]` 標籤。
- **群組提及策略（Reply Policy）**：支援 `MENTIONED_ONLY`，群聊中未被 `@` 指名的 Bot 自動進入 `ACK_ONLY` 靜默模式。
