package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	acp "github.com/coder/acp-go-sdk"
	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/daemon"
	"github.com/yldm-ai/neo-switch/internal/translate"
)

// AntigravityACP uses the official agy ACP server as the upstream model session. It never reads Antigravity credentials: agy resolves its own OAuth profile from the platform keyring.
//
// One agy process serves every request. agy runs sessions concurrently, so parallel Claude Code requests (subagents, background calls, several sessions sharing the daemon) do not queue behind each other. A request continues the session that holds its conversation when there is one, and otherwise gets a new session (see acp_session.go).
type AntigravityACP struct {
	path  string
	agent string
	model string
	// background answers Claude Code's background requests that read the conversation without building on it, such as prompt suggestions and session titles, "" for the main model (see backgroundModelFor).
	background string
	cwd        string

	mu   sync.Mutex
	conn *acpConn

	// backgroundMissing is set once the plan turned out not to offer the background model, which is logged once.
	backgroundMissing atomic.Bool

	// unguarded is set for good once agy ran a tool without the tool guard (acp_guard.go): every later request is refused.
	unguarded atomic.Bool

	// promptOverhead is how many input tokens agy adds around every prompt (its own agent instructions, under 1k for the slim neoswitch agent), learned from the usage of small fresh answers. ACP reports usage only after an answer, so estimates made before one need it.
	promptOverhead atomic.Int64

	// PendingSignIn returns the link of a sign-in by code that is waiting for its code, or "".
	PendingSignIn func() string
	// SignedInAt returns when the last sign-in finished, or the zero time. A sign-in can switch to another Google account, and an agy started before it may keep the account it started with, so a connection older than the sign-in is retired.
	SignedInAt func() time.Time
}

// ErrAntigravityCLIMissing means the official Antigravity CLI is not installed where NeoSwitch looks for it. Callers add the step that fixes it in their context.
var ErrAntigravityCLIMissing = errors.New("Google's Antigravity command-line tool (agy) is not installed")

// FindAntigravityCLI returns the agy executable NeoSwitch runs: ANTIGRAVITY_CLI_PATH when set, otherwise agy on PATH, otherwise agy in ~/.local/bin, where Google's official installer puts it. The installer adds that directory to shell profiles only, so a Claude Code started before the install, and the daemon it launches, do not have it on PATH.
func FindAntigravityCLI() (string, error) {
	if path := strings.TrimSpace(os.Getenv("ANTIGRAVITY_CLI_PATH")); path != "" {
		if _, err := exec.LookPath(path); err != nil {
			return "", fmt.Errorf("%w at %s", ErrAntigravityCLIMissing, path)
		}
		return path, nil
	}
	if path, err := exec.LookPath("agy"); err == nil {
		return path, nil
	}
	if path := AntigravityCLIInstallPath(); path != "" {
		if _, err := exec.LookPath(path); err == nil {
			return path, nil
		}
	}
	return "", ErrAntigravityCLIMissing
}

// AntigravityCLIInstallPath is where Google's official installer puts agy on macOS and Linux, or "" when the home directory is unknown or the platform has no such installer.
func AntigravityCLIInstallPath() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "bin", "agy")
}

func NewAntigravityACPFromEnv(ctx context.Context) (*AntigravityACP, error) {
	path, err := FindAntigravityCLI()
	if err != nil {
		return nil, err
	}
	agent := strings.TrimSpace(os.Getenv("ANTIGRAVITY_ACP_AGENT"))
	if agent == "" {
		agent = defaultACPAgent
	}
	// agy runs in a folder of NeoSwitch's own, never the user's project: see acp_guard.go.
	stateDir, err := daemon.Dir()
	if err != nil {
		return nil, fmt.Errorf("locate Antigravity ACP workspace: %w", err)
	}
	cwd, err := filepath.Abs(AntigravityWorkspace(stateDir))
	if err != nil {
		return nil, fmt.Errorf("locate Antigravity ACP workspace: %w", err)
	}
	model := strings.TrimSpace(os.Getenv("GEMINI_MODEL"))
	if model == "" {
		model = DefaultModel
	}
	return &AntigravityACP{path: path, agent: agent, model: model, background: backgroundModel(model), cwd: cwd}, nil
}

func (a *AntigravityACP) Model() string { return a.model }

// Login performs ACP's protocol-driven OAuth flow. The agy ACP server owns the browser callback and keyring state, so this method never reads a token or waits for an authorization code on stdin. It uses a dedicated agy process that exits when the login finishes.
func (a *AntigravityACP) Login(ctx context.Context) error {
	c, err := startACP(a.path, a.agent, a.cwd)
	if err != nil {
		return err
	}
	defer c.close()
	init, err := c.initialize(ctx, acp.ClientCapabilities{Auth: acp.AuthCapabilities{Terminal: false}})
	if err != nil {
		return errors.New("Google's Antigravity command-line tool (agy) did not respond. Update or reinstall it, then run /neoswitch:login again")
	}
	methodID := acp.AuthMethodId("oauth-personal")
	if len(init.AuthMethods) > 0 {
		if init.AuthMethods[0].Agent == nil {
			return errors.New("Antigravity ACP advertised an authentication method NeoSwitch cannot run")
		}
		methodID = init.AuthMethods[0].Agent.Id
	}
	if methodID == "" {
		return errors.New("Antigravity ACP did not advertise an authentication method")
	}
	if _, err := c.conn.Authenticate(ctx, acp.AuthenticateRequest{MethodId: methodID}); err != nil {
		// A cancelled sign-in reaches here as an internal JSON-RPC error; the cancellation is the real cause.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// The JSON-RPC error itself means nothing to the user; its message, such as "Authentication required", is kept as the reason.
		reason := err.Error()
		var rpcErr *acp.RequestError
		if errors.As(err, &rpcErr) {
			reason = rpcErr.Message
		}
		return fmt.Errorf("the Google sign-in was not completed (%s). Run /neoswitch:login to try again", reason)
	}
	return nil
}

