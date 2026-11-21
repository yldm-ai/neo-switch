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

Not supported on either path: server tools such as web search and code execution, URL image and document sources, and Claude thinking blocks. `cache_control` and `context_management` are ignored. Claude Code's `/model` choice does not change the model that answers: every Google route uses `GEMINI_MODEL`, which defaults to `gemini-4-neon`. NeoSwitch never falls back to another model silently.

The full endpoint contract is in [protocol-contract.md](protocol-contract.md), and ACP details are in [antigravity.md](antigravity.md).

## How the plugin works

`/neoswitch:on` and `/neoswitch:login` write three keys to the project's `.claude/settings.local.json`, as listed in the README: `ANTHROPIC_BASE_URL` and `ANTHROPIC_AUTH_TOKEN` for the proxy, and `CLAUDE_CODE_USE_GATEWAY=0`, because in gateway mode, which gateway launchers turn on, Claude Code would take the base URL from its environment and silently ignore the proxy. The token authorizes the loopback proxy and is not an Antigravity credential.

With Antigravity OAuth, `/neoswitch:on` first checks that `agy` is signed in and that the account's plan offers the model, by opening one session without a prompt, and leaves the project on Claude otherwise: a session switched to a route that cannot answer would keep failing until it closes. When another program took NeoSwitch's saved port, as can happen after a reboot, NeoSwitch moves to a free port; nothing pointed at the old one could work either way. The SessionStart hook starts one local daemon and registers the Claude Code process before its first model request. `/neoswitch:on` does the same for the session it runs in, before it writes the project settings: a session not started in gateway mode applies those settings from its next request without restarting. Removing them does not switch a running session back, so after `/neoswitch:off` an open session keeps NeoSwitch until it closes. Each registration names the session's project, and routing lasts as long as those sessions do: once no registered session uses a project, the daemon removes its three keys, so the next session there starts on Claude again, with its claude.ai connectors, which Claude Code turns off while `ANTHROPIC_AUTH_TOKEN` is set. The Stop and SessionEnd hooks run NeoSwitch with `--cached`: they use the binary already built or downloaded, and otherwise do nothing, so they never build or download in the middle of a session. The SessionEnd hook (`plugin/session-end.sh`) tells the daemon at once when a session ends, ahead of a session reopened right away; the daemon's own check every 10 seconds covers a session that crashed, and a daemon stopped by a signal, as at logout, releases every project first. The daemon edits the file only when something changes, does not recreate it when it is gone, and reads it again when it changed while being edited, since Claude Code writes the same file. A daemon from before this tracking does not acknowledge the project, and the session note then makes no promise about it. The daemon exits after all registered Claude Code processes have exited. It reads the backend and the Gemini key once when it starts, so a changed setup reaches new sessions only after every Claude Code window using the daemon has closed.

With the OAuth route, the daemon starts the official `agy acp` subprocess. NeoSwitch sends the translated request through an ACP session and translates the returned answer or tool-call JSON back to the local Anthropic-compatible endpoint. One `agy` process serves all sessions, and requests run concurrently in separate ACP sessions. `agy`'s own file, shell and web tools are refused on every request, so tools run only in Claude Code, under Claude Code's permission rules.

`/neoswitch:login` saves Antigravity OAuth as the backend, so a Gemini key exported for other tools does not take over. Without a saved choice, NeoSwitch uses Gemini credentials from the environment when present, and Antigravity OAuth when `agy` is installed. `NEOSWITCH_BACKEND=antigravity|gemini|openai|auto` overrides all of these, and `neoswitch status -v` shows the backend in use and why.

The plugin runs the binary through `plugin/neoswitch.sh`. With Go installed, it builds the binary from the plugin's own source on first use. Without Go, it downloads the release binary named by the repository's `VERSION` file for macOS or Linux (amd64 or arm64) and verifies its SHA-256 checksum before running it; when the download fails, for example offline right after an update, it runs the version it downloaded earlier. Between releases that binary can be older than the plugin source.

Claude Code does not auto-update third-party marketplaces unless the user turns it on, so the plugin checks for updates itself. At most every 6 hours, the SessionStart hook reads `main`'s `VERSION` from GitHub in a background process, without delaying the session. When it is newer than the running version, the session note and `/neoswitch:status` say so and give the update commands. `NEOSWITCH_NO_UPDATE_CHECK=1` or Claude Code's `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` turns the check off. The standalone CLI does not check.

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
neoswitch on [project]          # enable routing for a project
neoswitch off [project]         # disable routing for a project
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

To release, follow [Releases in CONTRIBUTING.md](../CONTRIBUTING.md#releases): bump `VERSION` on its own branch, push the matching tag (`v` followed by its contents, such as `v0.1.0`), and merge to `main` only after the release is published. The release workflow refuses a tag that does not match `VERSION`.

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the contribution workflow.
