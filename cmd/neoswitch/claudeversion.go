package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yldm-ai/neo-switch/internal/daemon"
)

// dialogMinVersion is the first Claude Code version where the plugin's mod shows /neoswitch:login's link and asks for the code itself, without a model reply: mods load from 2.1.287, and a command's output stays out of model requests (isTextDisplayOnly) from 2.1.326. The mod hands older versions to the skill, whose output reaches the user only through a model reply. plugin/mod/commands.ts holds the same number.
const dialogMinVersion = "2.1.326"

const claudeCodeExecEnv = "CLAUDE_CODE_EXECPATH"

// dialogVersionHint tells a user whose /neoswitch:login went through the skill that updating Claude Code shows the link at once. It says so only when the running Claude Code is known to be older than dialogMinVersion: the skill also runs where mods do not load at all (WSL, the desktop app over SSH, a policy that allows only managed mods), and there an update would not help.
func dialogVersionHint() string {
	version := runningClaudeCodeVersion()
	if !daemon.NewerVersion(dialogMinVersion, version) {
		return ""
	}
	return "Claude Code " + dialogMinVersion + " or later shows this link at once, without waiting for a reply, and asks for the code in a dialog. This session runs " + version + "; update it with: claude update"
}

// runningClaudeCodeVersion is the version of the Claude Code that runs this command, read from its executable, which Claude Code names in CLAUDE_CODE_EXECPATH, or "" when unknown. A native install keeps each version in a file named after it. Otherwise only an executable named claude is asked, and briefly: under npm the path is node, and the desktop app's executable is not one to start.
func runningClaudeCodeVersion() string {
	path := strings.TrimSpace(os.Getenv(claudeCodeExecEnv))
	if path == "" {
		return ""
	}
	if name := filepath.Base(path); name != "claude" {
		if _, ok := daemon.ParseVersion(name); ok && !strings.HasPrefix(name, "v") {
			return name
		}
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return ""
	}
	// Claude Code answers such as "2.1.330 (Claude Code)".
	version, rest, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	if _, ok := daemon.ParseVersion(version); !ok || rest != "(Claude Code)" {
		return ""
	}
	return version
}
