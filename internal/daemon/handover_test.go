package daemon

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// runDaemon starts a daemon in the background and returns its result channel.
func runDaemon(t *testing.T, s State, proxy http.Handler, opts Options) (cancel func(), done <-chan error) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	finished, exited := make(chan error, 1), make(chan struct{})
	go func() {
		finished <- Run(ctx, s, proxy, opts)
		close(exited)
	}()
	// The state folder is removed after this, so the daemon must be gone first.
	t.Cleanup(func() {
		stop()
		<-exited
	})
	return stop, finished
}

// waitVersion waits until the daemon on s reports version.
func waitVersion(t *testing.T, s State, version string) Info {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if info, ok := Status(s); ok && info.Version == version {
			return info
		}
		if time.Now().After(deadline) {
			t.Fatalf("no daemon of %s answered", version)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestNewerDaemonTakesOverSessionsWithoutReleasingThem(t *testing.T) {
	s := testState(t)
	procs := &fakeProcs{alive: map[int]bool{100: true, 200: true}}
	var released releases
	opts := Options{StartupGrace: time.Minute, PollInterval: 20 * time.Millisecond, Alive: procs.Alive, Release: released.add}
	old := opts
	old.Version = "v0.4.5"
	_, oldDone := runDaemon(t, s, http.NotFoundHandler(), old)
	waitVersion(t, s, "v0.4.5")
	if _, err := Attach(s, 100, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := AttachSession(s, Session{PID: 200, Project: "/p", ID: "gemini-session", OptIn: true}, nil); err != nil {
		t.Fatal(err)
	}

	next := opts
	next.Version, next.TakeOver = "v0.4.6", true
	runDaemon(t, s, http.NotFoundHandler(), next)
	info := waitVersion(t, s, "v0.4.6")
	if info.Handover != HandoverProtocol {
		t.Fatalf("successor reports handover %d", info.Handover)
	}
	select {
	case err := <-oldDone:
		if !errors.Is(err, ErrHandedOver) {
			t.Fatalf("predecessor ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("predecessor kept running after handing over")
	}
	// 交接不得撤销任何项目的路由：撤销会把 Gemini 会话静默切回 Claude。
	if got := released.get(); len(got) != 0 {
		t.Fatalf("released %v during the handover", got)
	}
	if info, _ := StatusOf(s, "gemini-session"); !info.OptedIn || info.Sessions != 2 {
		t.Fatalf("successor lost sessions: %+v", info)
	}
	// The window the predecessor only knew in memory still keeps the successor up after the opted-in one ends.
	procs.set(200, false)
	waitUntil(3*time.Second, func() bool {
		info, ok := Status(s)
		return ok && info.Sessions == 1 && len(released.get()) == 1
	})
	if info, ok := Status(s); !ok || info.Sessions != 1 {
		t.Fatalf("successor after the Gemini session ended: %+v %v", info, ok)
	}
	if got := released.get(); len(got) != 1 || got[0] != "/p" {
		t.Fatalf("successor released %v, want the project once its last opted-in session ended", got)
	}
}

func TestDaemonHandsOverOnlyToANewerVersionUnlessAsked(t *testing.T) {
	s := testState(t)
	alive := func(int) bool { return true }
	running := Options{StartupGrace: time.Minute, Alive: alive, Version: "v0.4.6"}
	_, done := runDaemon(t, s, http.NotFoundHandler(), running)
	waitVersion(t, s, "v0.4.6")
	if _, err := Attach(s, 100, "", nil); err != nil {
		t.Fatal(err)
	}

	for _, version := range []string{"v0.4.6+source.other", "v0.4.5", "dev"} {
		err := Run(context.Background(), s, http.NotFoundHandler(), Options{Alive: alive, Version: version, TakeOver: true})
		if !errors.Is(err, ErrHandoverRefused) {
			t.Fatalf("%s: Run = %v, want refused", version, err)
		}
	}
	if _, ok := Status(s); !ok {
		t.Fatal("a refused takeover stopped the running daemon")
	}

	// /neoswitch:on may replace a daemon of the same version, as after a source rebuild or a change of model.
	runDaemon(t, s, http.NotFoundHandler(), Options{StartupGrace: time.Minute, Alive: alive, Version: "v0.4.6+source.other", TakeOver: true, TakeOverExplicit: true})
	waitVersion(t, s, "v0.4.6+source.other")
	if err := <-done; !errors.Is(err, ErrHandedOver) {
		t.Fatalf("predecessor ended with %v", err)
	}
}

func TestHandoverNeedsTheKeyFromTheStateFolder(t *testing.T) {
	s := testState(t)
	runDaemon(t, s, http.NotFoundHandler(), Options{StartupGrace: time.Minute, Alive: func(int) bool { return true }, Version: "v0.4.5"})
	waitVersion(t, s, "v0.4.5")
	// The proxy token is in every switched project's settings, so it must not be enough.
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL()+handoverPath, strings.NewReader(`{"key":"`+s.Token+`","version":"v9.9.9"}`))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := s.client(5 * time.Second).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("handover with the proxy token: HTTP %d", resp.StatusCode)
	}
}

func TestReplyInProgressFinishesOnThePredecessor(t *testing.T) {
	s := testState(t)
	alive := func(int) bool { return true }
	entered, finish := make(chan struct{}), make(chan struct{})
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-finish
		w.WriteHeader(http.StatusTeapot)
	})
	_, oldDone := runDaemon(t, s, slow, Options{StartupGrace: time.Minute, Alive: alive, Version: "v0.4.5"})
	waitVersion(t, s, "v0.4.5")
	if _, err := AttachSession(s, Session{PID: 200, Project: "/p", ID: "gemini-session", OptIn: true}, nil); err != nil {
		t.Fatal(err)
	}
	status := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodPost, s.BaseURL()+"/v1/messages", strings.NewReader("{}"))
		req.Header.Set(TokenHeader, s.Token)
		req.Header.Set(sessionHeader, "gemini-session")
		resp, err := s.client(30 * time.Second).Do(req)
		if err != nil {
			status <- 0
			return
		}
		resp.Body.Close()
		status <- resp.StatusCode
	}()
	<-entered

	runDaemon(t, s, http.NotFoundHandler(), Options{StartupGrace: time.Minute, Alive: alive, Version: "v0.4.6", TakeOver: true})
	waitVersion(t, s, "v0.4.6")
	close(finish)
	if got := <-status; got != http.StatusTeapot {
		t.Fatalf("reply in progress ended with %d", got)
	}
	if err := <-oldDone; !errors.Is(err, ErrHandedOver) {
		t.Fatalf("predecessor ended with %v", err)
	}
}