// probeSession opens one session in a dedicated agy process and closes it without prompting, then stops that process.
func (a *AntigravityACP) probeSession(ctx context.Context) (acp.NewSessionResponse, error) {
	c, err := startACP(a.path, a.agent, a.cwd)
	if err != nil {
		return acp.NewSessionResponse{}, err
	}
	defer c.close()
	if _, err := c.initialize(ctx, acp.ClientCapabilities{}); err != nil {
		return acp.NewSessionResponse{}, fmt.Errorf("initialize Antigravity ACP: %w", err)
	}
	session, err := c.conn.NewSession(ctx, acp.NewSessionRequest{Cwd: a.cwd, McpServers: []acp.McpServer{}})
	if err != nil {
		return acp.NewSessionResponse{}, fmt.Errorf("create Antigravity ACP session: %w", err)
	}
	if session.SessionId != "" {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), acpCloseTimeout)
		_, _ = c.conn.CloseSession(closeCtx, acp.CloseSessionRequest{SessionId: session.SessionId})
		cancel()
	}
	return session, nil
}

// ListModels returns the models agy offers this account, in agy's order. agy advertises them as the values of a new session's model option, so this opens one session without prompting it. Like Login it uses a dedicated agy process, which it stops before returning.
func (a *AntigravityACP) ListModels(ctx context.Context) ([]string, error) {
	session, err := a.probeSession(ctx)
	if err != nil {
		return nil, err
	}
	models := modelIDs(session.ConfigOptions)
	if len(models) == 0 {
		return nil, errors.New("Antigravity ACP advertised no models for a new session")
	}
	return models, nil
}

func (a *AntigravityACP) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn == nil {
		return nil
	}
	err := a.conn.close()
	a.conn = nil
	return err
}

// Generate runs one request and returns the complete answer.
func (a *AntigravityACP) Generate(ctx context.Context, req translate.Request) (*genai.GenerateContentResponse, error) {
	var raw, thoughts strings.Builder
	var onThought func(string)
	if req.IncludeThoughts {
		onThought = func(text string) { thoughts.WriteString(text) }
	}
	x, err := a.run(ctx, req, func(text string) { raw.WriteString(text) }, onThought)
	if err != nil {
		return nil, err
	}
	parts, exact, err := acpAnswerParts(raw.String(), x.result)
	if malformed := (*malformedToolUseError)(nil); errors.As(err, &malformed) {
		raw.Reset()
		if err := a.repairToolUse(ctx, x, req, malformed, func(text string) { raw.WriteString(text) }); err != nil {
			return nil, err
		}
		parts, _, err = acpAnswerParts(raw.String(), x.result)
		exact = false
		err = secondMalformed(err)
	}
	x.end(parts, exact && err == nil)
	if err != nil {
		return nil, err
	}
	if thoughts.Len() > 0 {
		parts = append([]*genai.Part{{Text: thoughts.String(), Thought: true}}, parts...)
	}
	resp := acpFinalResponse(parts, x.result)
	if resp.UsageMetadata == nil {
		estimate, _ := a.CountTokens(ctx, req)
		resp.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: int32(estimate)}
	}
	return resp, nil
}

// Stream relays the answer while agy generates it. Text answers are decoded out of the {"text": ...} envelope incrementally; tool calls are delivered whole in the final chunk, which also carries the finish reason and usage. The session is kept for the conversation's next request only once Claude Code has taken that final chunk.
//
// ACP reports usage only when the prompt ends, but Claude Code sizes its context from the input tokens in message_start, which the first chunk becomes. That chunk therefore carries the prompt-size estimate; the final chunk carries agy's exact count.
func (a *AntigravityACP) Stream(ctx context.Context, req translate.Request) iter.Seq2[*genai.GenerateContentResponse, error] {
	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		estimate, _ := a.CountTokens(ctx, req)
		firstUsage := &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: int32(estimate)}
		// Ending the iteration early (Claude Code disconnected) cancels the prompt in agy.
		ctx, cancel := context.WithCancel(ctx)
		queue := newChunkQueue[acpChunk]()
		pushText := func(text string) { queue.push(acpChunk{text: text}) }
		type outcome struct {
			x   *acpExchange
			err error
		}
		done := make(chan outcome, 1)
		go func() {
			var onThought func(string)
			if req.IncludeThoughts {
				onThought = func(text string) { queue.push(acpChunk{thought: true, text: text}) }
			}
			x, err := a.run(ctx, req, pushText, onThought)
			done <- outcome{x, err}
		}()
		var (
			received bool
			x        *acpExchange
			// repaired is set once the session was asked to write a malformed tool call again.
			repaired bool
		)
		defer func() {
			cancel()
			switch {
			case !received:
				// The request is still running; its session is closed once it ends.
				go func() {
					if out := <-done; out.x != nil {
						out.x.end(nil, false)
					}
				}()
			case x != nil:
				x.end(nil, false)
			}
		}()
		var decoder envelopeDecoder
		relay := func() bool {
			for _, chunk := range queue.drain() {
				if chunk.thought {
					if chunk.text == "" {
						continue
					}
					if !yield(acpThoughtChunk(chunk.text, firstUsage), nil) {
						return false
					}
					firstUsage = nil
					continue
				}
				delta := decoder.feed(chunk.text)
				if delta == "" {
					continue
				}
				if !yield(acpTextChunk(delta, firstUsage), nil) {
					return false
				}
				firstUsage = nil
			}
			return true
		}
		for {
			select {
			case <-queue.ready:
				if !relay() {
					return
				}
			case out := <-done:
				received, x = true, out.x
				// Every chunk is queued before the prompt returns, so draining here loses nothing.
				if !relay() {
					return
				}
				if out.err != nil {
					yield(nil, out.err)
					return
				}
				parts, err := decoder.finish(x.result)
				if malformed := (*malformedToolUseError)(nil); errors.As(err, &malformed) && !repaired {
					// The tool call has not reached Claude Code, so the same session writes it again. Text streamed before it stays shown: the new answer is buffered, and finish delivers only what it adds to that text.
					repaired, received = true, false
					streamed := decoder.emitted.String()
					decoder = envelopeDecoder{mode: envelopeBuffered}
					decoder.emitted.WriteString(streamed)
					go func(x *acpExchange) {
						err := a.repairToolUse(ctx, x, req, malformed, pushText)
						if err != nil {
							// repairToolUse already closed the session and released agy, as run does when it fails.
							x = nil
						}
						done <- outcome{x, err}
					}(x)
					x = nil
					continue
				}
				if repaired {
					err = secondMalformed(err)
					decoder.exact = false
				}
				if err != nil {
					yield(nil, err)
					return
				}
				final := acpFinalResponse(parts, x.result)
				if final.UsageMetadata == nil {
					// No streamed chunk and no usage from agy: keep the estimate rather than report zero.
					final.UsageMetadata = firstUsage
				}
				delivered := yield(final, nil)
				x.end(decoder.answer, decoder.exact && delivered)
				x = nil
				return
			}
		}
	}
}

