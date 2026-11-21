package daemon

import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	attachPath = "/neoswitch/attach"
	detachPath = "/neoswitch/detach"
	optOutPath = "/neoswitch/optout"
	statusPath = "/neoswitch/status"
	// sessionHeader is the Claude Code session a request comes from; hooks see the same id as session_id, and skills as CLAUDE_CODE_SESSION_ID.
	sessionHeader = "X-Claude-Code-Session-Id"
)

// Options tune the lifetime rules; zero values select the production defaults.
type Options struct {
	// StartupGrace is how long the daemon waits for its first session before giving up.
	StartupGrace time.Duration
	// PollInterval is how often attached Claude Code processes are checked.
	PollInterval time.Duration
	// Alive reports whether a process exists; tests replace it.
	Alive func(pid int) bool
	// Backend describes the upstream this daemon serves, reported by Status.
	Backend string
	// Version is the NeoSwitch version of the daemon binary, reported by Status so a session started after an update can tell it is served by the earlier version.
	Version string
	// Model is the model the daemon answers with, fixed when it starts. Reported by Status so a session that asks for another model (GEMINI_MODEL) can tell it gets this one.
	Model string
	// Release is called with a project once no session that opted in there is left. The next session in the project then starts on Claude. It runs while attaches wait, so a session turning the project on again lands after it.
	Release func(project string)
	// IdleGrace keeps the daemon running after its last known session ended while requests of sessions it cannot place keep arriving: windows that applied a project's settings while another session switched it, without ever starting a hook NeoSwitch saw.
	IdleGrace time.Duration
	// Passthrough forwards a request that is not for Gemini, with the user's own credential, to Anthropic. With TLS, the daemon routes every request: the opted-in sessions' model requests go to the proxy, everything else here.
	Passthrough http.Handler
}

// Info is what a running daemon reports about itself. Version is empty for daemons from versions that did not report it.
type Info struct {
	Sessions int    `json:"sessions"`
	Backend  string `json:"backend"`
	Version  string `json:"version,omitempty"`
	// Model is empty for daemons from versions that did not report it.
	Model string `json:"model,omitempty"`
	// OptedIn answers for the session named in the status request: its model requests go to Gemini.
	OptedIn bool `json:"opted_in,omitempty"`
}

// Run serves proxy on s.Listen until every attached Claude Code process has exited, or until StartupGrace passes without any attach. Lifetime is tied to the sessions rather than an idle timer, so a session that sits idle for hours keeps working and nothing outlives the last session.
func Run(ctx context.Context, s State, proxy http.Handler, opts Options) error {
	if opts.StartupGrace == 0 {
		opts.StartupGrace = time.Minute
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = 10 * time.Second
	}
	if opts.Alive == nil {
		opts.Alive = processAlive
	}
	listener, err := net.Listen("tcp", s.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.Listen, err)
	}
	if s.TLS {
		config, err := serverTLS(s.dir)
		if err != nil {
			_ = listener.Close()
			return err
		}
		listener = tls.NewListener(listener, config)
	}
	if opts.Release == nil {
		opts.Release = func(string) {}
	}
	if opts.IdleGrace == 0 {
		opts.IdleGrace = 30 * time.Minute
	}
	sessions := newSessionSet(s.dir, opts.Release)
	sessions.idleGrace = opts.IdleGrace
	sessions.load(opts.Alive)
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+attachPath, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, s.Token) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body attachRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil || body.PID <= 1 || (body.OptIn && body.Session == "") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// A daemon that decided to stop refuses, so Attach starts a new one instead of attaching to one about to exit.
		if !sessions.add(body) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		// Daemons that predate project tracking answer 204, which tells a session its project stays routed.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(attachReply{Tracked: body.Project != "" || body.OptIn})
	})
	mux.HandleFunc("POST "+detachPath, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, s.Token) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			PID int `json:"pid"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil || body.PID <= 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		sessions.remove(body.PID)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST "+optOutPath, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, s.Token) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			Session string `json:"session"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil || body.Session == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(optOutReply{WasOptedIn: sessions.optOut(body.Session)})
	})
	mux.HandleFunc("GET "+statusPath, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, s.Token) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Info{Sessions: sessions.len(), Backend: opts.Backend, Version: opts.Version, Model: opts.Model, OptedIn: sessions.optedIn(r.URL.Query().Get("session"))})
	})
	if s.TLS {
		mux.Handle("/", &router{state: s, sessions: sessions, gemini: proxy, passthrough: opts.Passthrough})
	} else {
		mux.Handle("/", proxy)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ticker := time.NewTicker(opts.PollInterval)
	defer ticker.Stop()
	deadline := time.Now().Add(opts.StartupGrace)
	for {
		select {
		case err := <-served:
			return err
		case <-ctx.Done():
			// Stopped from outside, as at logout or shutdown: the sessions go too, so their projects return to Claude.
			sessions.stop()
			return shutdown(server)
		case <-ticker.C:
			if sessions.prune(opts.Alive, time.Now().After(deadline)) {
				return shutdown(server)
			}
		}
	}
}

