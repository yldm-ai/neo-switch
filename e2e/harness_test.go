//go:build e2e && unix

// Package e2e runs the real Claude Code CLI through NeoSwitch against a scripted Gemini upstream. It needs the `claude` executable (or CLAUDE_CLI_PATH) and never contacts a real model: the upstream is an httptest server that speaks the Gemini wire format, and Claude Code runs with a temporary HOME and config directory so it never reads the developer's configuration or credentials.
package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/yldm-ai/neo-switch/internal/provider"
	"github.com/yldm-ai/neo-switch/internal/server"
)

// claudeTimeout bounds every Claude Code run. A scripted turn takes a few seconds; the limit only catches a hang.
const claudeTimeout = 3 * time.Minute

const proxyToken = "e2e-proxy-token"

// ---- Gemini wire format ----

type geminiPart struct {
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
}

type functionCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type functionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiRequest struct {
	Contents          []geminiContent `json:"contents"`
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	Tools             []struct {
		FunctionDeclarations []struct {
			Name string `json:"name"`
		} `json:"functionDeclarations"`
	} `json:"tools,omitempty"`
}

// call is one request Claude Code caused NeoSwitch to send upstream.
type call struct {
	Method string // generateContent, streamGenerateContent or countTokens
	geminiRequest
}

// Tools returns the declared function names.
func (c *call) Tools() []string {
	var names []string
	for _, tool := range c.geminiRequest.Tools {
		for _, decl := range tool.FunctionDeclarations {
			names = append(names, decl.Name)
		}
	}
	return names
}

func (c *call) HasTool(name string) bool {
	for _, n := range c.Tools() {
		if n == name {
			return true
		}
	}
	return false
}