// CountTokens estimates the input tokens agy will bill for the request. ACP has no token-count method, so this is the prompt size at about four characters per token plus the overhead agy reported for earlier prompts; images are not counted.
func (a *AntigravityACP) CountTokens(ctx context.Context, req translate.Request) (int, error) {
	return promptTextTokens(req) + int(a.promptOverhead.Load()), nil
}

func promptTextTokens(req translate.Request) int {
	text, _ := acpPrompt(req, false)
	return len([]rune(text)) / 4
}

// maxOverheadLearnTokens bounds the estimated size of a fresh request used to learn promptOverhead. With the slim neoswitch agent (~0.5k-1k overhead), a large prompt's character-count estimate error would swamp the real overhead.
const maxOverheadLearnTokens = 4000

// run sends the request to agy and passes each chunk of the answer to onText (and, when onThought is non-nil, each reasoning chunk to onThought) as it arrives. It continues the session holding the request's conversation when there is one, and otherwise opens a new session that reads the whole conversation, in several prompts when it is too long for one. The caller ends the returned exchange; when run fails, it has already closed the session and released agy. When ctx ends first, the prompt is cancelled in agy.
func (a *AntigravityACP) run(ctx context.Context, req translate.Request, onText, onThought func(string)) (*acpExchange, error) {
	if a.unguarded.Load() {
		return nil, unguardedError
	}
	c, err := a.connection(ctx)
	if err != nil {
		return nil, err
	}
	x := &acpExchange{c: c, contents: req.Contents, model: a.modelFor(req)}
	x.role, x.owner = originRole(req.Origin)
	keys := historyKeys(req.Contents)
	pieces, continued := a.continuation(x, req, keys)
	fresh := x.session == nil
	if fresh {
		rawSystem, rawTools := rawContext(req)
		var appendText string
		var overflow []acpPiece
		if c.systemPromptAppend {
			sys, tools := neutralizeContext(rawSystem, rawTools)
			appendText, overflow = buildSystemPromptAppend(sys, tools)
		}
		if x.session, err = a.newSession(ctx, c, x.model, appendText); err != nil {
			c.release()
			return nil, err
		}
		if x.session.systemPrompted {
			x.session.system, x.session.systemText, x.session.tools = textKey(rawSystem), rawSystem, textKey(rawTools)
			pieces = append(overflow, messagePieces(req.Contents, c.imagePrompts)...)
		} else {
			pieces = a.freshPieces(x.session, req, c.imagePrompts)
		}
		if x.role == roleOther && len(req.Contents) > 0 {
			x.branch = keys[len(req.Contents)-1]
		}
	}
	instructions := acpAnswerInstructions
	if x.session.systemPrompted {
		instructions = ""
	}
	prompts := acpPrompts(pieces, instructions, continued)
	if len(prompts) > 1 {
		size := 0
		for _, prompt := range prompts {
			size += len(prompt.text)
		}
		acpLogger.Warn("Antigravity conversation sent in several prompts, as it is too long for one", "prompts", len(prompts), "bytes", size, "new_session", fresh, "class", req.Origin.Class, "kind", req.Origin.Kind)
	}
	for i, prompt := range prompts {
		var textSink, thoughtSink func(string)
		if i == len(prompts)-1 {
			textSink = onText
			if req.IncludeThoughts {
				thoughtSink = onThought
			}
		}
		if x.result, err = a.prompt(ctx, c, x.session, prompt, acp.SessionConfigValueId(strings.ToLower(string(req.ThinkingLevel))), answerUpdates(textSink, thoughtSink)); err != nil {
			c.closeSessions([]*acpSession{x.session})
			c.release()
			return nil, err
		}
	}
	// Learn only from a small new session of the main model read in one prompt without images: image tokens are not part of the text estimate, a continued session holds more than this request's text, the background model's overhead says nothing about the main thread's, and a large fresh prompt's character-count error would swamp the slim agent's small overhead.
	if fresh && x.session.model == a.model && len(prompts) == 1 && len(prompts[0].images) == 0 && x.result.Usage != nil {
		if est := promptTextTokens(req); est <= maxOverheadLearnTokens {
			if overhead := promptTokens(x.result.Usage) - int64(est); overhead > 0 {
				a.promptOverhead.Store(overhead)
			}
		}
	}
	return x, nil
}

// acpToolUseRepair asks a session whose answer was a malformed tool call to write it again; %s is what was wrong.
const acpToolUseRepair = "Your last answer started a tool call but was not valid JSON (%s), so Claude Code could not run it. Write the same answer again as exactly one JSON object and no markdown: {\"tool_uses\":[{\"name\":\"tool name\",\"input\":{...}},...]} or {\"tool_use\":{\"name\":\"tool name\",\"input\":{...}}}, optionally with \"text\". Inside JSON strings, escape every double quote as \\\" and every literal backslash as \\\\.\n\n"

// repairToolUse sends acpToolUseRepair to the session that wrote a malformed tool call and passes the new answer to onText. Like run, it closes the session and releases agy when it fails.
func (a *AntigravityACP) repairToolUse(ctx context.Context, x *acpExchange, req translate.Request, malformed *malformedToolUseError, onText func(string)) error {
	acpLogger.Warn("Antigravity answered with a malformed tool call; asking it to write the call again", "error", malformed.cause)
	result, err := a.prompt(ctx, x.c, x.session, acpPiece{text: fmt.Sprintf(acpToolUseRepair, malformed.cause)}, acp.SessionConfigValueId(strings.ToLower(string(req.ThinkingLevel))), answerText(onText))
	if err != nil {
		x.c.closeSessions([]*acpSession{x.session})
		x.c.release()
		return err
	}
	x.result = result
	return nil
}

// secondMalformed turns a tool call that is still malformed after repairToolUse into an error Claude Code retries, which starts the request over in a new session.
func secondMalformed(err error) error {
	if malformed := (*malformedToolUseError)(nil); errors.As(err, &malformed) {
		acpLogger.Warn("Antigravity wrote a malformed tool call again", "error", malformed.cause)
		return &translate.RetryableError{Message: "Antigravity answered twice with a tool call that is not valid JSON, so Claude Code could not run it. Try again, or run /neoswitch:off to continue this session on Claude."}
	}
	return err
}

// acpContinued opens the prompt of a session that already holds the conversation.
const acpContinued = "The conversation so far is in the previous messages of this session; this message adds what is new since your last answer.\n\n"

