# Design: per-session routing that never takes Claude away

Status: proposed, 2026-11-21. It supersedes how routing works today: `ANTHROPIC_AUTH_TOKEN` in project settings, and a project that is either routed or not.

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
| F9 | In print mode Claude Code retries a 400 about 10 times within 2 s before it shows the message. A 400 must therefore be cheap to answer again. | Probe server count |

## Architecture

A routed project sends **all** of its Claude Code traffic to the local daemon over TLS. The daemon is a per-session switch:

- **Opted-in sessions** get Gemini. A session opts in with `/neoswitch:on`, or with `/neoswitch:login`, which ends by turning the project on.
- **Every other request** passes through byte for byte to Anthropic with the user's own credential. That includes other windows in the project, sessions left over after a crash, and sessions that ran `/neoswitch:off`.

Project settings while routed:

```
ANTHROPIC_BASE_URL       = https://127.0.0.1:<port>
ANTHROPIC_CUSTOM_HEADERS = X-NeoSwitch-Token: <token>
NODE_EXTRA_CA_CERTS      = <state>/ca-bundle.pem   (the user's own NODE_EXTRA_CA_CERTS contents + NeoSwitch's CA)
CLAUDE_CODE_USE_GATEWAY  = 0
```

`ANTHROPIC_AUTH_TOKEN` is no longer written. Claude Code keeps the user's login, and connectors stay on (F2).

### Invariants

- **I1. The user's Claude credential travels only over TLS that only NeoSwitch can terminate, and only to Anthropic.**
  - The CA key and server key are private to the user (0600, in a 0700 folder).
  - The CA is name-constrained to `IP:127.0.0.1`, so it cannot vouch for any other host.
  - The daemon never logs, stores or parses `Authorization` or `x-api-key`. It forwards them only to the Anthropic upstream recorded for the project.
  - A process squatting the port cannot complete TLS, so it gets nothing: not the credential, not the conversation, and no chance to answer with `tool_use`. Today, by contrast, it gets the conversation and the proxy token, and can make Claude Code run tools.
- **I2. NeoSwitch's own failures never look like a Claude login problem.**
  - It never answers 401 or 403 for its own reasons. A 401 could make Claude Code act on the user's OAuth.
  - A permanent failure answers **400** and names the way out ("run /neoswitch:off to continue on Claude in this session").
  - A transient failure answers 529 so that Claude Code retries it: agy exited, a handshake timed out after a cold start, or the upstream is overloaded.
  - Anthropic's own statuses pass through unchanged.
- **I3. Every failure has a way back inside the same session.** `/neoswitch:off` takes the session out of the opted-in set, and its next request goes to Claude (F7). Nothing waits for a restart.
- **I4. Sessions that do not use NeoSwitch pay almost nothing.**
  - Hooks on that path are shell only. They never build, download, start agy or the daemon, or touch the network.
  - Every hook exits 0 and has its own short timeout.
  - The Go binary runs only when its cached build is fresh.
- **I5. The daemon outlives every session whose env points at it.** Settings are removed as soon as no session in the project is opted in, so new sessions start clean. Sessions that already applied the env keep it, so the daemon serves them as passthrough until they exit.

### Components

1. **TLS and CA.** Generated once into the state folder. The server certificate covers `127.0.0.1`, and the CA carries a critical nameConstraints extension. The CA bundle is written at enable time:
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

#### Transient project settings (added after F8)

The project's settings exist only until the sessions that opted in have used them:

1. `/neoswitch:on` registers its session (`CLAUDE_CODE_SESSION_ID`) with the daemon as opted in for the project, and only then writes the settings. The order matters: a removal can then never run between the write and the registration.
2. The session applies the settings on its next request, usually the reply to `/neoswitch:on` itself, which reaches the daemon with that session id. The daemon marks the session as applied.
3. Once every opted-in session of the project has applied the settings, the daemon removes them (under the sessions lock).

Opted-in sessions keep the applied env (F8). Other windows and new sessions in the project start without it, and never touch NeoSwitch, the daemon, or a credential path. A window that made a request inside that short window applied the env too. It is not opted in, so it passes through to Anthropic and keeps answering with Claude.

