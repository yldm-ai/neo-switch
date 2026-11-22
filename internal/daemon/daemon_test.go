package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEnsureStateIsStableAndPrivate(t *testing.T) {
	dir := t.TempDir()
	first, err := EnsureState(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EnsureState(dir)
	if err != nil || second != first {
		t.Fatalf("state changed: %+v %+v %v", first, second, err)
	}
	info, err := os.Stat(statePath(dir))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode = %v %v", info.Mode(), err)
	}
}

func TestEnableAndDisablePreserveOtherSettings(t *testing.T) {
	project := t.TempDir()
	s := testState(t)
	path := settingsPath(project)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(`{"permissions":{"allow":["Bash(ls)"]},"env":{"FOO":"bar"}}`), 0o644)

	if err := Enable(project, s, Route{Bundle: BundlePath(s.dir), UserHeaders: "X-Corp: 1"}); err != nil {
		t.Fatal(err)
	}
	if on, _ := Enabled(project, s); !on {
		t.Fatal("not enabled")
	}
	var settings map[string]any
	data, _ := os.ReadFile(path)
	_ = json.Unmarshal(data, &settings)
	env := settings["env"].(map[string]any)
	// No ANTHROPIC_AUTH_TOKEN: it would replace the user's Claude login in the session.
	if env["FOO"] != "bar" || env[envBaseURL] != "https://"+s.Listen || env[envAuthToken] != nil || env[envHeaders] != "X-Corp: 1\n"+TokenHeader+": "+s.Token || env[envExtraCAs] != BundlePath(s.dir) || settings["permissions"] == nil {
		t.Fatalf("settings after enable = %s", data)
	}
	// A session started by a gateway launcher keeps its gateway: forcing it through the proxy would pass its gateway credential on to Anthropic.
	if _, present := env[EnvUseGateway]; present {
		t.Fatalf("enable must leave Claude Code gateway mode alone: %s", data)
	}

	if err := Disable(project, s); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	settings = map[string]any{}
	_ = json.Unmarshal(data, &settings)
	env = settings["env"].(map[string]any)
	if len(env) != 1 || env["FOO"] != "bar" || settings["permissions"] == nil {
		t.Fatalf("settings after disable = %s", data)
	}
}

func TestDisableLeavesForeignBaseURL(t *testing.T) {
	project := t.TempDir()
	path := settingsPath(project)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://gateway.example","ANTHROPIC_AUTH_TOKEN":"theirs"}}`), 0o644)
	if err := Disable(project, State{Listen: "127.0.0.1:1", Token: "tok"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) == "" || !json.Valid(data) {
		t.Fatal("settings destroyed")
	}
	var settings map[string]map[string]string
	_ = json.Unmarshal(data, &settings)
	if settings["env"]["ANTHROPIC_BASE_URL"] != "https://gateway.example" || settings["env"]["ANTHROPIC_AUTH_TOKEN"] != "theirs" {
		t.Fatalf("foreign gateway removed: %s", data)
	}
}

// Versions up to 0.4.1 turned Claude Code gateway mode off for the project, which sent the gateway sessions there, such as those of `everyapi use claude`, to Anthropic with the gateway's credential. Switching the project again, or the SessionStart hook through ClearGatewayOverride, removes that entry. A value the user set is kept: any value other than "0", and a "0" in a project NeoSwitch did not route.
func TestGatewayOverrideOfEarlierVersionsIsRemoved(t *testing.T) {
	s, err := EnsureState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, gateway, base string
		cleared             bool
	}{
		{"neoswitch's override", "0", s.BaseURL(), true},
		{"user's gateway mode", "1", s.BaseURL(), false},
		{"user's own 0", "0", "", false},
		{"user's own 0 with a gateway", "0", "https://gateway.example", false},
	} {
		for _, via := range []string{"enable", "clear"} {
			t.Run(tc.name+" via "+via, func(t *testing.T) {
				project := t.TempDir()
				path := settingsPath(project)
				env := map[string]string{EnvUseGateway: tc.gateway}
				if tc.base != "" {
					env[envBaseURL] = tc.base
				}
				data, _ := json.Marshal(map[string]any{"env": env})
				_ = os.MkdirAll(filepath.Dir(path), 0o755)
				_ = os.WriteFile(path, data, 0o644)
				if via == "enable" {
					if err := Enable(project, s, Route{Bundle: BundlePath(s.dir)}); err != nil {
						t.Fatal(err)
					}
				} else if cleared, err := ClearGatewayOverride(project, s); err != nil || cleared != tc.cleared {
					t.Fatalf("ClearGatewayOverride = %v, %v, want %v", cleared, err, tc.cleared)
				}
				var settings map[string]map[string]any
				data, _ = os.ReadFile(path)
				_ = json.Unmarshal(data, &settings)
				_, present := settings["env"][EnvUseGateway]
				if present == tc.cleared {
					t.Fatalf("settings after %s = %s, want the gateway entry removed: %v", via, data, tc.cleared)
				}
			})
		}
	}
}