func shutdown(server *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}

type attachRequest struct {
	PID     int    `json:"pid"`
	Project string `json:"project,omitempty"`
	// Session and OptIn: the session ran /neoswitch:on, so its model requests go to Gemini.
	Session string `json:"session,omitempty"`
	OptIn   bool   `json:"opt_in,omitempty"`
}

type attachReply struct {
	// Tracked: the daemon turns the session's project off when it no longer needs the settings.
	Tracked bool `json:"tracked"`
}

type optOutReply struct {
	WasOptedIn bool `json:"was_opted_in"`
}

// optIn is a session that ran /neoswitch:on, in project. Its model requests go to Gemini while its process runs.
type optIn struct {
	PID     int    `json:"pid"`
	Project string `json:"project"`
}

// sessionSet holds the attached Claude Code processes, the projects each one turned on, and the sessions opted in to Gemini. Its state outlives the daemon on disk: the opt-ins, and the projects whose settings point here, so a daemon started again after a crash keeps every session where it was and can still clean up after the ones that ended.
type sessionSet struct {
	mu       sync.Mutex
	pids     map[int]map[string]bool
	optIns   map[string]*optIn
	projects map[string]bool
	attached bool
	// closing: the daemon decided to stop and takes no more sessions.
	closing bool
	release func(project string)
	dir     string
	// lastUnplaced is when a request last came from a session the registry does not name; idleGrace keeps the daemon up that long after it.
	lastUnplaced time.Time
	idleGrace    time.Duration
}

func newSessionSet(dir string, release func(string)) *sessionSet {
	return &sessionSet{pids: map[int]map[string]bool{}, optIns: map[string]*optIn{}, projects: map[string]bool{}, release: release, dir: dir}
}

// add reports false when the daemon is closing.
func (s *sessionSet) add(r attachRequest) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	// A session attaching without opting in only keeps the daemon running: whether a project's settings stay is decided by its opted-in sessions alone, so a window that merely started while they existed cannot remove them under another session that needs them.
	if s.pids[r.PID] == nil {
		s.pids[r.PID] = map[string]bool{}
	}
	if r.OptIn {
		s.optIns[r.Session] = &optIn{PID: r.PID, Project: r.Project}
		if r.Project != "" {
			s.projects[r.Project] = true
		}
		s.save()
	}
	s.attached = true
	return true
}

func (s *sessionSet) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pids)
}

func (s *sessionSet) optedIn(session string) bool {
	if session == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookup(session) != nil
}

// lookup finds the opt-in of session. A /clear gives the same process a new session id, so an id the daemon does not know is placed through the registry the SessionStart hook keeps, and the opt-in of its process moves to it. The caller holds mu.
func (s *sessionSet) lookup(session string) *optIn {
	if o := s.optIns[session]; o != nil {
		return o
	}
	pid := registeredPID(s.dir, session)
	if pid <= 1 {
		return nil
	}
	for id, o := range s.optIns {
		if o.PID == pid {
			delete(s.optIns, id)
			s.optIns[session] = o
			s.save()
			return o
		}
	}
	return nil
}

// sawRequest places the session a request came from and reports whether it is opted in. A session the daemon has not met, such as another window that applied the settings, keeps the daemon running: through its process when the registry names it, otherwise for idleGrace.
func (s *sessionSet) sawRequest(session string) bool {
	if session == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if o := s.lookup(session); o != nil {
		return true
	}
	if pid := registeredPID(s.dir, session); pid > 1 {
		if s.pids[pid] == nil {
			s.pids[pid] = map[string]bool{}
			s.attached = true
		}
	} else {
		s.lastUnplaced = time.Now()
	}
	return false
}

