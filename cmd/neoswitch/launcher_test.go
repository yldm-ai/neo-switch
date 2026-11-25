package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// launcher copies plugin/neoswitch.sh and the session start hook into a plugin tree of its own, on a PATH with a curl that always fails, as for a user offline right after an update, and with a Go whose builds fail when brokenGo is set (no Go otherwise). It returns a function that runs a script of the plugin folder with a cache folder of the test's choosing.
func launcher(t *testing.T, brokenGo bool) func(cache string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the plugin's launcher is a POSIX shell script")
	}
	root := t.TempDir()
	for _, dir := range []string{"plugin", "cmd", "internal"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"neoswitch.sh", "session-start.sh", "session-common.sh"} {
		script, err := os.ReadFile(filepath.Join("..", "..", "plugin", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "plugin", name), script, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("9.9.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeBin, "curl"), []byte("#!/bin/sh\nexit 22\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if brokenGo {
		goStub := "#!/bin/sh\ncase $1 in version) echo 'go version go1.0 test' ;; *) echo 'go: go.mod requires a newer go' >&2; exit 1 ;; esac\n"
		if err := os.WriteFile(filepath.Join(fakeBin, "go"), []byte(goStub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	state := t.TempDir()
	return func(cache string, args ...string) (string, string, error) {
		script := "neoswitch.sh"
		if len(args) > 0 && strings.HasSuffix(args[0], ".sh") {
			script, args = args[0], args[1:]
		}
		cmd := exec.Command(filepath.Join(root, "plugin", script), args...)
		// The hook reads a routed session's input from stdin; NeoSwitch's address in the env makes it routed.
		cmd.Stdin = strings.NewReader(`{"session_id":"s1","source":"startup"}`)
		cmd.Env = []string{"PATH=" + fakeBin + ":/usr/bin:/bin", "HOME=" + t.TempDir(), "XDG_CACHE_HOME=" + cache, "NEOSWITCH_STATE_DIR=" + state, "ANTHROPIC_BASE_URL=https://127.0.0.1:1"}
		var out, errOut strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &errOut
		err := cmd.Run()
		return out.String(), errOut.String(), err
	}
}

// cachedNeoSwitch puts a stand-in for an earlier NeoSwitch into cache/neoswitch, which reports its arguments.
func cachedNeoSwitch(t *testing.T, cache string) {
	t.Helper()
	dir := filepath.Join(cache, "neoswitch")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := "#!/bin/sh\ncase $1 in version) echo 'neoswitch v0.4.0' ;; is-newer) exit 1 ;; attach) echo '{\"systemMessage\":\"NeoSwitch v9.9.9 is available.\"}' ;; *) echo \"earlier NeoSwitch ran: $*\" ;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "neoswitch-v0.4.0-any"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
}

// When this plugin's NeoSwitch cannot be had, a NeoSwitch that ran before still does the job, so /neoswitch:off never depends on what just failed.
func TestLauncherFallsBackToTheCachedNeoSwitch(t *testing.T) {
	run := launcher(t, false)
	cache := t.TempDir()
	cachedNeoSwitch(t, cache)
	stdout, stderr, err := run(cache, "off", "/some/project")
	if err != nil {
		t.Fatalf("%v: %s", err, stderr)
	}
	if !strings.Contains(stdout, "earlier NeoSwitch ran: off /some/project") {
		t.Fatalf("stdout %q", stdout)
	}
	if !strings.Contains(stderr, "NeoSwitch fallback: NeoSwitch could not download") || !strings.Contains(stderr, "earlier neoswitch v0.4.0") {
		t.Fatalf("the fallback is not explained: %q", stderr)
	}
}

// A Go that cannot build the plugin's source is no reason to run an earlier NeoSwitch: the prebuilt release of the same version is the same program, so it is downloaded instead, and the failing build is not tried again for the same source.
func TestLauncherDownloadsWhenGoCannotBuild(t *testing.T) {
	run := launcher(t, true)
	cache := t.TempDir()
	_, stderr, _ := run(cache, "status")
	if !strings.Contains(stderr, "could not build itself with go version go1.0 test, so it uses the prebuilt NeoSwitch v9.9.9") || !strings.Contains(stderr, "downloading neoswitch_v9.9.9") {
		t.Fatalf("stderr %q", stderr)
	}
	_, stderr, _ = run(cache, "status")
	if strings.Contains(stderr, "could not build") {
		t.Fatalf("the failed build ran again: %q", stderr)
	}
}

// The session start hook shows why an earlier NeoSwitch answers: that NeoSwitch cannot know, and its own message (here, that an update is available) would leave the user thinking the update had worked.
func TestSessionStartSaysWhenAnEarlierNeoSwitchRuns(t *testing.T) {
	run := launcher(t, false)
	cache := t.TempDir()
	cachedNeoSwitch(t, cache)
	stdout, stderr, err := run(cache, "session-start.sh", "123")
	if err != nil {
		t.Fatalf("%v: %s", err, stderr)
	}
	var message struct{ SystemMessage string }
	if err := json.Unmarshal([]byte(stdout), &message); err != nil {
		t.Fatalf("not a hook message: %q (%v)", stdout, err)
	}
	if !strings.HasPrefix(message.SystemMessage, "NeoSwitch: NeoSwitch could not download") || !strings.Contains(message.SystemMessage, "earlier neoswitch v0.4.0") || !strings.HasSuffix(message.SystemMessage, "NeoSwitch v9.9.9 is available.") {
		t.Fatalf("message %q", message.SystemMessage)
	}
}

// Without any NeoSwitch, /neoswitch:off says exactly what to remove by hand and where the full removal is described, instead of failing like the session it should rescue.
func TestLauncherOffWithoutNeoSwitchSaysHowToRemoveItByHand(t *testing.T) {
	run := launcher(t, false)
	stdout, stderr, err := run(t.TempDir(), "off", "/some/project")
	if err != nil {
		t.Fatalf("%v: %s", err, stderr)
	}
	for _, want := range []string{"did not switch anything off", "/some/project/.claude/settings.local.json", "ANTHROPIC_BASE_URL", "http://127.0.0.1:", "X-NeoSwitch-Token line", "tls/bundle.pem", "put its path back", "CLAUDE_CODE_GATEWAY_HINT_HEADERS", "start a new Claude Code session", "#turn-it-off-update-or-remove"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("missing %q in %q", want, stdout)
		}
	}
}

func TestLauncherFailsOtherCommandsWithoutNeoSwitch(t *testing.T) {
	run := launcher(t, false)
	stdout, stderr, err := run(t.TempDir(), "attach", "--pid", "1")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || stdout != "" || !strings.Contains(stderr, "could not download") {
		t.Fatalf("err %v, stdout %q, stderr %q", err, stdout, stderr)
	}
}