// A project without settings has nothing to clean, and stays without a settings file.
func TestClearGatewayOverrideCreatesNoSettings(t *testing.T) {
	s, err := EnsureState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if cleared, err := ClearGatewayOverride(project, s); err != nil || cleared {
		t.Fatalf("ClearGatewayOverride = %v, %v", cleared, err)
	}
	if _, err := os.Stat(settingsPath(project)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("settings file created: %v", err)
	}
}

func TestEnableRefusesInvalidSettings(t *testing.T) {
	project := t.TempDir()
	path := settingsPath(project)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(`{"env": `), 0o644)
	if err := Enable(project, State{Listen: "127.0.0.1:1", Token: "t"}, Route{}); err == nil {
		t.Fatal("must not overwrite unparseable settings")
	}
	if data, _ := os.ReadFile(path); string(data) != `{"env": ` {
		t.Fatalf("file modified: %q", data)
	}
}

func TestInvalidSettingsNameTheLine(t *testing.T) {
	for name, tc := range map[string]struct{ content, want string }{
		"trailing comma": {"{\n  \"env\": {\n    \"A\": \"1\",\n  }\n}\n", "line 4 has a formatting mistake"},
		"truncated":      {"{\n  \"env\": ", "line 2 has a formatting mistake"},
		"not an object":  {"[1, 2]", "must hold a single JSON object"},
		"env not object": {`{"env": "x"}`, `"env" entry is not a JSON object`},
	} {
		t.Run(name, func(t *testing.T) {
			project := t.TempDir()
			path := settingsPath(project)
			_ = os.MkdirAll(filepath.Dir(path), 0o755)
			_ = os.WriteFile(path, []byte(tc.content), 0o644)
			err := Enable(project, State{Listen: "127.0.0.1:1", Token: "t"}, Route{})
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "invalid character") {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestCorruptStateNamesTheSlashCommand(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "proxy.json"), []byte("{"), 0o600)
	if _, _, err := LoadState(dir); err == nil || !strings.Contains(err.Error(), "/neoswitch:on") || strings.Contains(err.Error(), "`neoswitch") {
		t.Fatalf("got %v", err)
	}
}

func TestAttachFailsFastWhenAnotherProgramHoldsThePort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	s := State{Listen: listener.Addr().String(), Token: "t"}
	start := time.Now()
	_, err = Attach(s, 100, "", func() error { t.Fatal("must not spawn a daemon that cannot listen"); return nil })
	var busy *PortInUseError
	if !errors.As(err, &busy) || busy.Listen != s.Listen {
		t.Fatalf("got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %s", elapsed)
	}
}

type fakeProcs struct {
	mu    sync.Mutex
	alive map[int]bool
}

func (f *fakeProcs) Alive(pid int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alive[pid]
}

func (f *fakeProcs) set(pid int, alive bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alive[pid] = alive
}

func testState(t *testing.T) State {
	t.Helper()
	s, err := EnsureState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDaemonServesUntilLastSessionExits(t *testing.T) {
	s := testState(t)
	procs := &fakeProcs{alive: map[int]bool{}}
	proxy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	done := make(chan error, 1)
	spawn := func() error {
		go func() {
			done <- Run(context.Background(), s, proxy, Options{StartupGrace: time.Minute, PollInterval: 20 * time.Millisecond, Alive: procs.Alive, Version: "v9"})
		}()
		return nil
	}

	procs.set(100, true)
	procs.set(200, true)
	if _, err := Attach(s, 100, "", spawn); err != nil {
		t.Fatal(err)
	}
	if _, err := Attach(s, 200, "", func() error { t.Fatal("second attach must reuse the running daemon"); return nil }); err != nil {
		t.Fatal(err)
	}
	if info, ok := Status(s); !ok || info.Sessions != 2 || info.Version != "v9" {
		t.Fatalf("status = %+v %v", info, ok)
	}

	procs.set(100, false)
	time.Sleep(100 * time.Millisecond)
	if info, ok := Status(s); !ok || info.Sessions != 1 {
		t.Fatalf("after one exit: %+v %v", info, ok)
	}
	procs.set(200, false)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("daemon kept running after the last session exited")
	}
}

// releases records the projects a daemon released, in order.
type releases struct {
	mu   sync.Mutex
	list []string
}

func (r *releases) add(project string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.list = append(r.list, project)
}

func (r *releases) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.list)
}

