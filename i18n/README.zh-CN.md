# NeoSwitch

[English](../README.md) | **简体中文** | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Português (Brasil)](README.pt-BR.md) | [Русский](README.ru.md)

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

NeoSwitch 让 Claude Code 改用 Google 的 Gemini 4 Neon 模型来回答，Claude Code 的其他一切照旧。

[![三步设置 NeoSwitch](../docs/media/neoswitch-demo.gif)](../docs/media/neoswitch-demo.mp4)

开关以项目为单位，你的其他项目继续使用 Claude。插件支持 macOS 和 Linux，暂不支持 Windows。

## 与其他方案的差异

| | **NeoSwitch** | **配置管理 / 路由工具**<br>（如 CC Switch、Claude Code Router） | **独立 Antigravity 代理**<br>（如 antigravity-claude-proxy） |
|---|---|---|---|
| **安装形态** | Claude Code 原生插件，无需桌面应用或常驻服务运维 | 独立桌面应用，统一管理多种 CLI 配置 | 独立 Node.js 代理服务 + Web 控制台 |
| **切换粒度** | **按单个会话热切**（`/neoswitch:on`、`/neoswitch:off`），其他窗口照常使用 Claude | 全局切换供应商或统一本地路由 | 修改全局 `~/.claude/settings.json` 或环境变量 |
| **原生功能保留** | **完整保留**——不设置 `ANTHROPIC_AUTH_TOKEN`，claude.ai 连接器与 auto mode 安全检查照常可用 | 由外部配置或路由器接管 | 写入 `ANTHROPIC_AUTH_TOKEN`，会导致 claude.ai 连接器被禁用 |
| **Google 登录与凭证** | 通过官方 `agy` 使用 Google 账号，**不读取、不保存、不重放 OAuth 令牌**，并屏蔽 `agy` 自带工具 | 以 API Key 与第三方中转为主（不内置 `agy` 接入） | 代理自行保存或读取本地 OAuth 令牌，直连私有接口 |
| **最适合谁** | 主力使用官方 Claude Code，希望随时在单个会话里切到 Gemini 4 Neon | 在一个界面集中管理多款 CLI、API Key、中转与 MCP | 需要多账号轮询、Windows 支持，或通过 Antigravity 使用 Claude 模型 |

## 三步上手

**1. 安装插件。** 在 Claude Code 里依次运行下面两行，一次一行。每行请整行复制：第一行写的是 `neo-switch`，第二行是 `neoswitch`。

```text
/plugin marketplace add yldm-ai/neo-switch
```

```text
/plugin install neoswitch@neoswitch
```

然后打开自动更新，这样 Google 或 Claude Code 改动了 NeoSwitch 依赖的东西时，修复会自动送到你这里：运行 `/plugin`，打开 **Marketplaces** 标签页，选中 `neoswitch`，打开 auto-update。不是 Anthropic 官方的插件市场，Claude Code 默认不自动更新。

**2. 用你的 Google 账号登录。** 在要切换的项目里打开 Claude Code，运行：

```text
/neoswitch:login
```

