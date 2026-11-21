package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yldm-ai/neo-switch/internal/daemon"
	"github.com/yldm-ai/neo-switch/internal/provider"
)

// isolateBackendEnv clears every input resolveBackend reads, points the state at a temp dir, and makes agy available or not.
func isolateBackendEnv(t *testing.T, agyInstalled bool) string {
	t.Helper()
	for _, key := range []string{backendEnv, legacyACPEnv, pluginKeyEnv, pluginAntigravityEnv, "GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GENAI_USE_VERTEXAI", "GOOGLE_GENAI_USE_ENTERPRISE", "OPENAI_API_KEY", "OPENAI_MODEL", "OPENAI_BASE_URL"} {
		t.Setenv(key, "")
	}
	dir := t.TempDir()
	t.Setenv("NEOSWITCH_STATE_DIR", filepath.Join(dir, "state"))
	if agyInstalled {
		installAgy(t)
	} else {
		t.Setenv("ANTIGRAVITY_CLI_PATH", filepath.Join(dir, "agy"))
	}
	return filepath.Join(dir, "state")
}

func mustResolve(t *testing.T) string {
	t.Helper()
	kind, _, err := resolveBackend()
	if err != nil {
		t.Fatal(err)
	}
	return kind
}

func TestLoginChoiceWinsOverGeminiKeyInEnvironment(t *testing.T) {
	state := isolateBackendEnv(t, true)
	// A key exported for other tools, as many shells have.
	t.Setenv("GEMINI_API_KEY", "for-another-tool")
	if got := mustResolve(t); got != backendGemini {
		t.Fatalf("without a saved choice the key selects gemini, got %q", got)
	}
	if _, err := daemon.SaveBackend(state, backendAntigravity); err != nil {
		t.Fatal(err)
	}
	if got := mustResolve(t); got != backendAntigravity {
		t.Fatalf("after /neoswitch:login the saved choice must win, got %q", got)
	}
	t.Setenv(backendEnv, backendGemini)
	if got := mustResolve(t); got != backendGemini {
		t.Fatalf("%s must override the saved choice, got %q", backendEnv, got)
	}
}

func TestAutomaticBackend(t *testing.T) {
	isolateBackendEnv(t, true)
	if got := mustResolve(t); got != backendAntigravity {
		t.Fatalf("agy without Gemini credentials selects antigravity, got %q", got)
	}
	t.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "true")
	if got := mustResolve(t); got != backendGemini {
		t.Fatalf("Vertex AI credentials select gemini, got %q", got)
	}
}

func TestNoBackendExplainsBothPaths(t *testing.T) {
	isolateBackendEnv(t, false)
	if _, _, err := resolveBackend(); !errors.Is(err, errNoBackend) {
		t.Fatalf("expected errNoBackend, got %v", err)
	}
}

func TestLegacyPluginOptionsAreHonoured(t *testing.T) {
	isolateBackendEnv(t, false)
	t.Setenv(pluginKeyEnv, "saved-in-plugin-settings")
	if got := mustResolve(t); got != backendGemini {
		t.Fatalf("a key saved by an earlier plugin version selects gemini, got %q", got)
	}
	t.Setenv(pluginAntigravityEnv, "true")
	if got := mustResolve(t); got != backendAntigravity {
		t.Fatalf("the earlier OAuth toggle selects antigravity, got %q", got)
	}
}

func TestBackendCommandSavesAndClearsChoice(t *testing.T) {
	isolateBackendEnv(t, false)
	t.Setenv("GEMINI_API_KEY", "key")
	if err := backendCommand([]string{"antigravity"}); err != nil {
		t.Fatal(err)
	}
	if got := mustResolve(t); got != backendAntigravity {
		t.Fatalf("got %q", got)
	}
	if err := backendCommand([]string{"auto"}); err != nil {
		t.Fatal(err)
	}
	if got := mustResolve(t); got != backendGemini {
		t.Fatalf("auto must clear the saved choice, got %q", got)
	}
	if err := backendCommand([]string{"anthropic"}); err == nil {
		t.Fatal("an unknown backend must be rejected")
	}
	t.Setenv(backendEnv, "anthropic")
	if _, _, err := resolveBackend(); err == nil {
		t.Fatalf("an unknown %s must be rejected", backendEnv)
	}
}