// acpBranchContinued opens the prompt of a background session that answered an earlier background request of Claude Code's.
const acpBranchContinued = "Your last exchange in this session was a separate background request from Claude Code, not part of the conversation; disregard it and what it asked for. The conversation continues below from where it was before that request, followed by a new request.\n\n"

// continuation claims the idle session the request may continue (see acp_session.go), given the request's historyKeys, and returns what that session has not read yet: the system instructions and tool definitions when they changed, and the messages it does not hold. It leaves x.session nil when there is none.
func (a *AntigravityACP) continuation(x *acpExchange, req translate.Request, keys []string) ([]acpPiece, string) {
	n := len(req.Contents)
	if n < 2 || req.Contents[n-1] == nil || req.Contents[n-1].Role != genai.RoleUser {
		return nil, ""
	}
	s, from := x.c.claim(keys, x.role, x.owner, x.model)
	if s == nil {
		return nil, ""
	}
	x.session = s
	continued := acpContinued
	if from < n-1 {
		continued = acpBranchContinued
		x.branch = keys[n-1]
	}
	rawSystem, rawTools := rawContext(req)
	systemKey, toolsKey := textKey(rawSystem), textKey(rawTools)
	rawSystemToSend := rawSystem
	systemAppended := false
	switch {
	case systemKey == s.system:
		rawSystemToSend = ""
	case s.systemText != "" && strings.HasPrefix(rawSystem, s.systemText):
		delta := strings.TrimPrefix(rawSystem, s.systemText)
		if strings.TrimSpace(delta) == "" {
			rawSystemToSend = ""
		} else {
			rawSystemToSend = delta
			systemAppended = true
		}
	}
	if toolsKey == s.tools {
		rawTools = ""
	}
	s.system, s.systemText, s.tools = systemKey, rawSystem, toolsKey
	system, tools := neutralizeContext(rawSystemToSend, rawTools)
	pieces := contextPiecesForContinuation(system, systemAppended, tools)
	r := &promptRenderer{acceptImages: x.c.imagePrompts}
	for _, content := range req.Contents[from:] {
		pieces = append(pieces, r.entry(content))
	}
	return pieces, continued
}

// freshPieces renders the whole request for a new session and records what the session reads.
func (a *AntigravityACP) freshPieces(s *acpSession, req translate.Request, acceptImages bool) []acpPiece {
	rawSystem, rawTools := rawContext(req)
	s.system, s.systemText, s.tools = textKey(rawSystem), rawSystem, textKey(rawTools)
	return conversationPieces(req, acceptImages)
}

// newSession opens a session answering with model. When systemPrompt is non-empty and agy advertises _meta.antigravity.systemPrompt.append, it is attached on session/new so agy pins it in the session's system prompt. A background model the account's plan does not offer gives way to the main model: background requests are no reason to fail.
func (a *AntigravityACP) newSession(ctx context.Context, c *acpConn, model, systemPrompt string) (*acpSession, error) {
	req := acp.NewSessionRequest{Cwd: a.cwd, McpServers: []acp.McpServer{}}
	systemPrompted := c.systemPromptAppend && systemPrompt != ""
	if systemPrompted {
		meta, err := acp.NewMeta(map[string]any{
			"antigravity": map[string]any{
				"systemPrompt": map[string]any{
					"append": systemPrompt,
				},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("encode Antigravity systemPrompt meta: %w", err)
		}
		req.Meta = meta
	}
	session, err := c.conn.NewSession(ctx, req)
	if err != nil {
		return nil, a.requestError(ctx, c, "create Antigravity ACP session", err)
	}
	if session.SessionId == "" {
		return nil, errors.New("Antigravity ACP returned no session ID")
	}
	s := &acpSession{id: session.SessionId, options: session.ConfigOptions, systemPrompted: systemPrompted}
	offered := modelIDs(session.ConfigOptions)
	if model != a.model && len(offered) > 0 && !slices.Contains(offered, model) {
		if !a.backgroundMissing.Swap(true) {
			acpLogger.Warn("the account's Antigravity plan does not offer the background model; background requests use the main model", "background_model", model, "model", a.model)
		}
		model = a.model
	}
	// agy offers each account the models of its plan. A model outside them fails every request alike, so it is reported as unavailable, which Claude Code shows as it is, rather than as an error worth retrying.
	if len(offered) > 0 && !slices.Contains(offered, model) {
		c.closeSessions([]*acpSession{s})
		return nil, &translate.UnavailableError{Message: ModelNotOfferedMessage(model, offered) + " Run /neoswitch:off to continue this session on Claude."}
	}
	selected, err := c.conn.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{ValueId: &acp.SetSessionConfigOptionValueId{
		SessionId: s.id, ConfigId: acpModel, Value: acp.SessionConfigValueId(model),
	}})
	if err != nil {
		c.closeSessions([]*acpSession{s})
		return nil, a.requestErrorFor(ctx, c, model, "select Antigravity model "+model, err)
	}
	s.model = model
	// The options depend on the model: gemini-3.8-flash offers no xhigh thought level, for one.
	if len(selected.ConfigOptions) > 0 {
		s.options = selected.ConfigOptions
	}
	return s, nil
}

// answerText passes the text of agy's answer to onText.
func answerText(onText func(string)) func(acp.SessionUpdate) {
	return answerUpdates(onText, nil)
}

// answerUpdates passes answer text chunks to onText and internal reasoning chunks to onThought (when non-nil).
func answerUpdates(onText, onThought func(string)) func(acp.SessionUpdate) {
	return func(update acp.SessionUpdate) {
		if chunk := update.AgentMessageChunk; chunk != nil && chunk.Content.Text != nil && onText != nil {
			onText(chunk.Content.Text.Text)
		}
		if chunk := update.AgentThoughtChunk; chunk != nil && chunk.Content.Text != nil && onThought != nil {
			onThought(chunk.Content.Text.Text)
		}
	}
}

// prompt sends one prompt to the session and passes the session's updates to onUpdate, which runs on the SDK's notification goroutine and must not block.
func (a *AntigravityACP) prompt(ctx context.Context, c *acpConn, s *acpSession, prompt acpPiece, level acp.SessionConfigValueId, onUpdate func(acp.SessionUpdate)) (acp.PromptResponse, error) {
	// Claude Code's effort picks agy's thought level, which dominates time to first token. It is only a speed and depth hint, so it is skipped when this agy does not offer the level rather than failing the request.
	if level != "" && level != s.level && offersConfigValue(s.options, acpThoughtLevel, level) {
		if _, err := c.conn.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: s.id, ConfigId: acpThoughtLevel, Value: level,
		}}); err != nil {
			return acp.PromptResponse{}, a.requestErrorFor(ctx, c, s.model, "select Antigravity thought level "+string(level), err)
		}
		s.level = level
	}
	var watch errorLineWatch
	c.subscribe(s.id, func(update acp.SessionUpdate) {
		if call := update.ToolCall; call != nil {
			s.sawToolCall(call.ToolCallId)
		}
		if chunk := update.AgentMessageChunk; chunk != nil && chunk.Content.Text != nil {
			watch.feed(chunk.Content.Text.Text)
		}
		onUpdate(update)
	})
	defer c.unsubscribe(s.id)
	blocks := append([]acp.ContentBlock{acp.TextBlock(prompt.text)}, prompt.images...)
	result, err := c.conn.Prompt(ctx, acp.PromptRequest{SessionId: s.id, Prompt: blocks})
	if err == nil && result.StopReason != acp.StopReasonCancelled {
		if guardErr := a.checkGuard(c, s); guardErr != nil {
			return acp.PromptResponse{}, guardErr
		}
		if answerErr := agyAnswerError(watch.text(), s.model); answerErr != nil {
			return acp.PromptResponse{}, answerErr
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), acpCloseTimeout)
			_ = c.conn.Cancel(cancelCtx, acp.CancelNotification{SessionId: s.id})
			cancel()
		}
		return acp.PromptResponse{}, a.requestErrorFor(ctx, c, s.model, "Antigravity ACP prompt", err)
	}
	if result.StopReason == acp.StopReasonCancelled {
		return acp.PromptResponse{}, errors.New("Antigravity ACP cancelled the prompt")
	}
	return result, nil
}

