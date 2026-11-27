# NeoSwitch

[English](../README.md) | [简体中文](README.zh-CN.md) | **繁體中文** | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Português (Brasil)](README.pt-BR.md) | [Русский](README.ru.md)

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![Release](https://img.shields.io/github/v/release/yldm-ai/neo-switch)](https://github.com/yldm-ai/neo-switch/releases/latest) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

NeoSwitch 讓你把單一 Claude Code 工作階段切換到 Google 的 Gemini 4 Neon，同時保持你原有的 Claude Code 設定、連接器、權限和其他視窗不變。

[![三步驟設定 NeoSwitch](../docs/media/neoswitch-demo.gif)](../docs/media/neoswitch-demo.mp4)

一次切換一個工作階段——同一個專案裡的其他視窗繼續使用 Claude。外掛支援 macOS 和 Linux，暫不支援 Windows。

## 核心亮點

- **單一工作階段熱切，原地切回。** `/neoswitch:on` 和 `/neoswitch:off` 只作用於目前的工作階段，從下一則訊息立即生效；同專案的其他視窗和新的工作階段留在 Claude，最後一個切換的工作階段關閉後自動清理專案設定。
- **完整保留 Claude Code 原生體驗。** 不寫入 `ANTHROPIC_AUTH_TOKEN`，`claude.ai` 連接器照常運作；Antigravity 下內建 `WebSearch` 走 Google 搜尋；Auto Mode 開箱即用，內建預設保守攔截（fail-closed）的安全分類器（無須重新啟動、零 Claude 用量）；`/effort` 與可選思考區塊（`NEOSWITCH_SHOW_THINKING`）平滑對應到 Gemini。
- **原生秒出指令與即時配額。** 在 Claude Code 2.1.326 以上版本上，`/neoswitch:login`、`/neoswitch:on`、`/neoswitch:off` 和 `/neoswitch:status` 直接由本機回應，不消耗模型回合；輸入框下方常駐目前回答模型的狀態列；`/neoswitch:status` 零消耗顯示 5 小時與每週 Antigravity 配額進度條與重置時間（`-v` 可查看所有配額群組與剩餘 credits）。
- **專為真實長工作階段打造。** ACP 工作階段重用與分塊重放避開 `agy` 的 192 KB 截斷；精簡自訂 agent 將 `agy` 的提示開銷從約 14k token 壓到 1k 以下；支援平行工具呼叫與 JSON 逸出自動修復；提示建議與工作階段標題自動分流到 `gemini-3.8-flash`，節省 Gemini 4 Neon 配額。
- **本機、隔離、可逆。** 流量僅走本機 TLS，CA 私鑰簽發後即丟棄；NeoSwitch 從不讀取、儲存或重放 Google OAuth 權杖；`agy` 執行於獨立工作目錄中，其內建檔案與終端機工具由精簡 agent 與逐回合對帳的 `PreToolUse` hook 雙重封鎖；關閉時自動還原你原有的 `NODE_EXTRA_CA_CERTS` 與 `ANTHROPIC_CUSTOM_HEADERS`。
- **支援 SSH 與無縫更新。** `/neoswitch:login` 同時提供瀏覽器登入與遠端 SSH 貼上授權碼流程；外掛更新時新版 daemon 自動接管連接埠，無須關閉已開啟的視窗；編譯或下載失敗時自動退回已快取版本，確保 `/neoswitch:off` 始終可用。
- **多後端支援。** 預設透過官方 `agy` 使用 Google 帳號，也支援 `GEMINI_API_KEY`、Vertex AI 以及實驗性的 OpenAI Responses 後端。

## 快速上手

**1. 安裝外掛。** 在 Claude Code 裡依序執行下面兩行，一次一行。每行請整行複製：第一行寫的是 `neo-switch`，第二行是 `neoswitch`。

```text
/plugin marketplace add yldm-ai/neo-switch
```

```text
/plugin install neoswitch@neoswitch
```

開啟自動更新，這樣 Google 或 Claude Code 改動了 NeoSwitch 依賴的部分時，修正會自動送到你這裡：執行 `/plugin`，開啟 **Marketplaces** 分頁，選取 `neoswitch`，開啟 auto-update。Claude Code 對第三方外掛市集預設關閉自動更新。

**2. 用你的 Google 帳號登入。** 在要切換的專案裡開啟 Claude Code，執行：

```text
/neoswitch:login
```

它會先顯示 Google 的條款警告，若本機尚未安裝 Google 官方 [Antigravity 命令列工具（`agy`）](https://antigravity.google/download) 則自動裝到 `~/.local/bin`（約 230 MB），接著在瀏覽器開啟 Google 登入，並為目前工作階段開啟 NeoSwitch。指令也會印出連結供遠端/SSH 機器使用：在任意裝置開啟登入，並在 Claude Code 2.1.326+ 的對話框中貼上授權碼（較舊版本、WSL 或透過 SSH 連線的桌面版則執行 `/neoswitch:login <code>`）。再次執行 `/neoswitch:login` 可切換 Google 帳號，執行 `/neoswitch:login cancel` 保留目前帳號。想改用 API key 或 Vertex AI？見 [docs/cli.md](../docs/cli.md#gemini-api-key)（英文）。

> [!WARNING]
> [Google Antigravity 服務條款](https://antigravity.google/terms)（第 6 條）禁止透過其他工具使用 Antigravity，NeoSwitch 就屬於這類工具。Google 可能暫停或關閉這樣做的人的 Antigravity 帳號。`/neoswitch:login` 每次都會重複這則警告。不接受這個風險，就不要登入。

**3. 傳送下一則訊息。** 目前工作階段從下一則訊息起使用 Gemini 4 Neon，同專案其他視窗與新工作階段繼續使用 Claude。如果回覆提示因透過閘道啟動而需要重新啟動，請不經閘道在同一個專案裡重新執行 `claude`。

| 指令 | 用途 |
|---|---|
| `/neoswitch:login [code \| cancel]` | 透過官方 `agy` 登入、切換 Google 帳號或完成 SSH 授權碼登入，並開啟目前工作階段 |
| `/neoswitch:on` | 讓目前工作階段從下一則訊息起切換到 Gemini 4 Neon |
| `/neoswitch:off` | 讓目前工作階段從下一則訊息起切回 Claude |
| `/neoswitch:status [-v]` | 顯示目前模型、Google 登入狀態與 5 小時/每週配額進度條（`-v` 顯示全部配額群組、credits、後端與 daemon 詳情） |

## 與其他方案的差異

| | **NeoSwitch** | **設定管理 / 路由工具**<br>（如 CC Switch、Claude Code Router） | **獨立 Antigravity 代理**<br>（如 antigravity-claude-proxy） |
|---|---|---|---|
| **安裝形態** | Claude Code 原生外掛，無須桌面應用程式或常駐服務維運 | 獨立桌面應用程式，統一管理多種 CLI 設定 | 獨立 Node.js 代理服務 + Web 控制台 |
| **切換粒度** | **按單一工作階段切換**（`/neoswitch:on`、`/neoswitch:off`），其他視窗照常使用 Claude | 全域切換供應商或共用本地路由 | 修改全域 `~/.claude/settings.json` 或環境變數 |
| **原生功能保留** | **完整保留**——不寫入 `ANTHROPIC_AUTH_TOKEN`，`claude.ai` 連接器、`WebSearch`、Auto Mode、即時配額與原生 mod 指令照常運作 | 由外部設定或路由器接管 | 寫入 `ANTHROPIC_AUTH_TOKEN`，會導致 `claude.ai` 連接器停用 |
| **Google 登入與憑證** | 透過官方 `agy` 使用 Google 帳號，**不讀取、不儲存、不重放 OAuth 權杖**，並封鎖 `agy` 內建工具 | 以 API Key 與第三方中轉為主（未內建 `agy` 整合） | 代理自行儲存或讀取本地 OAuth 權杖，直連私有端點 |
| **最適合誰** | 主力使用官方 Claude Code，希望隨時在單一工作階段切到 Gemini 4 Neon | 在單一介面集中管理多款 CLI、API Key、中轉與 MCP | 需要多帳號輪詢、Windows 支援，或透過 Antigravity 使用 Claude 模型 |

## 使用須知

- **模型標籤：** Claude Code 頂部標題列仍顯示 Claude 型號名稱（例如 `claude-opus-5-5`），用 `/model` 切換也不會改變實際後端模型。在 Claude Code 2.1.326 以上版本上，NeoSwitch 生效時輸入框下方會顯示 `Gemini 4 Neon answers this session`。
- **首字延遲、`/effort` 與思考區塊：** 回覆可能需要 5 到 40 秒才開始出字。`/effort low` 可讓回覆更快開始（Antigravity 與 Gemini API / Vertex AI 皆有效），設定 `NEOSWITCH_SHOW_THINKING=on` 可將上游推理過程顯示為 Claude Code 思考區塊。
- **安裝或更新後的首次執行：** 安裝或更新後的第一條 NeoSwitch 指令可能需要等待約一分鐘，用來編譯或下載並驗證發布版執行檔。
- **Auto Mode：** 開箱即用，由 NeoSwitch 內建的 fail-closed 安全分類器負責判定（Antigravity 下使用 `gemini-3.8-flash`，零 Claude 用量、無須重新啟動）。設定 `NEOSWITCH_SAFEGUARD=off` 可關閉它；如需改由 Claude 用你的 Claude 登入檢查操作，可用 `CLAUDE_CODE_AUTO_MODE_SERVER=0 claude` 啟動（消耗少量 Claude 用量，Claude 無回應時退回 Gemini）。
- **每日一次提示：** 設定好 NeoSwitch 後，未開啟它的新工作階段每個專案每天最多顯示一行 `/neoswitch:on` 提示（執行過 `/neoswitch:off` 的專案不再顯示；設定 `NEOSWITCH_NO_HINT=1` 可關閉）。
- **自動更新：** 有新版本時，工作階段提示和 `/neoswitch:status` 會告知。開啟自動更新後，一旦有新的 Claude Code 工作階段啟動，已開啟的視窗也會自動平滑切換到新版 daemon。

## 出了問題怎麼辦

<details>
<summary>常見問題與排查（點擊展開）</summary>

| 你看到的情況 | 怎麼做 |
|---|---|
| 工作階段開頭的提示說 NeoSwitch 還沒設定好 | 執行 `/neoswitch:login`。下一則訊息就會使用 Gemini 4 Neon。 |
| 提示說沒有安裝 `agy` | `/neoswitch:login` 沒能自動裝好它。在終端機執行 `curl -fsSL https://antigravity.google/cli/install.sh \| bash`，或從 [Google 的下載頁](https://antigravity.google/download)安裝，再執行一次 `/neoswitch:login`。 |
| 每則訊息都出現連線錯誤，或者工作階段開頭的提示說本工作階段無法回答 | 執行 `/neoswitch:off`：在 Claude Code 2.1.326+ 上它會直接在本機切回 Claude，無須模型呼叫。如果 daemon 本身未執行或訊息仍然失敗，輸入 `/exit` 並重新執行 `claude`：新的工作階段會使用 Claude，執行 `/neoswitch:status` 可查看需要修復什麼。 |
| 提示說其他 Claude Code 視窗還在共用先前的設定 | 在目前工作階段執行 `/neoswitch:on`，已開啟的視窗會自動切換到目前版本。 |
| 提示說 `claude.ai` 連接器已停用，因為設定了另一個驗證來源 | 現在的 NeoSwitch 不會再造成這個提示，是舊版本造成的。請更新外掛（見下文）。如果仍然出現，代表有別的工具替 Claude Code 設定了 `ANTHROPIC_AUTH_TOKEN` 或 `ANTHROPIC_API_KEY`。 |
| 提示說有別的程式佔用了 NeoSwitch 使用的連接埠 | NeoSwitch 已經自動移至空閒連接埠，並讓該專案的下一次工作階段回到 Claude。直接開一個新工作階段（使用 Claude），需要時執行 `/neoswitch:on` 切回 Gemini。 |
| 提示說 NeoSwitch 下載不了它的程式 | 檢查網路連線，然後重新啟動 Claude Code。如果這台電腦之前執行過 NeoSwitch，它會繼續使用之前快取的版本，直到下載成功。 |
| `/neoswitch:off` 本身提示 NeoSwitch 無法執行 | 它會列出要從專案的 `.claude/settings.local.json` 裡刪除的項目；刪掉後開一個新工作階段。[移除 NeoSwitch](#關閉更新與移除) 的第 1 步列的是同樣的項目。 |
| 移除 NeoSwitch 後，某個專案裡的 Claude Code 連不上 | 這個專案仍然指向 NeoSwitch。依照 [移除 NeoSwitch](#關閉更新與移除) 第 1 步的說明，從 `.claude/settings.local.json` 裡刪掉它的項目，然後開一個新工作階段。 |
| `/neoswitch:login` 顯示為未知指令 | 更新外掛（見下文），重新啟動 Claude Code，再執行 `/reload-plugins`。 |
| 提示說你的 Google 帳號的 Antigravity 方案不提供 `gemini-4-neon` | NeoSwitch 已讓目前工作階段留在 Claude，並列出你方案支援的模型。再次執行 `/neoswitch:login` 換一個包含 `gemini-4-neon` 的帳號，或匯出 `GEMINI_MODEL` 指定列表中的某個模型。 |

</details>

## 關閉、更新與移除

- `/neoswitch:off` 讓這個工作階段從下一則訊息起回到 Claude，用你自己的 Claude 登入。`/neoswitch:on` 再把它切回 Gemini 4 Neon，不用重新設定。關閉工作階段也就結束了它的切換：每個新的工作階段都從 Claude 開始。

手動更新外掛：在終端機裡執行下面兩條指令，然後開一個新的 Claude Code 工作階段（已經開著的視窗會自動換到新版）：

```bash
claude plugin marketplace update neoswitch
claude plugin update neoswitch@neoswitch
```

如果你當初是用舊名稱 `yldm-ai/NeoSwitch` / `yldm-ai/neon-switch` 加入 marketplace，請先執行 `claude plugin marketplace add yldm-ai/neo-switch` 重新加入。

移除 NeoSwitch：

1. 先在每個開啟過的專案裡執行 `/neoswitch:off`。沒有關掉的專案仍然指向 NeoSwitch，移除後就無法回答了。如果已經移除了外掛，請在每個這樣的專案裡打開 `.claude/settings.local.json`，修改它的 `env` 物件：以 `https://127.0.0.1:` 開頭的 `ANTHROPIC_BASE_URL` 刪掉（舊版本是 `http://127.0.0.1:`，連同旁邊的 `ANTHROPIC_AUTH_TOKEN` 一起刪）；刪掉 `ANTHROPIC_CUSTOM_HEADERS` 裡 `X-NeoSwitch-Token` 那一行，沒有別的內容時整個項目都刪掉；指向 NeoSwitch 資料夾裡 `tls/bundle.pem` 的 `NODE_EXTRA_CA_CERTS` 刪掉，如果你原本在這裡設過自己的 CA 檔案，就把它的路徑填回去；值為 `1` 的 `CLAUDE_CODE_GATEWAY_HINT_HEADERS` 和值為 `0` 的 `CLAUDE_CODE_USE_GATEWAY` 也刪掉，除非是你自己設的。
2. 關掉正在使用 NeoSwitch 的 Claude Code 視窗，然後在終端機裡執行：

```bash
claude plugin uninstall neoswitch@neoswitch
claude plugin marketplace remove neoswitch
```

3. 刪除 NeoSwitch 保存在專案之外的檔案：

| 內容 | 位置 |
|---|---|
| 外掛下載或編譯出的 NeoSwitch 程式 | `$XDG_CACHE_HOME/neoswitch/`；未設定 `XDG_CACHE_HOME` 時為 `~/.cache/neoswitch/` |
| 執行狀態、TLS 憑證包、隔離 `agy` 工作目錄與 `daemon.log` | 設定了 `NEOSWITCH_STATE_DIR` 時就是它；否則 macOS 上是 `~/Library/Application Support/neoswitch/`，Linux 上是 `$XDG_CONFIG_HOME/neoswitch/` 或 `~/.config/neoswitch/` |

Google 登入資訊由 `agy` 保存，不歸 NeoSwitch 管，移除後仍然保留；`agy` 本身也一樣，包括 `/neoswitch:login` 幫你裝的那份。

## NeoSwitch 會改動什麼

當某個專案裡有工作階段在用 Gemini 4 Neon 時，NeoSwitch 會在這個專案的 `.claude/settings.local.json` 的 `env` 物件中加入四項；這個專案裡不再有工作階段使用它時，就把它們刪掉：

- `ANTHROPIC_BASE_URL`、`ANTHROPIC_CUSTOM_HEADERS` 和 `NODE_EXTRA_CA_CERTS`：讓這個專案的 Claude Code 請求經過一條只有 NeoSwitch 能回應的加密連線，送到這台電腦上的 NeoSwitch。你切換過的工作階段由 Gemini 4 Neon 回答；其他請求原樣轉給 Claude，用的是你自己的 Claude 登入，NeoSwitch 既不保存也不記錄它。請求標頭裡的金鑰只能用來存取本機的 NeoSwitch。如果檔案裡原本有你自己的 `NODE_EXTRA_CA_CERTS` 或 `ANTHROPIC_CUSTOM_HEADERS`，NeoSwitch 會保留一份副本，刪除自己的項目時把你的值放回去。
- `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1`：讓 Claude Code 在請求標頭裡標明請求來自主對話、子代理、內容壓縮或背景任務（例如提示建議），讓 NeoSwitch 能為主對話保留專屬的 Gemini 工作階段。如果你自己設過這一項，NeoSwitch 會保留你的設定。

在 Git 專案裡，NeoSwitch 還會把 `.claude/settings.local.json` 加進本機儲存庫自己的排除清單（`.git/info/exclude`），讓這個 token 不會被提交；你的 `.gitignore` 不會被改動。

NeoSwitch 只在這台電腦上執行，也只接受來自這台電腦的連線。它不記錄提示詞、回覆、key 或登入資訊，也不經過我們的任何伺服器。Google 登入資訊留在 `agy` 裡，NeoSwitch 從不讀取。

## 更多

哪些功能可用、哪些不可用，NeoSwitch 的運作方式，獨立命令列工具，環境變數，路線圖和開發說明：見 [docs/cli.md](../docs/cli.md)（英文）。另見 [Antigravity 登入詳情](../docs/antigravity.md)和[協定約定](../docs/protocol-contract.md)。

參與貢獻見 [CONTRIBUTING.md](../CONTRIBUTING.md)，私下回報安全漏洞見 [SECURITY.md](../SECURITY.md)，行為準則見 [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md)。

MIT License
