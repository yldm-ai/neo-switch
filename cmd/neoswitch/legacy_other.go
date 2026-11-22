//go:build !unix

package main

import (
	"errors"

	"github.com/yldm-ai/neo-switch/internal/daemon"
)

// stopLegacyDaemon: handovers are off on this platform (daemon.HandoverSupported), so nothing is replaced.
func stopLegacyDaemon(daemon.State) error {
	return errors.New("replacing a running NeoSwitch is not supported on this platform")
}
