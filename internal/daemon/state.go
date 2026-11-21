// Package daemon runs one shared NeoSwitch proxy for every project that opted in through `neoswitch on`, and stops it once the last Claude Code session using it has exited.
package daemon

import (
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// State is the proxy address and token shared by every enabled project. It is created once and reused so that settings written into projects stay valid across daemon restarts.
type State struct {
	Listen string `json:"listen"`
	Token  string `json:"token"`
	// Backend is the upstream the user chose, "antigravity", "gemini" or "openai"; empty means choose from the environment. /neoswitch:login sets it so a Gemini key exported for other tools does not override the account the user signed in with.
	Backend string `json:"backend,omitempty"`
	// TLS: the daemon serves HTTPS with NeoSwitch's local certificate. States from before it lack it; EnsureState moves them to TLS on a new port.
	TLS bool `json:"tls,omitempty"`
	// Upstream is where requests that do not go to Gemini are passed on, when it is not Anthropic's API: the ANTHROPIC_BASE_URL the user's sessions had before a project was switched, such as a company gateway.
	Upstream string `json:"upstream,omitempty"`

	dir string
}

func (s State) BaseURL() string {
	if s.TLS {
		return "https://" + s.Listen
	}
	return "http://" + s.Listen
}

// client talks to the daemon, trusting only NeoSwitch's own CA.
func (s State) client(timeout time.Duration) *http.Client {
	transport := &http.Transport{}
	if s.TLS {
		config, err := clientTLS(s.dir)
		if err != nil {
			// No CA, no trust: every request fails, and nothing is sent to whoever holds the port.
			config = &tls.Config{RootCAs: x509.NewCertPool()}
		}
		transport.TLSClientConfig = config
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

// Dir is where NeoSwitch keeps its state file and daemon log.
func Dir() (string, error) {
	if dir := os.Getenv("NEOSWITCH_STATE_DIR"); dir != "" {
		return dir, nil
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate config directory: %w", err)
	}
	return filepath.Join(config, "neoswitch"), nil
}

func statePath(dir string) string { return filepath.Join(dir, "proxy.json") }

// LoadState reads the shared state; ok is false when `neoswitch on` has never run.
func LoadState(dir string) (State, bool, error) {
	data, err := os.ReadFile(statePath(dir))
	if errors.Is(err, os.ErrNotExist) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil || s.Listen == "" || s.Token == "" {
		return State{}, false, fmt.Errorf("NeoSwitch's settings file %s is damaged. Delete it, then run /neoswitch:on again in every project where NeoSwitch was on", statePath(dir))
	}
	s.dir = dir
	return s, true, nil
}

// EnsureState returns the existing state or creates one with a free loopback port and a random token. The file is private to the user because the token authorizes use of their Gemini quota.
//
// A state from before TLS moves to a new port with TLS: a daemon of the earlier version may still serve sessions on the old port, which keeps working for them until they end.
func EnsureState(dir string) (State, error) {
	s, ok, err := LoadState(dir)
	if err != nil {
		return State{}, err
	}
	// Renewing replaces the CA, which only sessions connecting from now on would trust: it waits until no daemon answers.
	renew := !ok || !s.TLS
	if ok && s.TLS {
		_, running := Status(s)
		renew = !running
	}
	if err := ensureTLS(dir, renew); err != nil {
		return State{}, err
	}
	if ok && s.TLS {
		return s, nil
	}
	addr, err := freeLoopbackPort()
	if err != nil {
		return State{}, err
	}
	if !ok {
		var token [24]byte
		_, _ = rand.Read(token[:])
		s = State{Token: hex.EncodeToString(token[:])}
	}
	s.Listen, s.TLS, s.dir = addr, true, dir
	if err := writeState(dir, s); err != nil {
		return State{}, err
	}
	return s, nil
}

// SaveUpstream records where passthrough requests go; "" is Anthropic's API.
func SaveUpstream(dir string, s State, upstream string) (State, error) {
	if s.Upstream == upstream {
		return s, nil
	}
	s.Upstream = upstream
	return s, writeState(dir, s)
}

// MovePort gives NeoSwitch a new loopback port, keeping its token and backend. It is for a saved port that another program took, as can happen after a reboot: no NeoSwitch daemon can listen there any more, so nothing still pointing at it works either way.
func MovePort(dir string, s State) (State, error) {
	addr, err := freeLoopbackPort()
	if err != nil {
		return State{}, err
	}
	s.Listen = addr
	s.dir = dir
	return s, writeState(dir, s)
}

func freeLoopbackPort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("pick a loopback port: %w", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr, nil
}

// SaveBackend records the user's backend choice ("" for automatic), creating the state when needed.
func SaveBackend(dir, backend string) (State, error) {
	s, err := EnsureState(dir)
	if err != nil {
		return State{}, err
	}
	s.Backend = backend
	s.dir = dir
	return s, writeState(dir, s)
}

// writeState replaces the state file through a temp file so a crash never leaves it truncated. It is private to the user because the token authorizes use of their quota.
func writeState(dir string, s State) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	return WriteFileAtomic(statePath(dir), append(data, '\n'), 0o600)
}

// WriteFileAtomic replaces path through a new temp file in the same directory, so a crash never leaves a truncated file. The temp file is created exclusively under a random name: a fixed name could already exist as a symlink, planted by a cloned repository in its .claude directory, and writing through it would overwrite whatever file it points to.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
