package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yldm-ai/neo-switch/internal/translate"
)

// hookDecision runs the installed hook the way agy does: its command through the shell, in the folder holding hooks.json.
func hookDecision(t *testing.T, workspace, payload string) string {
	t.Helper()
	var config map[string]struct {
		PreToolUse []struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		}
	}
	data, err := os.ReadFile(filepath.Join(workspace, ".agents", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	guard := config["neoswitch-tool-guard"].PreToolUse
	if len(guard) != 1 || guard[0].Matcher != "*" || len(guard[0].Hooks) != 1 {
		t.Fatalf("hooks.json: %s", data)
	}
	cmd := exec.Command("sh", "-c", guard[0].Hooks[0].Command)
	cmd.Dir = filepath.Join(workspace, ".agents")
	cmd.Stdin = strings.NewReader(payload)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("hook: %v", err)
	}
	var decision struct {
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal(out, &decision); err != nil {
		t.Fatalf("hook printed %q", out)
	}
	return decision.Decision
}

func TestToolGuardAllowsOnlyGoogleSearch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("agy runs hooks through cmd on Windows")
	}
	// A folder with a space, as the macOS state folder has one.
	workspace := filepath.Join(t.TempDir(), "Application Support", "agy-workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := installToolGuard(workspace); err != nil {
		t.Fatal(err)
	}
	const session = "99eb8057-ce41-4d56-8205-5b409dafea52"
	for payload, want := range map[string]string{
		`{"conversationId":"` + session + `","toolCall":{"id":"t1","name":"search_web","args":{"query":"go"}}}`:                "allow",
		`{"conversationId":"` + session + `","toolCall":{"id":"t2","name":"invoke_subagent","args":{"Prompt":"read ~/.ssh"}}}`: "deny",
		`{"conversationId":"` + session + `","toolCall":{"id":"t3","name":"read_file","args":{"name":"search_web"}}}`:          "deny",
		`{"conversationId":"` + session + `","toolCall":{"id":"t4","name":"mcp_search_web","args":{}}}`:                        "deny",
		`not json`: "deny",
		``:         "deny",
	} {
		if got := hookDecision(t, workspace, payload); got != want {
			t.Errorf("%s: %s, want %s", payload, got, want)
		}
	}
	// Every decision for the session was recorded; payloads without a session record nothing.
	if n := guardDecisions(workspace, session); n != 4 {
		t.Fatalf("recorded %d decisions, want 4", n)
	}
	// A conversation ID cannot point the hook outside its records.
	_ = hookDecision(t, workspace, `{"conversationId":"../../escape","toolCall":{"name":"read_file"}}`)
	if _, err := os.Stat(filepath.Join(workspace, "escape")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the hook wrote outside its records")
	}
	forgetDecisions(workspace, session)
	if n := guardDecisions(workspace, session); n != 0 {
		t.Fatal("a closed session's records must go")
	}
}

func TestInstallToolGuardKeepsWhatIsCurrent(t *testing.T) {
	workspace := t.TempDir()
	if err := installToolGuard(workspace); err != nil {
		t.Fatal(err)
	}
	_, sum, _ := selfExecutable()
	bin := filepath.Join(workspace, ".agents", "bin")
	if _, err := os.Stat(filepath.Join(bin, "neoswitch-hook-"+sum)); err != nil && runtime.GOOS != "windows" {
		t.Fatalf("hook binary: %v", err)
	}
	hooks := filepath.Join(workspace, ".agents", "hooks.json")
	before, _ := os.Stat(hooks)
	// An old hook binary of another version, and an old session's records.
	old := time.Now().Add(-8 * 24 * time.Hour)
	stale := filepath.Join(bin, "neoswitch-hook-0000000000000000")
	record := filepath.Join(workspace, guardRecords, "old-session")
	for _, path := range []string{stale, record} {
		_ = os.WriteFile(path, []byte("x"), 0o600)
		_ = os.Chtimes(path, old, old)
	}
	time.Sleep(10 * time.Millisecond)
	if err := installToolGuard(workspace); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Stat(hooks); !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an unchanged hooks.json must not be rewritten under a running agy")
	}
	for _, path := range []string{stale, record} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s was not pruned", path)
		}
	}
	data, _ := os.ReadFile(hooks)
	if !bytes.Contains(data, []byte(`"./bin/neoswitch-hook-`+sum+` agy-hook pre-tool"`)) && runtime.GOOS != "windows" {
		t.Fatalf("the hook must be called by a relative path: %s", data)
	}
}

func TestACPStopsWhenAgyRunsAToolWithoutTheGuard(t *testing.T) {
	guarded := newFakeACP(t, "tool-guarded")
	if resp, err := guarded.Generate(context.Background(), textRequest("hi")); err != nil || generatedText(t, resp) != "done" {
		t.Fatalf("a guarded tool call must not stop the route: %v", err)
	}

	a := newFakeACP(t, "tool-unguarded")
	_, err := a.Generate(context.Background(), textRequest("hi"))
	var unavailable *translate.UnavailableError
	if !errors.As(err, &unavailable) || !strings.Contains(unavailable.Message, "tool guard") {
		t.Fatalf("unexpected error %v", err)
	}
	// Every later request is refused before it reaches agy, even one a new agy would answer.
	t.Setenv(fakeACPEnv, "ok")
	for _, req := range []translate.Request{textRequest("again"), searchRequest(translate.WebSearch{})} {
		if req.WebSearch != nil {
			_, err = a.SearchWeb(context.Background(), req, func(string) {})
		} else {
			_, err = a.Generate(context.Background(), req)
		}
		if !errors.As(err, &unavailable) {
			t.Fatalf("later request: %v", err)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn != nil {
		t.Fatal("the agy that ran a tool without the guard must be stopped")
	}
}

// agy runs in NeoSwitch's own folder: a project's .agents rules, skills, MCP servers and hooks must not load into it.
func TestAntigravityRunsOutsideTheProject(t *testing.T) {
	state := t.TempDir()
	t.Setenv("NEOSWITCH_STATE_DIR", state)
	t.Setenv("CLAUDE_PROJECT_DIR", t.TempDir())
	t.Setenv("ANTIGRAVITY_CLI_PATH", os.Args[0])
	a, err := NewAntigravityACPFromEnv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if a.cwd != AntigravityWorkspace(state) {
		t.Fatalf("agy runs in %s", a.cwd)
	}
}