它会先显示 Google 的条款警告，再通过 Google 的 [Antigravity 命令行工具（`agy`）](https://antigravity.google/download)在浏览器里打开 Google 登录，最后为这个项目开启 NeoSwitch。如果这台电脑上还没有 `agy`，它会先用 Google 官方安装脚本把 `agy` 装到 `~/.local/bin`（约 230 MB；安装脚本还会把这个目录加进 shell 的 PATH）。如果你习惯用终端，也可以改用 Gemini API key：见 [docs/cli.md](../docs/cli.md#gemini-api-key)（英文）。

命令同时会显示一个链接，用于打开的浏览器你用不了的情况（例如通过 SSH 连接的远程机器）：在任意设备的浏览器中打开链接并登录。Claude Code 2.1.326 及以上版本会立即显示链接，并弹出对话框让你粘贴页面显示的授权码。在更早的 Claude Code 中，以及在 WSL 或通过 SSH 连接的桌面版中，链接改由 Claude 的回复给出：运行 `/neoswitch:login` 加上授权码。已登录时再次运行 `/neoswitch:login` 可以切换到另一个 Google 账号：完成之前当前账号保持不变，运行 `/neoswitch:login cancel` 则保留当前账号。

> [!WARNING]
> [Google Antigravity 服务条款](https://antigravity.google/terms)（第 6 条）禁止通过其他工具使用 Antigravity，NeoSwitch 就属于这类工具。Google 可能暂停或关闭这样做的人的 Antigravity 账号。`/neoswitch:login` 每次都会重复这条警告。不接受这个风险，就不要登录。

**3. 发下一条消息。** 运行命令的那个窗口从下一条消息起就用 Gemini 4 Neon。其他窗口和新会话继续用 Claude，用的是你自己的 Claude 登录，connectors 也照常可用：想让哪个会话用 Gemini 4 Neon，就在那个会话里运行 `/neoswitch:on`。如果命令的回复要你重启（Claude Code 是通过网关启动的），就不经网关、在同一个项目里重新运行 `claude`。

## 开启后你会看到

- 设置好 NeoSwitch 之后，没有使用它的新会话开头会出现一条提示（每个项目每天最多一次）：`NeoSwitch is set up: /neoswitch:on answers this session with Gemini 4 Neon, and /neoswitch:off returns it to Claude any time.` 运行过 `/neoswitch:off` 的项目不会再出现；在环境变量里设置 `NEOSWITCH_NO_HINT=1` 可以关掉它。
- 标题栏仍然显示 Claude 的型号名，例如 `claude-opus-5-5`。这只是个标签，实际回答的是 Gemini 4 Neon。用 `/model` 换模型也不会改变这一点。
- 每次回答可能要等 5 到 40 秒才出现第一个字，这是正常的。如果你是用 `/neoswitch:login` 登录的，输入 `/effort low` 可以让回答更快开始。
- 安装或更新后的第一条 NeoSwitch 命令可能要等上一分钟，NeoSwitch 在做准备。
- 随时可以运行 `/neoswitch:status`，它会告诉你当前会话用的是 Claude 还是 Gemini 4 Neon，以及下一步该做什么。
- 在使用 Gemini 4 Neon 的会话里，auto mode 开箱即用：NeoSwitch 内置了默认保守拦截（fail-closed）的安全分类器（Antigravity 下默认使用 `gemini-3.8-flash`，其他后端使用当前配置的模型），无需重启 Claude Code，也不消耗 Claude 用量。如需关闭 NeoSwitch 内置检查，可设置 `NEOSWITCH_SAFEGUARD=off`；如需改由 Claude 检查每个操作，可用 `CLAUDE_CODE_AUTO_MODE_SERVER=0 claude` 启动（会消耗少量 Claude 用量，Claude 无应答时回退到 Gemini）。
- 有新版 NeoSwitch 时，会话开头的提示会告诉你，并给出更新命令。打开了自动更新的话，Claude Code 会自己装好新版；已经开着的窗口在有新的 Claude Code 会话启动时就会换到新版。Windows 上要关掉所有 Claude Code 窗口再重新打开后才生效。

## 出了问题怎么办

| 你看到的情况 | 怎么做 |
|---|---|
| 会话开头的提示说 NeoSwitch 还没设置好 | 运行 `/neoswitch:login`。下一条消息就会用 Gemini 4 Neon。 |
| 提示说没有安装 `agy` | `/neoswitch:login` 没能自动装上它。在终端里运行 `curl -fsSL https://antigravity.google/cli/install.sh \| bash`，或从 [Google 的下载页](https://antigravity.google/download)安装，再运行一次 `/neoswitch:login`。 |
| 每条消息都报连接错误，或者会话开头的提示说本会话无法回答 | 运行 `/neoswitch:off`：这个会话从下一条消息起就回到 Claude，即使这条命令自己的回复失败也一样。如果消息仍然失败，输入 `/exit` 并重新运行 `claude`：新会话用的是 Claude，再运行 `/neoswitch:status` 看看该修什么。 |
| 提示说其他 Claude Code 窗口还在共用之前的设置 | 在这个会话里运行 `/neoswitch:on`，所有开着的窗口都会换到这个会话的设置。Windows 上要关掉所有 Claude Code 窗口，重新打开 Claude Code，再运行 `/neoswitch:on`。 |
| 提示说 claude.ai connectors 已停用，因为设置了另一个认证来源 | 现在的 NeoSwitch 不会再导致这个提示，是旧版本造成的。请更新插件（见下文）。如果仍然出现，说明有别的工具给 Claude Code 设置了 `ANTHROPIC_AUTH_TOKEN` 或 `ANTHROPIC_API_KEY`。 |
| 提示说有别的程序占用了 NeoSwitch 需要的端口 | 关掉那个程序后重新启动 Claude Code，或者运行 `/neoswitch:off`。 |
| 提示说 NeoSwitch 下载不了它的程序 | 检查网络连接，然后重新启动 Claude Code。如果这台电脑之前运行过 NeoSwitch，它会继续用那个旧版本，直到下载成功。 |
| `/neoswitch:off` 本身提示 NeoSwitch 无法运行 | 它会列出要从项目的 `.claude/settings.local.json` 里删除的条目；删掉后开一个新会话。[卸载 NeoSwitch](#关闭更新与卸载) 的第 1 步列的是同样的条目。 |
| 卸载 NeoSwitch 后，某个项目里的 Claude Code 连不上 | 这个项目仍然指向 NeoSwitch。按 [卸载 NeoSwitch](#关闭更新与卸载) 第 1 步的说明，从 `.claude/settings.local.json` 里删掉它的条目，然后开一个新会话。 |
| `/neoswitch:login` 提示是未知命令 | 更新插件（见下文），重启 Claude Code，再运行 `/reload-plugins`。 |
| 提示说你的 Google 账号的 Antigravity 套餐不提供 gemini-4-neon | 你的套餐不包含 Gemini 4 Neon，NeoSwitch 无法用它回答，项目会保持使用 Claude。再次运行 `/neoswitch:login`，换一个套餐包含它的账号登录；或者运行 `/neoswitch:off` 并开一个新会话回到 Claude。 |

## 关闭、更新与卸载

- `/neoswitch:off` 让这个会话从下一条消息起回到 Claude，用你自己的 Claude 登录。`/neoswitch:on` 再把它切回 Gemini 4 Neon，不用重新设置。关闭会话也就结束了它的切换：每个新会话都从 Claude 开始。

手动更新：在终端里运行下面两条命令，然后开一个新的 Claude Code 会话，已经开着的窗口也会换到新版（Windows 上要关掉所有 Claude Code 窗口再重新打开）：

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
| 设置和日志：`proxy.json`（地址、访问密钥和所选方式）和 `daemon.log` | 设置了 `NEOSWITCH_STATE_DIR` 时就是它；否则 macOS 上是 `~/Library/Application Support/neoswitch/`，Linux 上是 `$XDG_CONFIG_HOME/neoswitch/` 或 `~/.config/neoswitch/` |

Google 登录信息由 `agy` 保存，不归 NeoSwitch 管，卸载后仍然保留；`agy` 本身也一样，包括 `/neoswitch:login` 帮你装的那份。

## NeoSwitch 会改动什么

当某个项目里有会话在用 Gemini 4 Neon 时，NeoSwitch 会往这个项目的 `.claude/settings.local.json` 的 `env` 对象中加入三项；这个项目里不再有会话使用它时，就把它们删掉：

- `ANTHROPIC_BASE_URL`、`ANTHROPIC_CUSTOM_HEADERS` 和 `NODE_EXTRA_CA_CERTS`：让这个项目的 Claude Code 请求经过一条只有 NeoSwitch 能应答的加密连接，发到这台电脑上的 NeoSwitch。你切换过的会话由 Gemini 4 Neon 回答；其他请求原样转给 Claude，用的是你自己的 Claude 登录，NeoSwitch 既不保存也不记录它。请求头里的密钥只能用来访问本机的 NeoSwitch。如果文件里原本有你自己的 `NODE_EXTRA_CA_CERTS` 或 `ANTHROPIC_CUSTOM_HEADERS`，NeoSwitch 会保留一份副本，删除自己的条目时把你的值放回去。

在 Git 项目里，NeoSwitch 还会把 `.claude/settings.local.json` 加进本地仓库自己的排除列表（`.git/info/exclude`），让这个 token 不会被提交；你的 `.gitignore` 不会被改动。

NeoSwitch 只在这台电脑上运行，也只接受来自这台电脑的连接。它不记录提示词、回复、key 或登录信息，也不经过我们的任何服务器。Google 登录信息留在 `agy` 里，NeoSwitch 从不读取。

## 更多

哪些功能可用、哪些不可用，NeoSwitch 的工作原理，独立命令行工具，环境变量，路线图和开发说明：见 [docs/cli.md](../docs/cli.md)（英文）。另见 [Antigravity 登录详情](../docs/antigravity.md)和[协议约定](../docs/protocol-contract.md)。

参与贡献见 [CONTRIBUTING.md](../CONTRIBUTING.md)，私下报告安全漏洞见 [SECURITY.md](../SECURITY.md)，行为准则见 [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md)。

MIT License
