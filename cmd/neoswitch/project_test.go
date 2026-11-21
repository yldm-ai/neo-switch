package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yldm-ai/neo-switch/internal/daemon"
)

// Tests run from inside Claude Code (its Bash tool) inherit the session's CLAUDECODE and CLAUDE_PID. Left set, they would make enableProject treat every test as a live session and start a daemon from the test binary for it. NEOSWITCH_PLUGIN would likewise make attach start the test binary as its background update check.
func TestMain(m *testing.M) {
	// The test binary also plays agy and the sign-in helper for the sign-in by code tests.
	// It plays the daemon too: /neoswitch:on starts it from the test binary. The binary is never run with this argument otherwise.
	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		if err := runDaemon(); err != nil {
			fmt.Fprintln(os.Stderr, "neoswitch:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if os.Getenv(fakeAgyEnv) != "" && len(os.Args) > 1 {
		switch {
		case os.Args[1] == "acp":
			runFakeAgy()
			os.Exit(0)
		case os.Args[1] == "antigravity":
			if err := antigravity(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "neoswitch:", err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	for _, key := range []string{"CLAUDECODE", "CLAUDE_PID", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_USE_GATEWAY", pluginEnv, "ANTHROPIC_BASE_URL", "ANTHROPIC_CUSTOM_HEADERS"} {
		_ = os.Unsetenv(key)
	}
	os.Exit(m.Run())
}

// inSession makes the test run as a Claude Code session, the test process standing for it, so /neoswitch:on can switch it. The daemon it starts is stopped when the test ends.
func inSession(t *testing.T) string {
	t.Helper()
	id := fmt.Sprintf("test-session-%d", time.Now().UnixNano())
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_PID", strconv.Itoa(os.Getpid()))
	t.Setenv("CLAUDE_CODE_SESSION_ID", id)
	t.Setenv("CLAUDE_CODE_USE_GATEWAY", "")
	t.Cleanup(func() {
		dir, err := daemon.Dir()
		if err != nil {
			return
		}
		if state, ok, err := daemon.LoadState(dir); err == nil && ok {
			daemon.OptOut(state, id)
			daemon.Detach(state, os.Getpid())
		}
	})
	return id
}

func settingsExist(t *testing.T, project string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(project, ".claude", "settings.local.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

// The session running /neoswitch:on applies the project settings on its next request. If NeoSwitch cannot serve it, writing them would send that request to a port nothing listens on, so the settings must stay untouched.
func TestOnInsideSessionWithoutBackendLeavesSettingsUntouched(t *testing.T) {
	isolateBackendEnv(t, false)
	inSession(t)
	project := t.TempDir()
	if err := on([]string{project}); !errors.Is(err, errNoBackend) {
		t.Fatalf("on without a backend = %v, want errNoBackend", err)
	}
	if settingsExist(t, project) {
		t.Fatal("settings were written although the session could not be served")
	}
}

// A gateway session keeps its own connection, so NeoSwitch cannot switch it, and writes nothing that would switch the project's other sessions instead.
func TestOnRefusesGatewaySession(t *testing.T) {
	isolateBackendEnv(t, false)
	inSession(t)
	t.Setenv("CLAUDE_CODE_USE_GATEWAY", "1")
	t.Setenv(backendEnv, backendGemini)
	project := t.TempDir()
	if err := on([]string{project}); err == nil || !strings.Contains(err.Error(), "gateway") {
		t.Fatalf("got %v", err)
	}
	if settingsExist(t, project) {
		t.Fatal("settings were written")
	}
}

// Switching is per session; from a terminal there is none to switch.
func TestOnOutsideClaudeCodeRefuses(t *testing.T) {
	isolateBackendEnv(t, false)
	t.Setenv(backendEnv, backendGemini)
	project := t.TempDir()
	if err := on([]string{project}); !errors.Is(err, errOutsideSession) {
		t.Fatalf("got %v", err)
	}
	if settingsExist(t, project) {
		t.Fatal("settings were written")
	}
}

func TestCallingSession(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_PID", "4242")
	if pid, inside := callingSession(); pid != 4242 || !inside {
		t.Fatalf("callingSession() = %d, %v", pid, inside)
	}
	t.Setenv("CLAUDE_PID", "")
	if pid, inside := callingSession(); pid != 0 || !inside {
		t.Fatalf("without CLAUDE_PID = %d, %v", pid, inside)
	}
	t.Setenv("CLAUDECODE", "")
	if _, inside := callingSession(); inside {
		t.Fatal("outside Claude Code must not count as a session")
	}
}
