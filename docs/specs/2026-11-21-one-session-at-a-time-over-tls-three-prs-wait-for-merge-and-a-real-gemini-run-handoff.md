---
lane: session-routing
goal: |
  ① Goal: NeoSwitch switches one Claude Code session at a time to Gemini and never harms the user's Claude login. Whatever fails on NeoSwitch's side, Claude keeps working (R2). A sign-in persists across days, and a session that does not use NeoSwitch is told once that it can (R1).
  ② Done when: the three stacked PRs (#55 → #56 → #57) are merged and released, and a real Gemini session was run on them. That run covers on, a message answered by Gemini, a second window answered by Claude, off answered by Claude in place, and no connectors warning. Reboot and Linux keyring checks have passed. Proof: the merged commits plus Sen's real-run readings, and the open probes of §4 answered or judged.
  ③ Distance: phases implemented 3/3 · PRs merged 0/3 · real Gemini run 0/1 · reboot check 0/1 · Linux check 0/1 · open probes answered 0/2.
arc: 11-21, first session of this lane. It started as "add a remote sign-in by code" (#52, merged), then: account switch and cancel; a Stop hook that checks the relayed link (#52); session-scoped routing (#53, merged); the plan-lacks-model fix (#54, merged); and finally the R1/R2 design in docs/design-session-routing.md, implemented in three phases (#55, #56, #57, open, stacked).
baseline: docs/design-session-routing.md (Requirements R1/R2, measured facts F1–F12, Invariants I1–I5, "As built", "Rejected", "Open items")
supersedes: none (first handoff of this lane; the repo had no handoffs before)
next: ① Sen merges #55, then retargets #56 to main and merges it, then does the same for #57. ② Sen runs one real Gemini session (see §4, first step written there). ③ The next AI session probes whether Task subagents send a different x-claude-code-session-id (§4).
---

# One session at a time over TLS: three PRs wait for merge and a real Gemini run

## §0 Story

**In one sentence**: NeoSwitch no longer writes `ANTHROPIC_AUTH_TOKEN`. A project's sessions reach a local daemon over TLS. Only the sessions that ran `/neoswitch:on` get Gemini; every other request passes through unchanged to Anthropic with the user's own login. So connectors stay on, `/neoswitch:off` returns to Claude in place, and a port squatter gets nothing. All of this is implemented and tested in real Claude Code with a fake model side, in three stacked PRs that are not merged yet.

**Timeline (this session, 2026-11-21)**:
- **#52 (merged)**: `/neoswitch:login` shows a link plus a pasted code beside the browser sign-in. Further pieces:
  - account switch, and `/neoswitch:login cancel`;
  - a Stop hook that catches a garbled relayed link (haiku dropped `/o` once in 4 runs).
- **#53 (merged)**: routing lasts only while the sessions that asked for it run (SessionEnd detach, daemon release). It was prompted by Sen's question about returning to default after a restart.
- **#54 (merged)**: Sen's fornix session answered `502 … retrying`. The root cause was that the account's plan lacks `gemini-4-neon` (it offers gemini-3.8-flash, claude-sonnet-4-6, claude-opus-4-6-thinking and gpt-oss-120b-medium). Requests now get a clear unavailable message, and login keeps the project on Claude.
- **Design** (`docs/design-session-routing.md`): Sen's R1/R2. Twelve facts were measured with probe servers and pty-driven Claude Code (F1–F12). Reviews:
  - a blind design arm converged on passthrough on its own;
  - an adversarial arm (12 findings, all adopted) found that the squat risk exists today and that 403 tells users to run /login;
  - a second adversarial arm killed the transient-settings variant (F11 + dependence on F8).
- **Phase 1, #55**:
  - no 401/403 for NeoSwitch's own failures (400 with the fix, 529 for transient agy failures);
  - `/neoswitch:on` preflights agy (signed in + model offered);
  - a taken port moves;
  - hooks use `neoswitch.sh --cached` and never build.
- **Phase 2, #56**:
  - TLS with a local CA whose key is discarded;
  - a router keyed by `x-claude-code-session-id`;
  - passthrough via `httputil.ReverseProxy`;
  - persisted opt-ins, a session registry, the UserPromptSubmit self-heal, and a level-triggered sweep.

  The first two-window run looked green but was not: the matches came from typed input. The real cause was F10 (Claude Code refuses IP name constraints). That was fixed and re-run green.
- **Phase 3, #57**: a once-a-day start hint in the shell, gated on the `ready` and `declined` state files.

**Key decisions and why**:
- **Passthrough with the user's own login instead of keeping `ANTHROPIC_AUTH_TOKEN`**: it is the only option that keeps connectors and gives an in-session way back to Claude. The credential exposure is bounded by TLS with a CA key that is never written. Sen accepted this ("请按照你的方案继续") after the design reply named the credential-transit question as his call.
- **Settings stay while an opted-in session runs, not transient**: every open window applies a settings write at once (F11), so transience bought little. It would have made the opted-in route depend on F8, which looks like an implementation accident, and Claude Code had just moved from 2.1.329 to 2.1.330 that day.
- **No silent fallback to Claude when Gemini fails**: the user would believe Gemini answered while their Claude quota was spent. Instead the session gets an explicit 400 plus `/neoswitch:off` in place. Reopen condition: the user asks for automatic fallback.
- **`neoswitch on` outside a session refuses**: switching is per session. Terminal users get `neoswitch claude`.

## §1 Metrics

- **Engineering**: main..feat/start-hint has 4 commits, 43 files, +1886 / −378 (README and 9 translations included, 114 lines each, in sync).
- **Quality**:
  - `go test -race -count=1 ./...`: 6/6 packages ok, 177 `Test*` functions. `go vet` is clean on darwin, linux and windows.
  - Test daemons exit after the run (`pgrep -lf neoswitch.test` = 0 after 12 s).
- **Real-run evidence**: Claude Code 2.1.330, agy 1.5.13, macOS. Two-window pty run with the model side faked by a local OpenAI-shaped server that answers 400 `MODEL-SIDE-REACHED`:
  - window A after `/on` reached the model side (8 hits per message: retried 400s, F9);
  - window B was answered by real Claude via passthrough (MANGO);
  - A after `/off` was answered by real Claude (PAPAYA);
  - connectors warning: none;
  - settings: removed at `/off`, and the daemon stopped after exit.
- **Reviews this session**: blind design arm 1; adversarial arms 4 (on #52's design, on #53, on the R1/R2 draft, on the transient delta).

## §2 Current state

- **Open PRs, stacked**:
  - #55 `feat/safety-baseline` (ce65e3e) → main;
  - #56 `feat/session-routing` (a72618c) → feat/safety-baseline;
  - #57 `feat/start-hint` (170f9e2) → feat/session-routing.

  This handoff is committed on top of #57.
- **Merged today**: #52, #53, #54 (main = 56d48f1).
- **Not released**: VERSION is still 0.2.1. Releasing is `scripts/release.sh`, through the `release` skill.
- **Sen's machine**:
  - fornix still has old-format settings (`http://127.0.0.1:53366` + `ANTHROPIC_AUTH_TOKEN`), served by the old 0.2.1 daemon (pid 73415, started 17:14).
  - The new code cleans the old format (`removeLegacy`) when the project is next disabled, and EnsureState moves the state to TLS on a new port, so the old daemon can serve its sessions until they end.
  - agy is signed in, but the plan lacks `gemini-4-neon`.

## §3 Baseline cross-walk

| Baseline item (`docs/design-session-routing.md`) | Status |
|---|---|
| R1 sign-in persists; switching works tomorrow; start hint | still load-bearing. Persistence rests on F1 (keyring refresh over ~2.5 h), with no reboot test yet. The hint is in #57. |
| R2 never affect the user's Claude login; Claude keeps working whatever fails | still load-bearing. The two-window run covers the main paths. Not covered: subagent ids, env loss (§4). |
| I1 credential only over TLS that only NeoSwitch terminates, only to Anthropic | intact, with an **explicit pivot**: the name-constrained CA was replaced by a discarded CA key (F10). The doc was updated. |
| I2 no 401/403 for own failures; 400 permanent, 529 transient | intact (#55; the router's own errors are 400). Watch F9: Claude Code retries 400 about 8–10 times. |
| I3 a way back inside the session | intact (`/neoswitch:off` opts out → passthrough, verified in real Claude Code) |
| I4 sessions not using NeoSwitch pay almost nothing | intact (hooks decide in shell; the binary runs only for routed sessions, with `--cached` on per-prompt/end hooks) |
| I5 the daemon outlives sessions pointing at it | intact (registry + 30-minute grace for unplaced windows + persisted opt-ins) |
| "Transient project settings" (an earlier draft section) | explicit pivot: rejected, the reasons recorded in "Why the settings are not transient" |

## §4 To settle / next tasks

| Item | Disposition |
|---|---|
| Merge #55 → #56 → #57 | `⚑ Sen decides` (his merge). Default: merge in order, retargeting each next PR to main. Nothing waits on a clock. |
| Real Gemini run on the merged code | `Left for next round` (Sen's account needed). First step: `export GEMINI_MODEL=gemini-3.8-flash` (the plan lacks gemini-4-neon), start `claude` in a test project, run `/neoswitch:on`, send one message, open a second window and send one, run `/neoswitch:off` in the first and send one more. Expected: Gemini, then Claude, then Claude, and no connectors warning. |
| Reboot persistence (R1) | `Left for next round` (Sen). First step: reboot, open a project, check the start hint appears once, run `/neoswitch:on`, and confirm one answer without signing in again. |
| Linux keyring (locked Secret Service, no D-Bus) | `Judged negative by precondition`: no Linux host this session. Reopen when `ssh <linux host> agy --version` answers. |
| Do Task subagents send another `x-claude-code-session-id`? If so, they pass through to Claude mid-Gemini-turn. | `Left for next round` (AI). First step: in the pty harness (recipe in ⛔ below), point a test project at a probe server that records `x-claude-code-session-id` per request. Ask the session to "use the Task tool to answer 2+2", then compare the ids. If they differ, the fix is the registry: subagent requests carry the parent pid? Probe first. |
| A session that loses applied env while the settings are still in the file | `Judged negative by precondition`. Reopen when any probe shows an opted-in session's request missing at the daemon while `opt-ins.json` lists it (compare daemon request counts against the session's turns). |
| F9: Claude Code retries a 400 about 8–10 times, so "model not offered" opens up to 10 agy sessions per message | `Judged negative by value`: each retry reuses the running agy and closes its session at once; no prompt reaches Google. |
| Windows native support (5 blockers found: `Signal(0)`, hooks without `sh`, MINGW uname, the sh-only agy installer, no-op helper checks) | `Judged negative by precondition`. Reopen when a Windows machine with Claude Code + Git Bash is available to test on. WSL is presumed to work as Linux but is unverified. |
| #52 leftovers: a real successful sign-in by code and a real account switch | `Left for next round` (Sen's accounts). First step: on an SSH host, `/neoswitch:login`, open the link, `/neoswitch:login <code>`. To switch accounts, run `/neoswitch:login` again while signed in. |
| Release after merge | `Left for next round`. First step: the `release` skill (`scripts/release.sh`). The update notice will then reach plugin users. |
| fornix's old-format settings / old daemon | `Already wrapped up` by code (`removeLegacy`, a TLS state on a new port). Sen can run `/neoswitch:off` in fornix any time to drop it now. |

**⛔ Do not step on these again**

| Rule | Why | Applies when | Does not apply when | Source |
|---|---|---|---|---|
| Never `pkill` NeoSwitch daemons broadly. Find a test daemon by the port in the test state's `proxy.json` (`lsof -iTCP:<port> -sTCP:LISTEN -t`). | Sen's real fornix daemon runs on 53366 from the plugin cache binary. A broad pkill was rightly blocked by the safety check. | Cleaning up after any e2e run | – | this session |
| This Claude Code shell has `CLAUDE_CODE_USE_GATEWAY=1` and `CLAUDECODE=1`. Unset them (`env -u`) when running the binary or `claude` as a "user". | `on` took the gateway branch and never attached, which looked like a bug. | Any manual run of `/tmp/nsbin on` or a pty Claude Code | Go tests (TestMain unsets them) | this session |
| In e2e checks, never match words that also appear in your typed prompt. Ask for a transformed answer ("uppercase form of kiwi" → `KIWI`). | The first two-window run "found" alpha and pong in the echoed input while every request had failed with a TLS error | pty / transcript-based checks | – | this session |
| Claude Code refuses any CA with IP name constraints (`UNSUPPORTED_CONSTRAINT_TYPE`) | F10. Unit tests and Go clients do not notice; only a real Claude Code run does. | Changing `internal/daemon/tls.go` | – | design doc F10 |
| The installed 0.2.1 plugin's hooks fire in every project. Disable it per test project with `"enabledPlugins":{"neoswitch@neoswitch":false}`, and write hooks pointing at the repo's `plugin/*.sh`. | An e2e run attached to an old-version daemon started by the installed plugin's hook. | pty e2e with the repo's hooks | – | this session |
| Use `/bin/ps` and absolute paths. Avoid `cd` in shell commands. | Sen's shell aliases `ps` (it prints all processes) and `cd` prints `ls` | any shell use | – | this session |
| Go tests spawn the daemon from the test binary (`TestMain` dispatches `daemon`). Tests that switch a session need `inSession(t)` and the fake agy (`installAgy`). | `on` now preflights agy and starts a real daemon | new cmd tests | – | `cmd/neoswitch/project_test.go` |

**Pty e2e recipe** (works, about 3 minutes): a temp project with `.claude/skills/{on,off}` copied from `plugin/skills` (with `${CLAUDE_PLUGIN_ROOT}` replaced by the repo path) and `.claude/settings.json` holding:
- `enabledPlugins` false for neoswitch;
- `permissions.allow` `Bash(<repo>/plugin/neoswitch.sh *)`;
- SessionStart, UserPromptSubmit and SessionEnd hooks to `<repo>/plugin/*.sh "$PPID"`.

Run `claude --model haiku` under Python `pty.fork` with:
- `NEOSWITCH_STATE_DIR` set to a fresh 0700 dir;
- `NEOSWITCH_BACKEND=openai`, `OPENAI_API_KEY=test`, `OPENAI_MODEL=test`, `OPENAI_BASE_URL=http://127.0.0.1:<fake>/v1`;
- the Claude Code variables unset;
- the trust dialog accepted with Down plus Enter.

## §5 Roadmap

NeoSwitch has no `docs/roadmap.md`. The lane's single index line, to keep here until one exists:

`session-routing` — phases 1–3 implemented (#55/#56/#57, open, stacked) · next: merge, then a real Gemini run, a reboot check and the subagent-id probe · design: `docs/design-session-routing.md`.