// optOut takes a session off Gemini; its next request passes through to Claude.
func (s *sessionSet) optOut(session string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.lookup(session)
	if o == nil {
		return false
	}
	for id, other := range s.optIns {
		if other == o {
			delete(s.optIns, id)
		}
	}
	s.save()
	s.releaseIfUnused(o.Project)
	return true
}

// releaseIfUnused releases project once no opted-in session of it is left. The caller holds mu, which the attach endpoint takes too: a session that turns the project on again registers before it writes the settings, so a release can never remove settings a registered session still needs.
func (s *sessionSet) releaseIfUnused(project string) {
	if project == "" {
		return
	}
	for _, o := range s.optIns {
		if o.Project == project {
			return
		}
	}
	s.release(project)
	delete(s.projects, project)
	s.save()
}

// remove forgets a session that ended, releasing the projects no other session uses.
func (s *sessionSet) remove(pid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drop(pid)
}

// prune drops exited processes, releasing their projects, sweeps projects whose settings still point here with nobody opted in, and reports whether the daemon should stop: every known session has exited and no unplaced session sent a request lately, or none attached before graceOver. Deciding to stop also refuses later attaches.
func (s *sessionSet) prune(alive func(int) bool, graceOver bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for pid := range s.pids {
		if !alive(pid) {
			s.drop(pid)
		}
	}
	for _, o := range s.optIns {
		if !alive(o.PID) {
			s.drop(o.PID)
		}
	}
	// Level-triggered: settings that came back, for example written by Claude Code from a stale copy, or left by a crash, go too.
	for project := range s.projects {
		s.releaseIfUnused(project)
	}
	pruneRegistry(s.dir, alive)
	idle := time.Since(s.lastUnplaced) > s.idleGrace
	if (!s.attached && graceOver && idle) || (s.attached && len(s.pids) == 0 && len(s.optIns) == 0 && idle) {
		s.closing = true
	}
	return s.closing
}

// stop releases every project and refuses later attaches.
func (s *sessionSet) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closing = true
	for pid := range s.pids {
		s.drop(pid)
	}
	for _, o := range s.optIns {
		s.drop(o.PID)
	}
	for project := range s.projects {
		s.releaseIfUnused(project)
	}
}

// drop removes pid and its opt-ins, and releases each of its projects that no remaining session needs. The caller holds mu.
func (s *sessionSet) drop(pid int) {
	delete(s.pids, pid)
	optedProjects := map[string]bool{}
	for session, o := range s.optIns {
		if o.PID == pid {
			optedProjects[o.Project] = true
			delete(s.optIns, session)
		}
	}
	if len(optedProjects) > 0 {
		s.save()
	}
	for project := range optedProjects {
		s.releaseIfUnused(project)
	}
}

func optInsPath(dir string) string { return filepath.Join(dir, "opt-ins.json") }

type savedSessions struct {
	OptIns   map[string]*optIn `json:"opt_ins"`
	Projects []string          `json:"projects"`
}

// load reads what a previous daemon kept, dropping opt-ins whose session has ended; their projects are released at the first prune.
func (s *sessionSet) load(alive func(int) bool) {
	if s.dir == "" {
		return
	}
	data, err := os.ReadFile(optInsPath(s.dir))
	if err != nil {
		return
	}
	var saved savedSessions
	if json.Unmarshal(data, &saved) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for session, o := range saved.OptIns {
		if o != nil && alive(o.PID) {
			s.optIns[session] = o
			s.attached = true
		}
	}
	for _, project := range saved.Projects {
		s.projects[project] = true
	}
	s.save()
}

// save keeps the opt-ins and projects on disk. The caller holds mu.
func (s *sessionSet) save() {
	if s.dir == "" {
		return
	}
	saved := savedSessions{OptIns: s.optIns}
	for project := range s.projects {
		saved.Projects = append(saved.Projects, project)
	}
	data, _ := json.Marshal(saved)
	_ = WriteFileAtomic(optInsPath(s.dir), data, 0o600)
}

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	// EPERM means the process exists but belongs to someone else, which still counts as alive.
	return err == nil || errors.Is(err, syscall.EPERM)
}

func authorized(r *http.Request, token string) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	value, ok := strings.CutPrefix(auth, "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(value)), []byte(token)) == 1
}

// Status asks a running daemon how many sessions it serves and which backend it uses; ok is false when no NeoSwitch daemon answers on s.Listen with s.Token.
func Status(s State) (info Info, ok bool) {
	return StatusOf(s, "")
}

