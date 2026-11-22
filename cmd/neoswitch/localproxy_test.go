package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yldm-ai/neo-switch/internal/daemon"
)

// Sessions that reach Anthropic through another local proxy, such as an account gateway on this computer, or through `neoswitch claude` or `neoswitch serve`, name a plain-HTTP loopback address too. Only the address a NeoSwitch from before TLS wrote, with its token, is NeoSwitch's: the others must start without a false "cannot answer" and keep the project's NeoSwitch setting.
const otherLocalProxy = "http://127.0.0.1:58197"

func TestAttachLeavesOtherLocalProxiesAlone(t *testing.T) {
	isolateBackendEnv(t, false)
	state, err := daemon.EnsureState(os.Getenv("NEOSWITCH_STATE_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	if err := daemon.Enable(projectKey(project), state, daemon.Route{Bundle: daemon.BundlePath(os.Getenv("NEOSWITCH_STATE_DIR"))}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_BASE_URL", otherLocalProxy)
	// The legacy case runs last: it turns the project off, as it should.
	for _, tc := range []struct {
		name   string
		token  string
		legacy bool
	}{
		{"another local proxy", "", false},
		{"neoswitch claude", "a-token-of-its-own", false},
		{"neoswitch before TLS", state.Token, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ANTHROPIC_AUTH_TOKEN", tc.token)
			out := stdoutOf(t, func() error { return attach([]string{"--pid", "1", "--session", "local-proxy-test"}) })
			enabled, err := daemon.Enabled(projectKey(project), state)
			if err != nil {
				t.Fatal(err)
			}
			if tc.legacy {
				if !strings.Contains(out, "still points at the earlier version") || enabled {
					t.Fatalf("a session NeoSwitch routed before TLS was not told, or the project stayed on: output %q, enabled %v", out, enabled)
				}
				return
			}
			if out != "" || !enabled {
				t.Fatalf("a session of another local proxy was treated as NeoSwitch's: output %q, project still on %v", out, enabled)
			}
		})
	}
}

// A session started by a gateway launcher such as `everyapi use claude` keeps the gateway's ANTHROPIC_BASE_URL in gateway mode, while its hooks see the project's NeoSwitch address. Its requests never reach NeoSwitch, so the hook must neither attach it nor tell it that it reaches Claude through NeoSwitch.
func TestAttachLeavesGatewaySessionsAlone(t *testing.T) {
	isolateBackendEnv(t, false)
	state, err := daemon.EnsureState(os.Getenv("NEOSWITCH_STATE_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	if err := daemon.Enable(projectKey(project), state, daemon.Route{Bundle: daemon.BundlePath(os.Getenv("NEOSWITCH_STATE_DIR"))}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_BASE_URL", state.BaseURL())
	t.Setenv("CLAUDE_CODE_USE_GATEWAY", "1")
	out := stdoutOf(t, func() error { return attach([]string{"--pid", "1", "--session", "gateway-test"}) })
	if out != "" {
		t.Fatalf("a gateway session was treated as routed through NeoSwitch: output %q", out)
	}
	if _, running := daemon.Status(state); running {
		t.Fatal("a gateway session started the daemon")
	}
}

// A project switched on by a version up to 0.4.1 still holds CLAUDE_CODE_USE_GATEWAY=0, which the hook's environment shows as applied. The hook removes it, so gateway sessions that start afterwards reach their gateway, and tells this session, which keeps the override until it closes, to start again if it came through a gateway.
func TestAttachRemovesGatewayOverrideOfEarlierVersions(t *testing.T) {
	isolateBackendEnv(t, false)
	stateDir := os.Getenv("NEOSWITCH_STATE_DIR")
	state, err := daemon.EnsureState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	if err := daemon.Enable(projectKey(project), state, daemon.Route{Bundle: daemon.BundlePath(stateDir)}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projectKey(project), ".claude", "settings.local.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(path, bytes.Replace(data, []byte(`"env": {`), []byte(`"env": {"CLAUDE_CODE_USE_GATEWAY": "0", `), 1), 0o600)
	t.Setenv("ANTHROPIC_BASE_URL", state.BaseURL())
	t.Setenv("CLAUDE_CODE_USE_GATEWAY", "0")
	out := stdoutOf(t, func() error {
		return attach([]string{"--pid", strconv.Itoa(os.Getpid()), "--session", "override-test"})
	})
	t.Cleanup(func() { daemon.Detach(state, os.Getpid()) })
	if !strings.Contains(out, "turned off gateway mode") {
		t.Fatalf("the session was not told about the removed override: %q", out)
	}
	if data, _ := os.ReadFile(path); bytes.Contains(data, []byte("CLAUDE_CODE_USE_GATEWAY")) {
		t.Fatalf("override still in the project settings: %s", data)
	}
	if on, _ := daemon.Enabled(projectKey(project), state); !on {
		t.Fatal("removing the override turned the project off")
	}
}

// The plugin's hooks start NeoSwitch only for sessions it may route; another local proxy's sessions must not start it on every session start and prompt.
func TestHooksLeaveOtherLocalProxiesAlone(t *testing.T) {
	// The hook reads the token from the state file as NeoSwitch writes it.
	stateDir := t.TempDir()
	state, err := daemon.EnsureState(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		base   string
		token  string
		routed bool
	}{
		{"another local proxy", otherLocalProxy, "", false},
		{"neoswitch claude", otherLocalProxy, "a-token-of-its-own", false},
		{"a token that is a pattern", otherLocalProxy, ".*", false},
		{"neoswitch before TLS", otherLocalProxy, state.Token, true},
		{"neoswitch", state.BaseURL(), "", true},
		{"anthropic", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("sh", "-c", `. "$1"; neoswitch_routed some-session`, "sh", filepath.Join("..", "..", "plugin", "session-common.sh"))
			cmd.Env = append(os.Environ(), "NEOSWITCH_STATE_DIR="+stateDir, "CLAUDE_PROJECT_DIR="+t.TempDir(), "ANTHROPIC_BASE_URL="+tc.base, "ANTHROPIC_AUTH_TOKEN="+tc.token)
			err := cmd.Run()
			var exit *exec.ExitError
			if err != nil && !errors.As(err, &exit) {
				t.Fatal(err)
			}
			if routed := err == nil; routed != tc.routed {
				t.Fatalf("neoswitch_routed = %v, want %v", routed, tc.routed)
			}
		})
	}
}

// stdoutOf returns what run writes to stdout, where hooks put their message.
func stdoutOf(t *testing.T, run func() error) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	runErr := run()
	os.Stdout = saved
	_ = w.Close()
	var out bytes.Buffer
	_, _ = io.Copy(&out, r)
	_ = r.Close()
	if runErr != nil {
		t.Fatal(runErr)
	}
	return out.String()
}
