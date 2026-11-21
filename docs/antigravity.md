# Antigravity account sessions

> [!WARNING]
> The [Google Antigravity Terms of Service](https://antigravity.google/terms) (section 6) treat accessing Antigravity through third-party software, tools or services as a breach and say Google may suspend or terminate the Antigravity accounts involved. Routing Claude Code through Antigravity OAuth falls under that rule. Use this route only if you accept that risk; otherwise use a Gemini API key or Vertex AI.

NeoSwitch does not read, export, or replay the OAuth/token profile used by Antigravity. The profile belongs to the official `agy` client and is stored through the platform's secure credential layer.

The safe diagnostic path is:

```bash
neoswitch antigravity probe
```

To start or switch the account session, run:

```bash
neoswitch antigravity login
```

This delegates ACP's protocol-driven authentication flow to the official `agy` CLI and keeps its credentials in the platform keyring. When `agy` is missing and `ANTIGRAVITY_CLI_PATH` is unset, the command first runs Google's official installer (`https://antigravity.google/cli/install.sh`, the one on Google's download page). NeoSwitch downloads the script whole over HTTPS only (redirects included) and refuses anything that does not look like a shell script. It runs the script with `bash` from a private temporary directory, without stdin or a terminal, and with only the variables an installer needs (home, shell, `PATH`, locale, proxy and CA-certificate settings), so no API key or token reaches it. Only one install runs at a time. A run that takes longer than 10 minutes, or that you interrupt, is stopped together with its child processes, and an `agy` it left half-installed is removed. The new `agy` must answer `agy --version` before NeoSwitch uses it. The script puts `agy` in `~/.local/bin`, checking the download against the SHA-512 in Google's release manifest (a guard against corrupted downloads; the trust in the download itself rests on HTTPS to Google's servers), and adds that folder to your shell profiles. The command's output names the profile files that changed. NeoSwitch also looks for `agy` in `~/.local/bin` when it is not on `PATH`, because a Claude Code started before the install does not see the profile change. The command does not read or print the resulting token, or open the interactive `agy` TUI. `agy` owns the browser callback. `neoswitch antigravity oauth` is accepted as an alias.

To use the authenticated Neon account from the Claude Code plugin, run `/neoswitch:login` (it also shows a link, and `/neoswitch:login <code>` finishes the sign-in on any device; see below). The plugin signs in through `agy acp`, enables the current project's route, and starts using the `gemini-4-neon` session model after the next Claude Code session. OAuth credentials remain inside `agy` and are never read by NeoSwitch. Signing in also saves Antigravity OAuth as NeoSwitch's backend, so a Gemini key in the environment does not override it. To use a Gemini API key afterwards, export `NEOSWITCH_BACKEND=gemini` along with `GEMINI_API_KEY`, or run `neoswitch backend gemini` with the standalone CLI; see [cli.md](cli.md#gemini-api-key).

**Two sign-ins at once.** Like Claude Code's and Codex's own logins, the command runs two of `agy`'s sign-ins side by side and takes whichever finishes first. `agy`'s browser sign-in opens Google in a browser on this computer and finishes on its own through a local callback. `agy`'s sign-in by code prints a Google link that works in a browser on any device, whose page then shows a code to paste back. The link is always shown, so a browser that opens where you cannot see it, or not at all, never strands the sign-in. The browser sign-in is left out when the session came in over SSH (`SSH_CONNECTION`, `SSH_CLIENT` or `SSH_TTY` is set) or, on Linux and BSD, when no desktop is running (neither `DISPLAY` nor `WAYLAND_DISPLAY`), so no browser pops up on a screen you cannot see, possibly in front of someone else. The sign-in by code is ACP's terminal authentication: NeoSwitch takes the method and its arguments from `agy`'s handshake, so an `agy` too old to offer it is reported instead of guessed at.

In a terminal, you paste the code into the command. A slash command cannot wait for input, so `/neoswitch:login` splits it in two. It starts a detached helper (`neoswitch antigravity sign-in-helper`, logging to `sign-in.log` in NeoSwitch's state folder) that runs both sign-ins, and shows the link, which is also saved in `sign-in-link.txt` there. Finishing in the browser needs nothing more: the helper saves the backend and turns the project on for the Claude Code session that ran `/neoswitch:login`, so its next message uses Gemini. If that session has closed meanwhile, the sign-in stands but the project stays on Claude, since routing lasts only as long as the sessions that asked for it. Otherwise `/neoswitch:login <code>` hands the code to the helper, and on success saves the backend and turns the project on. The helper stops the browser sign-in, and waits for its `agy` to exit, before it passes on a code, so only one of the two ever stores a sign-in. The code has to reach the `agy` process that printed the link, because that process holds the sign-in's PKCE verifier; the helper takes it over a Unix socket (`$XDG_RUNTIME_DIR/neoswitch-sign-in.sock` on Linux, `sign-in.sock` in the state folder elsewhere), and NeoSwitch refuses a folder that another user owns or can enter, since whoever can reach the socket could sign your `agy` in to their own account. The code alone is useless without that verifier, so its passing through the command line and the conversation is harmless. A sign-in waits 10 minutes and only until a newer `/neoswitch:login` replaces it; Google takes each code once, so NeoSwitch refuses an argument that cannot be a code, such as a plain word, instead of spending the exchange on it. `/neoswitch:off` for the project tells a waiting helper not to turn the project on. Paste only the code: Claude Code refuses to run a slash command whose argument holds the full page address with its `&`.

**Signing in again, or with another Google account.** NeoSwitch first checks whether `agy` is signed in, by opening a session, which `agy` refuses with ACP's auth-required error when signed out. When it is, the command turns the project on with the current sign-in at once, and starts only the sign-in by code beside it: `agy`'s browser sign-in would just return the current sign-in, and leave the link dead. Opening the link and running `/neoswitch:login <code>` signs in again, such as with another Google account; until then the current sign-in stays, and `/neoswitch:login cancel` (in a terminal, Enter alone) keeps it and drops the link. Every finished sign-in is recorded in `signed-in` in the state folder, and the daemon replaces an `agy` started before it, once the requests still using that `agy` end, because an `agy` already running may keep the account it started with.

**Checking the relayed link.** Claude Code gives a slash command no way to show the user text that can be clicked, other than the model's reply, so the model retypes the 700-character link and can change a character of it. The plugin's Stop hook (`plugin/stop.sh`, which starts NeoSwitch only for a reply that mentions `accounts.google.com`) compares a reply that shows the sign-in link with the link waiting for its code. A changed link sends the reply back to the model with the exact one, which the user also sees in the hook's notice; if the retyped link is wrong again, the exact link stays in a notice.

While a sign-in waits for its code, a session already routed to a signed-out `agy` cannot show the link, because the model that would relay it is that `agy`. Its error message then carries the link and the step that finishes the sign-in. After a sign-in, the next request starts a new `agy`, which uses the new sign-in.

The probe launches the official CLI in headless JSON mode with a fixed, non-sensitive prompt. It captures the result in memory, checks for an exact success response, and prints only whether the session is usable. It never prints `stdout`, `stderr`, an account email, a token, or usage details.

The ACP route uses `agy` as the model-side agent process. NeoSwitch sends the translated Claude request through an ACP session, selects `gemini-4-neon` (or `GEMINI_MODEL`), and translates the returned text or Claude tool call back to the local Anthropic-compatible proxy. OAuth credentials remain owned by `agy`. `agy` offers each account the models of its plan, listed in every new session. When the plan lacks the model, NeoSwitch answers with a message that says so and names the models the plan has, which Claude Code shows as it is instead of retrying; `/neoswitch:login` checks the same list and then leaves the project on Claude.

`agy` is a full coding agent with its own file, shell and web tools. NeoSwitch starts it without `--dangerously-skip-permissions` and rejects every tool permission request it sends, so those tools never run; tools run only in Claude Code, under its permission rules.

Images, including screenshots returned by Claude Code tools, are sent as ACP image blocks when `agy` advertises image prompts. PDFs and other attachments cannot travel over ACP; the prompt marks each one as omitted so the model knows it was there.

One `agy acp` process serves the daemon, and every request runs in its own ACP session. `agy` runs sessions concurrently, so subagents, background requests and several Claude Code sessions do not wait for each other. Text answers stream to Claude Code as `agy` generates them; tool calls arrive whole.

Claude Code's effort setting (`/effort`, `--effort`, `medium` by default) selects `agy`'s thought level for each request: `low`, `medium`, `high` and `xhigh` map directly and `max` uses `xhigh`. The thought level dominates the wait before the first token, so `low` answers simple questions noticeably faster. When `agy` does not offer a level, the request runs at `agy`'s default.

Answers report the input and output tokens `agy` bills, so Claude Code's context usage and automatic compaction see real numbers. `agy` adds its own agent instructions (about 14k tokens) to every prompt. That overhead is included in the reported usage, and `count_tokens` adds it to its estimate once the first answer has reported it.

Each request is bounded by the proxy's 10-minute upstream timeout. When the timeout expires or Claude Code disconnects, NeoSwitch cancels that session in `agy` and leaves the others running. If `agy` produced no output at all for a request that timed out, it is treated as stuck and restarted for the next request.

If the probe fails, run `neoswitch antigravity login` and retry the probe. Do not copy the token into `GEMINI_API_KEY`.

The Gemini API key route ([cli.md](cli.md#gemini-api-key)) does not carry the account risk above:

```text
Claude Code → NeoSwitch Anthropic-compatible proxy → Gemini API/Vertex API → Gemini 4 Neon
```

The ACP route is the one you choose by running `/neoswitch:login`, which saves it as the backend until `NEOSWITCH_BACKEND` or `neoswitch backend` chooses another. It works differently because ACP is an agent protocol and does not expose the raw Gemini HTTP contract. The adapter asks the Neon agent to return Claude-compatible text or tool-call JSON, while Claude Code continues to receive Anthropic-compatible responses from NeoSwitch.
