package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yldm-ai/neo-switch/internal/provider"
)

// fakeAgyEnv makes the test binary act as agy: `acp` answers the ACP handshake, offers both sign-ins, and creates sessions only when signed in; `acp ... --login` is the sign-in by code, accepting fakeCode. Being signed in is the file FAKE_SIGNED_IN, written by either sign-in. FAKE_BROWSER sets the browser sign-in: "after" finishes it after FAKE_BROWSER_DELAY, "fail" fails it, and anything else leaves it waiting for a browser nobody uses.
const fakeAgyEnv = "NEOSWITCH_FAKE_AGY"

const fakeCode = "4/0Afake-code_1"

func runFakeAgy() {
	signedIn := os.Getenv("FAKE_SIGNED_IN")
	if slices.Contains(os.Args, "--login") {
		fmt.Print("Open this link in a browser to log in with Google:\n\n  https://accounts.google.com/o/oauth2/auth?state=" + os.Getenv("FAKE_LINK_STATE") + "\n\nThen paste the authorization code: ")
		code, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(code) != fakeCode {
			fmt.Println("\ncouldn't log in: token exchange failed: oauth2: \"invalid_grant\" \"Bad Request\"")
			os.Exit(1)
		}
		_ = os.WriteFile(signedIn, []byte("code\n"), 0o600)
		os.Exit(0)
	}
	out := json.NewEncoder(os.Stdout)
	var mu sync.Mutex
	reply := func(id json.RawMessage, result any, rpcErr any) {
		mu.Lock()
		defer mu.Unlock()
		msg := map[string]any{"jsonrpc": "2.0", "id": id}
		if rpcErr != nil {
			msg["error"] = rpcErr
		} else {
			msg["result"] = result
		}
		_ = out.Encode(msg)
	}
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(in.Bytes(), &msg) != nil || len(msg.ID) == 0 {
			continue
		}
		switch msg.Method {
		case "initialize":
			reply(msg.ID, map[string]any{
				"protocolVersion": 1,
				"authMethods": []any{
					map[string]any{"id": "oauth-personal", "name": "Log in with Google"},
					map[string]any{"id": "terminal-login", "name": "Log in from the terminal", "type": "terminal", "args": []string{"--login"}},
				},
			}, nil)
		case "authenticate":
			id := msg.ID
			go func() {
				switch os.Getenv("FAKE_BROWSER") {
				case "after":
					delay, _ := time.ParseDuration(os.Getenv("FAKE_BROWSER_DELAY"))
					time.Sleep(delay)
					_ = os.WriteFile(signedIn, []byte("browser\n"), 0o600)
					reply(id, map[string]any{}, nil)
				case "fail":
					reply(id, nil, map[string]any{"code": -32603, "message": "could not open a browser"})
				}
			}()
		case "session/new":
			if _, err := os.Stat(signedIn); err != nil {
				reply(msg.ID, nil, map[string]any{"code": -32000, "message": "Authentication required"})
			} else {
				result := map[string]any{"sessionId": "fake"}
				// FAKE_MODELS, comma-separated, are the models the account's plan offers.
				if models := os.Getenv("FAKE_MODELS"); models != "" {
					var choices []any
					for _, model := range strings.Split(models, ",") {
						choices = append(choices, map[string]string{"value": model, "name": model})
					}
					result["configOptions"] = []any{map[string]any{"type": "select", "id": "model", "name": "Model", "currentValue": choices[0].(map[string]string)["value"], "options": choices}}
				}
				reply(msg.ID, result, nil)
			}
		default:
			reply(msg.ID, map[string]any{}, nil)
		}
	}
}

