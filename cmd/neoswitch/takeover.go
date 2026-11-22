package main

import (
	"github.com/yldm-ai/neo-switch/internal/daemon"
)

// An update reaches the open windows without closing them: the first NeoSwitch of the new version that attaches a session starts a daemon that takes the port over from the running one (internal/daemon/handover.go). /neoswitch:on does the same for another setup, such as a new model, which the user asked for there.

type takeoverPlan int

const (
	// keepDaemon: the running daemon is right, newer, or cannot be replaced from here.
	keepDaemon takeoverPlan = iota
	// handOver: start a daemon that asks the running one to hand over.
	handOver
	// replaceLegacy: the running daemon predates handovers; only /neoswitch:on replaces it, by stopping it (stopLegacyDaemon).
	replaceLegacy
)

// planTakeover decides what to do about the running daemon info, for a NeoSwitch configured with label and model. explicit is /neoswitch:on, which also replaces a daemon of the same release that runs another build or setup. A passive attach replaces only an earlier release that serves the same setup: a window's GEMINI_MODEL must not switch every other window, and a later daemon is never replaced, so two plugin versions open at once cannot take the port back and forth.
func planTakeover(dir string, info daemon.Info, label, model string, explicit bool) takeoverPlan {
	if !daemon.HandoverSupported() || daemon.NewerVersion(info.Version, version) {
		return keepDaemon
	}
	// A daemon from before versions were reported is earlier than any that reports one.
	earlier := info.Version == "" || daemon.NewerVersion(version, info.Version)
	otherSetup := info.Backend != "" && info.Backend != label || info.Model != "" && info.Model != model
	switch {
	case explicit && (earlier || otherSetup || info.Version != version):
	case !explicit && earlier && !otherSetup && !daemon.TakeoverFailedRecently(dir, version):
	default:
		return keepDaemon
	}
	if info.Handover >= daemon.HandoverProtocol {
		return handOver
	}
	if explicit {
		return replaceLegacy
	}
	return keepDaemon
}

// startTakeover starts the daemon that takes over and returns at once: hooks have seconds, and the running daemon keeps answering until its successor listens.
func startTakeover(dir string, explicit bool) bool {
	args := []string{"--take-over"}
	if explicit {
		args = append(args, "--explicit")
	}
	return spawnDaemon(dir, args...) == nil
}

// movingNote tells the session that started a takeover what the other windows will notice. The takeover runs on after the hook returns, so it is announced, not confirmed; /neoswitch:status says which version serves the windows.
func movingNote() string {
	return "NeoSwitch: handing every open window over to " + version + ". Each Gemini conversation then reads itself again on its next reply, which takes longer once."
}

// moveWindowsHint tells how the user moves the open windows to target, a version or setup, themselves. /neoswitch:on is offered only where it would do it: never on a platform without handovers, and never over a daemon of a later release, which this NeoSwitch does not replace.
func moveWindowsHint(info daemon.Info, target string) string {
	switch {
	case !daemon.HandoverSupported() || daemon.NewerVersion(info.Version, version):
		return closeWindowsHint(target)
	case info.Handover >= daemon.HandoverProtocol:
		return "Run /neoswitch:on here to move every open window to " + target + "."
	default:
		return "Run /neoswitch:on here to move every open window to " + target + "; a reply still in progress in another window stops."
	}
}

// closeWindowsHint is the way to target that always works.
func closeWindowsHint(target string) string {
	return "To use " + target + ", close every Claude Code window of a project that uses NeoSwitch, open Claude Code again and run /neoswitch:on."
}