func startDaemon(t *testing.T, s State, opts Options) (stop func(), done <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- Run(ctx, s, http.NotFoundHandler(), opts) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok := Status(s); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(cancel)
	return cancel, finished
}

// A project returns to Claude once no attached session uses it: at a detach right away, or at the next check for a session that died.
func TestDaemonReleasesProjectWhenItsLastSessionEnds(t *testing.T) {
	s := testState(t)
	procs := &fakeProcs{alive: map[int]bool{100: true, 200: true, 300: true}}
	var released releases
	startDaemon(t, s, Options{StartupGrace: time.Minute, PollInterval: 20 * time.Millisecond, Alive: procs.Alive, Release: released.add})
	noSpawn := func() error { t.Fatal("must reuse the running daemon"); return nil }
	for pid, project := range map[int]string{100: "/p", 200: "/p", 300: "/q"} {
		if tracked, err := AttachSession(s, Session{PID: pid, Project: project, ID: fmt.Sprint("s", pid), OptIn: true}, noSpawn); err != nil || !tracked {
			t.Fatalf("attach %d: tracked %v, %v", pid, tracked, err)
		}
	}
	// A window that only started while the settings existed does not keep them, and does not remove them either.
	if _, err := Attach(s, 400, "/p", noSpawn); err != nil {
		t.Fatal(err)
	}
	Detach(s, 400)
	Detach(s, 100)
	if got := released.get(); len(got) != 0 {
		t.Fatalf("/p released while session 200 still uses it: %v", got)
	}
	Detach(s, 200)
	if got := released.get(); !slices.Equal(got, []string{"/p"}) {
		t.Fatalf("after the last /p session detached: %v", got)
	}
	procs.set(300, false)
	time.Sleep(100 * time.Millisecond)
	if got := released.get(); !slices.Equal(got, []string{"/p", "/q"}) {
		t.Fatalf("after the /q session died: %v", got)
	}
}

// A daemon stopped from outside, as at logout, releases the projects of the sessions still attached.
func TestDaemonReleasesProjectsWhenStopped(t *testing.T) {
	s := testState(t)
	var released releases
	stop, done := startDaemon(t, s, Options{StartupGrace: time.Minute, Alive: func(int) bool { return true }, Release: released.add})
	if _, err := AttachSession(s, Session{PID: 100, Project: "/p", ID: "s100", OptIn: true}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Attach(s, 200, "", nil); err != nil {
		t.Fatal(err)
	}
	stop()
	<-done
	if got := released.get(); !slices.Equal(got, []string{"/p"}) {
		t.Fatalf("released %v", got)
	}
}

// Once the daemon decided to stop it refuses attaches, which then start a new daemon instead of joining one about to exit.
func TestClosingDaemonRefusesAttach(t *testing.T) {
	sessions := newSessionSet(t.TempDir(), func(string) {})
	if !sessions.add(attachRequest{PID: 100, Project: "/p"}) {
		t.Fatal("refused before closing")
	}
	if !sessions.prune(func(int) bool { return false }, false) {
		t.Fatal("the last session exited, yet the daemon keeps running")
	}
	if sessions.add(attachRequest{PID: 200, Project: "/p"}) {
		t.Fatal("a closing daemon took a session")
	}
}

func TestDaemonExitsWhenNobodyAttaches(t *testing.T) {
	s := testState(t)
	err := Run(context.Background(), s, http.NotFoundHandler(), Options{StartupGrace: 50 * time.Millisecond, PollInterval: 10 * time.Millisecond, Alive: func(int) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
}

func TestControlEndpointsRequireToken(t *testing.T) {
	s := testState(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Run(ctx, s, http.NotFoundHandler(), Options{StartupGrace: time.Minute}) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok := Status(s); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, ok := Status(State{Listen: s.Listen, Token: "wrong"}); ok {
		t.Fatal("status answered without the token")
	}
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL()+attachPath, strings.NewReader(`{"pid":4242}`))
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err := s.client(time.Second).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("attach with wrong token = %d", resp.StatusCode)
	}
	if info, _ := Status(s); info.Sessions != 0 {
		t.Fatalf("unauthorized attach registered a session: %+v", info)
	}
}

func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatal("own process reported dead")
	}
	if processAlive(1 << 30) {
		t.Fatal("nonexistent pid reported alive")
	}
}