// fakeSignInEnv points NeoSwitch at the fake agy, signed out, and a short private state directory, which a Unix socket path needs. It returns the state directory.
func fakeSignInEnv(t *testing.T) string {
	t.Helper()
	if !codeSignInSupported {
		t.Skip("sign-in by code needs Unix sockets")
	}
	dir, err := os.MkdirTemp("", "nss")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("NEOSWITCH_STATE_DIR", dir)
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("ANTIGRAVITY_CLI_PATH", os.Args[0])
	t.Setenv(fakeAgyEnv, "1")
	t.Setenv(pluginEnv, "1")
	t.Setenv("FAKE_SIGNED_IN", filepath.Join(dir, "fake-signed-in"))
	t.Setenv("FAKE_BROWSER", "")
	// No browser sign-in unless a test asks for one.
	t.Setenv("SSH_CONNECTION", "10.0.0.2 51000 10.0.0.1 22")
	t.Cleanup(func() {
		if socket, err := signInSocket(dir); err == nil {
			cancelPendingSignIn(socket)
		}
	})
	return dir
}

// withBrowser lets the sign-in also start the fake browser sign-in, set to how.
func withBrowser(t *testing.T, how string, delay time.Duration) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_CLIENT", "")
	t.Setenv("SSH_TTY", "")
	t.Setenv("DISPLAY", ":0")
	t.Setenv("FAKE_BROWSER", how)
	t.Setenv("FAKE_BROWSER_DELAY", delay.String())
}

func fakeSignedIn(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(os.Getenv("FAKE_SIGNED_IN"))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

// eventually waits for cond, which a detached helper makes true.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal(what)
}

