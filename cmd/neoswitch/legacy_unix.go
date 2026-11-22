//go:build unix

package main

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yldm-ai/neo-switch/internal/daemon"
)

// stopLegacyDaemon stops a daemon from before handovers (0.4.4 and earlier) that serves state's port, so /neoswitch:on can start the update in its place. It sends SIGKILL, not SIGTERM: on SIGTERM those versions turn every project back to Claude and drop every opt-in, which would silently put the Gemini sessions back on Claude. Killed, it leaves the opt-ins and settings on disk for the new daemon. A reply it is giving is cut off. It stops only a process named neoswitch that listens on the port, after its status answered with NeoSwitch's token, and its agy with it.
func stopLegacyDaemon(state daemon.State) error {
	_, port, err := net.SplitHostPort(state.Listen)
	if err != nil {
		return err
	}
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN", "-t").Output()
	if err != nil {
		return fmt.Errorf("find the process on port %s: %w", port, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]))
	if err != nil || pid <= 1 {
		return fmt.Errorf("find the process on port %s", port)
	}
	comm, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil || !strings.HasPrefix(filepath.Base(strings.TrimSpace(string(comm))), "neoswitch") {
		return fmt.Errorf("the process on port %s is not NeoSwitch", port)
	}
	if children, err := exec.Command("pgrep", "-P", strconv.Itoa(pid)).Output(); err == nil {
		for _, field := range strings.Fields(string(children)) {
			if child, err := strconv.Atoi(field); err == nil && child > 1 {
				_ = syscall.Kill(child, syscall.SIGKILL)
			}
		}
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, running := daemon.Status(state); !running {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("the earlier NeoSwitch did not stop")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