The release when the last opted-in session ends stays as a backstop, for a session that opted in but never sent a request.

Passthrough is therefore a safety net, not the normal path. It covers:

- a bystander inside the window;
- leftover settings after a crash;
- a session that ran `/neoswitch:off`.

`/neoswitch:off` only opts the session out, so its next request passes through to Claude. Overwriting `ANTHROPIC_BASE_URL` would also return the session to Claude directly (F8), but it would revert every other opted-in session in the project on its next request. Settings are per project; only the daemon can tell sessions apart.

The daemon, not the session's env, says whether a session uses Gemini: `/neoswitch:status` and the hooks ask it by session id.

## Behaviour the user sees

| Situation | Today | With this design |
|---|---|---|
| Session in a routed project | connectors off | connectors on |
| Gemini fails permanently (signed out, model not offered) | session dead until `/neoswitch:off` + new session | 400 names the fix; `/neoswitch:off` continues on Claude **in this session** |
| agy restarts or crashes briefly | 403 "Please run /login" | 529, retried by Claude Code |
| Other windows in the project when one turns it on | switched too | stay on Claude (passthrough) |
| Crash or reboot leaves settings | next session routed, may fail | next session restarts the daemon and answers with Claude unless it opts in |
| Port squatted while the daemon is down | squatter gets the conversation and can run tools | TLS fails, so the squatter gets nothing |
| Tomorrow, new session | (after #53) Claude, no hint | Claude, with a one-line hint; `/neoswitch:on` without signing in again |

## Rejected

- **Today's `ANTHROPIC_AUTH_TOKEN`, hardened (fail-closed).**
  - The credential never transits NeoSwitch.
  - But connectors stay off while routed, and a failing session cannot get back to Claude without a restart.
  - Its squat exposure is worse than the TLS design's (I1).
- **Silent fallback to Claude when Gemini fails.** The user would believe Gemini answered while their Claude quota is spent. An explicit 400 plus a one-command in-session way back is honest and as quick. Reopen condition: the user asks for automatic fallback. It would then be a per-user setting with a Stop-hook notice on every fallback.
- **Launcher-only routing (`neoswitch claude`).** It leaves nothing behind, but it loses switching from inside a session, which is the product.
- **User-level `~/.claude/settings.json`.** One failure would reach every project.
- **A persistent launchd/systemd agent.** It outlives an uninstall and would then break Claude in every routed project.

## Sub-decisions and where they come from

| Sub-decision | Source |
|---|---|
| Keep the user's login; authenticate with a custom header | Derived this round (F2, F3) |
| TLS with a name-constrained CA | Derived this round (F4, plus the review's finding that the squat risk exists today) |
| Route by `x-claude-code-session-id` | Derived this round (F5) |
| 400 for permanent and 529 for transient failures; never 401 or 403 | Derived this round (F6, plus the review's transient/permanent split) |
| No silent fallback | Derived this round (blind design arm); the reopen condition is recorded |
| Settings released when no session is opted in | Inherited from #53, re-keyed |
| Preflight, port reselection, per-prompt self-heal, hint health | Adversarial review findings #1, #10, #6, #7 |
| The Anthropic upstream is the one in effect when the project was enabled | Default; needs the gateway-user probe in phase 2 |
| Transient project settings, removed once every opted-in session applied them | Derived this round (F8) |
| `/neoswitch:off` opts out (passthrough) rather than overwriting the base URL | Derived this round (F8: an overwrite reverts every session in the project) |
| Register the opt-in before writing settings | Derived this round (race between write, apply and removal) |

## Open items

- Whether Anthropic's terms are fine with passing the user's own Claude Code requests through a local proxy unchanged. Claude Code itself supports `ANTHROPIC_BASE_URL` gateways. This is the owner's call.
- A reboot test and a Linux test (Secret Service keyring locked, no D-Bus) for R1. Only F1's refresh across 2.5 hours has been verified.
- Whether Claude Code's TLS stack (Bun) enforces nameConstraints. Probe: have the CA issue a certificate for another name and confirm it is rejected.

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