func TestDaemonTakesThePortBackWhenTheSuccessorNeverListens(t *testing.T) {
	s := testState(t)
	wait := successorWait
	successorWait = 200 * time.Millisecond
	t.Cleanup(func() { successorWait = wait })
	alive := func(int) bool { return true }
	runDaemon(t, s, http.NotFoundHandler(), Options{StartupGrace: time.Minute, PollInterval: 20 * time.Millisecond, Alive: alive, Version: "v0.4.5"})
	waitVersion(t, s, "v0.4.5")

	// A successor that crashed right after the handover.
	if _, err := requestHandover(s, "v0.4.6", false); err != nil {
		t.Fatal(err)
	}
	// Until the predecessor takes the port back, a session that finds it free must wait rather than start a daemon of its own.
	if !takeoverInProgress(s.dir) {
		t.Fatal("a frozen predecessor is not reported as a handover in progress")
	}
	// The handover is answered before the predecessor closes its listener, so the old listener may still answer for a moment: wait for the record, which is written only after the listener closed, before asking which daemon answers.
	if !waitUntil(5*time.Second, func() bool { return TakeoverFailedRecently(s.dir, "v0.4.6") }) {
		t.Fatal("the failed takeover was not recorded")
	}
	waitVersion(t, s, "v0.4.5")
	if takeoverInProgress(s.dir) {
		t.Fatal("the handover is still reported after the port was taken back")
	}
	if _, err := AttachSession(s, Session{PID: 300, Project: "/p", ID: "later", OptIn: true}, nil); err != nil {
		t.Fatalf("the daemon did not take its sessions back: %v", err)
	}
	if info, _ := StatusOf(s, "later"); !info.OptedIn {
		t.Fatal("attach after taking the port back was lost")
	}
}

