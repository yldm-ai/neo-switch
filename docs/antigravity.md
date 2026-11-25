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

**Without a model reply.** A skill always ends in a model turn, so as a skill `/neoswitch:login` showed its link only once the model had repeated the command's output: about 26 s on a session switched to Gemini, sometimes with a character dropped from the link, and not at all while that session's model failed. On Claude Code 2.1.326 or later the plugin's mod (`plugin/mod/commands.ts`, listed under `modules` in `plugin/hooks.json`) answers `/neoswitch:login`, `/neoswitch:status`, `/neoswitch:on` and `/neoswitch:off` itself, with no model request (as a skill, `/neoswitch:on` also spent its first Gemini reply on repeating its own output, and could shorten the notes in it): it runs the same command with `NEOSWITCH_DIALOG=1`, shows its output at once (with the terms warning and progress once the command is done: a hook gets 10 s of its own time, which reading a spawned process's output counts against, so the mod waits on one `$.process.run` instead, and says first when the run will build or download NeoSwitch), and, when the output ends in `@neoswitch-dialog browser|code|again`, opens Claude Code's own question dialog for the code. The code typed there goes to the command on stdin, never into the conversation, the prompt history or the command line, and an error never repeats it, since it may be something pasted by mistake. With the code the mod passes the link it showed, so a code for a sign-in that another `/neoswitch:login` replaced meanwhile is refused before `agy` spends its one exchange. Before the command runs, any problem (an older Claude Code, a missing API) hands the command to the skill; after, the mod answers it itself, because the skill would start a second sign-in and void the link already shown. Mods do not load in WSL, the desktop app over SSH, `claude -p` or under a policy that allows only managed mods, so the skill stays the fallback there; on a Claude Code known to be older than 2.1.326 (read from `CLAUDE_CODE_EXECPATH`), the skill's output says that an update shows the link at once.

**Signing in again, or with another Google account.** NeoSwitch first checks whether `agy` is signed in, by opening a session, which `agy` refuses with ACP's auth-required error when signed out. When it is, the command turns the project on with the current sign-in at once, and starts only the sign-in by code beside it: `agy`'s browser sign-in would just return the current sign-in, and leave the link dead. Opening the link and running `/neoswitch:login <code>` signs in again, such as with another Google account; until then the current sign-in stays, and `/neoswitch:login cancel` (in a terminal, Enter alone) keeps it and drops the link. Every finished sign-in is recorded in `signed-in` in the state folder, and the daemon replaces an `agy` started before it, once the requests still using that `agy` end, because an `agy` already running may keep the account it started with.

**Checking the relayed link.** Claude Code gives a slash command no way to show the user text that can be clicked, other than the model's reply, so the model retypes the 700-character link and can change a character of it. The plugin's Stop hook (`plugin/stop.sh`, which starts NeoSwitch only for a reply that mentions `accounts.google.com`) compares a reply that shows the sign-in link with the link waiting for its code. A changed link sends the reply back to the model with the exact one, which the user also sees in the hook's notice; if the retyped link is wrong again, the exact link stays in a notice.

While a sign-in waits for its code, a session already routed to a signed-out `agy` cannot show the link, because the model that would relay it is that `agy`. Its error message then carries the link and the step that finishes the sign-in. After a sign-in, the next request starts a new `agy`, which uses the new sign-in.

The probe launches the official CLI in headless JSON mode with a fixed, non-sensitive prompt. It captures the result in memory, checks for an exact success response, and prints only whether the session is usable. It never prints `stdout`, `stderr`, an account email, a token, or usage details.

