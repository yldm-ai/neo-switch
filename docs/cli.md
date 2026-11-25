# NeoSwitch reference

This page is for people who want the details: what works with Gemini 4 Neon, how NeoSwitch works, the standalone `neoswitch` command-line tool, its environment variables, the roadmap and development. To install and use the Claude Code plugin, the [README](../README.md) is all you need.

Plugin users do not have a `neoswitch` command in their terminal: the plugin keeps its own copy of the program in its cache directory and runs it through the `/neoswitch:*` slash commands. The commands below are for the standalone binary.

## Compatibility

Claude Code still runs the session, permissions, tool execution, MCP servers, skills, hooks, `CLAUDE.md` and `/resume`; NeoSwitch only replaces the model behind them. How well those features work depends on how well the model follows Claude Code's tool protocol, which has been exercised in everyday coding use. An automated end-to-end suite runs the real Claude Code CLI through NeoSwitch against a scripted Gemini server on every change and checks that tool calls and results, file edits, Bash, parallel calls, permission denials, hooks, skills, `/resume`, subagents and MCP tools pass through the Gemini API route intact; it does not measure how well a real model uses them.

| Capability | Gemini API / Vertex AI | Antigravity OAuth (ACP) |
|---|---|---|
| Streaming text | Supported | Supported |
| Tool calls, including parallel calls | Native Gemini function calling | Partial: the model writes Claude tool-call JSON, which arrives whole instead of streamed |
| Multi-turn tool use and `/resume` | Supported; thought signatures are carried in the tool IDs | Supported |
| Images | Supported | Supported when `agy` offers image input |
| PDFs | Supported | Not supported; the model is told a file was omitted |
| `/effort` | Sent as Gemini's thinking level; not verified against the live API, and dropped if the model rejects it | Mapped to `agy`'s thought level |
| Token usage and `count_tokens` | Reported; `count_tokens` is estimated on the Developer API | Reported, including about 14k tokens of `agy` instructions added to every prompt |
| Gemini 4 Neon access | Depends on your key or project; check with `neoswitch doctor` | Verified with a signed-in `agy` |

Not supported on either path: server tools such as code execution, URL image and document sources, and Claude thinking blocks. Claude Code's WebSearch works on the Antigravity route, which searches with Google ([antigravity.md](antigravity.md#web-search)), and not with a Gemini API key. `cache_control` and `context_management` are ignored. Claude Code's `/model` choice does not change the model that answers: every Google route uses `GEMINI_MODEL`, which defaults to `gemini-4-neon`. NeoSwitch never falls back to another model silently.

The full endpoint contract is in [protocol-contract.md](protocol-contract.md), and ACP details are in [antigravity.md](antigravity.md).

## How the plugin works