func TestOpenAIIsOptInOnly(t *testing.T) {
	isolateBackendEnv(t, true)
	t.Setenv("OPENAI_API_KEY", "sk-test-not-a-real-key")
	t.Setenv("OPENAI_MODEL", "test-model")
	if got := mustResolve(t); got != backendAntigravity {
		t.Fatalf("an OpenAI key must not change automatic selection, got %q", got)
	}
	t.Setenv("GEMINI_API_KEY", "key")
	if got := mustResolve(t); got != backendGemini {
		t.Fatalf("an OpenAI key must not change automatic selection, got %q", got)
	}
	if err := backendCommand([]string{"openai"}); err != nil {
		t.Fatal(err)
	}
	if got := mustResolve(t); got != backendOpenAI {
		t.Fatalf("`neoswitch backend openai` must select openai, got %q", got)
	}
	if err := backendCommand([]string{"auto"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(backendEnv, "OpenAI")
	if got := mustResolve(t); got != backendOpenAI {
		t.Fatalf("%s=openai must select openai, got %q", backendEnv, got)
	}
}

func TestOpenAIBackendRequiresModel(t *testing.T) {
	isolateBackendEnv(t, false)
	t.Setenv(backendEnv, backendOpenAI)
	t.Setenv("OPENAI_API_KEY", "sk-test-not-a-real-key")
	_, _, _, err := configuredBackend(context.Background())
	if err == nil || !strings.Contains(err.Error(), "OPENAI_MODEL") {
		t.Fatalf("a missing OPENAI_MODEL must be reported, got %v", err)
	}
	if strings.Contains(err.Error(), "sk-test") {
		t.Fatal("the error must not echo the key")
	}
	t.Setenv("OPENAI_MODEL", "test-model")
	_, model, label, err := configuredBackend(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if model != "test-model" || label != backendLabel(backendOpenAI, "") {
		t.Fatalf("model %q label %q", model, label)
	}
}

func TestOnRefusesWithoutBackend(t *testing.T) {
	state := isolateBackendEnv(t, false)
	inSession(t)
	project := t.TempDir()
	if err := on([]string{project}); !errors.Is(err, errNoBackend) {
		t.Fatalf("expected errNoBackend, got %v", err)
	}
	for _, path := range []string{filepath.Join(project, ".claude", "settings.local.json"), filepath.Join(state, "proxy.json")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s was written: %v", path, err)
		}
	}
	if message := errNoBackend.Error(); !strings.Contains(message, "/neoswitch:login") || strings.Contains(message, "export") {
		t.Fatalf("errNoBackend must name the slash command and no shell step: %q", message)
	}
}

func TestOnExcludesSettingsFromGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	isolateBackendEnv(t, true)
	inSession(t)
	// A global excludes file on the machine running the tests must not decide the outcome.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	settings := filepath.Join(".claude", "settings.local.json")
	for _, project := range []string{repo, filepath.Join(repo, "sub", "app")} {
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := on([]string{project}); err != nil {
			t.Fatal(err)
		}
		if !isGitIgnored(project, settings) {
			t.Fatalf("%s: settings are not ignored after on", project)
		}
		// Running on again must not add the pattern twice.
		if err := on([]string{project}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/.claude/settings.local.json", "/sub/app/.claude/settings.local.json"} {
		if n := strings.Count("\n"+string(data), "\n"+want+"\n"); n != 1 {
			t.Fatalf("exclude has %q %d times:\n%s", want, n, data)
		}
	}
}

func TestStatusAnswersInPlainLines(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   func(t *testing.T, project string)
		want    []string
		notWant []string
	}{
		{
			name: "on Claude, no backend",
			want: []string{"This session uses Claude.", "/neoswitch:login"},
		},
		{
			name:  "on Claude, agy installed",
			setup: func(t *testing.T, project string) { installAgy(t) },
			want:  []string{"This session uses Claude.", "Run /neoswitch:on to answer with Gemini 4 Neon from this session's next message."},
		},
		{
			name: "switched to Gemini",
			setup: func(t *testing.T, project string) {
				installAgy(t)
				inSession(t)
				mustOn(t, project)
			},
			want: []string{"This session uses Gemini 4 Neon", "NeoSwitch is running", "/neoswitch:off to continue on Claude in this session"},
		},
		{
			name: "switched, agy removed afterwards",
			setup: func(t *testing.T, project string) {
				installAgy(t)
				inSession(t)
				mustOn(t, project)
				t.Setenv("ANTIGRAVITY_CLI_PATH", filepath.Join(t.TempDir(), "agy"))
			},
			want:    []string{"cannot reach a model", "/neoswitch:login", "/neoswitch:off"},
			notWant: []string{"Gemini 4 Neon"},
		},
		{
			name: "passing through, service not running",
			setup: func(t *testing.T, project string) {
				installAgy(t)
				state, err := daemon.EnsureState(os.Getenv("NEOSWITCH_STATE_DIR"))
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv("ANTHROPIC_BASE_URL", state.BaseURL())
			},
			want: []string{"not running, so it cannot answer", "Start a new Claude Code session"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateBackendEnv(t, false)
			t.Setenv("ANTHROPIC_BASE_URL", "")
			project := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, project)
			}
			var out bytes.Buffer
			if err := printStatus(&out, project, false); err != nil {
				t.Fatal(err)
			}
			text := out.String()
			if lines := strings.Count(text, "\n"); lines < 2 || lines > 3 {
				t.Fatalf("want two or three lines, got:\n%s", text)
			}
			for _, word := range append([]string{"127.0.0.1", "daemon", "ACP", "-> gemini"}, tc.notWant...) {
				if strings.Contains(text, word) {
					t.Fatalf("output contains %q:\n%s", word, text)
				}
			}
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Fatalf("output lacks %q:\n%s", want, text)
				}
			}
			out.Reset()
			if err := printStatus(&out, project, true); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "version:  ") {
				t.Fatalf("verbose output lacks the details:\n%s", out.String())
			}
		})
	}
}