func TestFrozenSessionsChangeNothingOnDisk(t *testing.T) {
	var released releases
	sessions := newSessionSet(t.TempDir(), released.add)
	sessions.add(attachRequest{PID: 100, Project: "/p", Session: "a", OptIn: true})
	if _, ok := sessions.freeze(); !ok {
		t.Fatal("freeze refused")
	}
	if _, ok := sessions.freeze(); ok {
		t.Fatal("a second takeover was accepted")
	}
	sessions.prune(func(int) bool { return false }, true)
	if got := released.get(); len(got) != 0 {
		t.Fatalf("a frozen daemon released %v", got)
	}
	reloaded := newSessionSet(sessions.dir, nil)
	reloaded.load(func(int) bool { return true })
	if !reloaded.optedIn("a") {
		t.Fatal("a frozen daemon rewrote the opt-ins its successor reads")
	}
}

func TestOnlyOneTakeoverAtATime(t *testing.T) {
	dir := t.TempDir()
	release, ok := acquireTakeoverLock(dir)
	if !ok {
		t.Fatal("first lock refused")
	}
	if _, ok := acquireTakeoverLock(dir); ok {
		t.Fatal("second lock granted while the first is held")
	}
	if !takeoverInProgress(dir) {
		t.Fatal("held lock not reported")
	}
	release()
	if takeoverInProgress(dir) {
		t.Fatal("released lock still reported")
	}
}

// The successor's sessions are the predecessor's, not the disk's: an opt-out the predecessor saved while a successor waited, or a stale file, must not put a session back on Gemini.
func TestSuccessorTakesTheHandedOverSessionsNotTheDisk(t *testing.T) {
	s := testState(t)
	alive := func(int) bool { return true }
	_, oldDone := runDaemon(t, s, http.NotFoundHandler(), Options{StartupGrace: time.Minute, Alive: alive, Version: "v0.4.5"})
	waitVersion(t, s, "v0.4.5")
	if _, err := AttachSession(s, Session{PID: 200, Project: "/p", ID: "kept", OptIn: true}, nil); err != nil {
		t.Fatal(err)
	}
	stale := `{"opt_ins":{"kept":{"pid":200,"project":"/p"},"turned-off":{"pid":300,"project":"/p"}},"projects":["/p"]}`
	if err := WriteFileAtomic(optInsPath(s.dir), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	runDaemon(t, s, http.NotFoundHandler(), Options{StartupGrace: time.Minute, Alive: alive, Version: "v0.4.6", TakeOver: true})
	waitVersion(t, s, "v0.4.6")
	<-oldDone
	if info, _ := StatusOf(s, "turned-off"); info.OptedIn {
		t.Fatal("an opt-in only the disk held came back with the takeover")
	}
	if info, _ := StatusOf(s, "kept"); !info.OptedIn {
		t.Fatal("the handed-over opt-in was lost")
	}
}

func TestFrozenDaemonDoesNotStopOnItsOwn(t *testing.T) {
	sessions := newSessionSet(t.TempDir(), nil)
	sessions.add(attachRequest{PID: 100})
	sessions.freeze()
	// Every session gone and the grace over: a daemon that is not handing over would stop here.
	if sessions.prune(func(int) bool { return false }, true) {
		t.Fatal("a frozen daemon decided to stop, which would cut the replies it drains")
	}
}

func TestTakeoverLockIsRemovedOnlyByItsHolder(t *testing.T) {
	dir := t.TempDir()
	release, ok := acquireTakeoverLock(dir)
	if !ok {
		t.Fatal("lock refused")
	}
	// Another process took the lock over meanwhile.
	if err := os.WriteFile(takeoverLockPath(dir), []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	release()
	if _, err := os.Stat(takeoverLockPath(dir)); err != nil {
		t.Fatal("release removed a lock this process no longer held")
	}
}
