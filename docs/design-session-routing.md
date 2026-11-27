# Design: per-session routing that never takes Claude away

Status: phases 1 to 3 implemented, 2026-11-21. It supersedes how routing works today: `ANTHROPIC_AUTH_TOKEN` in project settings, and a project that is either routed or not.

## Requirements

- **R1.** A sign-in persists. Switching to NeoSwitch still works after a reboot without signing in again. A session opened without NeoSwitch is told at its start that it can switch to the Neon model.
- **R2.** NeoSwitch never affects the user's existing Claude login. Whatever goes wrong on NeoSwitch's side, the user's Claude keeps working.

## What was measured (Claude Code 2.1.329, agy 1.5.13, macOS)

| # | Fact | How |
|---|---|---|
| F1 | agy keeps its sign-in in the keyring with a refresh token. A sign-in made at 13:03 with an access token valid until 14:03 still opened sessions at 16:06 and 16:38. | agy log, ACP probes |
| F2 | With `ANTHROPIC_BASE_URL` set and no `ANTHROPIC_AUTH_TOKEN`, Claude Code sends the user's own claude.ai OAuth Bearer (beta `oauth-2025-04-20`) to that URL. The "claude.ai connectors are disabled" warning does not appear. | Local probe server that recorded header names and auth length only |
| F3 | `ANTHROPIC_CUSTOM_HEADERS` from project settings is sent with every request. | Same probe |
| F4 | `NODE_EXTRA_CA_CERTS` from project settings is honoured for an `https://127.0.0.1` base URL, including when the settings are written mid-session. Without it, TLS verification fails. | Local TLS server with a throwaway CA; interactive session switched mid-way |
| F5 | Every request carries `x-claude-code-session-id`. It equals the hooks' `session_id`, the skills' `${CLAUDE_SESSION_ID}`, and `CLAUDE_CODE_SESSION_ID` in a skill's `!` command. | Interactive probe |
| F6 | How Claude Code shows an error status: **400** `invalid_request_error` → "API Error: 400 <message>", not retried. **403** → "Please run /login · …". **404** → "the selected model may not exist". **5xx** → retried with backoff for over 40 s without showing the message. | Probe server per status |
| F7 | A skill's `!` command runs before the model turn, so `/neoswitch:off` takes effect even when the session's route fails. | Observed (fornix) |
| F8 | A running session keeps project env it applied after the entries are removed, applies a changed value on its next request, and goes straight back to Anthropic when `ANTHROPIC_BASE_URL` is overwritten with `https://api.anthropic.com`. | Interactive TLS probe: write, remove, change, overwrite |
| F9 | Claude Code retries a 400 about 8 to 10 times within seconds, in print mode and interactively, before it shows the message. A 400 must therefore be cheap to answer again. | Probe server count; two-window test |
| F10 | Claude Code's TLS stack refuses every certificate under a CA that has name constraints on IP addresses (`UNSUPPORTED_CONSTRAINT_TYPE`), whether or not the address fits. NeoSwitch's CA therefore carries none; its key is discarded after signing the server certificate instead. | Two-window test, Claude Code 2.1.330 |
| F11 | Every open Claude Code window of a project applies a settings change at once, through its file watcher, not only on its next request. | Adversarial review of the transient design (Claude Code source) |
| F12 | Two real windows in one project: the one that ran `/neoswitch:on` reached the model side; the other, sharing the settings, was answered by Claude through passthrough with the user's own login; after `/neoswitch:off` the first window was answered by Claude in the same session; no connectors warning appeared; the settings were removed at `/neoswitch:off`, and the daemon stopped after both exited. | Two-window test, Claude Code 2.1.330, fake model side |
| F13 | A Task subagent's requests, including one running in the background, carry its parent's `x-claude-code-session-id`, plus an `x-claude-code-agent-id` header, and `cc_is_subagent=true` in the system prompt's billing line. So a subagent of an opted-in session is routed with it to Gemini. | Recording passthrough probe, `claude -p` with a Task subagent, Claude Code 2.1.330, 2026-11-21 |

