package provider

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/translate"
)

// AntigravityACP uses the official agy ACP server as the upstream model session. It never reads Antigravity credentials: agy resolves its own OAuth profile from the platform keyring.
//
// One agy process serves every request. Each request gets its own ACP session, and agy runs sessions concurrently, so parallel Claude Code requests (subagents, background calls, several sessions sharing the daemon) do not queue behind each other.
type AntigravityACP struct {
	path  string
	agent string
	model string
	cwd   string

	mu   sync.Mutex
	conn *acpConn

	// promptOverhead is how many input tokens agy adds around every prompt (its own agent instructions, about 14k for bare-llm), learned from the usage of earlier answers. ACP reports usage only after an answer, so estimates made before one need it.
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
		agent = "bare-llm"
	}
	cwd := strings.TrimSpace(os.Getenv("CLAUDE_PROJECT_DIR"))
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("locate Antigravity ACP workspace: %w", err)
		}
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("locate Antigravity ACP workspace: %w", err)
	}
	model := strings.TrimSpace(os.Getenv("GEMINI_MODEL"))
	if model == "" {
		model = DefaultModel
	}
	return &AntigravityACP{path: path, agent: agent, model: model, cwd: cwd}, nil
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

// Generate runs one prompt and returns the complete answer.
func (a *AntigravityACP) Generate(ctx context.Context, req translate.Request) (*genai.GenerateContentResponse, error) {
	var raw strings.Builder
	result, err := a.run(ctx, req, func(text string) { raw.WriteString(text) })
	if err != nil {
		return nil, err
	}
	parts, err := acpAnswerParts(raw.String(), result)
	if err != nil {
		return nil, err
	}
	resp := acpFinalResponse(parts, result)
	if resp.UsageMetadata == nil {
		estimate, _ := a.CountTokens(ctx, req)
		resp.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: int32(estimate)}
	}
	return resp, nil
}

// Stream relays the answer while agy generates it. Text answers are decoded out of the {"text": ...} envelope incrementally; tool calls are delivered whole in the final chunk, which also carries the finish reason and usage.
//
// ACP reports usage only when the prompt ends, but Claude Code sizes its context from the input tokens in message_start, which the first chunk becomes. That chunk therefore carries the prompt-size estimate; the final chunk carries agy's exact count.
func (a *AntigravityACP) Stream(ctx context.Context, req translate.Request) iter.Seq2[*genai.GenerateContentResponse, error] {
	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		estimate, _ := a.CountTokens(ctx, req)
		firstUsage := &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: int32(estimate)}
		// Ending the iteration early (Claude Code disconnected) cancels the prompt in agy.
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		queue := newChunkQueue()
		type outcome struct {
			result acp.PromptResponse
			err    error
		}
		done := make(chan outcome, 1)
		go func() {
			result, err := a.run(ctx, req, queue.push)
			done <- outcome{result, err}
		}()
		var decoder envelopeDecoder
		relay := func() bool {
			for _, chunk := range queue.drain() {
				delta := decoder.feed(chunk)
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
				// Every chunk is queued before the prompt returns, so draining here loses nothing.
				if !relay() {
					return
				}
				if out.err != nil {
					yield(nil, out.err)
					return
				}
				parts, err := decoder.finish(out.result)
				if err != nil {
					yield(nil, err)
					return
				}
				final := acpFinalResponse(parts, out.result)
				if final.UsageMetadata == nil {
					// No streamed chunk and no usage from agy: keep the estimate rather than report zero.
					final.UsageMetadata = firstUsage
				}
				yield(final, nil)
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

// run sends the request as a prompt in a fresh ACP session and passes each answer chunk to onText as it arrives. The session is closed afterwards; when ctx ends first, the prompt is cancelled in agy.
func (a *AntigravityACP) run(ctx context.Context, req translate.Request, onText func(string)) (acp.PromptResponse, error) {
	c, err := a.connection(ctx)
	if err != nil {
		return acp.PromptResponse{}, err
	}
	defer c.release()
	session, err := c.conn.NewSession(ctx, acp.NewSessionRequest{Cwd: a.cwd, McpServers: []acp.McpServer{}})
	if err != nil {
		return acp.PromptResponse{}, a.requestError(ctx, c, "create Antigravity ACP session", err)
	}
	if session.SessionId == "" {
		return acp.PromptResponse{}, errors.New("Antigravity ACP returned no session ID")
	}
	sid := session.SessionId
	defer func() {
		// Close with a context of its own so a cancelled request still releases its session.
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), acpCloseTimeout)
		defer cancel()
		_, _ = c.conn.CloseSession(closeCtx, acp.CloseSessionRequest{SessionId: sid})
	}()
	// agy offers each account the models of its plan. A model outside them fails every request alike, so it is reported as unavailable, which Claude Code shows as it is, rather than as an error worth retrying.
	if offered := modelIDs(session.ConfigOptions); len(offered) > 0 && !slices.Contains(offered, a.model) {
		return acp.PromptResponse{}, &translate.UnavailableError{Message: ModelNotOfferedMessage(a.model, offered) + " Run /neoswitch:off and start a new session to go back to Claude."}
	}
	if _, err := c.conn.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{ValueId: &acp.SetSessionConfigOptionValueId{
		SessionId: sid, ConfigId: acpModel, Value: acp.SessionConfigValueId(a.model),
	}}); err != nil {
		return acp.PromptResponse{}, a.requestError(ctx, c, "select Antigravity model "+a.model, err)
	}
	// Claude Code's effort picks agy's thought level, which dominates time to first token. It is only a speed and depth hint, so it is skipped when this agy does not offer the level rather than failing the request.
	if level := acp.SessionConfigValueId(strings.ToLower(string(req.ThinkingLevel))); level != "" && offersConfigValue(session.ConfigOptions, acpThoughtLevel, level) {
		if _, err := c.conn.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: sid, ConfigId: acpThoughtLevel, Value: level,
		}}); err != nil {
			return acp.PromptResponse{}, a.requestError(ctx, c, "select Antigravity thought level "+string(level), err)
		}
	}
	c.subscribe(sid, func(update acp.SessionUpdate) {
		if chunk := update.AgentMessageChunk; chunk != nil && chunk.Content.Text != nil {
			onText(chunk.Content.Text.Text)
		}
	})
	defer c.unsubscribe(sid)
	text, images := acpPrompt(req, c.imagePrompts)
	blocks := append([]acp.ContentBlock{acp.TextBlock(text)}, images...)
	result, err := c.conn.Prompt(ctx, acp.PromptRequest{SessionId: sid, Prompt: blocks})
	if err != nil {
		if ctx.Err() != nil {
			cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), acpCloseTimeout)
			_ = c.conn.Cancel(cancelCtx, acp.CancelNotification{SessionId: sid})
			cancel()
		}
		return acp.PromptResponse{}, a.requestError(ctx, c, "Antigravity ACP prompt", err)
	}
	if result.StopReason == acp.StopReasonCancelled {
		return acp.PromptResponse{}, errors.New("Antigravity ACP cancelled the prompt")
	}
	// Learn only from text-only prompts: image tokens are not part of the text estimate and would inflate the overhead.
	if result.Usage != nil && len(images) == 0 {
		if overhead := result.Usage.InputTokens - int64(promptTextTokens(req)); overhead > 0 {
			a.promptOverhead.Store(overhead)
		}
	}
	return result, nil
}

