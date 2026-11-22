//go:build unix

package main

import (
	"os"
	"testing"

	"github.com/yldm-ai/neo-switch/internal/daemon"
)

// Daemons from before handovers are replaced by killing them: unlike SIGTERM, which turns every project back to Claude, that leaves the opt-ins and settings on disk, and the next daemon serves the sessions where they were.
func TestStoppingADaemonFromBeforeHandoversKeepsItsSessions(t *testing.T) {
	stateDir := isolateBackendEnv(t, true)
	id := inSession(t)
	project := t.TempDir()
	mustOn(t, project)
	state, ok, err := daemon.LoadState(stateDir)
	if err != nil || !ok {
		t.Fatalf("state: %v %v", ok, err)
	}
	if err := stopLegacyDaemon(state); err != nil {
		t.Fatal(err)
	}
	if _, running := daemon.Status(state); running {
		t.Fatal("the daemon still answers")
	}
	if !settingsExist(t, project) {
		t.Fatal("stopping the daemon turned the project back to Claude")
	}
	if _, err := daemon.AttachSession(state, daemon.Session{PID: os.Getpid(), ID: id}, func() error { return spawnDaemon(stateDir) }); err != nil {
		t.Fatal(err)
	}
	if info, ok := daemon.StatusOf(state, id); !ok || !info.OptedIn {
		t.Fatalf("the next daemon lost the session's opt-in: %+v %v", info, ok)
	}
}
