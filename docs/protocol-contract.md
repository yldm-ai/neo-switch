# NeoSwitch protocol contract

## Scope

NeoSwitch presents the subset of the Anthropic Messages API that Claude Code needs and translates it to Gemini `generateContent` / `streamGenerateContent` through the official `google.golang.org/genai` SDK. An experimental, opt-in OpenAI backend reuses the same translation; see [OpenAI backend (experimental)](#openai-backend-experimental). The proxy is local-only by default and is not a general-purpose multi-provider gateway.

## Endpoints

| Method | Path | Contract |
|---|---|---|
| GET | `/healthz` | Returns `200 {"status":"ok"}` when the process is serving. It does not call Gemini. |
| GET | `/v1/models` | Returns the configured model as an Anthropic-style model entry. |
| POST | `/v1/messages` | Accepts an Anthropic Messages request and returns JSON, or Anthropic SSE when `stream` is true. |
| POST | `/v1/messages/count_tokens` | Returns `{"input_tokens": n}` for the same request shape (no `max_tokens` required). |

Query strings such as `?beta=true` are ignored. Every POST endpoint requires the configured token as `Authorization: Bearer`, `x-api-key` or `X-NeoSwitch-Token` when a token is set.

## Credentials

genai resolves credentials from the standard environment: `GEMINI_API_KEY` (or `GOOGLE_API_KEY`) for the Gemini Developer API, or `GOOGLE_GENAI_USE_VERTEXAI=true` with `GOOGLE_CLOUD_PROJECT`, `GOOGLE_CLOUD_LOCATION` and Application Default Credentials for Vertex AI. The Developer API is called on `v1beta` unless `GOOGLE_GENAI_API_VERSION` is set. `GEMINI_BASE_URL` overrides the API host; a trailing `/v1beta` or `/v1` is accepted and stripped.

## Request translation

- `model` is accepted for Claude Code compatibility; the backend model is selected by `GEMINI_MODEL`.
- `system` and mid-conversation `role: "system"` messages (Claude Code sends its environment block this way) become Gemini `systemInstruction` text, in order.
- `user` / `assistant` messages become `user` / `model` contents. Messages that translate to no parts are dropped, because Gemini rejects empty contents.
- Content blocks:

| Anthropic block | Gemini |
|---|---|
| `text` | text part; empty text is dropped |
| `image` (base64) | `inlineData` |
| `document` (base64, e.g. PDF) | `inlineData` |
| `document` (text source) | text part |
| `tool_use` | `functionCall`, with the thought signature and call ID recovered from the tool ID |
| `tool_result` | `functionResponse` with `{"output": text}` or `{"error": text}` when `is_error`; images inside become multimodal `functionResponse.parts` |
| `thinking`, `redacted_thinking` | dropped: Claude's signed reasoning has no Gemini representation |
| anything else, URL sources | rejected with `400` |

- Tools are sent as one Gemini tool with every function declaration. `input_schema` is passed unmodified as `parametersJsonSchema` (full JSON Schema), not the OpenAPI-subset `parameters` field, which rejects keywords Claude Code schemas use (`$schema`, `additionalProperties`, `allOf`, `exclusiveMinimum`, ...). Server tools (typed `web_search_*`, `code_execution_*`, ...) are rejected, except a request whose only tool is `web_search_*`. Claude Code's WebSearch tool sends exactly that, and a backend that can search answers it (`server.WebSearcher`). The answer is a `server_tool_use` block per query, then one `web_search_tool_result` block holding the pages, then the summary as text, with `usage.server_tool_use.web_search_requests` set. A stream starts at the first query, so later failures become `error` events. A search that ran no query, or more than `max_uses`, answers with a `web_search_tool_result_error` (`unavailable`, `max_uses_exceeded`). Only the Antigravity route can search; the Gemini API route still rejects the request.
- `tool_choice` `auto` / `any` / `none` / `tool` map to function calling modes `AUTO` / `ANY` / `NONE` / `ANY` restricted to the named function.
- `max_tokens`, `temperature`, `top_p`, `top_k` and `stop_sequences` map to the generation config. `thinking`, `metadata`, `context_management` and `cache_control` are ignored.
- `output_config.effort` (`low`, `medium`, `high`, `xhigh`, `max`; Claude Code sends `medium` by default) maps to Gemini thinking levels `LOW` / `MEDIUM` / `HIGH` / `XHIGH` / `XHIGH`. The Antigravity ACP route applies it as `agy`'s `thought_level` when the session offers that level. The Gemini API / Vertex AI route sends it as `generationConfig.thinkingConfig.thinkingLevel`. The levels have not been verified against the live API for each model, so if Gemini rejects a request with HTTP `400` whose message names the thinking configuration, NeoSwitch resends it once without the level and stops sending a level for that model until the daemon restarts. For streams the resend happens only when the rejection arrives before the first chunk. No request without an effort carries a thinking config. Unknown effort values are ignored.

## Tool call IDs and thought signatures

Gemini 3+ attaches a `thoughtSignature` to function calls and rejects a later request whose history replays the call without it (finish reason `MISSING_THOUGHT_SIGNATURE`). Claude Code keeps only Anthropic blocks in its history, so NeoSwitch embeds the signature and Gemini's call ID in the `tool_use` ID it mints: `toolu_ns_` followed by base64url JSON. The ID is restricted to `[A-Za-z0-9_-]`, needs no server-side state, and survives proxy restarts and `claude --resume`. Tool IDs not minted by NeoSwitch carry no signature: they come from Claude, in a session that ran on Anthropic before switching to NeoSwitch or was resumed through it. Gemini validates only the first function call of each model step, so that call gets the placeholder signature `skip_thought_signature_validator` that Gemini documents for history from other models; later parallel calls stay unsigned.

## Response translation

- Text parts become `text` blocks; `thought` parts are skipped; each `functionCall` becomes a `tool_use` block. Responses containing a tool call have `stop_reason: "tool_use"`.
- Finish reasons: `STOP` → `end_turn` / `tool_use`; `MAX_TOKENS` → `max_tokens`; safety, recitation and blocklist reasons → `refusal`. A blocked prompt (no candidates, `promptFeedback.blockReason`) is a `refusal` with empty content. Finish reasons that mean the turn failed (`MALFORMED_FUNCTION_CALL`, `UNEXPECTED_TOOL_CALL`, `MISSING_THOUGHT_SIGNATURE`, `OTHER`, ...) are errors, never a silent `end_turn`.
- Usage: `input_tokens` = prompt tokens minus cached tokens, `cache_read_input_tokens` = cached tokens, `output_tokens` = candidate plus thinking tokens.

## Streaming

`stream: true` relays Gemini's SSE stream as it arrives. NeoSwitch waits for the first Gemini chunk before writing anything, so failures before generation starts keep a real HTTP status. Event order:

```text
message_start
ping
(content_block_start content_block_delta+ content_block_stop)*
message_delta
message_stop
```

Consecutive text chunks share one text block. Gemini delivers each function call whole (argument streaming is not requested), so a `tool_use` block is emitted as start, a single complete `input_json_delta`, stop. A failure after streaming started ends the stream with an Anthropic `error` event and no `message_stop`.

## Token counting

On Vertex AI the system instruction and tools are counted natively. The Gemini Developer API countTokens call accepts contents only, so NeoSwitch prepends the system instruction and the serialized tool declarations as contents; the result is an estimate within the framing tokens Gemini adds around them.

## Failure policy

| Upstream | NeoSwitch response |
|---|---|
| translation error (unsupported block, unknown tool_use_id) | `400 invalid_request_error`, before any upstream call |
| Gemini 400 | `400 invalid_request_error`; token-limit errors are prefixed `prompt is too long:` so Claude Code offers compaction |
| Gemini 401 / 403 | `400 invalid_request_error` naming the credentials to check. Never 401 or 403: Claude Code reads both as a problem with the user's Claude login and tells them to run `/login` |
| Gemini 404 / 413 | same status, `not_found_error` / `request_too_large` |
| upstream that cannot answer until the user acts (agy signed out or missing, a plan without the model) | `400 invalid_request_error` with the fix, shown by Claude Code as it is and not retried |
| agy crashed or its handshake timed out | `529 overloaded_error`, which Claude Code retries; the next request starts agy again |
| agy reports that Google rate-limits the account | `429 rate_limit_error` quoting agy's reason, which Claude Code retries with backoff |
| agy reports the account's quota for the model used up | `400 invalid_request_error` quoting agy's reason, not retried |
| agy reports the conversation too long for the model | `400 invalid_request_error` starting `prompt is too long`, which makes Claude Code offer compaction |
| wrong NeoSwitch token, no backend | `400 invalid_request_error` naming `/neoswitch:on` or `/neoswitch:login` |
| Gemini 429 | `429 rate_limit_error` |
| Gemini 503 | `529 overloaded_error` |
| other Gemini 5xx | same status, `api_error` |
| no response within 10 minutes | `504 api_error` |
| network failure, unusable response | `502 api_error` |

- Missing credentials fail at startup.
- No automatic retry or model fallback: Claude Code owns retries; genai's retry option is left unset.
- Logs contain no prompts, tool arguments, credentials or responses.

## OpenAI backend (experimental)

`NEOSWITCH_BACKEND=openai` (or `neoswitch backend openai`) sends requests to the OpenAI Responses API (`POST /v1/responses`) through the official `github.com/openai/openai-go/v3` SDK. Automatic selection never picks it, even when `OPENAI_API_KEY` is set. The backend converts the translated Gemini request above into a Responses request and the result back into a Gemini response, so request validation, tool IDs, stop reasons, usage mapping, streaming events and the failure policy are shared with the Gemini backend. It has only been tested against a scripted upstream (`internal/provider/openai_test.go`), not a live OpenAI account.

Configuration: `OPENAI_API_KEY` (required, never logged or echoed; key-shaped strings are removed from upstream error messages), `OPENAI_MODEL` (required; there is no default model), `OPENAI_BASE_URL` (optional, must include the version path, e.g. `https://api.openai.com/v1`). The SDK also honours `OPENAI_ORG_ID` and `OPENAI_PROJECT_ID`. SDK retries are disabled because Claude Code owns retries.

Mapped:

| Anthropic / Gemini side | Responses API |
|---|---|
| `system` and system messages | `instructions`, joined with blank lines |
| user text, base64 images, base64 PDFs | `message` input items with `input_text`, `input_image` (data URL, detail `auto`), `input_file` (data URL) |
| assistant text | assistant `message` input items with string content |
| `tool_use` | `function_call` item; `call_id` is recovered from the NeoSwitch tool ID, which embeds the OpenAI `call_id` exactly as it embeds Gemini's call ID |
| `tool_result` | `function_call_output` item with the same `call_id`; text as a string, images and PDFs as an `input_text` / `input_image` / `input_file` list |
| tools | `function` tools with `input_schema` passed unmodified as `parameters` and `strict: false` (strict mode requires every property to be required, which Claude Code schemas are not) |
| `tool_choice` `auto` / `any` / `none` / `tool` | `auto` / `required` / `none` / `{"type":"function","name":...}` |
| `max_tokens`, `temperature`, `top_p` | `max_output_tokens`, `temperature`, `top_p` |
| output `message` text, `function_call` items | text blocks, `tool_use` blocks; parallel calls each keep their own `call_id` |
| status `completed` / `incomplete` (`max_output_tokens`) / `incomplete` (`content_filter`) / `refusal` content | `end_turn` or `tool_use` / `max_tokens` / `refusal` / `refusal` with the refusal text |
| usage | `input_tokens` minus `cached_tokens` → `input_tokens`, `cached_tokens` → `cache_read_input_tokens`, `output_tokens` (which include reasoning tokens) → `output_tokens` |
| streaming | `response.output_text.delta` → text deltas; each `function_call` is emitted whole when its `response.output_item.done` arrives; `response.completed` / `response.incomplete` carry the stop reason and usage |
| HTTP errors | the upstream status, mapped by the failure policy above; `context_length_exceeded` becomes `prompt is too long` |
| `response.failed` and stream `error` events | `rate_limit_exceeded` → 429, `server_error` → 500, `invalid_prompt` and image errors → 400, others → 502 |
| `count_tokens` | `POST /v1/responses/input_tokens` with the same input, instructions, tools and tool choice (an exact count from OpenAI, not an estimate) |

Not mapped:

- `stop_sequences` and `top_k`: the Responses API has no equivalent; they are not sent.
- `output_config.effort` / `/effort`: not sent as `reasoning.effort`, because `OPENAI_MODEL` can name a model without reasoning support, which rejects the parameter.
- Reasoning items: returned reasoning is dropped and not replayed on the next turn. Requests use `store: false` and no `previous_response_id`, so reasoning models start each turn without their earlier reasoning.
- The `is_error` flag of a `tool_result`: `function_call_output` has no error flag, so only the result text is sent.
- Hosted tools (web search, file search, code interpreter, ...), images in assistant turns, media types other than images and PDF, and output items other than messages, function calls and reasoning (rejected as errors).
- Function call arguments are not streamed incrementally; a tool call arrives whole.
- Error messages are prefixed `OpenAI:` but still appear inside the server's generic `Gemini returned HTTP ...` wording.

## Compatibility matrix

| Scenario | Status | Acceptance check |
|---|---|---|
| Text, non-streaming and streaming | Supported | `server` tests; deltas merged into one block; e2e `text` |
| System prompt and system messages | Supported | `translate` tests |
| Claude Code built-in tool schemas | Supported | `testdata/claude-code-tools.json`, captured from Claude Code 2.1.329, passes unmodified; every e2e scenario sends the tools Claude Code really declares |
| Tool call → tool result → answer | Supported | `provider` wire test replays the signature through genai serialization; e2e `read`, `edit and write` and `bash` check the result content, the replayed signature and the file changed on disk |
| Parallel tool calls | Supported | Each call gets its own ID; results are sent in call order (`translate` test, e2e `parallel tool calls`) |
| Images and PDFs, including inside tool results | Supported | `translate` tests |
| Upstream errors | Supported | Status mapping table test |
| `count_tokens` | Supported, estimated on the Developer API | `provider` and `server` tests |
| Session resume | Supported | Stateless tool IDs; e2e `resume` checks that `claude -p --resume` replays the earlier call with its signature and its result |
| Claude Code permissions | Supported | e2e `permission denial`: a refused Bash call reaches the model as an `error` function response and the command does not run |
| Hooks | Supported | e2e `hooks`: project `PreToolUse` and `PostToolUse` hooks fire for a model tool call and receive the NeoSwitch tool ID |
| Skills | Supported | e2e `skill`: a project skill is advertised, invoked through the Skill tool and its instructions reach the model |
| Subagents | Supported | e2e `subagent`: the Agent tool's subagent sends its own request and its answer returns to the parent |
| MCP tools | Supported | e2e `mcp`: a deferred MCP tool from `claude mcp serve` is loaded with ToolSearch and called |
| Antigravity OAuth via ACP | Supported as an opt-in local adapter | Uses the official `agy acp` process through `github.com/coder/acp-go-sdk` and never reads OAuth tokens. Concurrent sessions, streamed text, usage and stop reasons are passed through. A conversation's session is continued with only its new message, and no prompt exceeds the 192,000 bytes `agy` reads ([antigravity.md](antigravity.md)). `provider` tests drive a scripted ACP server for concurrency, streaming with split escapes, tool calls, images, cancellation, agent exit, a stuck agent, session reuse, split prompts and Google's refusals. The scripted server refuses any prompt over agy's limit. Images are sent when `agy` advertises them; PDFs are marked as omitted |

The e2e scenarios live in `e2e/` behind the `e2e` build tag and run in CI. Each runs the real Claude Code CLI (`claude -p`, with a temporary home and config directory) through an in-process NeoSwitch proxy against a scripted Gemini server, which returns function calls with thought signatures and answers 400 when the next request does not carry what Claude Code should have sent back. They need no Gemini key or Anthropic login. One scenario (`TestClaudeCodeThroughAntigravity`) runs the Antigravity ACP route against a scripted `agy`. It checks that a tool loop continues one ACP session and sends it only the new message, and that Claude Code sends its request-class header from the project setting NeoSwitch writes. Session reuse rests on both. Run them with `go test -tags e2e ./e2e/`; `CLAUDE_CLI_PATH` selects the Claude Code executable.