// errorLineWatch keeps a prompt's answer text only while it can still be agy's error line (see agyErrorAnswer), so ordinary answers are not copied. Chunks arrive on the SDK's notification goroutine; text is read after Prompt returns.
type errorLineWatch struct {
	mu      sync.Mutex
	decided bool
	keep    bool
	answer  strings.Builder
}

func (w *errorLineWatch) feed(chunk string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.decided {
		w.answer.WriteString(chunk)
		trimmed := strings.TrimLeftFunc(w.answer.String(), unicode.IsSpace)
		if trimmed == "" || strings.HasPrefix(agyErrorMark, trimmed) && trimmed != agyErrorMark {
			return
		}
		w.decided, w.keep = true, strings.HasPrefix(trimmed, agyErrorMark)
		if !w.keep {
			w.answer.Reset()
		}
		return
	}
	if w.keep {
		w.answer.WriteString(chunk)
	}
}

func (w *errorLineWatch) text() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.keep {
		return ""
	}
	return w.answer.String()
}

// requestError turns a failed ACP call into the error the proxy reports. A context error is returned as such so the server maps a deadline to 504. When a deadline passes while agy has produced no output for any session in acpStuckAfter, agy is treated as stuck and restarted, so one wedged process cannot fail every later request. A slow request on a working agy only cancels its own session.
//
// Other failures reach Claude Code as plain sentences: the JSON-RPC error agy sent goes to the daemon log only. An agy that exited or asks for authentication cannot answer any request, so it is reported as unavailable, which Claude Code does not retry.
func (a *AntigravityACP) requestError(ctx context.Context, c *acpConn, action string, err error) error {
	return a.requestErrorFor(ctx, c, a.model, action, err)
}

// requestErrorFor is requestError for a request answered by model, which a used-up quota names.
func (a *AntigravityACP) requestErrorFor(ctx context.Context, c *acpConn, model, action string, err error) error {
	select {
	case <-c.conn.Done():
		a.discard(c)
		return a.retryable(fmt.Errorf("%s: agy exited unexpectedly: %w", action, err))
	default:
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) && c.quietFor(acpStuckAfter) {
			a.discard(c)
		}
		return fmt.Errorf("%s: %w", action, ctxErr)
	}
	var rpcErr *acp.RequestError
	if errors.As(err, &rpcErr) && rpcErr.Code == acpAuthRequired {
		// A signed-out agy is replaced, so the first request after the user signs in again starts one that reads the new sign-in.
		a.discard(c)
		return a.signedOut(fmt.Errorf("%s: %w", action, err))
	}
	acpLogger.Warn("Antigravity request failed", "action", action, "error", err)
	if errors.As(err, &rpcErr) {
		if classified := agyRefusal(rpcErr, model); classified != nil {
			return classified
		}
	}
	return &translate.UpstreamError{Reason: "Antigravity could not answer this request. Try again; if it keeps failing, run /neoswitch:status, or run /neoswitch:off to continue this session on Claude."}
}

// agy reports why Google refused a request in the text of its JSON-RPC error. These patterns sort the reasons Claude Code must handle differently: a used-up quota, which retrying cannot fix; a rate limit, which passes; a conversation too long for the model, which compaction fixes.
var (
	agyQuotaPattern     = regexp.MustCompile(`(?i)exhausted your quota|quota (?:has been |was )?exceeded|exceeded your (?:current )?quota|out of credits|usage limit reached|insufficient credits`)
	agyRateLimitPattern = regexp.MustCompile(`(?i)resource[_ ]exhausted|rate[ _-]?limit|too many requests|\b429\b`)
	agyTooLongPattern   = regexp.MustCompile(`(?i)prompt is too long|too many tokens|context (?:window|length)|input token count .*exceeds|exceeds the maximum`)
)

// agyRefusal turns an agy error that says why Google refused the request into the error Claude Code should show, or returns nil. The reason is quoted in agy's own words, which can name when a quota resets; it is taken from the error message, or from a string in its data, never the JSON itself.
func agyRefusal(rpcErr *acp.RequestError, model string) error {
	return refusalFor(append([]string{rpcErr.Message}, jsonStrings(rpcErr.Data)...), model)
}

