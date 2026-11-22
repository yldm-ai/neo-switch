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

// acquireTakeoverLock makes this process the only one taking over. A lock older than takeoverLockAge, or of a process that is gone, was left by a crash and is taken.
func acquireTakeoverLock(dir string) (release func(), ok bool) {
	path := takeoverLockPath(dir)
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, _ = file.WriteString(strconv.Itoa(os.Getpid()) + "\n")
			_ = file.Close()
			return func() { _ = os.Remove(path) }, true
		}
		if !errors.Is(err, fs.ErrExist) || takeoverInProgress(dir) {
			return nil, false
		}
		_ = os.Remove(path)
	}
	return nil, false
}

// takeoverInProgress reports whether a NeoSwitch process is taking over: while it holds the lock, the port may be free for a moment, and a session that finds it free must wait for the successor rather than start a daemon of its own.
func takeoverInProgress(dir string) bool {
	path := takeoverLockPath(dir)
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > takeoverLockAge {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	return err == nil && processAlive(pid)
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
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(h)
		successor <- body.Version
	}
}