// System returns the system instruction text.
func (c *call) System() string {
	if c.SystemInstruction == nil {
		return ""
	}
	var b strings.Builder
	for _, p := range c.SystemInstruction.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// Text returns all text in the contents, including function call arguments and function responses, for substring checks.
func (c *call) Text() string {
	data, _ := json.Marshal(c.Contents)
	return string(data)
}

// FirstUserText returns the text of the first user content, which carries the prompt of the conversation (the user's prompt for the main agent, the task for a subagent).
func (c *call) FirstUserText() string {
	for _, content := range c.Contents {
		if content.Role != "user" {
			continue
		}
		var b strings.Builder
		for _, p := range content.Parts {
			b.WriteString(p.Text)
			b.WriteString("\n")
		}
		return b.String()
	}
	return ""
}

// Responses returns every functionResponse in the contents, in order.
func (c *call) Responses() []*functionResponse {
	var out []*functionResponse
	for _, content := range c.Contents {
		for _, p := range content.Parts {
			if p.FunctionResponse != nil {
				out = append(out, p.FunctionResponse)
			}
		}
	}
	return out
}

// Response returns the last functionResponse for the named function, or nil.
func (c *call) Response(name string) *functionResponse {
	responses := c.Responses()
	for i := len(responses) - 1; i >= 0; i-- {
		if responses[i].Name == name {
			return responses[i]
		}
	}
	return nil
}

// Calls returns every functionCall part the model content of the history replays, with its thought signature.
func (c *call) Calls() []geminiPart {
	var out []geminiPart
	for _, content := range c.Contents {
		if content.Role != "model" {
			continue
		}
		for _, p := range content.Parts {
			if p.FunctionCall != nil {
				out = append(out, p)
			}
		}
	}
	return out
}

// output returns the text a function response carries: NeoSwitch sends {"output": text} for a successful tool result and {"error": text} for an error result.
func (r *functionResponse) output() (string, bool) {
	if text, ok := r.Response["error"].(string); ok {
		return text, true
	}
	text, _ := r.Response["output"].(string)
	return text, false
}

// reply is a scripted model turn.
type reply struct {
	Parts []geminiPart
}

func text(s string) reply { return reply{Parts: []geminiPart{{Text: s}}} }

// toolCall builds a function call part carrying a thought signature, as Gemini 3+ returns them.
func toolCall(name, signature string, args map[string]any) geminiPart {
	return geminiPart{FunctionCall: &functionCall{Name: name, Args: args}, ThoughtSignature: sig(signature)}
}

func calls(parts ...geminiPart) reply { return reply{Parts: parts} }

// sig encodes a readable signature the way Gemini sends signatures: base64 bytes.
func sig(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// rejection is returned by a script to answer the request with HTTP 400, the way Gemini rejects a request (for example a replayed function call without its thought signature). The test fails as well.
type rejection struct{ reason string }

func (r rejection) Error() string { return r.reason }

func reject(format string, args ...any) error { return rejection{fmt.Sprintf(format, args...)} }

// script decides the model's turn for one main-conversation request.
type script func(c *call) (reply, error)

// ---- fake upstream ----

type upstream struct {
	t      *testing.T
	script script
	mu     sync.Mutex
	calls  []*call
}

// newUpstream starts a scripted Gemini server and an in-process NeoSwitch proxy in front of it, and returns the proxy URL.
func newUpstream(t *testing.T, s script) (*upstream, string) {
	t.Helper()
	u := &upstream{t: t, script: s}
	gemini := httptest.NewServer(u)
	t.Cleanup(gemini.Close)

	for _, name := range []string{"GOOGLE_API_KEY", "GOOGLE_GENAI_USE_VERTEXAI", "GOOGLE_GENAI_USE_ENTERPRISE", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION", "GOOGLE_CLOUD_REGION", "GOOGLE_GEMINI_BASE_URL", "GOOGLE_VERTEX_BASE_URL", "GOOGLE_GENAI_API_VERSION", "GEMINI_MODEL"} {
		t.Setenv(name, "")
	}
	t.Setenv("GEMINI_API_KEY", "test")
	t.Setenv("GEMINI_BASE_URL", gemini.URL)
	backend, err := provider.NewGeminiFromEnv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(server.NewHandler(server.Config{Backend: backend, Model: backend.Model, Token: proxyToken}))
	t.Cleanup(proxy.Close)
	return u, proxy.URL
}

func (u *upstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("x-goog-api-key") != "test" {
		u.t.Errorf("upstream request without the test API key: %s %s", r.Method, r.URL.Path)
		http.Error(w, `{"error":{"code":401,"message":"bad key","status":"UNAUTHENTICATED"}}`, http.StatusUnauthorized)
		return
	}
	path := r.URL.Path
	if r.Method == http.MethodGet && strings.HasPrefix(path, "/v1beta/models/") {
		writeJSON(w, map[string]any{"name": strings.TrimPrefix(path, "/v1beta/")})
		return
	}
	prefix := "/v1beta/models/gemini-4-neon:"
	if r.Method != http.MethodPost || !strings.HasPrefix(path, prefix) {
		u.t.Errorf("unexpected upstream request %s %s", r.Method, path)
		http.NotFound(w, r)
		return
	}
	c := &call{Method: strings.TrimPrefix(path, prefix)}
	if err := json.NewDecoder(r.Body).Decode(&c.geminiRequest); err != nil {
		u.t.Errorf("decode upstream request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	u.mu.Lock()
	u.calls = append(u.calls, c)
	u.mu.Unlock()

	switch c.Method {
	case "countTokens":
		writeJSON(w, map[string]any{"totalTokens": 42})
		return
	case "generateContent", "streamGenerateContent":
	default:
		u.t.Errorf("unexpected upstream method %q", c.Method)
		http.NotFound(w, r)
		return
	}

	var turn reply
	var err error
	if len(c.Tools()) == 0 && !strings.Contains(c.System(), "security classifier") {
		// Claude Code's own side requests (titles, summaries, classifiers) declare no tools; they are not part of the scripted conversation.
		turn = text("ok")
	} else {
		turn, err = u.script(c)
	}
	if err != nil {
		u.t.Errorf("upstream rejected request %d: %v", len(u.calls), err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": 400, "message": err.Error(), "status": "INVALID_ARGUMENT"}})
		return
	}
	usage := map[string]any{"promptTokenCount": 100, "candidatesTokenCount": 10, "totalTokenCount": 110}
	if c.Method == "generateContent" {
		writeJSON(w, map[string]any{
			"candidates":    []any{map[string]any{"content": map[string]any{"role": "model", "parts": turn.Parts}, "finishReason": "STOP"}},
			"usageMetadata": usage,
		})
		return
	}
	// Stream each part as its own chunk, the way Gemini streams, and finish in a final chunk.
	w.Header().Set("Content-Type", "text/event-stream")
	for _, p := range turn.Parts {
		writeSSE(w, map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []geminiPart{p}}}}})
	}
	writeSSE(w, map[string]any{
		"candidates":    []any{map[string]any{"content": map[string]any{"role": "model", "parts": []geminiPart{}}, "finishReason": "STOP"}},
		"usageMetadata": usage,
	})
}

// Calls returns the requests received so far.
func (u *upstream) Calls() []*call {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]*call(nil), u.calls...)
}

