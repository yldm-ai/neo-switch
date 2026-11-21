# NeoSwitch

[English](../README.md) | **简体中文** | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Português (Brasil)](README.pt-BR.md) | [Русский](README.ru.md)

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

NeoSwitch 让 Claude Code 改用 Google 的 Gemini 4 Neon 模型来回答，Claude Code 的其他一切照旧。

[![三步设置 NeoSwitch](../docs/media/neoswitch-demo.gif)](../docs/media/neoswitch-demo.mp4)

开关以项目为单位，你的其他项目继续使用 Claude。插件支持 macOS 和 Linux，暂不支持 Windows。

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

命令同时会显示一个链接，用于打开的浏览器你用不了的情况（例如通过 SSH 连接的远程机器）：在任意设备的浏览器中打开链接并登录，然后运行 `/neoswitch:login` 加上页面显示的授权码。已登录时再次运行 `/neoswitch:login` 可以切换到另一个 Google 账号：完成之前当前账号保持不变，运行 `/neoswitch:login cancel` 则保留当前账号。

> [!WARNING]
> [Google Antigravity 服务条款](https://antigravity.google/terms)（第 6 条）禁止通过其他工具使用 Antigravity，NeoSwitch 就属于这类工具。Google 可能暂停或关闭这样做的人的 Antigravity 账号。`/neoswitch:login` 每次都会重复这条警告。不接受这个风险，就不要登录。

**3. 发下一条消息。** 运行命令的那个窗口从下一条消息起就用 Gemini 4 Neon，这个项目里的新会话也一样。如果命令的回复要你重启（Claude Code 是通过网关启动的，或者别的 Claude Code 窗口还在用 NeoSwitch 之前的设置），就关掉它提到的窗口，然后在同一个项目里重新运行 `claude`。这个项目里最后一个使用 NeoSwitch 的会话关闭后，项目会切回 Claude，下次打开的会话就是 Claude；想再用 Gemini 4 Neon，运行 `/neoswitch:on`。

## 开启后你会看到

- 新会话开头会出现一条提示，例如 `NeoSwitch: this session uses Gemini 4 Neon through your Google account (Antigravity sign-in). Ignore the Claude model name in the header. The first reply can take a while.`
- 标题栏仍然显示 Claude 的型号名，例如 `claude-opus-5-5`。这只是个标签，实际回答的是 Gemini 4 Neon。用 `/model` 换模型也不会改变这一点。
- 每次回答可能要等 5 到 40 秒才出现第一个字，这是正常的。如果你是用 `/neoswitch:login` 登录的，输入 `/effort low` 可以让回答更快开始。
- 安装或更新后的第一条 NeoSwitch 命令可能要等上一分钟，NeoSwitch 在做准备。
- 随时可以运行 `/neoswitch:status`，它会告诉你当前会话用的是 Claude 还是 Gemini 4 Neon，以及下一步该做什么。
- 有新版 NeoSwitch 时，会话开头的提示会告诉你，并给出更新命令。打开了自动更新的话，Claude Code 会自己装好新版，关掉所有 Claude Code 窗口再重新打开后生效。

## 出了问题怎么办

| 你看到的情况 | 怎么做 |
|---|---|
| 会话开头的提示说 NeoSwitch 还没设置好 | 运行 `/neoswitch:login`。下一条消息就会用 Gemini 4 Neon。 |
| 提示说没有安装 `agy` | `/neoswitch:login` 没能自动装上它。在终端里运行 `curl -fsSL https://antigravity.google/cli/install.sh \| bash`，或从 [Google 的下载页](https://antigravity.google/download)安装，再运行一次 `/neoswitch:login`。 |
| 每条消息都报连接错误，或者会话开头的提示说本会话无法回答 | 就在这个项目里运行 `/neoswitch:off`。即使它自己的回复也显示连接错误，项目也已经切回去了。然后输入 `/exit` 并重新运行 `claude`：你回到了 Claude，再运行 `/neoswitch:status` 看看该修什么。 |
| 提示说其他 Claude Code 窗口还在共用之前的设置 | 关掉所有 Claude Code 窗口，重新打开 Claude Code，再运行 `/neoswitch:on`。 |
| 提示说 claude.ai connectors 已停用，因为设置了另一个认证来源 | 这个会话在用 NeoSwitch 时属于正常现象：它的本地访问密钥优先于你的 claude.ai 登录。项目切回 Claude 之后（最后一个使用 NeoSwitch 的会话关闭，或运行 `/neoswitch:off`），下一个会话里 connectors 就会恢复。 |
| 提示说有别的程序占用了 NeoSwitch 需要的端口 | 关掉那个程序后重新启动 Claude Code，或者运行 `/neoswitch:off`。 |
| 提示说 NeoSwitch 下载不了它的程序 | 检查网络连接，然后重新启动 Claude Code。 |
| `/neoswitch:login` 提示是未知命令 | 更新插件（见下文），重启 Claude Code，再运行 `/reload-plugins`。 |
| 提示说你的 Google 账号的 Antigravity 套餐不提供 gemini-4-neon | 你的套餐不包含 Gemini 4 Neon，NeoSwitch 无法用它回答，项目会保持使用 Claude。再次运行 `/neoswitch:login`，换一个套餐包含它的账号登录；或者运行 `/neoswitch:off` 并开一个新会话回到 Claude。 |

## 关闭、更新与卸载

- `/neoswitch:off` 让这个项目的新会话切回 Claude；已经在用 Gemini 4 Neon 的会话会一直用到你输入 `/exit`。`/neoswitch:on` 把项目再切回 Gemini 4 Neon，不用重新设置，从下一条消息开始生效。关闭项目里最后一个使用 NeoSwitch 的会话，效果相同。

手动更新：在终端里运行下面两条命令，然后关掉所有 Claude Code 窗口再重新打开：

```bash
claude plugin marketplace update neoswitch
claude plugin update neoswitch@neoswitch
```

如果你当初是用旧名字 `yldm-ai/NeoSwitch` / `yldm-ai/neon-switch` 添加的 marketplace，先运行 `claude plugin marketplace add yldm-ai/neo-switch` 重新添加。

卸载 NeoSwitch：

1. 先在每个开启过的项目里运行 `/neoswitch:off`。没有关掉的项目仍然指向 NeoSwitch，卸载后就无法回答了。如果已经卸载了插件，请在每个这样的项目里打开 `.claude/settings.local.json`，从 `env` 对象中删掉 `ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN` 和 `CLAUDE_CODE_USE_GATEWAY`。
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

在你开启的每个项目里，NeoSwitch 会往 `.claude/settings.local.json` 的 `env` 对象中加入三项，`/neoswitch:off` 或关闭最后一个使用 NeoSwitch 的会话时会把它们删掉：

- `ANTHROPIC_BASE_URL` 和 `ANTHROPIC_AUTH_TOKEN`：让这个项目的新 Claude Code 会话连到这台电脑上的 NeoSwitch。这个 token 只能用来访问本机的 NeoSwitch，不是 Google 或 Anthropic 的凭证。
- `CLAUDE_CODE_USE_GATEWAY=0`：让这些会话直接连 NeoSwitch。有些工具会通过别的网关启动 Claude Code 并打开网关模式，那样 Claude Code 就会忽略 NeoSwitch。

在 Git 项目里，NeoSwitch 还会把 `.claude/settings.local.json` 加进本地仓库自己的排除列表（`.git/info/exclude`），让这个 token 不会被提交；你的 `.gitignore` 不会被改动。

NeoSwitch 只在这台电脑上运行，也只接受来自这台电脑的连接。它不记录提示词、回复、key 或登录信息，也不经过我们的任何服务器。Google 登录信息留在 `agy` 里，NeoSwitch 从不读取。

## 更多

哪些功能可用、哪些不可用，NeoSwitch 的工作原理，独立命令行工具，环境变量，路线图和开发说明：见 [docs/cli.md](../docs/cli.md)（英文）。另见 [Antigravity 登录详情](../docs/antigravity.md)和[协议约定](../docs/protocol-contract.md)。

参与贡献见 [CONTRIBUTING.md](../CONTRIBUTING.md)，私下报告安全漏洞见 [SECURITY.md](../SECURITY.md)，行为准则见 [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md)。

MIT License
