package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The registry maps Claude Code session ids to their processes. The SessionStart hook writes one file per session, named after its id and holding its process id, with a single shell write that costs a session nothing else; it does so only where NeoSwitch has a state folder. The daemon reads it to place the requests of sessions it has not met: a /clear gives the process a new id, and a window may apply a project's settings without NeoSwitch ever attaching it.

func registryDir(dir string) string { return filepath.Join(dir, "sessions") }

// registeredPID returns the process of session, or 0.
func registeredPID(dir, session string) int {
	if dir == "" || session == "" || strings.ContainsAny(session, `/\.`) {
		return 0
	}
	data, err := os.ReadFile(filepath.Join(registryDir(dir), session))
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

// pruneRegistry removes the entries of processes that have exited.
func pruneRegistry(dir string, alive func(int) bool) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(registryDir(dir))
	if err != nil {
		return
	}
	for _, entry := range entries {
		if pid := registeredPID(dir, entry.Name()); pid <= 1 || !alive(pid) {
			_ = os.Remove(filepath.Join(registryDir(dir), entry.Name()))
		}
	}
}