// MainCalls returns the generate requests that declared tools, i.e. agent turns.
func (u *upstream) MainCalls() []*call {
	var out []*call
	for _, c := range u.Calls() {
		if c.Method != "countTokens" && len(c.Tools()) > 0 {
			out = append(out, c)
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeSSE(w http.ResponseWriter, v any) {
	data, _ := json.Marshal(v)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// ---- Claude Code ----

// claudePath returns the Claude Code executable, skipping the test when there is none.
func claudePath(t *testing.T) string {
	t.Helper()
	if path := os.Getenv("CLAUDE_CLI_PATH"); path != "" {
		return path
	}
	path, err := exec.LookPath("claude")
	if err != nil {
		// CI sets NEOSWITCH_E2E_REQUIRE_CLAUDE so a broken install fails the job instead of skipping the suite.
		if os.Getenv("NEOSWITCH_E2E_REQUIRE_CLAUDE") != "" {
			t.Fatal("NEOSWITCH_E2E_REQUIRE_CLAUDE is set but `claude` is not on PATH")
		}
		t.Skip("Claude Code is not installed: put `claude` on PATH or set CLAUDE_CLI_PATH to run the end-to-end suite")
	}
	return path
}

// project is a temporary project directory with an isolated home for Claude Code.
type project struct {
	t      *testing.T
	claude string
	dir    string
	home   string
	proxy  string
}

func newProject(t *testing.T) *project {
	t.Helper()
	root := t.TempDir()
	// Resolve symlinks (macOS /var -> /private/var) so paths match the ones Claude Code reports.
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	p := &project{t: t, claude: claudePath(t), dir: filepath.Join(root, "project"), home: filepath.Join(root, "home")}
	for _, dir := range []string{p.dir, p.home} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func (p *project) path(name string) string { return filepath.Join(p.dir, name) }

func (p *project) write(name, content string) {
	p.t.Helper()
	path := p.path(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		p.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		p.t.Fatal(err)
	}
}

func (p *project) read(name string) string {
	p.t.Helper()
	data, err := os.ReadFile(p.path(name))
	if err != nil {
		p.t.Fatal(err)
	}
	return string(data)
}

// env is the complete environment of a Claude Code run. It is built from scratch, not inherited, so nothing from the developer's shell (real API keys, an enclosing Claude Code session, terminal integrations) leaks in.
func (p *project) env() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + p.home,
		"CLAUDE_CONFIG_DIR=" + filepath.Join(p.home, ".claude"),
		"XDG_CONFIG_HOME=" + filepath.Join(p.home, ".config"),
		"XDG_CACHE_HOME=" + filepath.Join(p.home, ".cache"),
		"XDG_DATA_HOME=" + filepath.Join(p.home, ".local", "share"),
		"TMPDIR=" + os.TempDir(),
		"LANG=C.UTF-8",
		"TERM=dumb",
		"SHELL=/bin/bash",
		"ANTHROPIC_BASE_URL=" + p.proxy,
		"ANTHROPIC_AUTH_TOKEN=" + proxyToken,
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		"DISABLE_AUTOUPDATER=1",
		"DISABLE_TELEMETRY=1",
		"DISABLE_ERROR_REPORTING=1",
		"CLAUDE_CODE_DISABLE_FEEDBACK_SURVEY=1",
	}
}

// result is the final message of `claude -p --output-format json`.
type result struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	IsError   bool   `json:"is_error"`
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
	NumTurns  int    `json:"num_turns"`
	// PermissionDenials lists the tool calls Claude Code refused.
	PermissionDenials []struct {
		ToolName string `json:"tool_name"`
	} `json:"permission_denials"`
}

// run runs `claude -p` in the project with the given extra arguments and returns its JSON result. It fails the test when Claude Code exits with an error or reports one.
func (p *project) run(prompt string, args ...string) result {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), claudeTimeout)
	defer cancel()
	argv := append([]string{"-p", prompt, "--output-format", "json"}, args...)
	cmd := exec.CommandContext(ctx, p.claude, argv...)
	cmd.Dir = p.dir
	cmd.Env = p.env()
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Run Claude Code in its own process group so the tools, hooks and MCP servers it starts are killed with it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 10 * time.Second
	err := cmd.Run()
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	if ctx.Err() != nil {
		p.t.Fatalf("claude did not finish within %s\nstdout: %s\nstderr: %s", claudeTimeout, stdout.String(), stderr.String())
	}
	var out result
	if decodeErr := json.Unmarshal(lastLine(stdout.Bytes()), &out); decodeErr != nil {
		p.t.Fatalf("claude output is not a JSON result (exit: %v): %v\nstdout: %s\nstderr: %s", err, decodeErr, stdout.String(), stderr.String())
	}
	if err != nil || out.IsError {
		p.t.Fatalf("claude failed (exit: %v)\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	return out
}

func lastLine(data []byte) []byte {
	var last []byte
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	for scanner.Scan() {
		if line := bytes.TrimSpace(scanner.Bytes()); len(line) > 0 {
			last = append(last[:0], line...)
		}
	}
	return last
}
