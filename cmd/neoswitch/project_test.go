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
		// A test of updates starts daemons of two releases from this one binary.
		if v := os.Getenv(testVersionEnv); v != "" {
			version = v
		}
		if err := runDaemon(os.Args[2:]); err != nil {
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

// testVersionEnv names the version the daemons this test binary starts report.
const testVersionEnv = "NEOSWITCH_TEST_VERSION"

// actAsVersion makes this process, and the daemons it starts, NeoSwitch v.
func actAsVersion(t *testing.T, v string) {
	t.Helper()
	released := version
	t.Cleanup(func() { version = released })
	version = v
	t.Setenv(testVersionEnv, v)
}

// An update reaches the windows already open: the first attach of the new release moves the running daemon's sessions to a daemon of that release, without turning any project back to Claude.
func TestAttachOfANewerReleaseTakesOverTheRunningDaemon(t *testing.T) {
	if !daemon.HandoverSupported() {
		t.Skip("handovers are off on this platform")
	}
	stateDir := isolateBackendEnv(t, true)
	id := inSession(t)
	project := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	actAsVersion(t, "v0.4.4")
	mustOn(t, project)
	state, ok, err := daemon.LoadState(stateDir)
	if err != nil || !ok {
		t.Fatalf("state: %v %v", ok, err)
	}
	if info, ok := daemon.Status(state); !ok || info.Version != "v0.4.4" {
		t.Fatalf("first daemon: %+v %v", info, ok)
	}

	actAsVersion(t, "v0.4.5")
	t.Setenv("ANTHROPIC_BASE_URL", state.BaseURL())
	if err := attach([]string{"--quiet", "--pid", strconv.Itoa(os.Getpid()), "--session", id}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if info, ok := daemon.StatusOf(state, id); ok && info.Version == "v0.4.5" {
			if !info.OptedIn {
				t.Fatalf("the session lost Gemini in the takeover: %+v", info)
			}
			break
		}
		if time.Now().After(deadline) {
			log, _ := os.ReadFile(filepath.Join(stateDir, "daemon.log"))
			t.Fatalf("no daemon of v0.4.5 took over; daemon.log:\n%s", log)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !settingsExist(t, project) {
		t.Fatal("the takeover turned the project back to Claude")
	}
}
