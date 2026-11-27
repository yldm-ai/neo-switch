# NeoSwitch

[English](../README.md) | **简体中文** | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Português (Brasil)](README.pt-BR.md) | [Русский](README.ru.md)

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![Release](https://img.shields.io/github/v/release/yldm-ai/neo-switch)](https://github.com/yldm-ai/neo-switch/releases/latest) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

NeoSwitch 让你把单个 Claude Code 会话切换到 Google 的 Gemini 4 Neon，同时保持你原有的 Claude Code 配置、连接器、权限和其他窗口不变。

[![三步设置 NeoSwitch](../docs/media/neoswitch-demo.gif)](../docs/media/neoswitch-demo.mp4)

按单个会话切换——同一个项目里的其他窗口继续使用 Claude。插件支持 macOS 和 Linux，暂不支持 Windows。

## 核心亮点

- **单会话热切，原地切回。** `/neoswitch:on` 和 `/neoswitch:off` 只作用于当前会话，从下一条消息立即生效；同项目的其他窗口和新建会话留在 Claude，最后一个切换会话关闭后自动清理项目配置。
- **完整保留 Claude Code 原生体验。** 不写入 `ANTHROPIC_AUTH_TOKEN`，`claude.ai` 连接器照常工作；Antigravity 下内置 `WebSearch` 走 Google 搜索；Auto Mode 开箱即用，自带默认保守拦截（fail-closed）的安全分类器（无需重启、零 Claude 用量）；`/effort` 和可选思考块（`NEOSWITCH_SHOW_THINKING`）平滑映射到 Gemini。
- **原生秒出命令与实时配额。** 在 Claude Code 2.1.326 及以上版本上，`/neoswitch:login`、`/neoswitch:on`、`/neoswitch:off` 和 `/neoswitch:status` 直接本地应答，不消耗模型回合；输入框下方常驻当前回答模型的提示行；`/neoswitch:status` 零消耗展示 5 小时与每周 Antigravity 配额进度条及重置时间（`-v` 可看全部配额组和剩余 credits）。
- **专为真实长会话打磨。** ACP 会话复用与分块重放绕开 `agy` 的 192 KB 截断；瘦身自定义 agent 将 `agy` 的提示开销从约 14k token 压到 1k 以下；支持并行工具调用与 JSON 转义自动修复；提示建议与会话标题自动分流到 `gemini-3.8-flash`，节省 Gemini 4 Neon 配额。
- **本地、隔离、可逆。** 流量仅走本机 TLS，CA 私钥签发后即丢弃；NeoSwitch 从不读取、保存或重放 Google OAuth 令牌；`agy` 运行在独立工作目录中，其自带文件与终端工具被瘦身 agent 与逐回合对账的 `PreToolUse` hook 双重封锁；关闭时自动还原你原有的 `NODE_EXTRA_CA_CERTS` 和 `ANTHROPIC_CUSTOM_HEADERS`。
- **支持 SSH 与无缝更新。** `/neoswitch:login` 同时提供浏览器登录和远程 SSH 粘贴码流程；插件更新时新 daemon 自动接管端口，无需关闭已开窗口；构建或下载失败时自动回退已缓存版本，确保 `/neoswitch:off` 始终可用。
- **多后端支持。** 默认通过官方 `agy` 使用 Google 账号，也支持 `GEMINI_API_KEY`、Vertex AI 以及实验性的 OpenAI Responses 后端。

## 快速上手

**1. 安装插件。** 在 Claude Code 里依次运行下面两行，一次一行。每行请整行复制：第一行写的是 `neo-switch`，第二行是 `neoswitch`。

```text
/plugin marketplace add yldm-ai/neo-switch
```

```text
/plugin install neoswitch@neoswitch
```

打开自动更新，这样 Google 或 Claude Code 改动了 NeoSwitch 依赖的部分时，修复会自动送到你这里：运行 `/plugin`，打开 **Marketplaces** 标签页，选中 `neoswitch`，打开 auto-update。Claude Code 对第三方插件市场默认关闭自动更新。

**2. 用你的 Google 账号登录。** 在要切换的项目里打开 Claude Code，运行：

```text
/neoswitch:login
```

它会先显示 Google 的条款警告，若本机尚未安装 Google 官方 [Antigravity 命令行工具（`agy`）](https://antigravity.google/download) 则自动装到 `~/.local/bin`（约 230 MB），然后在浏览器中打开 Google 登录，并为当前会话开启 NeoSwitch。命令也会输出一个链接供远程/SSH 机器使用：在任意设备打开登录，并在 Claude Code 2.1.326+ 的弹窗中粘贴授权码（旧版本、WSL 或通过 SSH 连接的桌面版则运行 `/neoswitch:login <code>`）。再次运行 `/neoswitch:login` 可切换 Google 账号，运行 `/neoswitch:login cancel` 保留当前账号。想改用 API key 或 Vertex AI？见 [docs/cli.md](../docs/cli.md#gemini-api-key)（英文）。

> [!WARNING]
> [Google Antigravity 服务条款](https://antigravity.google/terms)（第 6 条）禁止通过其他工具使用 Antigravity，NeoSwitch 就属于这类工具。Google 可能暂停或关闭这样做的人的 Antigravity 账号。`/neoswitch:login` 每次都会重复这条警告。不接受这个风险，就不要登录。

**3. 发下一条消息。** 当前会话从下一条消息起使用 Gemini 4 Neon，同项目其他窗口和新会话继续使用 Claude。如果回复提示因通过网关启动而需要重启，请不经网关在同一项目里重新运行 `claude`。

| 命令 | 作用 |
|---|---|
| `/neoswitch:login [code \| cancel]` | 通过官方 `agy` 登录、切换 Google 账号或完成 SSH 授权码登录，并开启当前会话 |
| `/neoswitch:on` | 让当前会话从下一条消息起切换到 Gemini 4 Neon |
| `/neoswitch:off` | 让当前会话从下一条消息起切回 Claude |
| `/neoswitch:status [-v]` | 查看当前模型、Google 登录状态以及 5 小时/每周配额进度条（`-v` 显示全部配额组、credits、后端与 daemon 详情） |

## 与其他方案的差异

| | **NeoSwitch** | **配置管理 / 路由工具**<br>（如 CC Switch、Claude Code Router） | **独立 Antigravity 代理**<br>（如 antigravity-claude-proxy） |
|---|---|---|---|
| **安装形态** | Claude Code 原生插件，无需桌面应用或常驻服务运维 | 独立桌面应用，统一管理多种 CLI 配置 | 独立 Node.js 代理服务 + Web 控制台 |
| **切换粒度** | **按单个会话热切**（`/neoswitch:on`、`/neoswitch:off`），其他窗口照常使用 Claude | 全局切换供应商或统一本地路由 | 修改全局 `~/.claude/settings.json` 或环境变量 |
| **原生功能保留** | **完整保留**——不写入 `ANTHROPIC_AUTH_TOKEN`，`claude.ai` 连接器、`WebSearch`、Auto Mode、实时配额与原生 mod 命令照常可用 | 由外部配置或路由器接管 | 写入 `ANTHROPIC_AUTH_TOKEN`，会导致 `claude.ai` 连接器被禁用 |
| **Google 登录与凭证** | 通过官方 `agy` 使用 Google 账号，**不读取、不保存、不重放 OAuth 令牌**，并屏蔽 `agy` 自带工具 | 以 API Key 与第三方中转为主（不内置 `agy` 接入） | 代理自行保存或读取本地 OAuth 令牌，直连私有接口 |
| **最适合谁** | 主力使用官方 Claude Code，希望随时在单个会话里切到 Gemini 4 Neon | 在一个界面集中管理多款 CLI、API Key、中转与 MCP | 需要多账号轮询、Windows 支持，或通过 Antigravity 使用 Claude 模型 |

## 使用须知

- **模型标签：** Claude Code 顶部标题栏仍显示 Claude 型号名（如 `claude-opus-5-5`），用 `/model` 切换也不会改变实际后端模型。在 Claude Code 2.1.326 及以上版本上，NeoSwitch 生效时输入框下方会显示 `Gemini 4 Neon answers this session`。
- **首字延迟、`/effort` 与思考块：** 回复可能需要 5 到 40 秒才开始出字。`/effort low` 可让回复更快开始（Antigravity 与 Gemini API / Vertex AI 均有效），设置 `NEOSWITCH_SHOW_THINKING=on` 可将上游推理过程展示为 Claude Code 思考块。
- **安装或更新后的首次运行：** 安装或更新后的第一条 NeoSwitch 命令可能需要等待约一分钟，用于编译或下载并校验发布版二进制文件。
- **Auto Mode：** 开箱即用，由 NeoSwitch 内置的 fail-closed 安全分类器负责判定（Antigravity 下使用 `gemini-3.8-flash`，零 Claude 用量、无需重启）。设置 `NEOSWITCH_SAFEGUARD=off` 可关闭它；如需改由 Claude 用你的 Claude 登录检查操作，可用 `CLAUDE_CODE_AUTO_MODE_SERVER=0 claude` 启动（消耗少量 Claude 用量，Claude 无应答时回退到 Gemini）。
- **每日一次提示：** 设置好 NeoSwitch 后，未开启它的新会话每个项目每天最多显示一行 `/neoswitch:on` 提示（运行过 `/neoswitch:off` 的项目不再显示；设置 `NEOSWITCH_NO_HINT=1` 可关闭）。
- **自动更新：** 有新版本时，会话提示和 `/neoswitch:status` 会告知。开启自动更新后，一旦有新的 Claude Code 会话启动，已打开的窗口也会自动平滑切换到新版 daemon。

## 出了问题怎么办

<details>
<summary>常见问题与排查（点击展开）</summary>

| 你看到的情况 | 怎么做 |
|---|---|
| 会话开头的提示说 NeoSwitch 还没设置好 | 运行 `/neoswitch:login`。下一条消息就会用 Gemini 4 Neon。 |
| 提示说没有安装 `agy` | `/neoswitch:login` 没能自动装上它。在终端里运行 `curl -fsSL https://antigravity.google/cli/install.sh \| bash`，或从 [Google 的下载页](https://antigravity.google/download)安装，再运行一次 `/neoswitch:login`。 |
| 每条消息都报连接错误，或者会话开头的提示说本会话无法回答 | 运行 `/neoswitch:off`：在 Claude Code 2.1.326+ 上它会直接在本地切回 Claude，无需模型调用。如果 daemon 本身未运行或消息仍然失败，输入 `/exit` 并重新运行 `claude`：新会话会使用 Claude，运行 `/neoswitch:status` 可查看需要修复什么。 |
| 提示说其他 Claude Code 窗口还在共用之前的设置 | 在当前会话里运行 `/neoswitch:on`，已打开的窗口会自动切换到当前版本。 |
| 提示说 `claude.ai` 连接器已停用，因为设置了另一个认证来源 | 现在的 NeoSwitch 不会再导致这个提示，是旧版本造成的。请更新插件（见下文）。如果仍然出现，说明有别的工具给 Claude Code 设置了 `ANTHROPIC_AUTH_TOKEN` 或 `ANTHROPIC_API_KEY`。 |
| 提示说有别的程序占用了 NeoSwitch 使用的端口 | NeoSwitch 已经自动迁移到空闲端口，并让该项目的下一次会话回到 Claude。直接开一个新会话（使用 Claude），需要时运行 `/neoswitch:on` 切回 Gemini。 |
| 提示说 NeoSwitch 下载不了它的程序 | 检查网络连接，然后重新启动 Claude Code。如果这台电脑之前运行过 NeoSwitch，它会继续使用之前缓存的版本，直到下载成功。 |
| `/neoswitch:off` 本身提示 NeoSwitch 无法运行 | 它会列出要从项目的 `.claude/settings.local.json` 里删除的条目；删掉后开一个新会话。[卸载 NeoSwitch](#关闭更新与卸载) 的第 1 步列的是同样的条目。 |
| 卸载 NeoSwitch 后，某个项目里的 Claude Code 连不上 | 这个项目仍然指向 NeoSwitch。按 [卸载 NeoSwitch](#关闭更新与卸载) 第 1 步的说明，从 `.claude/settings.local.json` 里删掉它的条目，然后开一个新会话。 |
| `/neoswitch:login` 提示是未知命令 | 更新插件（见下文），重启 Claude Code，再运行 `/reload-plugins`。 |
| 提示说你的 Google 账号的 Antigravity 套餐不提供 `gemini-4-neon` | NeoSwitch 已让当前会话留在 Claude，并列出了你套餐支持的模型。再次运行 `/neoswitch:login` 换一个包含 `gemini-4-neon` 的账号，或导出 `GEMINI_MODEL` 指定列表中的某个模型。 |

</details>

## 关闭、更新与卸载

- `/neoswitch:off` 让这个会话从下一条消息起回到 Claude，用你自己的 Claude 登录。`/neoswitch:on` 再把它切回 Gemini 4 Neon，不用重新设置。关闭会话也就结束了它的切换：每个新会话都从 Claude 开始。

手动更新：在终端里运行下面两条命令，然后开一个新的 Claude Code 会话（已经开着的窗口会自动换到新版）：

```bash
claude plugin marketplace update neoswitch
claude plugin update neoswitch@neoswitch
```

如果你当初是用旧名字 `yldm-ai/NeoSwitch` / `yldm-ai/neon-switch` 添加的 marketplace，先运行 `claude plugin marketplace add yldm-ai/neo-switch` 重新添加。

卸载 NeoSwitch：

1. 先在每个开启过的项目里运行 `/neoswitch:off`。没有关掉的项目仍然指向 NeoSwitch，卸载后就无法回答了。如果已经卸载了插件，请在每个这样的项目里打开 `.claude/settings.local.json`，修改它的 `env` 对象：以 `https://127.0.0.1:` 开头的 `ANTHROPIC_BASE_URL` 删掉（旧版本是 `http://127.0.0.1:`，连同旁边的 `ANTHROPIC_AUTH_TOKEN` 一起删）；删掉 `ANTHROPIC_CUSTOM_HEADERS` 里 `X-NeoSwitch-Token` 那一行，没有别的内容时整个条目都删掉；指向 NeoSwitch 文件夹里 `tls/bundle.pem` 的 `NODE_EXTRA_CA_CERTS` 删掉，如果你原来在这里设过自己的 CA 文件，就把它的路径填回去；值为 `1` 的 `CLAUDE_CODE_GATEWAY_HINT_HEADERS` 和值为 `0` 的 `CLAUDE_CODE_USE_GATEWAY` 也删掉，除非是你自己设的。
2. 关掉正在使用 NeoSwitch 的 Claude Code 窗口，然后在终端里运行：

```bash
claude plugin uninstall neoswitch@neoswitch
claude plugin marketplace remove neoswitch
```

3. 删除 NeoSwitch 保存在项目之外的文件：

| 内容 | 位置 |
|---|---|
| 插件下载或编译出的 NeoSwitch 程序 | `$XDG_CACHE_HOME/neoswitch/`；未设置 `XDG_CACHE_HOME` 时为 `~/.cache/neoswitch/` |
| 运行状态、TLS 证书包、隔离 `agy` 工作目录与 `daemon.log` | 设置了 `NEOSWITCH_STATE_DIR` 时就是它；否则 macOS 上是 `~/Library/Application Support/neoswitch/`，Linux 上是 `$XDG_CONFIG_HOME/neoswitch/` 或 `~/.config/neoswitch/` |

Google 登录信息由 `agy` 保存，不归 NeoSwitch 管，卸载后仍然保留；`agy` 本身也一样，包括 `/neoswitch:login` 帮你装的那份。

## NeoSwitch 会改动什么

当某个项目里有会话在用 Gemini 4 Neon 时，NeoSwitch 会往这个项目的 `.claude/settings.local.json` 的 `env` 对象中加入四项；这个项目里不再有会话使用它时，就把它们删掉：

- `ANTHROPIC_BASE_URL`、`ANTHROPIC_CUSTOM_HEADERS` 和 `NODE_EXTRA_CA_CERTS`：让这个项目的 Claude Code 请求经过一条只有 NeoSwitch 能应答的加密连接，发到这台电脑上的 NeoSwitch。你切换过的会话由 Gemini 4 Neon 回答；其他请求原样转给 Claude，用的是你自己的 Claude 登录，NeoSwitch 既不保存也不记录它。请求头里的密钥只能用来访问本机的 NeoSwitch。如果文件里原本有你自己的 `NODE_EXTRA_CA_CERTS` 或 `ANTHROPIC_CUSTOM_HEADERS`，NeoSwitch 会保留一份副本，删除自己的条目时把你的值放回去。
- `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1`：让 Claude Code 在请求头里标明请求来自你的主对话、子代理、上下文压缩还是后台任务（如提示建议），以便 NeoSwitch 为主对话保留独立的 Gemini 会话。如果你自己设过这一项，NeoSwitch 会保留你的设置。

在 Git 项目里，NeoSwitch 还会把 `.claude/settings.local.json` 加进本地仓库自己的排除列表（`.git/info/exclude`），让这个 token 不会被提交；你的 `.gitignore` 不会被改动。

NeoSwitch 只在这台电脑上运行，也只接受来自这台电脑的连接。它不记录提示词、回复、key 或登录信息，也不经过我们的任何服务器。Google 登录信息留在 `agy` 里，NeoSwitch 从不读取。

## 更多

哪些功能可用、哪些不可用，NeoSwitch 的工作原理，独立命令行工具，环境变量，路线图和开发说明：见 [docs/cli.md](../docs/cli.md)（英文）。另见 [Antigravity 登录详情](../docs/antigravity.md)和[协议约定](../docs/protocol-contract.md)。

参与贡献见 [CONTRIBUTING.md](../CONTRIBUTING.md)，私下报告安全漏洞见 [SECURITY.md](../SECURITY.md)，行为准则见 [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md)。

MIT License
