//go:build e2e && unix

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yldm-ai/neo-switch/internal/provider"
	"github.com/yldm-ai/neo-switch/internal/server"
)

// fakeAgyEnv makes the test binary act as `agy acp` when NeoSwitch starts it: a scripted ACP server that answers in NeoSwitch's JSON envelope and logs every prompt it receives to the file the variable names.
const fakeAgyEnv = "NEOSWITCH_E2E_FAKE_AGY_LOG"

// fakeAgyReadPath is the file the scripted model asks Claude Code to read.
const fakeAgyReadPath = "NEOSWITCH_E2E_FAKE_AGY_READ"

func TestMain(m *testing.M) {
	if log := os.Getenv(fakeAgyEnv); log != "" {
		runFakeAgy(log, os.Getenv(fakeAgyReadPath))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// agyPrompt is one prompt the fake agy received.
type agyPrompt struct {
	Session string `json:"session"`
	Bytes   int    `json:"bytes"`
	// Kind is how the fake answered: tool (asked to read the file), final (answered from the file), part (a part of a split conversation), side (anything else, such as a session title).
	Kind string `json:"kind"`
}

// runFakeAgy serves ACP on stdin and stdout until NeoSwitch closes them.
func runFakeAgy(logPath, readPath string) {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 64<<20)
	out := json.NewEncoder(os.Stdout)
	send := func(msg map[string]any) {
		msg["jsonrpc"] = "2.0"
		_ = out.Encode(msg)
	}
	sessions := 0
	for in.Scan() {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				SessionID string `json:"sessionId"`
				Prompt    []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			} `json:"params"`
		}
		if json.Unmarshal(in.Bytes(), &msg) != nil || msg.ID == nil {
			continue
		}
		reply := func(result any) { send(map[string]any{"id": msg.ID, "result": result}) }
		switch msg.Method {
		case "initialize":
			reply(map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"promptCapabilities": map[string]bool{"image": false}}, "authMethods": []any{}})
		case "session/new":
			sessions++
			reply(map[string]any{"sessionId": "agy-session-" + strings.Repeat("x", sessions), "configOptions": []any{map[string]any{
				"type": "select", "id": "model", "name": "Model", "currentValue": provider.DefaultModel,
				"options": []any{map[string]string{"value": provider.DefaultModel, "name": provider.DefaultModel}},
			}}})
		case "session/prompt":
			var text strings.Builder
			for _, block := range msg.Params.Prompt {
				text.WriteString(block.Text)
			}
			prompt := text.String()
			kind, answer := "side", `{"text":"ok"}`
			switch {
			case strings.Contains(prompt, "Do not answer it yet"):
				kind = "part"
			case strings.Contains(prompt, "read-marker-agy"):
				kind, answer = "final", `{"text":"the file says read-marker-agy"}`
			case strings.Contains(prompt, "What does notes.txt say"):
				input, _ := json.Marshal(map[string]string{"file_path": readPath})
				kind, answer = "tool", `{"tool_use":{"name":"Read","input":`+string(input)+`}}`
			}
			if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
				_ = json.NewEncoder(f).Encode(agyPrompt{Session: msg.Params.SessionID, Bytes: len(prompt), Kind: kind})
				f.Close()
			}
			send(map[string]any{"method": "session/update", "params": map[string]any{"sessionId": msg.Params.SessionID, "update": map[string]any{
				"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": answer},
			}}})
			reply(map[string]any{"stopReason": "end_turn", "usage": map[string]any{"inputTokens": 100, "outputTokens": 10, "totalTokens": 110}})
		default:
			// session/set_config_option, session/close and the rest only need an answer.
			reply(map[string]any{})
		}
	}
}

// TestClaudeCodeThroughAntigravity drives the real Claude Code CLI through NeoSwitch's Antigravity route, with a scripted agy behind it. It pins what session reuse rests on: Claude Code sends the answer back the way NeoSwitch built it, so the tool loop continues one ACP session and sends only what is new; and Claude Code applies the hint headers setting NeoSwitch writes into the project.
func TestClaudeCodeThroughAntigravity(t *testing.T) {
	p := newProject(t)
	logPath := filepath.Join(t.TempDir(), "agy.log")
	t.Setenv(fakeAgyEnv, logPath)
	t.Setenv(fakeAgyReadPath, p.path("notes.txt"))
	t.Setenv("ANTIGRAVITY_CLI_PATH", os.Args[0])
	t.Setenv("NEOSWITCH_STATE_DIR", t.TempDir())
	t.Setenv("GEMINI_MODEL", "")
	backend, err := provider.NewAntigravityACPFromEnv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })

	var mu sync.Mutex
	classes := map[string]int{}
	handler := server.NewHandler(server.Config{Backend: backend, Model: backend.Model(), Token: proxyToken})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/messages" {
			mu.Lock()
			classes[r.Header.Get("X-Claude-Code-Request-Class")]++
			mu.Unlock()
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(proxy.Close)
	p.proxy = proxy.URL

	// The entry NeoSwitch's Enable writes, in the file it writes it to.
	p.write(".claude/settings.local.json", `{"env":{"CLAUDE_CODE_GATEWAY_HINT_HEADERS":"1"}}`)
	p.write("notes.txt", "read-marker-agy\n")
	out := p.run("What does notes.txt say?")
	if out.Result != "the file says read-marker-agy" {
		t.Fatalf("result = %q", out.Result)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var tool, final *agyPrompt
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var prompt agyPrompt
		if err := json.Unmarshal([]byte(line), &prompt); err != nil {
			t.Fatalf("agy log line %q: %v", line, err)
		}
		switch prompt.Kind {
		case "tool":
			tool = &prompt
		case "final":
			final = &prompt
		}
	}
	if tool == nil || final == nil {
		t.Fatalf("agy did not see the tool loop:\n%s", data)
	}
	if final.Session != tool.Session {
		t.Fatalf("the tool result went to a new session, so Claude Code's copy of the answer no longer matches NeoSwitch's:\n%s", data)
	}
	if final.Bytes*4 > tool.Bytes {
		t.Fatalf("the continued session read %d bytes after %d; it must get only the new message:\n%s", final.Bytes, tool.Bytes, data)
	}
	mu.Lock()
	defer mu.Unlock()
	if classes["main"] < 2 {
		t.Fatalf("request classes %v: Claude Code did not send x-claude-code-request-class from the project's settings", classes)
	}
}
