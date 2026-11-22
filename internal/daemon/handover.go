package daemon

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// A newer NeoSwitch takes the port over from the daemon that serves the open windows, so an update reaches them without closing every window: their sessions are pinned to the port, its TLS certificate and its token, which both daemons share on disk. The daemon being replaced hands over the sessions only it knows, stops changing the state on disk, and lets the requests it is answering finish; its successor listens as soon as the port is free. See docs/design-session-routing.md, "Updates".
//
// Windows does not take part yet: the handover is untested there (detach and port reuse behave differently), so its daemons keep serving until their sessions end.

// HandoverProtocol is the version of the takeover protocol a daemon speaks, reported in Info. Daemons up to 0.4.4 report none and cannot hand over.
const HandoverProtocol = 1

const handoverPath = "/neoswitch/handover"

// ErrHandedOver is what Run returns after a successor took the port and the requests in progress finished.
var ErrHandedOver = errors.New("handed over to a newer NeoSwitch")

// ErrHandoverRefused means the running daemon would not hand over: the caller is not newer, or another takeover is under way.
var ErrHandoverRefused = errors.New("the running NeoSwitch daemon refused to hand over")

// ErrTakeoverBusy means another NeoSwitch process is taking over right now.
var ErrTakeoverBusy = errors.New("another NeoSwitch is taking over")

var (
	// successorWait is how long a daemon that handed over waits for its successor to answer before it takes the port back.
	successorWait = 5 * time.Second
	// bindWait is how long a successor retries the port, which frees once its predecessor closed the listener.
	bindWait = 3 * time.Second
	// drainLimit bounds how long the requests in progress may take to finish after a handover; a Gemini reply can run for minutes.
	drainLimit = 10 * time.Minute
	// takeoverLockAge is how long a takeover lock blocks others; a takeover takes seconds, so an older lock was left by a crash.
	takeoverLockAge = 30 * time.Second
	// failedTakeoverPause keeps every window from retrying a version that could not take over, which each attempt would cost a replay of every Gemini conversation.
	failedTakeoverPause = 6 * time.Hour
)

// HandoverSupported reports whether daemons on this platform take part in handovers.
func HandoverSupported() bool { return runtime.GOOS != "windows" }

// Handover is what a daemon gives its successor: the sessions it tracks only in memory, and the opt-ins and projects it last saved.
type Handover struct {
	PIDs     []int             `json:"pids"`
	OptIns   map[string]*optIn `json:"opt_ins"`
	Projects []string          `json:"projects"`
}

type handoverRequest struct {
	Key     string `json:"key"`
	Version string `json:"version"`
	// Explicit: the user ran /neoswitch:on, which may also replace a daemon of the same version with another setup.
	Explicit bool `json:"explicit,omitempty"`
}

// acceptsSuccessor reports whether a daemon of version own hands over to one of version successor: only to a later release, so two plugin versions alive at once never take the port back and forth, or, when the user asked for it, to one that is not older.
func acceptsSuccessor(own, successor string, explicit bool) bool {
	if NewerVersion(successor, own) {
		return true
	}
	return explicit && !NewerVersion(own, successor)
}

func handoverKeyPath(dir string) string    { return filepath.Join(dir, "handover.key") }
func handingOverPath(dir string) string    { return filepath.Join(dir, "handing-over") }
func takeoverLockPath(dir string) string   { return filepath.Join(dir, "takeover.lock") }
func failedTakeoverPath(dir string) string { return filepath.Join(dir, "takeover-failed") }

// handoverKey authorizes a handover. It is not the proxy token, which sits in the settings of every project NeoSwitch switched, where code in that project can read it; the key stays in the private state folder. Both daemons read it from there; the first one to need it makes it.
func handoverKey(dir string) (string, error) {
	path := handoverKeyPath(dir)
	for attempt := 0; attempt < 2; attempt++ {
		if data, err := os.ReadFile(path); err == nil {
			if key := strings.TrimSpace(string(data)); key != "" {
				return key, nil
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		var raw [24]byte
		_, _ = rand.Read(raw[:])
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			// Another daemon made it first; read its key.
			continue
		}
		if err != nil {
			return "", err
		}
		key := hex.EncodeToString(raw[:])
		_, werr := file.WriteString(key + "\n")
		if cerr := file.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(path)
			return "", werr
		}
		return key, nil
	}
	return "", fmt.Errorf("read %s", path)
}

