package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// autoModeServerEnv switches auto mode's server-side check off; Claude Code reads it once, at launch.
const autoModeServerEnv = "CLAUDE_CODE_AUTO_MODE_SERVER"

// autoModeSafeguardEnv turns NeoSwitch's built-in Auto Mode classifier off when set to 0/false/no/off.
const autoModeSafeguardEnv = "NEOSWITCH_SAFEGUARD"

// autoModeNote is shown once per session only when the user explicitly turned off NeoSwitch's
// built-in Auto Mode classifier (NEOSWITCH_SAFEGUARD=off) while running in auto mode without
// CLAUDE_CODE_AUTO_MODE_SERVER=0.
const autoModeNote = "NeoSwitch: NEOSWITCH_SAFEGUARD is off, so auto mode cannot check this session's actions and Claude Code will ask before actions. Unset NEOSWITCH_SAFEGUARD to use NeoSwitch's built-in check, start Claude Code with " + autoModeServerEnv + "=0 to have Claude check actions with your Claude login, or choose another permission mode with Shift+Tab."

// autoModeUnchecked reports whether a session in permission mode mode, started with serverVal
// as CLAUDE_CODE_AUTO_MODE_SERVER and safeguardVal as NEOSWITCH_SAFEGUARD, has auto mode on
// without any check enabled.
func autoModeUnchecked(mode, serverVal, safeguardVal string) bool {
	if mode != "auto" {
		return false
	}
	if !envOff(safeguardVal) {
		return false
	}
	return !envOff(serverVal)
}

func envOff(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "no", "off":
		return true
	}
	return false
}

// autoModeNotesKept is how long the marks of sessions that saw the note are kept; a session left open longer sees it once more.
const autoModeNotesKept = 7 * 24 * time.Hour

// firstAutoModeNote reports whether session has not seen the note yet, and marks it as seen. Marks older than autoModeNotesKept are removed.
func firstAutoModeNote(dir, session string) bool {
	if session == "" || strings.ContainsAny(session, `/\.`) {
		return false
	}
	notes := filepath.Join(dir, "auto-mode-noted")
	if err := os.MkdirAll(notes, 0o700); err != nil {
		return false
	}
	file, err := os.OpenFile(filepath.Join(notes, session), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false
	}
	_ = file.Close()
	if entries, err := os.ReadDir(notes); err == nil {
		for _, entry := range entries {
			if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > autoModeNotesKept {
				_ = os.Remove(filepath.Join(notes, entry.Name()))
			}
		}
	}
	return true
}