// requestError turns a failed ACP call into the error the proxy reports. A context error is returned as such so the server maps a deadline to 504. When a deadline passes while agy has produced no output for any session in acpStuckAfter, agy is treated as stuck and restarted, so one wedged process cannot fail every later request. A slow request on a working agy only cancels its own session.
//
// Other failures reach Claude Code as plain sentences: the JSON-RPC error agy sent goes to the daemon log only. An agy that exited or asks for authentication cannot answer any request, so it is reported as unavailable, which Claude Code does not retry.
func (a *AntigravityACP) requestError(ctx context.Context, c *acpConn, action string, err error) error {
	select {
	case <-c.conn.Done():
		a.discard(c)
		return a.unavailable(fmt.Errorf("%s: agy exited unexpectedly: %w", action, err))
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
	return &translate.UpstreamError{Reason: "Antigravity could not answer this request. Try again; if it keeps failing, run /neoswitch:login to sign in again."}
}

// acpUnavailableMessage is what Claude Code shows when agy cannot serve any request: it did not start, did not finish the handshake, exited, or asks for a sign-in.
const acpUnavailableMessage = "Antigravity is not responding, possibly because you are signed out. Run /neoswitch:login to sign in again, or run /neoswitch:off and start a new session to go back to Claude."

// unavailable logs the cause, which can hold raw JSON-RPC text, and returns the plain error the proxy reports. While a sign-in by code waits, the message gives its link and the step that finishes it.
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
		return nil, a.unavailable(fmt.Errorf("initialize Antigravity ACP: %w", err))
	}
	c.imagePrompts = init.AgentCapabilities.PromptCapabilities.Image
	a.conn = c
	c.acquire()
	return c, nil
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
	cmd          *exec.Cmd
	conn         *acp.ClientSideConnection
	imagePrompts bool

	started time.Time

	mu    sync.Mutex
	sinks map[acp.SessionId]func(acp.SessionUpdate)
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
	c := &acpConn{cmd: cmd, started: time.Now(), sinks: map[acp.SessionId]func(acp.SessionUpdate){}}
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

// chunkQueue hands answer chunks from the SDK's notification goroutine to the streaming request without ever blocking the former: a slow Claude Code connection must not stall updates for every other session.
type chunkQueue struct {
	mu     sync.Mutex
	chunks []string
	ready  chan struct{}
}

func newChunkQueue() *chunkQueue { return &chunkQueue{ready: make(chan struct{}, 1)} }

func (q *chunkQueue) push(chunk string) {
	q.mu.Lock()
	q.chunks = append(q.chunks, chunk)
	q.mu.Unlock()
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

func (q *chunkQueue) drain() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	chunks := q.chunks
	q.chunks = nil
	return chunks
}