// refusalFor is agyRefusal for the reasons agy gave, in its own words.
func refusalFor(reasons []string, model string) error {
	find := func(pattern *regexp.Regexp) string {
		for _, reason := range reasons {
			if pattern.MatchString(reason) {
				return shorten(strings.TrimSpace(reason), 300)
			}
		}
		return ""
	}
	if reason := find(agyQuotaPattern); reason != "" {
		return &translate.UnavailableError{Message: fmt.Sprintf("Your Google account has used up its Antigravity quota for %s. Antigravity said: %q. Wait until the quota resets, or run /neoswitch:off to continue this session on Claude.", model, reason)}
	}
	if reason := find(agyTooLongPattern); reason != "" {
		return &translate.UnavailableError{Message: fmt.Sprintf("prompt is too long: Antigravity said: %q. Run /compact to shorten the conversation.", reason)}
	}
	if reason := find(agyRateLimitPattern); reason != "" {
		return &translate.RateLimitError{Message: fmt.Sprintf("Antigravity is limiting how many requests your Google account can send. Antigravity said: %q. Wait a minute and try again, or run /neoswitch:off to continue this session on Claude.", reason)}
	}
	return nil
}

// agyErrorAnswer matches an answer that is nothing but the error line agy prints when Google refused the turn, such as "⚠ RESOURCE_EXHAUSTED (code 429): Resource has been exhausted (e.g. check quota).". agy ends such a turn normally (stop reason end_turn) and sends the line as the agent's message, not as a JSON-RPC error, so without this check it would reach Claude Code as the model's answer: never retried, and kept in the conversation as if the model had said it.
// agyErrorMark opens agy's error line; a chunk may end inside its bytes.
const agyErrorMark = "⚠"

var agyErrorAnswer = regexp.MustCompile(`(?s)^⚠\x{FE0F}?\s*(.*?\(code (\d{3})\).*)$`)

// agyAnswerError returns the error an answer made of agy's error line stands for, or nil for any other answer. The reason is classified like a JSON-RPC error first; the HTTP code agy names decides the rest, so a refusal that names no known reason is still retried when Google said it would pass.
func agyAnswerError(answer, model string) error {
	match := agyErrorAnswer.FindStringSubmatch(strings.TrimSpace(answer))
	if match == nil {
		return nil
	}
	reason := shorten(strings.TrimSpace(match[1]), 300)
	acpLogger.Warn("Antigravity answered with an error instead of an answer", "reason", reason)
	if classified := refusalFor([]string{reason}, model); classified != nil {
		return classified
	}
	code, _ := strconv.Atoi(match[2])
	switch {
	case code == http.StatusTooManyRequests:
		return &translate.RateLimitError{Message: fmt.Sprintf("Antigravity is limiting how many requests your Google account can send. Antigravity said: %q. Wait a minute and try again, or run /neoswitch:off to continue this session on Claude.", reason)}
	case code >= 500:
		return &translate.RetryableError{Message: fmt.Sprintf("Google could not answer this request for now. Antigravity said: %q. Try again in a moment, or run /neoswitch:off to continue this session on Claude.", reason)}
	default:
		return &translate.UnavailableError{Message: fmt.Sprintf("Google refused this request. Antigravity said: %q. Run /neoswitch:status to check the account, or run /neoswitch:off to continue this session on Claude.", reason)}
	}
}

// jsonStrings returns the string values in a JSON document, in order.
func jsonStrings(raw json.RawMessage) []string {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case string:
			out = append(out, v)
		case []any:
			for _, item := range v {
				walk(item)
			}
		case map[string]any:
			keys := make([]string, 0, len(v))
			for key := range v {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				walk(v[key])
			}
		}
	}
	walk(value)
	return out
}

// shorten cuts s to at most n characters, marking the cut.
func shorten(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// unguardedError is what every request gets once agy ran a tool without the tool guard.
var unguardedError = &translate.UnavailableError{Message: "NeoSwitch stopped using Antigravity in this session: Antigravity ran one of its own tools without going through NeoSwitch's tool guard, which keeps every tool in Claude Code under its permission rules. Update the NeoSwitch plugin, or run /neoswitch:off to continue this session on Claude."}

// checkGuard compares the tool calls agy reported for a session with the decisions the tool guard recorded for it. Fewer decisions mean agy ran a tool without asking the guard; the Antigravity route then stops for the rest of the daemon's life, as it can no longer keep tools in Claude Code.
func (a *AntigravityACP) checkGuard(c *acpConn, s *acpSession) error {
	calls := s.toolCallCount()
	if calls == 0 {
		return nil
	}
	if decisions := guardDecisions(c.workspace, string(s.id)); decisions < calls {
		a.unguarded.Store(true)
		acpLogger.Error("Antigravity ran a tool without NeoSwitch's tool guard; the Antigravity route is stopped", "error", errUnguarded, "tool_calls", calls, "decisions", decisions, "workspace", c.workspace)
		a.discard(c)
		return unguardedError
	}
	return nil
}

// acpUnavailableMessage is what Claude Code shows when agy cannot serve any request: it did not start, did not finish the handshake, exited, or asks for a sign-in.
const acpUnavailableMessage = "NeoSwitch could not start Google's Antigravity command-line tool (agy). Run /neoswitch:login, which installs or repairs it, or run /neoswitch:off to continue this session on Claude."

// acpRetryMessage is shown only when every retry of a transient agy failure failed.
const acpRetryMessage = "Google's Antigravity command-line tool (agy) stopped or did not start in time, and NeoSwitch could not get it answering again. Try again in a moment; if it keeps happening, run /neoswitch:status, or run /neoswitch:off to continue this session on Claude."

// unavailable logs the cause, which can hold raw JSON-RPC text, and returns the plain error the proxy reports. While a sign-in by code waits, the message gives its link and the step that finishes it.
// retryable reports an agy failure the next request is likely to fix: agy crashed, or its handshake timed out on a cold start. Claude Code retries it; answering it as unavailable would stop the session on a hiccup.
func (a *AntigravityACP) retryable(cause error) error {
	acpLogger.Warn("Antigravity failed, retrying", "error", cause)
	return &translate.RetryableError{Message: acpRetryMessage}
}

func (a *AntigravityACP) unavailable(cause error) error {
	acpLogger.Warn("Antigravity unavailable", "error", cause)
	return &translate.UnavailableError{Message: acpUnavailableMessage}
}

// signedOut is unavailable for an agy that refused a request for want of a sign-in. When a sign-in waits for its code, the message carries the link: a session routed to a signed-out agy cannot answer, so its model can never show the user that link, while Claude Code shows this message as it is.
func (a *AntigravityACP) signedOut(cause error) error {
	if a.PendingSignIn != nil {
		if link := a.PendingSignIn(); link != "" {
			acpLogger.Warn("Antigravity signed out", "error", cause)
			return &translate.UnavailableError{Message: "Antigravity cannot answer until you finish signing in. Open this link in a browser on any device, sign in with Google, then run /neoswitch:login followed by the code the page shows:\n\n" + link}
		}
	}
	return a.unavailable(cause)
}

// connection returns the running agy, starting and initializing one when there is none or the previous one exited.
func (a *AntigravityACP) connection(ctx context.Context) (*acpConn, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn != nil {
		select {
		case <-a.conn.conn.Done():
			_ = a.conn.close()
			a.conn = nil
		default:
			if a.SignedInAt == nil || !a.conn.started.Before(a.SignedInAt()) {
				a.conn.acquire()
				return a.conn, nil
			}
			// Requests already on it finish with the sign-in they started with.
			a.conn.retire()
			a.conn = nil
		}
	}
	c, err := startACP(a.path, a.agent, a.cwd)
	if err != nil {
		return nil, a.unavailable(err)
	}
	init, err := c.initialize(ctx, acp.ClientCapabilities{})
	if err != nil {
		_ = c.close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("initialize Antigravity ACP: %w", ctxErr)
		}
		return nil, a.retryable(fmt.Errorf("initialize Antigravity ACP: %w", err))
	}
	c.imagePrompts = init.AgentCapabilities.PromptCapabilities.Image
	c.systemPromptAppend = supportsSystemPromptAppend(init)
	a.conn = c
	c.acquire()
	return c, nil
}

