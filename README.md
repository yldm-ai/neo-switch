# NeoSwitch

**English** | [简体中文](i18n/README.zh-CN.md) | [繁體中文](i18n/README.zh-TW.md) | [日本語](i18n/README.ja.md) | [한국어](i18n/README.ko.md) | [Español](i18n/README.es.md) | [Français](i18n/README.fr.md) | [Deutsch](i18n/README.de.md) | [Português (Brasil)](i18n/README.pt-BR.md) | [Русский](i18n/README.ru.md)

[![Claude Code plugin](https://img.shields.io/badge/Claude%20Code-plugin-D97757)](https://github.com/yldm-ai/neo-switch/blob/main/.claude-plugin/plugin.json) [![Release](https://img.shields.io/github/v/release/yldm-ai/neo-switch)](https://github.com/yldm-ai/neo-switch/releases/latest) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/yldm-ai/neo-switch/blob/main/LICENSE)

NeoSwitch lets you switch individual Claude Code sessions to Google's Gemini 4 Neon while keeping your Claude Code setup, connectors, permissions, and other windows unchanged.

[![Setting up NeoSwitch in three steps](docs/media/neoswitch-demo.gif)](docs/media/neoswitch-demo.mp4)

Switch one session at a time — other windows in the same project keep using Claude. The plugin works on macOS and Linux; Windows is not supported yet.

## Highlights

- **One session at a time, switch back in place.** `/neoswitch:on` and `/neoswitch:off` affect only the current session from its next message; other windows in the same project and new sessions stay on Claude, and closing the last switched session removes NeoSwitch's project entries automatically.
- **Claude Code stays Claude Code.** No `ANTHROPIC_AUTH_TOKEN` is written, so `claude.ai` connectors keep working; built-in `WebSearch` runs via Google on Antigravity; Auto Mode works out of the box with a fail-closed safety classifier (no restart, zero Claude usage); `/effort` and optional thinking blocks (`NEOSWITCH_SHOW_THINKING`) map cleanly to Gemini.
- **Instant native commands and live quota.** On Claude Code 2.1.326 or later, `/neoswitch:login`, `/neoswitch:on`, `/neoswitch:off` and `/neoswitch:status` answer directly with no model turn, a status line under the prompt shows which model answers, and `/neoswitch:status` shows 5-hour and weekly Antigravity quota bars and reset times with zero quota spent (`-v` adds all quota groups and credits).
- **Built for long real sessions.** ACP session reuse and chunking avoid `agy`'s 192 KB prompt truncation, a slim custom agent cuts `agy`'s prompt overhead from ~14k to under 1k tokens, parallel tool calls include JSON escape repair, and prompt suggestions and session titles route to `gemini-3.8-flash` to save Gemini 4 Neon quota.
- **Local, isolated, reversible.** Traffic stays on local TLS whose CA private key is discarded after signing; NeoSwitch never reads, stores or replays Google OAuth tokens; `agy` runs in an isolated workspace with its own file and shell tools blocked by both the slim agent and a verified `PreToolUse` hook; your own `NODE_EXTRA_CA_CERTS` and `ANTHROPIC_CUSTOM_HEADERS` are backed up and restored when NeoSwitch turns off.
- **SSH-ready and seamless updates.** `/neoswitch:login` opens a browser and prints a link + code flow for remote/SSH machines, plugin updates hand the daemon port over automatically without closing open windows, and build or download failures fall back to a cached release so `/neoswitch:off` always has a way out.
- **Multiple backends.** Use your Google account through official `agy`, or bring a `GEMINI_API_KEY`, Vertex AI, or the experimental OpenAI Responses backend.

## Quick start

**1. Install the plugin.** In Claude Code, run these two lines one at a time. Copy each line whole: the first one says `neo-switch`, the second one `neoswitch`.

```text
/plugin marketplace add yldm-ai/neo-switch
```

```text
/plugin install neoswitch@neoswitch
```

Turn on automatic updates so fixes reach you when Google or Claude Code change something NeoSwitch depends on: run `/plugin`, open the **Marketplaces** tab, choose `neoswitch` and turn on auto-update. Claude Code leaves it off for third-party marketplaces.

**2. Sign in with your Google account.** Open Claude Code in the project you want to switch and run:

```text
/neoswitch:login
```

It shows Google's terms warning, installs Google's official [Antigravity CLI (`agy`)](https://antigravity.google/download) into `~/.local/bin` (~230 MB) if missing, opens Google sign-in in your browser, and switches this session on. It also prints a link for remote/SSH machines: open it on any device, sign in, and paste the code into the Claude Code 2.1.326+ dialog (or run `/neoswitch:login <code>` on older versions, WSL, or the desktop app over SSH). Run `/neoswitch:login` again to switch Google accounts, or `/neoswitch:login cancel` to keep the current one. Prefer an API key or Vertex AI? See [docs/cli.md](docs/cli.md#gemini-api-key).

> [!WARNING]
> The [Google Antigravity Terms of Service](https://antigravity.google/terms) (section 6) forbid using Antigravity through other tools, and NeoSwitch is such a tool. Google may suspend or close the Antigravity accounts of people who do. `/neoswitch:login` repeats this warning every time. Do not sign in unless you accept that risk.

**3. Send your next message.** This session uses Gemini 4 Neon from its next message while other windows and new sessions stay on Claude. If the reply asks you to restart because Claude Code was started through a gateway, start `claude` again without it in the same project.

| Command | What it does |
|---|---|
| `/neoswitch:login [code \| cancel]` | Sign in via official `agy`, switch Google accounts, or finish an SSH code sign-in, then switch this session on |
| `/neoswitch:on` | Switch this session to Gemini 4 Neon from its next message |
| `/neoswitch:off` | Switch this session back to Claude from its next message |
| `/neoswitch:status [-v]` | Show active model, Google sign-in, and 5-hour/weekly quota bars (`-v` adds all quota groups, credits, backend, and daemon details) |

## How NeoSwitch compares

| | **NeoSwitch** | **Config managers & routers**<br>(e.g. CC Switch, Claude Code Router) | **Standalone Antigravity proxies**<br>(e.g. antigravity-claude-proxy) |
|---|---|---|---|
| **Setup** | Native Claude Code plugin — no desktop app or server to manage | Separate desktop app managing configs across many CLIs | Standalone Node.js proxy server + Web UI |
| **Scope** | **One session at a time** (`/neoswitch:on`, `/neoswitch:off`); other windows stay on Claude | Global provider switching or shared local router | Global `~/.claude/settings.json` or shell environment |
| **Claude Code features** | **Kept intact** — no `ANTHROPIC_AUTH_TOKEN`, so `claude.ai` connectors, `WebSearch`, Auto Mode, live quota, and native mod commands keep working | Managed through external config or router | Sets `ANTHROPIC_AUTH_TOKEN`, which disables `claude.ai` connectors |
| **Google sign-in** | Uses your Google account through official `agy` — **never reads, stores, or replays OAuth tokens**; `agy`'s own tools are blocked | Primarily API keys and third-party relays (no `agy` integration) | Stores or reads local OAuth tokens to call private endpoints directly |
| **Best for** | Keeping your normal Claude Code setup and switching individual sessions to Gemini 4 Neon | Managing many CLIs, API keys, relays, and MCPs in one place | Multi-account rotation, Windows, or running Claude models via Antigravity |

## Good to know

- **Model label:** Claude Code's top header keeps showing the Claude model name (such as `claude-opus-5-5`), and `/model` does not change the backend model. On Claude Code 2.1.326 or later, a status line under the prompt displays `Gemini 4 Neon answers this session` while NeoSwitch is active.
- **Latency, `/effort` and thinking:** A reply can take 5 to 40 seconds before its first words appear. `/effort low` makes replies start sooner (on both Antigravity and Gemini API / Vertex AI), and `NEOSWITCH_SHOW_THINKING=on` shows upstream reasoning as Claude Code thinking blocks.
- **First run after install or update:** The first NeoSwitch command after an install or update can take up to a minute while NeoSwitch builds or downloads the verified release binary.
- **Auto Mode:** Works out of the box with NeoSwitch's fail-closed safety classifier (`gemini-3.8-flash` on Antigravity, zero Claude usage, no restart). Set `NEOSWITCH_SAFEGUARD=off` to turn it off, or start Claude Code with `CLAUDE_CODE_AUTO_MODE_SERVER=0 claude` to check actions with your Claude login instead (uses some Claude usage, falling back to Gemini if Claude does not answer).
- **Once-a-day note:** Once NeoSwitch is set up, a new session that does not use it opens at most once a day per project with a one-line `/neoswitch:on` note (skipped in projects where you ran `/neoswitch:off`; `NEOSWITCH_NO_HINT=1` turns it off).
- **Automatic updates:** When a newer release is available, the session note and `/neoswitch:status` say so. With auto-update enabled, open Claude Code windows move to the updated daemon automatically as soon as a new session starts.

## If something goes wrong

<details>
<summary>Common issues and fixes (click to expand)</summary>

| What you see | What to do |
|---|---|
| The session note says NeoSwitch is not set up yet | Run `/neoswitch:login`. Your next message uses Gemini 4 Neon. |
| A message says `agy` is not installed | `/neoswitch:login` could not install it by itself. Run `curl -fsSL https://antigravity.google/cli/install.sh \| bash` in a terminal, or install it from [Google's download page](https://antigravity.google/download), then run `/neoswitch:login` again. |
| Every message fails with a connection error, or the session note says this session cannot answer | Run `/neoswitch:off`: on Claude Code 2.1.326+ it switches this session back to Claude locally with no model call. If the daemon itself is not running or messages keep failing, type `/exit` and start `claude` again: a new session uses Claude, and `/neoswitch:status` tells you what to fix. |
| The note says other Claude Code windows still share the earlier setup | Run `/neoswitch:on` in this session: open windows move to the current version automatically. |
| A warning says `claude.ai` connectors are disabled because another auth source is set | NeoSwitch no longer causes it; an earlier version did. Update the plugin (see below). If it stays, another tool sets `ANTHROPIC_AUTH_TOKEN` or `ANTHROPIC_API_KEY` for Claude Code. |
| The note says another program took the port NeoSwitch used | NeoSwitch already moved to a free port and returned the project to Claude for the next session. Start a new session (which uses Claude) and run `/neoswitch:on` to use Gemini. |
| The note says NeoSwitch could not download its program | Check your internet connection and start Claude Code again. If NeoSwitch ran on this computer before, it keeps using that earlier cached version until the download works. |
| `/neoswitch:off` itself says NeoSwitch could not run | It lists the entries to delete from the project's `.claude/settings.local.json`; delete them and start a new session. Step 1 of [removing NeoSwitch](#turn-it-off-update-or-remove) lists the same entries. |
| Claude Code cannot connect in a project after NeoSwitch was removed | The project still points at NeoSwitch. Delete its entries from `.claude/settings.local.json`, as step 1 of [removing NeoSwitch](#turn-it-off-update-or-remove) describes, and start a new session. |
| `/neoswitch:login` is an unknown command | Update the plugin (see below), restart Claude Code, then run `/reload-plugins`. |
| A message says your Google account's Antigravity plan does not offer `gemini-4-neon` | NeoSwitch left this session on Claude and listed the models your plan does offer. Run `/neoswitch:login` to sign in with an account whose plan includes `gemini-4-neon`, or export `GEMINI_MODEL` set to one of the listed models. |

</details>

## Turn it off, update or remove

- `/neoswitch:off` switches this session back to Claude from its next message, with your own Claude login. `/neoswitch:on` switches it to Gemini 4 Neon again without setting anything up again. Closing a session ends its switch: every new session starts on Claude.

To update by hand, run these in a terminal, then open a new Claude Code session (open windows move to the update automatically):

```bash
claude plugin marketplace update neoswitch
claude plugin update neoswitch@neoswitch
```

If you added the marketplace under its old name `yldm-ai/NeoSwitch` / `yldm-ai/neon-switch`, add it again with `claude plugin marketplace add yldm-ai/neo-switch` first.

To remove NeoSwitch:

1. Run `/neoswitch:off` in each project you switched on. A project left on keeps pointing at NeoSwitch and stops answering once NeoSwitch is gone. If you already removed the plugin, open `.claude/settings.local.json` in each such project and change its `env` object: delete `ANTHROPIC_BASE_URL` when it starts with `https://127.0.0.1:` (from earlier versions `http://127.0.0.1:`, with the `ANTHROPIC_AUTH_TOKEN` next to it); delete the `X-NeoSwitch-Token` line of `ANTHROPIC_CUSTOM_HEADERS`, and the whole entry when nothing else is left; delete `NODE_EXTRA_CA_CERTS` when it names `tls/bundle.pem` in NeoSwitch's folder, or put back your own CA file if you had one there; and delete `CLAUDE_CODE_GATEWAY_HINT_HEADERS` set to `1` and `CLAUDE_CODE_USE_GATEWAY` set to `0`, unless you set them yourself.
2. Close the Claude Code windows that use NeoSwitch, then run these in a terminal:

```bash
claude plugin uninstall neoswitch@neoswitch
claude plugin marketplace remove neoswitch
```

3. Delete the files NeoSwitch keeps outside your projects:

| What | Where |
|---|---|
| The NeoSwitch program the plugin downloads or builds | `$XDG_CACHE_HOME/neoswitch/`, or `~/.cache/neoswitch/` when `XDG_CACHE_HOME` is unset |
| State, TLS bundle, isolated `agy` workspace, and `daemon.log` | `NEOSWITCH_STATE_DIR` when set; otherwise `~/Library/Application Support/neoswitch/` on macOS and `$XDG_CONFIG_HOME/neoswitch/` or `~/.config/neoswitch/` on Linux |

The Google sign-in is kept by `agy`, not by NeoSwitch, and stays in place; so does `agy` itself, even when `/neoswitch:login` installed it.

## What NeoSwitch changes

While a session in a project uses Gemini 4 Neon, NeoSwitch adds four entries to the `env` object of that project's `.claude/settings.local.json`, and removes them once no session there uses it:

- `ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS` and `NODE_EXTRA_CA_CERTS` send the project's Claude Code requests to NeoSwitch on this computer, over an encrypted connection only NeoSwitch can answer. NeoSwitch answers the sessions you switched with Gemini 4 Neon and passes every other request on to Claude unchanged, with your own Claude login, which it neither keeps nor logs. The header holds a key that only opens NeoSwitch on this computer. If the file had a `NODE_EXTRA_CA_CERTS` or `ANTHROPIC_CUSTOM_HEADERS` of your own, NeoSwitch keeps a copy and puts your value back when it removes its entries.
- `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1` makes Claude Code say in a header whether a request comes from your conversation itself, a subagent, a compaction or a background task such as a prompt suggestion. NeoSwitch needs this to keep your conversation's Gemini session for your conversation. If you set this entry yourself, NeoSwitch leaves your value alone.

In a Git project, NeoSwitch also adds `.claude/settings.local.json` to the clone's own exclude list (`.git/info/exclude`), so the token stays out of commits; your `.gitignore` is not touched.

NeoSwitch runs only on this computer and accepts connections only from it. It logs no prompts, replies, keys or sign-in details, and no server of ours is involved. The Google sign-in stays inside `agy`; NeoSwitch never reads it.

## More

What works and what does not, how NeoSwitch works, the standalone command-line tool, environment variables, the roadmap and development: [docs/cli.md](docs/cli.md). Also: [Antigravity sign-in details](docs/antigravity.md) and the [protocol contract](docs/protocol-contract.md).

See [CONTRIBUTING.md](CONTRIBUTING.md) to contribute, [SECURITY.md](SECURITY.md) to report a vulnerability privately, and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for the code of conduct.

MIT License
