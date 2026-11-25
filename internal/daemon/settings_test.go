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
	if err := Enable(project, s, Route{Bundle: BundlePath(s.dir)}); err != nil {
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

// A CA file and headers the user set in the project's settings are replaced while NeoSwitch routes the project (the bundle holds the CA's certificates, and the headers keep the user's lines) and come back when it stops, so a company proxy's CA is not lost by turning NeoSwitch on and off.
func TestDisablePutsBackTheUsersOwnEntries(t *testing.T) {
	s := testState(t)
	project := t.TempDir()
	path := settingsPath(project)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"env":{"NODE_EXTRA_CA_CERTS":"/etc/company-ca.pem","ANTHROPIC_CUSTOM_HEADERS":"X-Team: a"}}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	route := Route{Bundle: BundlePath(s.dir), UserHeaders: "X-Team: a"}
	for range 2 {
		// A second Enable finds NeoSwitch's values and must keep the copy of the user's.
		if err := Enable(project, s, route); err != nil {
			t.Fatal(err)
		}
	}
	env := readEnv(t, project)
	if env[envExtraCAs] != BundlePath(s.dir) || env[envHeaders] != route.Headers(s) {
		t.Fatalf("while on: %v", env)
	}
	if err := Disable(project, s); err != nil {
		t.Fatal(err)
	}
	env = readEnv(t, project)
	if env[envExtraCAs] != "/etc/company-ca.pem" || env[envHeaders] != "X-Team: a" || env[envBaseURL] != nil {
		t.Fatalf("after Disable: %v", env)
	}
	if _, err := os.Stat(replacedPath(project, s)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the copy stayed after it was put back: %v", err)
	}
}

// Without a copy of the user's values, Enable leaves the project as it was rather than lose them.
func TestEnableLeavesTheProjectAloneWhenItCannotKeepTheUsersEntries(t *testing.T) {
	s := testState(t)
	project := t.TempDir()
	path := settingsPath(project)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"env":{"NODE_EXTRA_CA_CERTS":"/etc/company-ca.pem"}}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	// A file where the folder for the copies should be.
	if err := os.WriteFile(filepath.Join(s.dir, "replaced"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Enable(project, s, Route{Bundle: BundlePath(s.dir)}); err == nil {
		t.Fatal("Enable succeeded without keeping the user's CA file")
	}
	if data, _ := os.ReadFile(path); string(data) != original {
		t.Fatalf("settings changed: %s", data)
	}
}

func readEnv(t *testing.T, project string) map[string]any {
	t.Helper()
	settings, err := readSettings(project)
	if err != nil {
		t.Fatal(err)
	}
	env, _ := settings["env"].(map[string]any)
	return env
}