// supportsSystemPromptAppend reports whether agy advertised _meta.antigravity.systemPrompt.append: true in its initialize response (on agentCapabilities._meta or top-level _meta).
func supportsSystemPromptAppend(init acp.InitializeResponse) bool {
	check := func(meta acp.Meta) bool {
		var ag struct {
			SystemPrompt struct {
				Append bool `json:"append"`
			} `json:"systemPrompt"`
		}
		return meta.Get("antigravity", &ag) == nil && ag.SystemPrompt.Append
	}
	return check(init.AgentCapabilities.Meta) || check(init.Meta)
}

// discard stops c if it is still the current connection; requests already holding it fail and the next request starts a new agy.
func (a *AntigravityACP) discard(c *acpConn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn == c {
		a.conn = nil
	}
	_ = c.close()
}

// acpInitializeTimeout bounds the ACP handshake with a new agy. A working agy answers it at once; one that hangs (a broken update, a prompt waiting for input) would otherwise hold the request silently for the whole upstream timeout. Generation itself is not limited by it.
var acpInitializeTimeout = 30 * time.Second

const (
	// acpAuthRequired is the ACP error code for a request that needs the user to authenticate first.
	acpAuthRequired = -32000
	// acpCloseTimeout bounds the session/close and session/cancel messages sent after a request has ended.
	acpCloseTimeout = 5 * time.Second
	// acpStuckAfter is how long agy must have been silent on every session before a timed-out request restarts it.
	acpStuckAfter = time.Minute
	// acpThoughtLevel is agy's session config option for reasoning depth (low, medium, high, xhigh).
	acpThoughtLevel acp.SessionConfigId = "thought_level"
	// acpModel is agy's session config option for the model that answers the session.
	acpModel acp.SessionConfigId = "model"
)

// offersConfigValue reports whether a session's select option id offers value.
func offersConfigValue(options []acp.SessionConfigOption, id acp.SessionConfigId, value acp.SessionConfigValueId) bool {
	for _, choice := range configValues(options, id) {
		if choice == value {
			return true
		}
	}
	return false
}

// modelIDs returns the models a new session offers, in agy's order.
func modelIDs(options []acp.SessionConfigOption) []string {
	values := configValues(options, acpModel)
	models := make([]string, len(values))
	for i, value := range values {
		models[i] = string(value)
	}
	return models
}

// ModelNotOfferedMessage says that the account's Antigravity plan lacks model, naming the models it has and how to pick one.
func ModelNotOfferedMessage(model string, offered []string) string {
	return fmt.Sprintf("Your Google account's Antigravity plan does not offer %s, so Antigravity cannot answer with it. It offers %s. To use one of them, set GEMINI_MODEL to it before starting Claude Code.", model, strings.Join(offered, ", "))
}

// configValues returns the values a session's select option id offers, grouped or not, in the order the agent lists them.
func configValues(options []acp.SessionConfigOption, id acp.SessionConfigId) []acp.SessionConfigValueId {
	var values []acp.SessionConfigValueId
	for _, option := range options {
		if option.Select == nil || option.Select.Id != id {
			continue
		}
		if ungrouped := option.Select.Options.Ungrouped; ungrouped != nil {
			for _, choice := range *ungrouped {
				values = append(values, choice.Value)
			}
		}
		if grouped := option.Select.Options.Grouped; grouped != nil {
			for _, group := range *grouped {
				for _, choice := range group.Options {
					values = append(values, choice.Value)
				}
			}
		}
	}
	return values
}

// acpLogger receives the SDK's connection diagnostics, which end up in the daemon log. Some of them carry the raw JSON-RPC line, which can hold prompt or answer text; that attribute is dropped because NeoSwitch logs no conversation content.
var acpLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
	Level: slog.LevelWarn,
	ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
		if attr.Key == "raw" {
			return slog.Attr{}
		}
		return attr
	},
}))

// acpConn is one running `agy acp` process and the client side of its ACP connection.
type acpConn struct {
	cmd *exec.Cmd
	// workspace is the folder agy runs in, which holds the tool guard's records.
	workspace    string
	conn               *acp.ClientSideConnection
	imagePrompts       bool
	systemPromptAppend bool

	started time.Time

	mu    sync.Mutex
	sinks map[acp.SessionId]func(acp.SessionUpdate)
	// idle holds answered sessions waiting for the next request of their conversation.
	idle []*acpSession
	// active counts the requests using the connection; a retired one closes when the last of them ends.
	active  int
	retired bool
	// lastUpdate is when agy last sent a session update for any session, in Unix nanoseconds; zero means never.
	lastUpdate atomic.Int64

	closeOnce sync.Once
	closeErr  error
}

var _ acp.Client = (*acpConn)(nil)

