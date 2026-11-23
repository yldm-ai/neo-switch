package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yldm-ai/neo-switch/internal/daemon"
)

func TestAutoModeUnchecked(t *testing.T) {
	for _, c := range []struct {
		mode, value string
		want        bool
	}{
		{"auto", "", true},
		{"auto", "1", true},
		{"auto", "0", false},
		{"auto", " False ", false},
		{"auto", "off", false},
		{"default", "", false},
		{"acceptEdits", "", false},
		{"bypassPermissions", "", false},
		{"", "", false},
	} {
		if got := autoModeUnchecked(c.mode, c.value); got != c.want {
			t.Errorf("mode %q, %s=%q: %v", c.mode, autoModeServerEnv, c.value, got)
		}
	}
}

func TestFirstAutoModeNote(t *testing.T) {
	dir := t.TempDir()
	if !firstAutoModeNote(dir, "a") || firstAutoModeNote(dir, "a") {
		t.Fatal("the note is shown once per session")
	}
	if !firstAutoModeNote(dir, "b") {
		t.Fatal("another session sees it too")
	}
	for _, bad := range []string{"", "../a", "x/y", "."} {
		if firstAutoModeNote(dir, bad) {
			t.Fatalf("session %q", bad)
		}
	}
	old := filepath.Join(dir, "auto-mode-noted", "a")
	stale := time.Now().Add(-autoModeNotesKept - time.Hour)
	if err := os.Chtimes(old, stale, stale); err != nil {
		t.Fatal(err)
	}
	if !firstAutoModeNote(dir, "c") {
		t.Fatal("c")
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("an old mark is kept: %v", err)
	}
}

// Before a prompt, a session that Gemini answers in auto mode is told once how to get auto mode's check; other sessions and modes hear nothing.
func TestAttachBeforeAPromptNotesAutoMode(t *testing.T) {
	stateDir := isolateBackendEnv(t, true)
	id := inSession(t)
	project := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	mustOn(t, project)
	state, ok, err := daemon.LoadState(stateDir)
	if err != nil || !ok {
		t.Fatalf("state: %v %v", ok, err)
	}
	t.Setenv("ANTHROPIC_BASE_URL", state.BaseURL())
	t.Setenv(autoModeServerEnv, "")
	prompt := func(session, mode string) string {
		return stdoutOf(t, func() error {
			return attach([]string{"--quiet", "--pid", strconv.Itoa(os.Getpid()), "--session", session, "--mode", mode})
		})
	}
	if out := prompt(id, "default"); out != "" {
		t.Fatalf("default mode: %q", out)
	}
	t.Setenv(autoModeServerEnv, "0")
	if out := prompt(id, "auto"); out != "" {
		t.Fatalf("started with the server check off: %q", out)
	}
	t.Setenv(autoModeServerEnv, "")
	if out := prompt(id, "auto"); !strings.HasPrefix(out, `{"systemMessage":`) || !strings.Contains(out, autoModeServerEnv+"=0") {
		t.Fatalf("auto mode: %q", out)
	}
	if out := prompt(id, "auto"); out != "" {
		t.Fatalf("the note again: %q", out)
	}
	if out := prompt("a-session-on-claude", "auto"); out != "" {
		t.Fatalf("a session that Claude answers: %q", out)
	}
	if err := os.RemoveAll(filepath.Join(stateDir, "auto-mode-noted")); err != nil {
		t.Fatal(err)
	}
	daemon.OptOut(state, id)
	if out := prompt(id, "auto"); out != "" {
		t.Fatalf("after /neoswitch:off: %q", out)
	}
}