## Architecture

A routed project sends **all** of its Claude Code traffic to the local daemon over TLS. The daemon is a per-session switch:

- **Opted-in sessions** get Gemini. A session opts in with `/neoswitch:on`, or with `/neoswitch:login`, which ends by turning the project on.
- **Every other request** passes through byte for byte to Anthropic with the user's own credential. That includes other windows in the project, sessions left over after a crash, and sessions that ran `/neoswitch:off`.

Project settings while routed:

```
ANTHROPIC_BASE_URL       = https://127.0.0.1:<port>
ANTHROPIC_CUSTOM_HEADERS = X-NeoSwitch-Token: <token>
NODE_EXTRA_CA_CERTS      = <state>/tls/bundle.pem   (the user's own NODE_EXTRA_CA_CERTS contents + NeoSwitch's CA)
CLAUDE_CODE_GATEWAY_HINT_HEADERS = 1               (only when the project does not set it already)
```

`CLAUDE_CODE_GATEWAY_HINT_HEADERS` makes Claude Code send `x-claude-code-request-class`, `x-claude-1p-request-class` and `x-claude-code-agent-id` with every request. Without it, Claude Code sends them only to its own API, depending on a remote flag. The Antigravity route keeps an ACP session per sender (see [antigravity.md](antigravity.md)), so background requests that share the main thread's history do not take its session. Claude Code reads the variable whenever it builds a request's client, so a running session picks it up on its next request (F8). Passed-through requests carry the headers too. Claude Code sends the same headers when it talks to Anthropic's API directly, and the variable exists for gateways. Disable removes a `1` the same way it removes `CLAUDE_CODE_USE_GATEWAY=0`.

`CLAUDE_CODE_USE_GATEWAY` is not written. A gateway-mode session (for example `everyapi use claude`) takes `ANTHROPIC_BASE_URL` from its process environment, so it keeps its gateway; `/neoswitch:on` refuses such sessions, and forcing them through the daemon would pass the gateway's credential on to an upstream that does not accept it. The SessionStart hook does not attach them either.

`ANTHROPIC_AUTH_TOKEN` is no longer written. Claude Code keeps the user's login, and connectors stay on (F2).

### Invariants

- **I1. The user's Claude credential travels only over TLS that only NeoSwitch can terminate, and only to Anthropic.**
  - The CA key and server key are private to the user (0600, in a 0700 folder).
  - The CA's private key is discarded once it has signed the server certificate, so it cannot vouch for any other host. (Name constraints were the first plan; Claude Code refuses them, F10.)
  - The daemon never logs, stores or parses `Authorization` or `x-api-key`. It forwards them only to the Anthropic upstream recorded for the project.
  - A process squatting the port cannot complete TLS, so it gets nothing: not the credential, not the conversation, and no chance to answer with `tool_use`. Today, by contrast, it gets the conversation and the proxy token, and can make Claude Code run tools.
- **I2. NeoSwitch's own failures never look like a Claude login problem.**
  - It never answers 401 or 403 for its own reasons. A 401 could make Claude Code act on the user's OAuth.
  - A permanent failure answers **400** and names the way out ("run /neoswitch:off to continue on Claude in this session").
  - A transient failure answers 529 so that Claude Code retries it: agy exited, a handshake timed out after a cold start, or the upstream is overloaded.
  - Anthropic's own statuses pass through unchanged.