// StatusOf is Status, also answering whether session is opted in to Gemini.
func StatusOf(s State, session string) (info Info, ok bool) {
	target := s.BaseURL() + statusPath
	if session != "" {
		target += "?session=" + url.QueryEscape(session)
	}
	req, _ := http.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := s.client(time.Second).Do(req)
	if err != nil {
		return Info{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Info{}, false
	}
	if json.NewDecoder(resp.Body).Decode(&info) != nil {
		return Info{}, false
	}
	return info, true
}

// PortInUseError means another program holds the proxy's saved port, so no NeoSwitch daemon can listen there.
type PortInUseError struct {
	Listen string
}

func (e *PortInUseError) Error() string { return "another program is using " + e.Listen }

// portBusyWait is how long a held port may stay silent before it is blamed on another program. A NeoSwitch daemon that is starting or shutting down holds the port briefly without answering the status probe.
var portBusyWait = 2 * time.Second

// Attach registers a Claude Code process with the daemon, starting the daemon through spawn first when none is running, and reports whether the daemon tracks project: it then turns the project off once no attached session uses it. project is "" for a session that turns no project on. It tries three times because a daemon that is shutting down (its last session just exited) can still answer the status probe and then refuse the attach. A port held by another program fails at once with *PortInUseError instead of waiting for a daemon that can never listen.
func Attach(s State, pid int, project string, spawn func() error) (tracked bool, err error) {
	return AttachSession(s, Session{PID: pid, Project: project}, spawn)
}

// Session is a Claude Code session attaching to the daemon. With OptIn, ID is its session id and its model requests go to Gemini until it opts out or ends.
type Session struct {
	PID     int
	Project string
	ID      string
	OptIn   bool
}

// AttachSession is Attach for a session that may opt in to Gemini.
func AttachSession(s State, session Session, spawn func() error) (tracked bool, err error) {
	request := attachRequest{PID: session.PID, Project: session.Project, Session: session.ID, OptIn: session.OptIn}
	for attempt := 0; attempt < 3; attempt++ {
		if tracked, err = attachOnce(s, request, spawn); err == nil {
			return tracked, nil
		}
		var busy *PortInUseError
		if errors.As(err, &busy) {
			return false, err
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false, err
}

// OptOut takes a session off Gemini, so its next request passes through to Claude, and reports whether it was on Gemini. ok is false when no daemon answered.
func OptOut(s State, session string) (wasOptedIn, ok bool) {
	body, _ := json.Marshal(map[string]string{"session": session})
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL()+optOutPath, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client(5 * time.Second).Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	var reply optOutReply
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&reply) != nil {
		return false, false
	}
	return reply.WasOptedIn, true
}

// Detach tells the daemon a session ended, so the projects only it used return to Claude at once rather than at the daemon's next check, which a session reopened right away would beat. A daemon that does not answer, or predates detaching, is left to that check.
func Detach(s State, pid int) {
	body, _ := json.Marshal(map[string]int{"pid": pid})
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL()+detachPath, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client(5 * time.Second).Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
}

// portFree reports whether nothing listens on addr.
func portFree(addr string) bool {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

// answersWithin reports whether a NeoSwitch daemon answers on s within d.
func answersWithin(s State, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if _, ok := Status(s); ok {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func attachOnce(s State, request attachRequest, spawn func() error) (bool, error) {
	if _, ok := Status(s); !ok && !portFree(s.Listen) {
		if !answersWithin(s, portBusyWait) {
			return false, &PortInUseError{Listen: s.Listen}
		}
	} else if !ok {
		if err := spawn(); err != nil {
			return false, fmt.Errorf("start NeoSwitch daemon: %w", err)
		}
		deadline := time.Now().Add(15 * time.Second)
		for {
			if _, ok := Status(s); ok {
				break
			}
			if time.Now().After(deadline) {
				return false, fmt.Errorf("NeoSwitch daemon did not come up on %s", s.Listen)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	body, _ := json.Marshal(request)
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL()+attachPath, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client(5 * time.Second).Do(req)
	if err != nil {
		return false, fmt.Errorf("attach session: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return false, nil
	case http.StatusOK:
		var reply attachReply
		if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
			return false, fmt.Errorf("attach session: %w", err)
		}
		return reply.Tracked, nil
	}
	return false, fmt.Errorf("attach session: daemon answered HTTP %d", resp.StatusCode)
}