func TestCodeSignInFinishesWithTheCode(t *testing.T) {
	dir := fakeSignInEnv(t)
	t.Setenv("FAKE_LINK_STATE", "first")
	link, err := startCodeSignIn(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if link != "https://accounts.google.com/o/oauth2/auth?state=first" {
		t.Fatalf("link %q", link)
	}
	if got := pendingSignInLink(dir); got != link {
		t.Fatalf("pending link %q", got)
	}
	if err := submitSignInCode(dir, fakeCode); err != nil {
		t.Fatal(err)
	}
	if got := fakeSignedIn(t); got != "code" {
		t.Fatalf("signed in by %q", got)
	}
	if got := pendingSignInLink(dir); got != "" {
		t.Fatalf("a finished sign-in still shows its link %q", got)
	}
	if err := submitSignInCode(dir, fakeCode); !errors.Is(err, errNoPendingSignIn) || !strings.Contains(err.Error(), "/neoswitch:login") {
		t.Fatalf("a second code must find no sign-in, got %v", err)
	}
}

// A newer sign-in replaces the one before, and the code of a rejected exchange cannot be retried: agy needs a new link.
func TestCodeSignInReplacesEarlierLinkAndReportsRejectedCode(t *testing.T) {
	dir := fakeSignInEnv(t)
	project := t.TempDir()
	t.Setenv("FAKE_LINK_STATE", "first")
	if err := antigravityLogin([]string{"--on", project}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LINK_STATE", "second")
	if err := antigravityLogin([]string{"--on", project}); err != nil {
		t.Fatal(err)
	}
	if link := pendingSignInLink(dir); !strings.HasSuffix(link, "state=second") {
		t.Fatalf("pending %q", link)
	}
	err := antigravityLogin([]string{"--on", project, "4/0Awrong"})
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("got %v", err)
	}
	if err := submitSignInCode(dir, fakeCode); !errors.Is(err, errNoPendingSignIn) {
		t.Fatalf("got %v", err)
	}
	if settingsExist(t, project) || fakeSignedIn(t) != "" {
		t.Fatal("a rejected code signed in")
	}
}

// The plugin's two steps: the link without turning the project on, as the session would then reach a signed-out agy, then the code, which finishes the sign-in and turns the project on.
func TestLoginShowsLinkThenTheCodeTurnsProjectOn(t *testing.T) {
	dir := fakeSignInEnv(t)
	project := t.TempDir()
	if err := antigravityLogin([]string{"--on", project}); err != nil {
		t.Fatal(err)
	}
	if pendingSignInLink(dir) == "" {
		t.Fatal("no sign-in waits for a code")
	}
	if settingsExist(t, project) {
		t.Fatal("the project was turned on before the sign-in finished")
	}
	if err := antigravityLogin([]string{"--on", project, fakeCode}); err != nil {
		t.Fatal(err)
	}
	if !settingsExist(t, project) {
		t.Fatal("the project was not turned on")
	}
}

// A signed-in agy turns the project on at once, and the sign-in by code beside it can switch the account or be cancelled; until either, the current sign-in stays.
func TestLoginWhenSignedInOffersSignInAgain(t *testing.T) {
	dir := fakeSignInEnv(t)
	withBrowser(t, "after", 0)
	project := t.TempDir()
	if err := os.WriteFile(os.Getenv("FAKE_SIGNED_IN"), []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := antigravityLogin([]string{"--on", project}); err != nil {
		t.Fatal(err)
	}
	if !settingsExist(t, project) {
		t.Fatal("a signed-in agy did not turn the project on")
	}
	if pendingSignInLink(dir) == "" {
		t.Fatal("no link to sign in again")
	}
	// The browser sign-in would return the current account, so it must not run: the fake's would sign in as "browser".
	time.Sleep(300 * time.Millisecond)
	if got := fakeSignedIn(t); got != "before" || !lastSignIn(dir).IsZero() {
		t.Fatalf("signed in by %q, last sign-in %v", got, lastSignIn(dir))
	}
	if err := antigravityLogin([]string{"--on", project, fakeCode}); err != nil {
		t.Fatal(err)
	}
	if got := fakeSignedIn(t); got != "code" || lastSignIn(dir).IsZero() {
		t.Fatalf("signed in by %q, last sign-in %v", got, lastSignIn(dir))
	}
}

func TestLoginCancelKeepsCurrentSignIn(t *testing.T) {
	dir := fakeSignInEnv(t)
	project := t.TempDir()
	if err := os.WriteFile(os.Getenv("FAKE_SIGNED_IN"), []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := antigravityLogin([]string{"--on", project}); err != nil {
		t.Fatal(err)
	}
	if err := antigravityLogin([]string{"--on", project, "cancel"}); err != nil {
		t.Fatal(err)
	}
	if pendingSignInLink(dir) != "" {
		t.Fatal("the cancelled link still waits")
	}
	if err := submitSignInCode(dir, fakeCode); !errors.Is(err, errNoPendingSignIn) {
		t.Fatalf("a code after cancel got %v", err)
	}
	if got := fakeSignedIn(t); got != "before" || !settingsExist(t, project) {
		t.Fatalf("signed in by %q, project on %v", got, settingsExist(t, project))
	}
	if err := antigravityLogin([]string{"cancel"}); err != nil {
		t.Fatal("cancelling with nothing waiting must not fail:", err)
	}
}

// The Stop hook: a reply that shows the waiting link exactly passes, a changed one is sent back with the exact link, and a second changed one leaves the exact link to the user.
func TestCheckRelayedLink(t *testing.T) {
	dir := fakeSignInEnv(t)
	link, err := startCodeSignIn(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	garbled := strings.Replace(link, "/o/oauth2", "/oauth2", 1) + "&redirect_uri=https%3A%2F%2Fantigravity.google"
	check := func(message string, retried bool) map[string]string {
		t.Helper()
		in, _ := json.Marshal(map[string]any{"last_assistant_message": message, "stop_hook_active": retried})
		var out strings.Builder
		if err := checkRelayedLink(strings.NewReader(string(in)), &out); err != nil {
			t.Fatal(err)
		}
		if out.Len() == 0 {
			return nil
		}
		var got map[string]string
		if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := check("Open this link:\n\n"+link+"&redirect_uri=antigravity\n\nthen paste the code.", false); got != nil {
		// The fake's link lacks agy's redirect, so the reply names it itself; the exact link is in it.
		t.Fatalf("an exact link was flagged: %v", got)
	}
	if got := check("Open this link:\n\n"+garbled, false); got["decision"] != "block" || !strings.HasSuffix(got["reason"], "\n\n"+link) {
		t.Fatalf("a changed link: %v", got)
	}
	if got := check("Here it is again:\n\n"+garbled, true); got["systemMessage"] == "" || !strings.HasSuffix(got["systemMessage"], "\n\n"+link) {
		t.Fatalf("a changed link after the retry: %v", got)
	}
	if got := check("Nothing about signing in here.", false); got != nil {
		t.Fatalf("an unrelated reply: %v", got)
	}
	cancelPendingSignIn(mustSocket(t, dir))
	if got := check("Open this link:\n\n"+garbled, false); got != nil {
		t.Fatalf("with no sign-in waiting: %v", got)
	}
}

func mustSocket(t *testing.T, dir string) string {
	t.Helper()
	socket, err := signInSocket(dir)
	if err != nil {
		t.Fatal(err)
	}
	return socket
}

// The link is shown even when a browser opens; finishing in the browser needs no code, and the helper turns the project on.
func TestBrowserSignInFinishesWithoutTheCode(t *testing.T) {
	dir := fakeSignInEnv(t)
	withBrowser(t, "after", 300*time.Millisecond)
	project := t.TempDir()
	if err := antigravityLogin([]string{"--on", project}); err != nil {
		t.Fatal(err)
	}
	if pendingSignInLink(dir) == "" {
		t.Fatal("no link was shown beside the browser")
	}
	eventually(t, "the browser sign-in did not turn the project on", func() bool { return settingsExist(t, project) })
	if got := fakeSignedIn(t); got != "browser" {
		t.Fatalf("signed in by %q", got)
	}
	eventually(t, "the finished sign-in still shows its link", func() bool { return pendingSignInLink(dir) == "" })
	if err := submitSignInCode(dir, fakeCode); !errors.Is(err, errNoPendingSignIn) || !strings.Contains(err.Error(), "browser") {
		t.Fatalf("a code after the browser must find no sign-in and say why, got %v", err)
	}
}

// A browser that cannot open leaves the code: the link keeps working.
func TestCodeWorksWhenTheBrowserFails(t *testing.T) {
	dir := fakeSignInEnv(t)
	withBrowser(t, "fail", 0)
	project := t.TempDir()
	if err := antigravityLogin([]string{"--on", project}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := antigravityLogin([]string{"--on", project, fakeCode}); err != nil {
		t.Fatal(err)
	}
	if !settingsExist(t, project) || fakeSignedIn(t) != "code" {
		t.Fatalf("turned on %v, signed in by %q", settingsExist(t, project), fakeSignedIn(t))
	}
	if pendingSignInLink(dir) != "" {
		t.Fatal("the finished sign-in still shows its link")
	}
}

// /neoswitch:off after /neoswitch:login keeps a later browser sign-in from turning the project back on.
func TestOffBeforeBrowserSignInKeepsProjectOff(t *testing.T) {
	dir := fakeSignInEnv(t)
	withBrowser(t, "after", time.Second)
	project := t.TempDir()
	if err := antigravityLogin([]string{"--on", project}); err != nil {
		t.Fatal(err)
	}
	if err := off([]string{project}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the browser sign-in did not finish", func() bool { return fakeSignedIn(t) == "browser" && pendingSignInLink(dir) == "" })
	time.Sleep(300 * time.Millisecond)
	if settingsExist(t, project) {
		t.Fatal("the browser sign-in turned the project back on after off")
	}
}

func TestSignInCode(t *testing.T) {
	for input, want := range map[string]string{
		"4/0Afake-code_1": "4/0Afake-code_1",
		" '4/0Aquoted' ":  "4/0Aquoted",
		"https://antigravity.google/oauth-callback?state=x&code=4/0Afrom-url&scope=a+b": "4/0Afrom-url",
	} {
		got, err := signInCode(input)
		if err != nil || got != want {
			t.Errorf("signInCode(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"", "two words", "remote", "help", "cancelled", "https://antigravity.google/oauth-callback?state=x", "4/0A\x1b[2J"} {
		if _, err := signInCode(input); err == nil {
			t.Errorf("signInCode(%q) accepted", input)
		}
	}
}

func TestRemoteSession(t *testing.T) {
	for _, key := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY", "DISPLAY", "WAYLAND_DISPLAY"} {
		t.Setenv(key, "")
	}
	t.Setenv("DISPLAY", ":0")
	if remoteSession() {
		t.Fatal("a local desktop session is not remote")
	}
	t.Setenv("SSH_CONNECTION", "10.0.0.2 51000 10.0.0.1 22")
	if !remoteSession() {
		t.Fatal("an SSH session is remote")
	}
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("DISPLAY", "")
	if want := runtime.GOOS != "darwin" && runtime.GOOS != "windows"; remoteSession() != want {
		t.Fatalf("without a display, remote = %v on %s", !want, runtime.GOOS)
	}
}

func TestSignInSocketLocation(t *testing.T) {
	if !codeSignInSupported {
		t.Skip("sign-in by code needs Unix sockets")
	}
	dir, err := os.MkdirTemp("", "nss")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	runtimeDir := filepath.Join(dir, "run")
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	old := runtimeOS
	t.Cleanup(func() { runtimeOS = old })

	runtimeOS = func() string { return "linux" }
	if got, err := signInSocket(dir); err != nil || got != filepath.Join(runtimeDir, "neoswitch-sign-in.sock") {
		t.Fatalf("linux: %q, %v", got, err)
	}
	runtimeOS = func() string { return "darwin" }
	if got, err := signInSocket(dir); err != nil || got != filepath.Join(dir, "sign-in.sock") {
		t.Fatalf("darwin: %q, %v", got, err)
	}
	if _, err := signInSocket(filepath.Join(dir, strings.Repeat("x", 100))); err == nil || !strings.Contains(err.Error(), "NEOSWITCH_STATE_DIR") {
		t.Fatalf("a long path must be refused clearly, got %v", err)
	}
	// Another user who can enter the folder could sign this user's agy in to their own account.
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := signInSocket(dir); err == nil || !strings.Contains(err.Error(), "chmod 700") {
		t.Fatalf("an open folder must be refused, got %v", err)
	}
}

// A browser sign-in that finishes after the session that asked for it closed signs in, but leaves the project on Claude: routing lasts as long as the session that wanted it.
func TestBrowserSignInAfterSessionClosedKeepsProjectOff(t *testing.T) {
	dir := fakeSignInEnv(t)
	withBrowser(t, "after", 500*time.Millisecond)
	project := t.TempDir()
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_PID", strconv.Itoa(gone.Process.Pid))
	if err := antigravityLogin([]string{"--on", project}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the browser sign-in did not finish", func() bool { return fakeSignedIn(t) == "browser" && pendingSignInLink(dir) == "" })
	time.Sleep(300 * time.Millisecond)
	if settingsExist(t, project) {
		t.Fatal("a closed session's project was turned on")
	}
}

// An account whose Antigravity plan lacks the model would fail every request of the project, so signing in leaves the project on Claude and says why, whether agy was signed in already or the sign-in just finished.
func TestLoginKeepsProjectOffWhenPlanLacksModel(t *testing.T) {
	dir := fakeSignInEnv(t)
	t.Setenv("FAKE_MODELS", "gemini-3.8-flash,claude-sonnet-4-6")
	project := t.TempDir()
	if err := os.WriteFile(os.Getenv("FAKE_SIGNED_IN"), []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := antigravityLogin([]string{"--on", project}); err != nil {
		t.Fatal(err)
	}
	if settingsExist(t, project) {
		t.Fatal("signed in already: the project was turned on without the model")
	}
	if err := antigravityLogin([]string{"--on", project, fakeCode}); err != nil {
		t.Fatal(err)
	}
	if settingsExist(t, project) || pendingSignInLink(dir) != "" {
		t.Fatal("after a sign-in by code: the project was turned on without the model")
	}
	if problem := modelNotOffered(nil); !strings.Contains(problem, "gemini-3.8-flash, claude-sonnet-4-6") || !strings.Contains(problem, "GEMINI_MODEL") {
		t.Fatalf("explanation %q", problem)
	}
	t.Setenv("FAKE_MODELS", "gemini-3.8-flash,"+provider.DefaultModel)
	if err := antigravityLogin([]string{"--on", project}); err != nil {
		t.Fatal(err)
	}
	if !settingsExist(t, project) {
		t.Fatal("a plan with the model did not turn the project on")
	}
}