- **I3. Every failure has a way back inside the same session.** `/neoswitch:off` takes the session out of the opted-in set, and its next request goes to Claude (F7). Nothing waits for a restart.
  - **The way back does not depend on what failed.** When this plugin's NeoSwitch cannot be built or downloaded, `neoswitch.sh` runs the latest NeoSwitch in the cache, so `/neoswitch:off` still works after a failed update. With none cached, `/neoswitch:off` names the entries to delete from `.claude/settings.local.json` by hand: only a NeoSwitch that ran can have written them. A message whose way back may fail too (NeoSwitch could not start at all) ends with the README's removal steps.
  - **Turning NeoSwitch off leaves the settings as the user had them.** Enable writes over `NODE_EXTRA_CA_CERTS` and `ANTHROPIC_CUSTOM_HEADERS` (the bundle holds the user's CA certificates, and the headers keep the user's lines), so it first keeps the user's own values from the project's settings in `<state>/replaced/`, or changes nothing if it cannot; Disable puts them back. Before 0.4.10 Disable deleted them, losing, for one, a company proxy's CA file.
  - **No failure decides for the user.** A failure never switches a project or session back to Claude by itself, since a user may have no Claude account and use only Gemini; the message names the cause, the fix, and `/neoswitch:off` as the way to Claude, which the user chooses. The one exception predates this rule: when another program took NeoSwitch's port, the project's settings are removed, since they would send its sessions to that program.
- **I4. Sessions that do not use NeoSwitch pay almost nothing.**
  - Hooks on that path are shell only. They never build, download, start agy or the daemon, or touch the network.
  - Every hook exits 0 and has its own short timeout.
  - The Go binary runs only when its cached build is fresh.
- **I5. The daemon outlives every session whose env points at it.** Settings are removed as soon as no session in the project is opted in, so new sessions start clean. Sessions that already applied the env keep it, so the daemon serves them as passthrough until they exit.

### Components

1. **TLS and CA.** Generated once into the state folder. The server certificate covers `127.0.0.1` and lasts 397 days; the CA's key is never written (F10). The CA bundle is written at enable time:
   - If the user's environment already has `NODE_EXTRA_CA_CERTS`, for example a corporate proxy CA, its contents are kept in the bundle. Without that, the project setting would replace their CA and break their Claude.
   - Hooks and the CLI talk to the daemon through the same CA, so none of them sends the token to an impostor.
