package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The daemon disables a project on its own after the session ends, when a worktree may be gone already: it must not bring the folder back.
func TestDisableLeavesMissingSettingsMissing(t *testing.T) {
	s := testState(t)
	project := filepath.Join(t.TempDir(), "removed-worktree")
	if err := Disable(project, s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(project); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Disable created %s: %v", project, err)
	}
}

// Claude Code writes the same file; a Disable with nothing to remove must not rewrite it.
func TestDisableWithoutNeoSwitchEntriesKeepsFile(t *testing.T) {
	s := testState(t)
	project := t.TempDir()
	path := settingsPath(project)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "{\"permissions\":{\"allow\":[\"Bash(ls)\"]}}"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := Disable(project, s); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	info, _ := os.Stat(path)
	if err != nil || string(data) != original || !info.ModTime().Equal(old) || info.Mode().Perm() != 0o644 {
		t.Fatalf("file rewritten: %q, %v, mode %v", data, err, info.Mode())
	}
}

func TestEnableThenDisableRestoresSettings(t *testing.T) {
	s := testState(t)
	project := t.TempDir()
	if err := Enable(project, s); err != nil {
		t.Fatal(err)
	}
	if on, err := Enabled(project, s); err != nil || !on {
		t.Fatalf("enabled %v, %v", on, err)
	}
	if err := Disable(project, s); err != nil {
		t.Fatal(err)
	}
	if on, err := Enabled(project, s); err != nil || on {
		t.Fatalf("after Disable: enabled %v, %v", on, err)
	}
}

// A saved port another program took is replaced by a free one; the token and backend stay, so nothing else changes.
func TestMovePortKeepsTokenAndBackend(t *testing.T) {
	dir := t.TempDir()
	s, err := SaveBackend(dir, "antigravity")
	if err != nil {
		t.Fatal(err)
	}
	moved, err := MovePort(dir, s)
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Listen == s.Listen || loaded != moved || loaded.Token != s.Token || loaded.Backend != "antigravity" {
		t.Fatalf("before %+v, moved %+v, saved %+v", s, moved, loaded)
	}
}