func TestSaveBackendKeepsAddressAndToken(t *testing.T) {
	dir := t.TempDir()
	first, err := EnsureState(dir)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := SaveBackend(dir, "antigravity")
	if err != nil {
		t.Fatal(err)
	}
	// Enabled projects store the address and token, so recording a choice must not change them.
	if saved.Listen != first.Listen || saved.Token != first.Token {
		t.Fatalf("state changed: %+v -> %+v", first, saved)
	}
	loaded, ok, err := LoadState(dir)
	if err != nil || !ok || loaded.Backend != "antigravity" {
		t.Fatalf("loaded %+v %v %v", loaded, ok, err)
	}
	if _, err := SaveBackend(dir, ""); err != nil {
		t.Fatal(err)
	}
	if loaded, _, _ = LoadState(dir); loaded.Backend != "" {
		t.Fatalf("choice not cleared: %+v", loaded)
	}
}

// A cloned repository can commit .claude/settings.local.json.neoswitch.tmp, the temp name older versions used, as a symlink to a file of the user's. Enabling the project must not write through it.
func TestEnableDoesNotWriteThroughPlantedSymlink(t *testing.T) {
	project := t.TempDir()
	victim := filepath.Join(t.TempDir(), "authorized_keys")
	if err := os.WriteFile(victim, []byte("ssh-ed25519 AAAA user\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := settingsPath(project)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path + ".neoswitch.tmp", path + ".tmp"} {
		if err := os.Symlink(victim, name); err != nil {
			t.Fatal(err)
		}
	}
	if err := Enable(project, State{Listen: "127.0.0.1:4711", Token: "tok"}, Route{}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(victim); string(data) != "ssh-ed25519 AAAA user\n" {
		t.Fatalf("victim file was overwritten: %q", data)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("settings file: %v, %v", info, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 3 {
		t.Fatalf("want the settings file and the two planted links, got %v", entries)
	}
}

// Settings an earlier NeoSwitch wrote over plain HTTP, with its token, are cleaned up too; a foreign plain-HTTP address is not.
func TestDisableRemovesSettingsFromBeforeTLS(t *testing.T) {
	s := testState(t)
	project := t.TempDir()
	path := settingsPath(project)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(`{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:5555","ANTHROPIC_AUTH_TOKEN":"`+s.Token+`","CLAUDE_CODE_USE_GATEWAY":"0","FOO":"bar"}}`), 0o644)
	if err := Disable(project, s); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "{\n  \"env\": {\n    \"FOO\": \"bar\"\n  }\n}\n" {
		t.Fatalf("after disable: %s", data)
	}
	_ = os.WriteFile(path, []byte(`{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:5555","ANTHROPIC_AUTH_TOKEN":"someone-else"}}`), 0o644)
	if err := Disable(project, s); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "someone-else") {
		t.Fatalf("a foreign address was removed: %s", data)
	}
}

// The router: an opted-in session's model requests go to Gemini without the user's Claude credential; everything else passes through to Claude with it; a request without the token or from another host gets a 400, never a 401.
func TestRouterSendsOnlyOptedInModelRequestsToGemini(t *testing.T) {
	s := testState(t)
	var gemini, passthrough []string
	var mu sync.Mutex
	record := func(list *[]string, status int) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			*list = append(*list, r.URL.Path+" auth="+r.Header.Get("Authorization")+" token="+r.Header.Get(TokenHeader))
			mu.Unlock()
			w.WriteHeader(status)
		})
	}
	var released releases
	startDaemon(t, s, Options{StartupGrace: time.Minute, Alive: func(int) bool { return true }, Release: released.add, Passthrough: record(&passthrough, http.StatusOK)})
	// Replace the proxy: startDaemon serves NotFound; start another daemon would need another port, so route through a fresh router instead.
	sessions := newSessionSet(t.TempDir(), released.add)
	rt := &router{state: s, sessions: sessions, gemini: record(&gemini, http.StatusTeapot), passthrough: record(&passthrough, http.StatusOK)}
	sessions.add(attachRequest{PID: 100, Project: "/p", Session: "on", OptIn: true})
	send := func(method, path, session, token, host string) int {
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Host = host
		req.Header.Set("Authorization", "Bearer users-claude-login")
		if token != "" {
			req.Header.Set(TokenHeader, token)
		}
		if session != "" {
			req.Header.Set(sessionHeader, session)
		}
		rec := httptest.NewRecorder()
		rt.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := send("POST", "/v1/messages", "on", s.Token, s.Listen); code != http.StatusTeapot {
		t.Fatalf("opted-in model request: %d", code)
	}
	if code := send("POST", "/v1/messages", "other-window", s.Token, s.Listen); code != http.StatusOK {
		t.Fatalf("other session: %d", code)
	}
	if code := send("GET", "/api/oauth/profile", "on", s.Token, s.Listen); code != http.StatusOK {
		t.Fatalf("opted-in non-model request: %d", code)
	}
	for _, bad := range []struct{ token, host string }{{"wrong", s.Listen}, {"", s.Listen}, {s.Token, "evil.example:" + strings.Split(s.Listen, ":")[1]}} {
		if code := send("POST", "/v1/messages", "on", bad.token, bad.host); code != http.StatusBadRequest {
			t.Fatalf("token %q host %q: %d", bad.token, bad.host, code)
		}
	}
	mu.Lock()
	seenGemini, seenPassthrough := slices.Clone(gemini), slices.Clone(passthrough)
	mu.Unlock()
	gemini, passthrough = seenGemini, seenPassthrough
	if len(gemini) != 1 || strings.Contains(gemini[0], "users-claude-login") || strings.Contains(gemini[0], s.Token) {
		t.Fatalf("gemini saw %v", gemini)
	}
	if len(passthrough) != 2 || !strings.Contains(passthrough[0], "users-claude-login") || strings.Contains(strings.Join(passthrough, " "), s.Token) {
		t.Fatalf("passthrough saw %v", passthrough)
	}
	if got := released.get(); len(got) != 0 {
		t.Fatalf("settings released while the session is opted in: %v", got)
	}
	if !sessions.optOut("on") || sessions.optedIn("on") {
		t.Fatal("opt-out")
	}
	if got := released.get(); !slices.Equal(got, []string{"/p"}) {
		t.Fatalf("settings kept after the last opted-in session left: %v", got)
	}
	if code := send("POST", "/v1/messages", "on", s.Token, s.Listen); code != http.StatusOK {
		t.Fatalf("after opt-out the session must pass through to Claude: %d", code)
	}
}