2. **Request router (daemon).**
   - Checks `Host` is `127.0.0.1:<port>`, which blocks DNS rebinding, and checks `X-NeoSwitch-Token`. A failed check is a 400 with a message, never a 401.
   - Strips `X-NeoSwitch-Token`, then routes on `x-claude-code-session-id`. An opted-in session goes to Gemini (today's translation path). Anything else goes to `httputil.ReverseProxy` → the project's Anthropic upstream, streaming flushed at once.
   - The upstream is `https://api.anthropic.com` unless the session that enabled the project had a non-NeoSwitch `ANTHROPIC_BASE_URL` (a user gateway). That URL is recorded per project at enable time.
   - Users on Bedrock or Vertex (`CLAUDE_CODE_USE_BEDROCK`/`_VERTEX`) are refused at enable time, because Claude Code does not use the base URL for them.
3. **Session registry.**
   - SessionStart, in every session, records `session_id → pid` in the state folder with one shell write. It records nothing else.
   - The daemon's liveness set is every session that sent it a request: header id → registry → pid. It also includes the attached sessions.
   - The daemon exits when none of them is alive. Records of dead pids are pruned.
4. **Opt-in set.**
   - `/neoswitch:on` and `/neoswitch:login` pass `${CLAUDE_SESSION_ID}` (F5). The daemon keeps the opted-in ids per project.
   - `/neoswitch:off` removes the current session from that set, and its next request goes to Claude. When the project has no opted-in session left, NeoSwitch removes the project's settings. The daemon also removes them when the last opted-in session ends (the #53 release path, now keyed by opt-in rather than attach).
5. **Preflight.** `/neoswitch:on` checks before writing any settings:
   - that agy is signed in and the plan offers the model (`SignedIn` returns both);
   - that the port is free or ours.

   It refuses otherwise, so a session is never switched to a route that cannot answer. A busy port after a reboot gets a new port, which is safe because no settings point at the old one once the project is released.
6. **Self-heal before each prompt.**
   - A UserPromptSubmit hook runs only when the session's `ANTHROPIC_BASE_URL` is NeoSwitch's.
   - It checks the daemon over TLS and restarts it if it died. It never builds, prints nothing on success, and finishes within a short timeout.
   - This covers a daemon that crashed mid-session and windows that applied the env without attaching.
7. **Start hint (R1).**
   - In a session that is not routed, SessionStart prints one line if all of these hold:
     - the last backend check recorded in state is "ok", written by login, on, or the daemon after its first answer;
     - the project was not turned off explicitly;
     - the hint was not shown for this project today.
   - The line: "NeoSwitch: set up already. /neoswitch:on answers with Gemini 4 Neon in this session; /neoswitch:off returns to Claude any time."
   - It never starts agy. A recorded failure (signed out, model not offered) is shown once, instead of the hint, with its fix.
   - `NEOSWITCH_NO_HINT=1` turns it off.
8. **Leftovers.**
   - Settings left after a power loss point at the daemon, whose port the next session restarts. A session that is not opted in then passes through, so it answers with Claude, not with an error.
   - Any NeoSwitch run also sweeps recorded projects with no opted-in live session and removes their settings.
   - Uninstalling the plugin without `/neoswitch:off` remains a documented gap: no hook fires on uninstall.

### Why the settings are not transient

A draft had the project settings removed as soon as every opted-in session had used them, so other windows would never touch NeoSwitch. Its review rejected it:

- every open window applies a settings write at once (F11), so the other windows are affected anyway;
- the opted-in session's route would then rest on F8, which looks like an accident of Claude Code's implementation (`Object.assign` into the environment that never deletes), while 2.1.330 already carries machinery that rebuilds child environments; if it changed, every opted-in session would fall back to Claude without a word.

So the settings stay while an opted-in session of the project runs. What is kept from the draft: `/neoswitch:off` opts out instead of overwriting the base URL (an overwrite would revert every session of the project), the opt-in is registered before the settings are written, and the daemon is the source of truth for whether a session uses Gemini.

### As built (phase 2)

- **Opt-ins** are keyed by session id and kept with the process id in `<state>/opt-ins.json`, together with the projects whose settings point at the daemon, so a daemon started again (by the UserPromptSubmit hook after a crash) keeps every session where it was.
- **Session registry.** The SessionStart and UserPromptSubmit hooks write `<state>/sessions/<session id>` with the process id, only where the state folder exists. The daemon uses it to move an opt-in to the new id `/clear` gives the same process, and to keep running for windows that applied the settings without attaching (F11). A window it cannot place keeps it running for 30 minutes after its last request.
- **Release.** A project's settings go when its last opted-in session opts out or ends: at once through SessionEnd, within 10 s through the daemon's check for a crash, and on SIGTERM. The same check removes settings that point at the daemon with nobody opted in, so leftovers and stale rewrites do not survive. A session that merely started while the settings existed neither keeps nor removes them. Release and registration take one lock, synchronously.
- **Hooks** decide from the session's env, the project's settings file, or the opt-ins file, not from the env alone (a window that applied the settings later may not show them to a hook).
- **One model per daemon.** The daemon answers every switched session with the model it started with, and reports it; `/neoswitch:on` checks the account offers that model, not the session's own `GEMINI_MODEL`, and says when the two differ.
- **The daemon starts without a model backend**: sessions passing through to Claude need it whatever happened on the model side.
- **Updates.** Running sessions are pinned to the daemon's port, its certificate and its token, which are all on disk, so a newer daemon can serve them on the same port. `internal/daemon/handover.go` does it:
  - **Trigger.** An `attach` (session start, or the hook before a prompt in a window whose own binary is gone from the cache) that finds a daemon of an *earlier release* serving the same backend and model starts `neoswitch daemon --take-over` and returns at once. `/neoswitch:on` does it for another build, backend or model too (`--explicit`), since the user asked for this setup. A later daemon is never replaced, so two plugin versions open at once cannot take the port back and forth, and a window's own `GEMINI_MODEL` never switches the others without `/neoswitch:on`.
  - **Handover.** The successor takes a lock in the state folder, asks the running daemon to hand over with a key from the state folder (the proxy token is in every switched project's settings, where project code can read it), and listens as soon as the port frees. The predecessor freezes first: it answers attach, opt-out and detach with 503 (the clients retry and reach the successor), saves and releases nothing, and gives the successor the sessions it knew only in memory. Then it closes its listener and lets the replies in progress finish, for up to 10 minutes, without running `sessions.stop()`, which would turn every project back to Claude.
  - **Failure.** If nothing answers on the port 5 s after the handover, the predecessor listens again, unfreezes, and records the version, which no window tries again for 6 hours. While the lock is held, an attach that finds the port free waits for the successor instead of starting a daemon of its own.
  - **Cost.** Every Gemini conversation reads itself again on its next reply (antigravity.md, "Sessions"). The session that started the takeover is told that the windows are being handed over (the takeover finishes after the hook returns); the others see a slower reply once.
  - **Notes.** A note offers `/neoswitch:on` only where it would move the windows; over a daemon of a later release, on Windows, or when an earlier daemon could not be stopped, it says to close the windows of projects that use NeoSwitch.
  - **Daemons up to 0.4.4** cannot hand over. On a passive attach the note tells the user to run `/neoswitch:on`, which stops such a daemon with SIGKILL (never SIGTERM, for the reason above) after checking that it is a process named neoswitch listening on the port, and starts the update. A reply in progress in another window is cut off, which the note says.
  - **Windows** does not take part until a takeover is tested there (`HandoverSupported`): `detach` is a no-op, so a hook timeout may take the successor with it, and the port may not be free while the predecessor drains. Reopen when a takeover test runs on `windows-latest` in CI.
  - **Why automatic.** On 2026-11-23 a v0.4.0 daemon served every window for a day after the plugin updated to v0.4.4, so a security fix (the tool guard of 0.4.3) did not reach the sessions using Gemini. The note asking to close every window had reached the user, and the model had relayed it; closing every window was simply too costly to do. A note alone repeats that.
  - **Cache.** `neoswitch.sh` keeps the binaries of later releases when it prunes: a window of an earlier plugin rebuilds its binary on `/clear` or resume, and deleting the update then would put the hooks of every window back on the earlier release. Plugins up to 0.4.4 still ship the old prune, so the protection holds from 0.4.5 on. `--cached` runs the window's own binary, or else the latest release cached.
- **Auto mode** (updated for Claude Code 2.1.332, 2026-11-26). In default Auto Mode, Claude Code sends `safeguards: [{"type": "dangerous_tool_use", "classifier_context": {...}}]` on `POST /v1/messages` and reads `safeguard_results` on the response (`message_delta.delta` when streaming). NeoSwitch evaluates those tool calls natively (`internal/server/safeguard.go`, `internal/provider/acp_safeguard.go`) so Auto Mode works out of the box when switching with `/neoswitch:on`, without restarting Claude Code or spending Claude usage:
  - **Backend-local classifier.** Before finishing a turn that produced `tool_use` blocks, NeoSwitch runs an isolated, tool-free classifier pass over the bounded recent transcript (with untrusted `tool_result` fencing) and the whitelisted `classifier_context`. On Antigravity ACP it opens an ephemeral session (closed immediately, never occupying the idle session pool) on `NEOSWITCH_SAFEGUARD_MODEL` → background model (`gemini-3.8-flash`) → main model with low thinking; other backends use their own `Generate` method with JSON output. Turns with no `tool_use` blocks (and `WebSearch` turns) return `available` with empty `tool_uses` at zero latency. While judging a streaming turn, NeoSwitch sends SSE `ping` keepalives.
  - **Fail closed, never rubber-stamp.** Tool call IDs are mapped to short ordinal keys (`"0"`, `"1"`, ...) in the classifier prompt so long `toolu_ns_` IDs are never truncated; verdicts require an explicit boolean `flagged` field. Any timeout (20 s), backend error, tool-use attempt by the classifier, unreadable JSON, or missing verdict returns `status: {"type": "unavailable", "reason": "timeout"|"error"}`, which Claude Code treats fail-closed without disabling `safeguards` on later turns.
  - **Opt-out & client-side fallback (`internal/daemon/autocheck.go`).** Setting `NEOSWITCH_SAFEGUARD=off` disables NeoSwitch's classifier and omits `safeguard_results`; in that state, the UserPromptSubmit hook shows a one-time per-session `systemMessage` (marked in `<state>/auto-mode-noted/`, kept 7 days) unless `CLAUDE_CODE_AUTO_MODE_SERVER=0` is set. Users who explicitly start Claude Code with `CLAUDE_CODE_AUTO_MODE_SERVER=0` keep the client-side passthrough path: `autocheck.go` recognizes Claude Code's auxiliary check by shape (`x-claude-code-request-class: auxiliary`, no tools, `<transcript>\n` … `</transcript>\n`) and passes it through to Anthropic with the user's credential (falling back to Gemini only if Anthropic does not return 2xx).
- **Upgrade from before TLS.** A state from before TLS moves to TLS on a new port, keeping its token, so a daemon of the earlier version can serve its sessions until they end; `Disable` also removes the earlier plain-HTTP entries. A session counts as routed the earlier way only when it also carries NeoSwitch's token: other local proxies, `neoswitch claude` and `neoswitch serve` name plain-HTTP loopback addresses too, and their sessions must neither start NeoSwitch nor be told they cannot answer.

Open, with reopen conditions:
- ~~Requests of subagents may carry another session id~~: a Task subagent sends its parent's id, also in the background (F13). Reopen when a Claude Code release adds a request kind that runs under its own session id; the probe is a recording passthrough at `ANTHROPIC_BASE_URL` comparing `x-claude-code-session-id` across requests.
- A session that loses the applied env while its settings are still in the file is not detected. Reopen when a probe shows it happening on any Claude Code version.

## Behaviour the user sees

| Situation | Today | With this design |
|---|---|---|
| Session in a routed project | connectors off | connectors on |
| Gemini fails permanently (signed out, model not offered) | session dead until `/neoswitch:off` + new session | 400 names the fix; `/neoswitch:off` continues on Claude **in this session** |
| agy restarts or crashes briefly | 403 "Please run /login" | 529, retried by Claude Code |
| Other windows in the project when one turns it on | switched too | stay on Claude (passthrough) |
| Crash or reboot leaves settings | next session routed, may fail | next session restarts the daemon and answers with Claude unless it opts in |
| Port squatted while the daemon is down | squatter gets the conversation and can run tools | TLS fails, so the squatter gets nothing |
| Plugin updated while windows are open | the earlier daemon serves every window until all of them close | the next session moves every open window to the update; replies in progress finish (macOS, Linux) |
| Auto mode in a Gemini session | every action stops to ask (`server_no_result`), or needs restart with `CLAUDE_CODE_AUTO_MODE_SERVER=0` | works out of the box via NeoSwitch's built-in fail-closed safety classifier (no Claude usage, no restart); `CLAUDE_CODE_AUTO_MODE_SERVER=0` remains available for Claude-judged checks |
| Tomorrow, new session | (after #53) Claude, no hint | Claude, with a one-line hint; `/neoswitch:on` without signing in again |

## Rejected

- **Today's `ANTHROPIC_AUTH_TOKEN`, hardened (fail-closed).**
  - The credential never transits NeoSwitch.
  - But connectors stay off while routed, and a failing session cannot get back to Claude without a restart.
  - Its squat exposure is worse than the TLS design's (I1).
- **Silent fallback to Claude when Gemini fails.** The user would believe Gemini answered while their Claude quota is spent. An explicit 400 plus a one-command in-session way back is honest and as quick. Reopen condition: the user asks for automatic fallback. It would then be a per-user setting with a Stop-hook notice on every fallback.
- **Synthesizing Claude OAuth classifier calls from NeoSwitch.** Using the user's Claude login to call Haiku behind the scenes would violate Invariant I1 (never storing or replaying Claude credentials), silently spend Claude quota in default Auto Mode, and break when Claude quota is exhausted or under `neoswitch serve`. NeoSwitch instead evaluates `safeguards` natively on the configured backend (using `gemini-3.8-flash` on Antigravity) with a strict fail-closed JSON contract.
- **Recognizing the check by its model.** The check uses the session's main model unless another is configured, and a Gemini session's subagents may name other Claude models, which would send them to Claude against the opt-in.
- **Writing `CLAUDE_CODE_AUTO_MODE_SERVER=0` into the project settings.** It would arrive too late for the session that ran `/neoswitch:on` (read once at launch), and it would take the server check away from the Claude sessions of the project, or auto mode altogether where Anthropic turned the local check off.
- **Launcher-only routing (`neoswitch claude`).** It leaves nothing behind, but it loses switching from inside a session, which is the product.
- **User-level `~/.claude/settings.json`.** One failure would reach every project.
- **A persistent launchd/systemd agent.** It outlives an uninstall and would then break Claude in every routed project.

## Sub-decisions and where they come from

| Sub-decision | Source |
|---|---|
| Keep the user's login; authenticate with a custom header | Derived this round (F2, F3) |
| TLS with a local CA whose key is discarded | Derived this round (F4, F10, plus the review's finding that the squat risk exists today) |
| Route by `x-claude-code-session-id` | Derived this round (F5) |
| 400 for permanent and 529 for transient failures; never 401 or 403 | Derived this round (F6, plus the review's transient/permanent split) |
| No silent fallback | Derived this round (blind design arm); the reopen condition is recorded |
| Settings released when no session is opted in | Inherited from #53, re-keyed |
| Preflight, port reselection, per-prompt self-heal, hint health | Adversarial review findings #1, #10, #6, #7 |
| The Anthropic upstream is the one in effect when the project was enabled | Default; needs the gateway-user probe in phase 2 |
| Settings kept while an opted-in session runs (not transient) | Review of the transient draft (F11, and the dependence on F8) |
| Registry of session id → process; opt-in moves with the process across `/clear` | Review of the transient draft |
| No name constraints on the CA; its key discarded instead | Measured (F10) |
| `/neoswitch:off` opts out (passthrough) rather than overwriting the base URL | Derived this round (F8: an overwrite reverts every session in the project) |
| Native fail-closed `safeguard_results` evaluator on the active backend (`gemini-3.8-flash` on ACP), keeping `CLAUDE_CODE_AUTO_MODE_SERVER=0` passthrough as opt-in | 4-mode live probe on Claude Code 2.1.332 + dual-arm review (2026-11-26) |
| Register the opt-in before writing settings | Derived this round (race between write, apply and removal) |

## Open items

- Whether Anthropic's terms are fine with passing the user's own Claude Code requests through a local proxy unchanged. Claude Code itself supports `ANTHROPIC_BASE_URL` gateways. This is the owner's call.
- A reboot test and a Linux test (Secret Service keyring locked, no D-Bus) for R1. Only F1's refresh across 2.5 hours has been verified.
- ~~Whether Claude Code's TLS stack enforces nameConstraints~~: it refuses IP name constraints outright (F10).

## Phases

1. **Safety baseline**, independent of passthrough. Ships first.
   - The error statuses of I2: 400/529, no 401 or 403. This also fixes #54's 403.
   - The preflight in `/neoswitch:on`, and port reselection.
   - Hooks only run the binary when its build is fresh.
2. **Architecture.**
   - TLS and CA, the custom-header token, the router with passthrough, the registry, the opt-in set, and in-session `/neoswitch:off`.
   - Settings lifecycle: #53, re-keyed by opt-in.
   - The sweep.
3. **R1 hint.** Health recording, the hint and its opt-out, and the README with its 9 translations.