// acpEnv is the environment agy runs with: NeoSwitch's own, minus Claude Code's variables. agy signs in through its own keyring profile and never needs them, and they can hold an Anthropic API key or the token of the Claude Code session that ran /neoswitch:login.
func acpEnv(environ []string) []string {
	env := make([]string, 0, len(environ))
	for _, kv := range environ {
		if strings.HasPrefix(kv, "ANTHROPIC_") || strings.HasPrefix(kv, "CLAUDE_") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func startACP(path, agent, cwd string) (*acpConn, error) {
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		return nil, fmt.Errorf("prepare the Antigravity workspace: %w", err)
	}
	if err := installToolGuard(cwd); err != nil {
		return nil, fmt.Errorf("install the Antigravity tool guard: %w", err)
	}
	// No --dangerously-skip-permissions: agy's own tools must ask through RequestPermission, which always refuses. Tools run only in Claude Code, under its permission rules.
	cmd := exec.Command(path, "acp", "--agent", agent)
	cmd.Dir = cwd
	cmd.Env = acpEnv(os.Environ())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("start Antigravity ACP: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("start Antigravity ACP: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Antigravity ACP: %w", err)
	}
	c := &acpConn{cmd: cmd, workspace: cwd, started: time.Now(), sinks: map[acp.SessionId]func(acp.SessionUpdate){}}
	c.conn = acp.NewClientSideConnection(c, stdin, stdout)
	c.conn.SetLogger(acpLogger)
	return c, nil
}

// initialize performs the ACP handshake within acpInitializeTimeout.
func (c *acpConn) initialize(ctx context.Context, capabilities acp.ClientCapabilities) (acp.InitializeResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, acpInitializeTimeout)
	defer cancel()
	return c.conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion:    acp.ProtocolVersionNumber,
		ClientCapabilities: capabilities,
		ClientInfo:         &acp.Implementation{Name: "neoswitch", Version: "1"},
	})
}

func (c *acpConn) close() error {
	c.closeOnce.Do(func() {
		if err := c.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			c.closeErr = err
		}
		_ = c.cmd.Wait()
	})
	return c.closeErr
}

// quietFor reports whether agy has sent no session update for d.
func (c *acpConn) quietFor(d time.Duration) bool {
	last := c.lastUpdate.Load()
	return last == 0 || time.Since(time.Unix(0, last)) > d
}

func (c *acpConn) subscribe(sid acp.SessionId, sink func(acp.SessionUpdate)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sinks[sid] = sink
}

func (c *acpConn) acquire() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active++
}

func (c *acpConn) release() {
	c.mu.Lock()
	c.active--
	idle := c.retired && c.active == 0
	c.mu.Unlock()
	if idle {
		_ = c.close()
	}
}

// retire closes the connection once no request uses it.
func (c *acpConn) retire() {
	c.mu.Lock()
	c.retired = true
	idle := c.active == 0
	c.mu.Unlock()
	if idle {
		_ = c.close()
	}
}

func (c *acpConn) unsubscribe(sid acp.SessionId) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sinks, sid)
}

// SessionUpdate routes an update to the request that owns the session. It runs on the SDK's single notification goroutine shared by all sessions, so sinks must not block.
func (c *acpConn) SessionUpdate(_ context.Context, n acp.SessionNotification) error {
	c.lastUpdate.Store(time.Now().UnixNano())
	if commands := n.Update.AvailableCommandsUpdate; commands != nil {
		names := make([]string, 0, len(commands.AvailableCommands))
		for _, command := range commands.AvailableCommands {
			names = append(names, command.Name)
		}
		agyCommands.add(names)
	}
	c.mu.Lock()
	sink := c.sinks[n.SessionId]
	c.mu.Unlock()
	if sink != nil {
		sink(n.Update)
	}
	return nil
}

// agy is a full coding agent with its own file, shell and web tools, and agy versions without the requested agent fall back to the default one. The prompt tells it not to use them, but that is only an instruction, so every tool permission request is rejected: Claude Code is the only place tools may run. The other client methods are refused rather than left waiting.

func (c *acpConn) RequestPermission(_ context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	return acp.RequestPermissionResponse{Outcome: rejectPermission(req.Options)}, nil
}

// rejectPermission picks the agent's own reject option, preferring a one-time rejection, and falls back to cancelling the request when the agent offers none.
func rejectPermission(options []acp.PermissionOption) acp.RequestPermissionOutcome {
	for _, kind := range []acp.PermissionOptionKind{acp.PermissionOptionKindRejectOnce, acp.PermissionOptionKindRejectAlways} {
		for _, o := range options {
			if o.Kind == kind {
				return acp.NewRequestPermissionOutcomeSelected(o.OptionId)
			}
		}
	}
	return acp.NewRequestPermissionOutcomeCancelled()
}

func (c *acpConn) ReadTextFile(context.Context, acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	return acp.ReadTextFileResponse{}, acp.NewMethodNotFound(acp.ClientMethodFsReadTextFile)
}

func (c *acpConn) WriteTextFile(context.Context, acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	return acp.WriteTextFileResponse{}, acp.NewMethodNotFound(acp.ClientMethodFsWriteTextFile)
}

func (c *acpConn) CreateElicitation(context.Context, acp.CreateElicitationRequest) (acp.CreateElicitationResponse, error) {
	return acp.CreateElicitationResponse{}, acp.NewMethodNotFound(acp.ClientMethodElicitationCreate)
}

func (c *acpConn) CompleteElicitation(context.Context, acp.CompleteElicitationNotification) error {
	return nil
}

func (c *acpConn) CreateTerminal(context.Context, acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalCreate)
}

func (c *acpConn) KillTerminal(context.Context, acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalKill)
}

func (c *acpConn) TerminalOutput(context.Context, acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalOutput)
}

func (c *acpConn) ReleaseTerminal(context.Context, acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalRelease)
}

func (c *acpConn) WaitForTerminalExit(context.Context, acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, acp.NewMethodNotFound(acp.ClientMethodTerminalWaitForExit)
}

// chunkQueue hands chunks from the SDK's notification goroutine to a streaming caller without ever blocking the former: a slow Claude Code connection must not stall updates for every other session.
type chunkQueue[T any] struct {
	mu     sync.Mutex
	chunks []T
	ready  chan struct{}
}

func newChunkQueue[T any]() *chunkQueue[T] { return &chunkQueue[T]{ready: make(chan struct{}, 1)} }

func (q *chunkQueue[T]) push(chunk T) {
	q.mu.Lock()
	q.chunks = append(q.chunks, chunk)
	q.mu.Unlock()
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

func (q *chunkQueue[T]) drain() []T {
	q.mu.Lock()
	defer q.mu.Unlock()
	chunks := q.chunks
	q.chunks = nil
	return chunks
}

// acpChunk is one streamed piece of the answer, or of the reasoning before it.
type acpChunk struct {
	thought bool
	text    string
}