`/neoswitch:status` (`neoswitch status`) checks whether `agy` holds a usable sign-in first by opening one ACP session without prompting (refused immediately when signed out, avoiding print mode's 60-second OAuth wait). When signed in and the account offers the target model (whether this session is switched to Gemini or still on Claude), it runs `agy -p /usage --output-format json` (available since `agy` 1.1.11; zero turns and zero quota spent) inside NeoSwitch's guarded workspace to show the target model group's 5-hour and weekly usage with plain-text progress bars. Passing `-v` shows every quota group and remaining AI credits (`agy -p /credits --output-format json`). If `agy` is too old or the quota query fails, `status` omits the usage block instead of failing.

The ACP route uses `agy` as the model-side agent process. NeoSwitch sends the translated Claude request through an ACP session, selects `gemini-4-neon` (or `GEMINI_MODEL`), and translates the returned text or Claude tool call back to the local Anthropic-compatible proxy. OAuth credentials remain owned by `agy`. `agy` offers each account the models of its plan, listed in every new session. When the plan lacks the model, NeoSwitch answers with a message that says so and names the models the plan has, which Claude Code shows as it is instead of retrying; `/neoswitch:login` checks the same list and then leaves the project on Claude.

`agy` is a full coding agent with its own tools. The `bare-llm` agent NeoSwitch uses reports `read_file`, `write_file`, `edit_file`, `run_command`, `manage_task`, `search_web`, `ask_question`, `schedule`, `define_subagent`, `invoke_subagent`, `manage_subagents`, `send_message`, `run_workflow` and `swarm`.

Refusing `agy`'s permission requests is not enough to keep tools in Claude Code: most of those tools never ask. Verified without a guard:

- `invoke_subagent` and `swarm` started agents that read a file in the project.
- `schedule` started a background task.
- `define_subagent` and `send_message` ran.

So `agy` runs under a tool guard:

- **A folder of NeoSwitch's own:** `agy` runs in `agy-workspace` in NeoSwitch's state folder, never in the user's project. A project's `.agents` rules, skills, MCP servers and hooks therefore do not load into it either.
- **A hook on every tool:** that folder's `.agents/hooks.json` runs a `PreToolUse` hook before every tool. The hook is a copy of the NeoSwitch binary named by its content and called by a relative path. It allows `search_web` and denies everything else.
- **Verified with `agy`:**
  - `invoke_subagent`, `schedule` and `read_file` are denied.
  - A hook that fails, prints something else or times out blocks the tool.
  - A deny wins over an allow from the user's own hooks.
  - `search_web` still works.
- **Checked on every prompt:** the hook records each decision. NeoSwitch counts the decisions against the tool calls `agy` reports. A tool call without a decision means `agy` no longer runs the hook, for example after an update changed how it finds hooks. The Antigravity route then stops until the daemon restarts, and every request says why. The tool calls of that one prompt have already run by then.

`search_web` runs without asking: it touches nothing on this computer, but its query reaches Google. NeoSwitch uses it for WebSearch (below). Claude Code's own tools run only in Claude Code, under its permission rules.

**Which model answers.** Claude Code's header and its system prompt keep naming the Claude model the session asks for; sending a Gemini model ID instead would lose the effort level NeoSwitch maps to Gemini's thinking, and a running session does not change its model. So, on Claude Code versions that load the mod, a line under the prompt says "Gemini 4 Neon answers this session" (Claude Code shows it under the plugin's name) while the daemon sends this session's model requests to Gemini. `neoswitch label` asks the daemon, after every NeoSwitch command and, while the line shows or a sign-in may still finish in the browser, before each prompt, so the line also follows a switch made from a terminal or another window. Every request also opens with one fixed sentence (`provider.ModelIdentity`) telling the model that a model the instructions name is only the session's label; it names no model, so it stays true for the background model, and it never changes, so it does not disturb caching or session reuse.

## Background requests

At the end of every turn Claude Code asks for a prompt suggestion, and it names sessions with short side requests. These requests read the conversation without building on it, so they go to `gemini-3.8-flash` instead of spending the main model's quota. `agy` reports quotas per model.

- **Background model:** prompt suggestions (class `auxiliary`, kind `followup-generation`) and Claude Code's tool-free side queries such as session titles (kind `summary-extraction` without tools).
- **Main model:** everything that builds on the conversation. That is the main thread, compactions, subagents, workflow agents, memory extraction and WebSearch. The away summary stays on the main model too: it is sent the way memory extraction is, and the two cannot be told apart.
- **Sessions:** a session is only ever continued with the model that answers it.
- **Thought levels:** they follow the session's model. `gemini-3.8-flash` offers no `xhigh`, so a request asking for it runs at the model's default.
- **Fallback:** when the account's plan does not offer the background model, background requests use the main model, and the daemon log says so once.
- **Setting:** `NEOSWITCH_BACKGROUND_MODEL` chooses another model, or `main` keeps everything on the main model.
- **Auto Mode safeguards:** when Claude Code requests a `dangerous_tool_use` safeguard verdict for emitted tool calls, NeoSwitch runs an ephemeral, tool-free ACP session on `NEOSWITCH_SAFEGUARD_MODEL` (defaulting to the background model, then the main model) and closes it immediately so it does not take a slot in the idle session pool.

## Web search

Claude Code's WebSearch tool asks Anthropic's API to run its web search server tool. NeoSwitch answers that request with `agy`'s Google search instead, so WebSearch works without Claude and without another key:

- **Search:** a session of its own, never reused, asks the model to search for the request with `search_web` and then list the pages. ACP reports each search's query, which Claude Code shows while the search runs, but not what it found.
- **Links:** the pages come from the model's own list. NeoSwitch keeps a page only when its link is one of Google's search-grounding redirects and resolves to a page; that one request goes to Google, without following further. A link the model wrote from memory or copied wrongly is dropped. So every link Claude Code receives came from the search and points at the real page.
- **No search:** a model that answers without searching produces a web search error, not results.
- **`allowed_domains` and `blocked_domains`:** Google's search cannot be held to them. The model is asked to respect them, and NeoSwitch filters the links. When either is set, the summary is left out, since it may quote any site.
- **`max_uses`:** a search that runs more queries is stopped with `max_uses_exceeded`.

With a Gemini API key, WebSearch is still not supported.

Images, including screenshots returned by Claude Code tools, are sent as ACP image blocks when `agy` advertises image prompts. PDFs and other attachments cannot travel over ACP; the prompt marks each one as omitted so the model knows it was there.

One `agy acp` process serves the daemon. `agy` runs sessions concurrently, so subagents, background requests and several Claude Code sessions do not wait for each other. Text answers stream to Claude Code as `agy` generates them; tool calls arrive whole.

`agy` reads only the first 192,000 bytes of a prompt. It replaces the rest with a `<truncated N bytes>` marker and tells the model to read the full transcript with its own tools, which NeoSwitch refuses. A Claude Code conversation passes that size after a few file reads, and its newest messages come last. So NeoSwitch never sends `agy` a prompt over 150,000 bytes:

- **After an answer:** the ACP session stays open for up to 30 minutes, up to 16 sessions per `agy`, filed under the conversation it holds.
- **Next request of the same conversation:** it sends only the new message, plus the system instructions or tool definitions when they changed. The session keeps the rest, up to `agy`'s context window of about a million tokens.
- **Any other request:** a new session reads the whole conversation, in several prompts when it is long. Examples are a subagent, a compaction, a conversation that was edited or rewound, and the first request after the daemon or `agy` restarted, or after a newer NeoSwitch took the daemon over (design-session-routing.md, "Updates"). Every prompt but the last asks the model only to reply "ok". The daemon log records each conversation sent this way.
- **Who owns a session:** `agy` cannot fork a session, yet at the end of every turn Claude Code sends background requests that carry the conversation's whole history. Examples are a prompt suggestion, memory extraction, an away summary and `auto_dream`. Claude Code's hint headers say who sent each request, and sessions belong to their sender:
  - **The main thread** continues only its own session, so its next message never reads the conversation again.
  - **A compaction** continues the main thread's session, so the summary reads the whole conversation for the price of one instruction. That session is closed afterwards.
  - **Each subagent, workflow agent and kind of background request** keeps a session of its own. A background session reads only what the main thread did since its last run, with a note to disregard its previous background exchange.
  - **Closing order:** past 16 idle sessions, sessions of main threads are closed last.
  - **Older Claude Code:** without the headers, any request may continue a session holding its conversation, so a background request can take the main thread's session.
- **Matching a conversation:** a tool call is matched by its ID and name, not its input, because Claude Code sends back the input as its tool read it. Its Edit tool adds `"replace_all": false`, for one; matching on the input lost the session, and read the whole conversation again, after every edit.
- **Not reused:** a session whose answer Claude Code did not receive, that answered with something other than exactly one JSON object, or that had to write a tool call again.

When the model writes more than one JSON object, typically the same tool call twice, NeoSwitch merges the texts and runs only the first tool call.

The model writes JSON by hand, and gets string escapes wrong when a tool call holds a regular expression or a shell command. NeoSwitch repairs what it can tell apart: a backslash that starts no JSON escape (`\|`, `\.`, `\d`, `C:\users`) is kept as a backslash, a `\b` in a tool call's input stays the two characters the model meant rather than a backspace, and so does a `\f` in a command, where Claude Code would refuse a form feed (a file's real form feeds are kept). A tool call that is still not JSON, typically for an unescaped quote, is not passed on as text, which would end the turn with raw JSON on screen: the same session is asked once to write it again, and if that fails too, Claude Code retries the request. A tool call cut off by the output limit is passed on as text, since writing it again would hit the same limit.

`agy` reads its own slash commands (`/compact`, `/plan`, `/goal`, and the user's own commands and skills it advertises) anywhere in a prompt where a space comes before the slash, not only at the start: a tool result quoting `Run /compact` was taken as a `/compact` the user typed, and the model answered that it could not run it. `agy acp` has no switch to turn this off (`--disable-slash-commands` is for print mode only), so NeoSwitch writes each command name in a form `agy` does not read as a command and the model reads unchanged: in JSON (tool calls, tool results, tool definitions) the slash becomes the escape `\/`, and in plain text an invisible word joiner (U+2060) goes before it.

When Google refuses a request, Claude Code shows the reason in `agy`'s own words:

| Reason | What Claude Code does |
| --- | --- |
| The account's quota for the model is used up | Shows the reason without retrying, with `/neoswitch:off` as the way to continue on Claude |
| A rate limit | Retries with backoff (429) |
| The conversation is too long for the model | Reports `prompt is too long`, and Claude Code offers `/compact` |

Claude Code's effort setting (`/effort`, `--effort`, `medium` by default) selects `agy`'s thought level for each request: `low`, `medium`, `high` and `xhigh` map directly and `max` uses `xhigh`. The thought level dominates the wait before the first token, so `low` answers simple questions noticeably faster. When `agy` does not offer a level, the request runs at `agy`'s default.

Answers report the input and output tokens `agy` bills, so Claude Code's context usage and automatic compaction see real numbers. ACP's input tokens leave out those read from cache, which in a continued session are most of the conversation. NeoSwitch adds them back and reports them as cache reads. `agy` adds its own agent instructions (about 14k tokens) to every prompt. That overhead is included in the reported usage, and `count_tokens` adds it to its estimate once the first answer has reported it.

Each request is bounded by the proxy's 10-minute upstream timeout. When the timeout expires or Claude Code disconnects, NeoSwitch cancels that session in `agy` and leaves the others running. If `agy` produced no output at all for a request that timed out, it is treated as stuck and restarted for the next request.

If the probe fails, run `neoswitch antigravity login` and retry the probe. Do not copy the token into `GEMINI_API_KEY`.

The Gemini API key route ([cli.md](cli.md#gemini-api-key)) does not carry the account risk above:

```text
Claude Code → NeoSwitch Anthropic-compatible proxy → Gemini API/Vertex API → Gemini 4 Neon
```

The ACP route is the one you choose by running `/neoswitch:login`, which saves it as the backend until `NEOSWITCH_BACKEND` or `neoswitch backend` chooses another. It works differently because ACP is an agent protocol and does not expose the raw Gemini HTTP contract. The adapter asks the Neon agent to return Claude-compatible text or tool-call JSON, while Claude Code continues to receive Anthropic-compatible responses from NeoSwitch.