// A session that opted in keeps Gemini when the daemon restarts, as the per-prompt hook does after a crash.
func TestOptInsSurviveARestart(t *testing.T) {
	dir := t.TempDir()
	first := newSessionSet(dir, func(string) {})
	first.add(attachRequest{PID: 100, Project: "/p", Session: "kept", OptIn: true})
	first.add(attachRequest{PID: 200, Project: "/p", Session: "gone", OptIn: true})
	second := newSessionSet(dir, func(string) {})
	second.load(func(pid int) bool { return pid == 100 })
	if !second.optedIn("kept") || second.optedIn("gone") {
		t.Fatalf("after restart: %v", second.optIns)
	}
}

// /clear gives the same process a new session id. The registry the SessionStart hook keeps maps the new id to the process, so the session stays on Gemini.
func TestOptInFollowsTheProcessToANewSessionID(t *testing.T) {
	dir := t.TempDir()
	sessions := newSessionSet(dir, func(string) {})
	sessions.add(attachRequest{PID: 4242, Project: "/p", Session: "before-clear", OptIn: true})
	if err := os.MkdirAll(registryDir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(registryDir(dir), "after-clear"), []byte("4242\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !sessions.sawRequest("after-clear") {
		t.Fatal("the session lost Gemini after /clear")
	}
	if sessions.sawRequest("someone-else") {
		t.Fatal("an unknown session was sent to Gemini")
	}
}

// Settings that point here while nobody is opted in are removed at the next check, for example after a crash or when Claude Code wrote back a stale copy.
func TestPruneSweepsSettingsNobodyNeeds(t *testing.T) {
	var released releases
	sessions := newSessionSet(t.TempDir(), released.add)
	sessions.add(attachRequest{PID: 100, Project: "/p", Session: "s", OptIn: true})
	sessions.prune(func(int) bool { return true }, false)
	if got := released.get(); len(got) != 0 {
		t.Fatalf("released while opted in: %v", got)
	}
	sessions.prune(func(int) bool { return false }, false)
	if got := released.get(); !slices.Equal(got, []string{"/p"}) {
		t.Fatalf("after the session died: %v", got)
	}
}

// A window NeoSwitch never attached keeps the daemon running while it sends requests: the daemon cannot see its process, so it waits idleGrace after the last one.
func TestUnplacedRequestsKeepTheDaemonUp(t *testing.T) {
	sessions := newSessionSet(t.TempDir(), func(string) {})
	sessions.idleGrace = time.Hour
	sessions.add(attachRequest{PID: 100})
	sessions.sawRequest("never-attached")
	if sessions.prune(func(int) bool { return false }, true) {
		t.Fatal("the daemon stopped under a window that is still sending requests")
	}
	sessions.idleGrace = 0
	if !sessions.prune(func(int) bool { return false }, true) {
		t.Fatal("the daemon kept running with nobody left")
	}
}