// acquireTakeoverLock makes this process the only one taking over. A lock older than takeoverLockAge, or of a process that is gone, was left by a crash and is taken. release removes the lock only while it is still this process's, so a contender that took a stale lock at the same moment cannot remove the winner's.
func acquireTakeoverLock(dir string) (release func(), ok bool) {
	path := takeoverLockPath(dir)
	for attempt := 0; attempt < 2; attempt++ {
		if writePIDFile(path, true) == nil {
			return func() { removeOwnPIDFile(path) }, true
		}
		if heldByLiveProcess(path) {
			return nil, false
		}
		_ = os.Remove(path)
	}
	return nil, false
}

// takeoverInProgress reports whether a handover is under way: while a successor holds the lock, or its predecessor has just frozen, the port may be free for a moment, and a session that finds it free must wait for the successor rather than start a daemon of its own, which would take the port and keep the predecessor from taking it back.
func takeoverInProgress(dir string) bool {
	return heldByLiveProcess(takeoverLockPath(dir)) || heldByLiveProcess(handingOverPath(dir))
}

// beginHandingOver marks that this daemon froze for a successor; endHandingOver clears the mark once the successor answers or the daemon took its port back.
func beginHandingOver(dir string) { _ = writePIDFile(handingOverPath(dir), false) }
func endHandingOver(dir string)   { removeOwnPIDFile(handingOverPath(dir)) }

// writePIDFile writes this process's ID to path, failing when path exists if exclusive.
func writePIDFile(path string, exclusive bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if exclusive {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	file, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return err
	}
	_, err = file.WriteString(strconv.Itoa(os.Getpid()) + "\n")
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	return err
}

func filePID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

func removeOwnPIDFile(path string) {
	if filePID(path) == os.Getpid() {
		_ = os.Remove(path)
	}
}

// heldByLiveProcess reports whether path is a recent PID file of a running process; older ones were left by a crash.
func heldByLiveProcess(path string) bool {
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > takeoverLockAge {
		return false
	}
	pid := filePID(path)
	return pid > 1 && processAlive(pid)
}

// TakeoverFailedRecently reports whether version tried to take over lately and its predecessor had to take the port back.
func TakeoverFailedRecently(dir, version string) bool {
	path := failedTakeoverPath(dir)
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > failedTakeoverPause {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && strings.TrimSpace(string(data)) == version
}

func recordFailedTakeover(dir, version string) {
	_ = WriteFileAtomic(failedTakeoverPath(dir), []byte(version+"\n"), 0o600)
}

// requestHandover asks the daemon on s to hand over to a daemon of version.
func requestHandover(s State, version string, explicit bool) (*Handover, error) {
	key, err := handoverKey(s.dir)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(handoverRequest{Key: key, Version: version, Explicit: explicit})
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL()+handoverPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client(5 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("ask the running daemon to hand over: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		var h Handover
		if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
			return nil, fmt.Errorf("read the handover: %w", err)
		}
		return &h, nil
	case http.StatusConflict:
		return nil, ErrHandoverRefused
	}
	return nil, fmt.Errorf("ask the running daemon to hand over: it answered HTTP %d", resp.StatusCode)
}

// handoverHandler answers a successor's request. On success the session set is frozen, so this daemon no longer writes the state its successor reads, and successor receives the version that took over.
func handoverHandler(s State, sessions *sessionSet, own string, successor chan<- string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body handoverRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		key, err := handoverKey(s.dir)
		if err != nil || subtle.ConstantTimeCompare([]byte(body.Key), []byte(key)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !acceptsSuccessor(own, body.Version, body.Explicit) {
			w.WriteHeader(http.StatusConflict)
			return
		}
		h, ok := sessions.freeze()
		if !ok {
			w.WriteHeader(http.StatusConflict)
			return
		}
		beginHandingOver(s.dir)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(h)
		successor <- body.Version
	}
}
