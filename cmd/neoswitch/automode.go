package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// autoModeServerEnv switches auto mode's server-side check off; Claude Code reads it once, at launch.
const autoModeServerEnv = "CLAUDE_CODE_AUTO_MODE_SERVER"

// autoModeNote explains what Claude Code's own message about the check cannot: in a session NeoSwitch answers with Gemini, auto mode's default check, which runs on Anthropic's server, never sees the actions, so Claude Code stops before each one. Started with the check off, Claude Code asks for it itself, and NeoSwitch sends that request to Claude (internal/daemon/autocheck.go).
const autoModeNote = "NeoSwitch: auto mode cannot check this session's actions, as its check runs on Anthropic's server, which never sees what Gemini does, so Claude Code will stop before actions to ask you. To use auto mode with Gemini, start Claude Code with " + autoModeServerEnv + "=0: Claude Code then asks Claude about each action itself, with your Claude login. Or choose another permission mode with Shift+Tab."

// autoModeUnchecked reports whether a session in permission mode mode, started with value as autoModeServerEnv, has auto mode on without a check that works with Gemini.
func autoModeUnchecked(mode, value string) bool {
	if mode != "auto" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "no", "off":
		return false
	}
	return true
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