Switching is per session. `/neoswitch:on` (and `/neoswitch:login`, which ends with it) registers its session, by the id Claude Code sends with every request (`CLAUDE_CODE_SESSION_ID`), with the local daemon, then writes four keys to the project's `.claude/settings.local.json`: `ANTHROPIC_BASE_URL` (`https://127.0.0.1:<port>`), `ANTHROPIC_CUSTOM_HEADERS` (NeoSwitch's token header, after the user's own headers), `NODE_EXTRA_CA_CERTS` (a bundle of the user's own extra CAs and NeoSwitch's local CA), and `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1` (unless the project sets it already), which makes Claude Code say which part of it sent each request. Gateway mode (`CLAUDE_CODE_USE_GATEWAY`) is left alone: a session started by a gateway launcher such as `everyapi use claude` takes the base URL from its environment, so it keeps talking to its gateway and never reaches NeoSwitch. Versions up to 0.4.1 wrote `CLAUDE_CODE_USE_GATEWAY=0`, which sent such sessions through NeoSwitch to Anthropic with the gateway's credential; switching a project on or off removes that entry. No `ANTHROPIC_AUTH_TOKEN` is written: every session keeps the user's own Claude login, so claude.ai connectors stay on. Every Claude Code window in the project applies the keys, but the daemon routes by session: the model requests of registered sessions go to Gemini without the user's credential, and every other request passes through unchanged to Anthropic (or to the gateway the user's `ANTHROPIC_BASE_URL` named when the project was switched) with the user's login, which the daemon neither stores nor logs. `/neoswitch:off` takes the session out of the registered set, so its next request passes through to Claude.

The daemon serves TLS with a certificate from a CA it makes once and whose private key it never writes, so a program that takes the port while the daemon is down can complete no handshake and gets neither the credential nor the conversation; the CLI and hooks trust only that CA. The CA carries no name constraints: Claude Code's TLS stack refuses any certificate under a CA that constrains IP addresses. The server certificate lasts 397 days and is renewed, with a new CA, only while no daemon runs.

The registered sessions and the projects whose keys point at the daemon are kept in the state folder, so a daemon started again keeps every session where it was. The keys stay in a project while one of its registered sessions runs, and are removed when the last one opts out or ends, at once through the SessionEnd hook, within 10 seconds when a session crashed, and when the daemon is stopped by a signal; a check every 10 seconds also removes keys that point at the daemon with nobody registered. The SessionStart hook records each session's id and process in `<state>/sessions/` (only where the state folder exists), which places a request from a /clear-ed session, whose id changed, and keeps the daemon running for windows that applied the keys without ever attaching; a window the daemon cannot place keeps it running for 30 minutes after its last request. The UserPromptSubmit hook starts the daemon again before a prompt when a session's requests go through it.

With Antigravity OAuth, `/neoswitch:on` first checks that `agy` is signed in and that the account's plan offers the model, by opening one session without a prompt, and leaves the session on Claude otherwise: a session switched to a route that cannot answer would keep failing. When another program took NeoSwitch's saved port, as can happen after a reboot, NeoSwitch moves to a free port. The Stop, SessionEnd and UserPromptSubmit hooks run NeoSwitch with `--cached`: they use the binary already built or downloaded, and otherwise do nothing, so they never build or download in the middle of a session.

With the OAuth route, the daemon starts the official `agy acp` subprocess. NeoSwitch sends the translated request through an ACP session and translates the returned answer or tool-call JSON back to the local Anthropic-compatible endpoint. One `agy` process serves all sessions, and requests run concurrently in separate ACP sessions. `agy`'s own file, shell and web tools are refused on every request, so tools run only in Claude Code, under Claude Code's permission rules.

`/neoswitch:login` saves Antigravity OAuth as the backend, so a Gemini key exported for other tools does not take over. Without a saved choice, NeoSwitch uses Gemini credentials from the environment when present, and Antigravity OAuth when `agy` is installed. `NEOSWITCH_BACKEND=antigravity|gemini|openai|auto` overrides all of these, and `neoswitch status -v` shows the backend in use and why.

The plugin runs the binary through `plugin/neoswitch.sh`. With Go installed, it builds the binary from the plugin's own source on first use. Without Go, it downloads the release binary named by the repository's `VERSION` file for macOS or Linux (amd64 or arm64) and verifies its SHA-256 checksum before running it; when the build or the download fails, for example offline right after an update, it runs the latest NeoSwitch in its cache, so `/neoswitch:off` keeps working; with none cached, `/neoswitch:off` names the settings entries to delete by hand. Between releases the binary can be older than the plugin source.

Claude Code does not auto-update third-party marketplaces unless the user turns it on, so the plugin checks for updates itself. At most every 6 hours, the SessionStart hook reads `main`'s `VERSION` from GitHub in a background process (it always names the release the marketplace pins), without delaying the session. When it is newer than the running version, the session note and `/neoswitch:status` say so and give the update commands. `NEOSWITCH_NO_UPDATE_CHECK=1` or Claude Code's `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` turns the check off. The standalone CLI does not check.

In a new session that does not use NeoSwitch, the SessionStart hook offers `/neoswitch:on`, at most once a day per project, using the shell alone. It does so only when the last check of the route by `/neoswitch:on` or `/neoswitch:login` passed (the `ready` file in the state folder), never in a project where `/neoswitch:off` ran (the `declined` file), never on resume, `/clear` or compaction, and never with `NEOSWITCH_NO_HINT` set.

The plugin is not supported on Windows: the daemon's check for open Claude Code sessions does not work there, so it would stop shortly after starting.

## Standalone CLI

Each tagged version publishes binaries for macOS (Apple Silicon and Intel), Linux (amd64 and arm64) and Windows (amd64), with SHA-256 checksums, on the [Releases](https://github.com/yldm-ai/neo-switch/releases) page. The Windows build is compiled but not yet tested. You can also build from source:

```bash
go build -o neoswitch ./cmd/neoswitch
go install ./cmd/neoswitch
```

Commands:

```bash
neoswitch claude [-- args]      # start Claude Code through a local proxy with an ephemeral port and token
neoswitch serve                 # run the Anthropic-compatible proxy for another client
neoswitch doctor                # check the Claude CLI and whether the credentials can use the model
neoswitch antigravity login     # ACP OAuth login through the official agy client (alias: antigravity oauth): a browser opens, and the printed link takes a pasted code from any device
neoswitch antigravity login cancel  # drop a sign-in still waiting for its code; the current sign-in stays
neoswitch antigravity probe     # verify the saved agy session without printing credentials
neoswitch models                # list the models the credentials can use, marking the configured one
neoswitch backend [choice]      # show or choose the backend: auto, antigravity, gemini or openai
neoswitch on [project]          # inside a Claude Code session: switch that session to Gemini
neoswitch off [project]         # inside a Claude Code session: switch it back to Claude
neoswitch status [-v] [project] # show whether a project uses NeoSwitch and what to do next (-v adds details)
neoswitch version               # print the NeoSwitch version
```

`neoswitch doctor` looks the model up and runs a one-line generation, then reports whether the credentials work or why they do not: rejected credentials, missing permission, region restriction, unknown model, quota, or an unavailable service. With the Antigravity route it prints the Terms warning and runs `neoswitch antigravity probe`.

### Gemini API key

Create a key in [Google AI Studio](https://aistudio.google.com/apikey); whether it can use Gemini 4 Neon depends on the key. NeoSwitch reads it from `GEMINI_API_KEY` in the environment and never saves it.

With the plugin, export the key in the shell you start Claude Code from, and add the same line to your shell profile (such as `~/.zshrc`) so every new session has it. Then start `claude` in the project and run `/neoswitch:on`; your next message goes through NeoSwitch. If you signed in with `/neoswitch:login` before, that saved choice wins over the key, so also export `NEOSWITCH_BACKEND=gemini`:

```bash
export GEMINI_API_KEY='your-key'
export NEOSWITCH_BACKEND=gemini   # only needed after an earlier /neoswitch:login
```

With the standalone CLI, `neoswitch backend gemini` saves that choice instead:

```bash
export GEMINI_API_KEY='your-key'
neoswitch doctor
neoswitch claude
```

### Vertex AI

```bash
export GOOGLE_GENAI_USE_VERTEXAI=true
export GOOGLE_CLOUD_PROJECT=my-project
export GOOGLE_CLOUD_LOCATION=global
neoswitch doctor
neoswitch claude
```

The Google Cloud SDK's Application Default Credentials must already be available to the process. Access to Gemini 4 Neon depends on your project and region.

### OpenAI (experimental)

The OpenAI backend sends requests to the OpenAI Responses API through the official `openai-go` SDK. It is opt-in: automatic selection never picks it, so set `NEOSWITCH_BACKEND=openai` or run `neoswitch backend openai`. `OPENAI_MODEL` is required; NeoSwitch does not choose a model for you. `OPENAI_BASE_URL` is optional and must include `/v1`. It has only been tested against a scripted upstream, not a live OpenAI account. What is and is not mapped is listed in [protocol-contract.md](protocol-contract.md#openai-backend-experimental).

```bash
export NEOSWITCH_BACKEND=openai
export OPENAI_API_KEY='your-key'
export OPENAI_MODEL='model-name'
neoswitch doctor
neoswitch claude
```

### Proxy for another client

```bash
export NEOSWITCH_AUTH_TOKEN="$(openssl rand -hex 24)"
GEMINI_API_KEY='your-key' neoswitch serve
```

Set both `ANTHROPIC_BASE_URL` and `ANTHROPIC_AUTH_TOKEN` in the client. `neoswitch claude` configures those variables automatically with an ephemeral port and token.

### Upgrade and remove

Download a newer binary from the [Releases](https://github.com/yldm-ai/neo-switch/releases) page, or install the latest version with Go:

```bash
go install github.com/yldm-ai/neo-switch/cmd/neoswitch@latest
```

Run `neoswitch off` in each project you enabled before removing NeoSwitch. To remove a binary installed with `go install`, delete `neoswitch` from `$(go env GOBIN)`, or from `$(go env GOPATH)/bin` when `GOBIN` is unset. The state directory is listed in the README.

## Environment variables

| Variable | Effect |
|---|---|
| `NEOSWITCH_BACKEND` | `antigravity`, `gemini`, `openai` or `auto`; overrides the saved choice |
| `NEOSWITCH_NO_UPDATE_CHECK=1` | Plugin only: do not check GitHub for a newer NeoSwitch |
| `NEOSWITCH_STATE_DIR` | State directory (default: `~/Library/Application Support/neoswitch/` on macOS, `$XDG_CONFIG_HOME/neoswitch/` or `~/.config/neoswitch/` on Linux) |
| `GEMINI_API_KEY` | Gemini Developer API key |
| `GOOGLE_GENAI_USE_VERTEXAI=true` with `GOOGLE_CLOUD_PROJECT` and `GOOGLE_CLOUD_LOCATION` | Use Vertex AI with Application Default Credentials instead |
| `GEMINI_MODEL` | Backend model (default: `gemini-4-neon`) |
| `NEOSWITCH_BACKGROUND_MODEL` | Antigravity route only. The model for Claude Code's prompt suggestions and session titles, which read the conversation without building on it (default: `gemini-3.8-flash`). `main` keeps them on `GEMINI_MODEL`. Without this variable, a `GEMINI_MODEL` other than the default keeps them on that model too. A model the account's plan does not offer falls back to `GEMINI_MODEL` ([antigravity.md](antigravity.md#background-requests)) |
| `GEMINI_BASE_URL` | Gemini API base URL |
| `OPENAI_API_KEY` | OpenAI API key (experimental `openai` backend) |
| `OPENAI_MODEL` | OpenAI model, required for the `openai` backend |
| `OPENAI_BASE_URL` | OpenAI API base URL including `/v1` (default: `https://api.openai.com/v1`) |
| `CLAUDE_CLI_PATH` | Claude executable (default: `claude`) |
| `ANTIGRAVITY_CLI_PATH` | Antigravity executable (default: `agy` on `PATH`, then `~/.local/bin/agy`, where Google's installer puts it). When set, `/neoswitch:login` does not install `agy` |
| `NEOSWITCH_LISTEN` | `serve` listen address (default: `127.0.0.1:8080`) |
| `NEOSWITCH_AUTH_TOKEN` | Token for standalone `serve`; required when `NEOSWITCH_LISTEN` is not a loopback address |

## Compatibility boundary

NeoSwitch uses the official Gemini SDK or the unmodified local `agy acp` process. It does not extract OAuth tokens, call Antigravity private HTTP APIs, relay credentials to a remote proxy, or modify hosts files.

Anthropic does not document routing Claude Code to non-Claude models, so Claude Code updates can change behavior that NeoSwitch relies on.

## Roadmap

- More model providers. OpenAI is available as an experimental backend and still needs verification against the live API. Providers that already offer an Anthropic-compatible endpoint can be used by setting `ANTHROPIC_BASE_URL` directly and do not need NeoSwitch.

## Development

```bash
go test ./...
go test -race ./...
go vet ./...
```

To release, follow [Releases in CONTRIBUTING.md](../CONTRIBUTING.md#releases): on its own branch, bump `VERSION` and the plugin's pinned tag in `.claude-plugin/marketplace.json`, push the matching tag (`v` followed by `VERSION`, such as `v0.1.0`), and merge to `main` only after the release is published. Users get the plugin from the pinned tag, never from `main`. The release workflow refuses a tag that does not match `VERSION`.

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the contribution workflow.