// installAgy points NeoSwitch at the fake agy of signin_test.go, signed in to an account whose plan offers the default model, so turning a project on passes its check of agy.
func installAgy(t *testing.T) {
	t.Helper()
	t.Setenv("ANTIGRAVITY_CLI_PATH", os.Args[0])
	t.Setenv(fakeAgyEnv, "1")
	signedIn := filepath.Join(t.TempDir(), "fake-signed-in")
	if err := os.WriteFile(signedIn, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_SIGNED_IN", signedIn)
	t.Setenv("FAKE_MODELS", provider.DefaultModel)
}

func mustOn(t *testing.T, project string) {
	t.Helper()
	if err := on([]string{project}); err != nil {
		t.Fatal(err)
	}
}

func TestAttachNotice(t *testing.T) {
	agy := backendLabel(backendAntigravity, "")
	for _, tc := range []struct {
		name    string
		notice  string
		label   string
		info    daemon.Info
		running bool
		want    string
	}{
		{"success names the model", "", agy, daemon.Info{Backend: agy, Version: version}, true, "this session uses Gemini 4 Neon through your Google account (Antigravity sign-in). Ignore the Claude model name in the header. The first reply can take a while."},
		{"api key", "", "Gemini Developer API", daemon.Info{Backend: "Gemini Developer API", Version: version}, true, "through a Gemini API key"},
		{"earlier backend", "", "Gemini Developer API", daemon.Info{Backend: agy, Version: version}, true, "still share the earlier setup (your Google account (Antigravity sign-in))"},
		{"earlier version", "", agy, daemon.Info{Backend: agy, Version: "v0.0.1"}, true, "still use the earlier NeoSwitch version"},
		{"daemon without version", "", agy, daemon.Info{Backend: agy}, true, "Close all Claude Code windows"},
		{"gateway notice only", "NeoSwitch: gateway.", agy, daemon.Info{Backend: agy, Version: version}, true, "NeoSwitch: gateway."},
		{"tracked project", "", agy, daemon.Info{Backend: agy, Version: version}, true, "/neoswitch:off continues this session on Claude."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := attachNotice(tc.notice, provider.DefaultModel, tc.label, tc.info, tc.running, tc.name == "tracked project")
			// Only a daemon that tracks the project turns it off; promising it otherwise would be false.
			if tc.name != "tracked project" && strings.Contains(got, "continues this session on Claude") {
				t.Fatalf("an untracked session was promised the way back: %q", got)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if tc.notice != "" && strings.Contains(got, "this session uses") {
				t.Fatalf("a notice must not be followed by a success claim: %q", got)
			}
		})
	}
}

func TestSessionBlockedNamesNeoSwitchOnce(t *testing.T) {
	for _, problem := range []string{errNoBackend.Error(), "the proxy stopped"} {
		message := sessionBlocked(problem)
		if !strings.HasPrefix(message, "NeoSwitch") || strings.Count(message, "NeoSwitch") != 1 {
			t.Fatalf("sessionBlocked(%q) = %q, want NeoSwitch named once at the start", problem, message)
		}
	}
}

func TestSessionStartProblemsNameTheFix(t *testing.T) {
	isolateBackendEnv(t, false)
	t.Setenv(backendEnv, backendAntigravity)
	_, _, _, err := configuredBackend(context.Background())
	message := sessionBlocked(err.Error())
	for _, want := range []string{"/neoswitch:login", "/neoswitch:off"} {
		if !strings.Contains(message, want) {
			t.Fatalf("%q lacks %q", message, want)
		}
	}
	for _, jargon := range []string{"PATH", "exec", "stat ", "`neoswitch"} {
		if strings.Contains(message, jargon) {
			t.Fatalf("%q contains %q", message, jargon)
		}
	}
}

func TestLoopbackAddress(t *testing.T) {
	for listen, want := range map[string]bool{
		"127.0.0.1:8080": true, "localhost:8080": true, "[::1]:8080": true, "127.0.0.2:1": true,
		":8080": false, "0.0.0.0:8080": false, "[::]:8080": false, "192.168.1.5:8080": false, "example.com:80": false, "garbage": false,
	} {
		if got := loopbackAddress(listen); got != want {
			t.Errorf("loopbackAddress(%q) = %v, want %v", listen, got, want)
		}
	}
}

// Standalone serve must not hand the user's Google account or API key to the network without a token.
func TestServeRefusesOpenListenWithoutToken(t *testing.T) {
	isolateBackendEnv(t, true)
	t.Setenv(backendEnv, backendAntigravity)
	t.Setenv("NEOSWITCH_LISTEN", "0.0.0.0:0")
	t.Setenv("NEOSWITCH_AUTH_TOKEN", "")
	if err := serve(); err == nil || !strings.Contains(err.Error(), "NEOSWITCH_AUTH_TOKEN") {
		t.Fatalf("got %v", err)
	}
}
