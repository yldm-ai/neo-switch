package provider

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Tool guard.
//
// agy is a full coding agent. Its model is told to leave its own tools alone, and NeoSwitch refuses every permission request agy sends, but many of agy's tools never ask: verified, invoke_subagent and swarm start agents that read files on this computer, and schedule starts background tasks. Tools must run only in Claude Code, under its permission rules, so agy runs in a folder of NeoSwitch's own (never the user's project, whose .agents rules, skills, MCP servers and hooks would load into agy) holding an agy PreToolUse hook that refuses every tool except search_web, the Google search NeoSwitch uses for Claude Code's WebSearch:
//   - agy runs the hook before each tool, its subagents' included; a hook that fails, prints something else or times out blocks the tool, and a deny wins over an allow from the user's own hooks (all verified with agy).
//   - The hook is a copy of this executable named by its content, so a plugin update that deletes the old binary does not break a running daemon's hook, and it is called by a path relative to the hook folder, which needs no quoting on any system.
//   - The hook records each decision per session, and NeoSwitch counts them against the tool calls agy reports. A tool call without a decision means agy no longer runs the hook, for example after an update changed how it finds hooks; the Antigravity route then stops for good (see checkGuard).

const (
	// AgyHookCommand is the subcommand agy's hook runs.
	AgyHookCommand = "agy-hook"
	// agyAllowedTool is the only agy tool the guard lets run.
	agyAllowedTool = "search_web"
	// guardRecords is where the hook records its decisions, one file per session, relative to the workspace.
	guardRecords = ".neoswitch/tool-decisions"
	// guardRecordsFromHook is the same folder seen from the hook, which agy runs in the folder holding hooks.json.
	guardRecordsFromHook = "../" + guardRecords
	// guardRecordAge is how long a session's decisions are kept; sessions end long before.
	guardRecordAge = 24 * time.Hour
)

// AntigravityWorkspace is the folder agy runs in, inside NeoSwitch's state folder.
func AntigravityWorkspace(stateDir string) string {
	return filepath.Join(stateDir, "agy-workspace")
}

var (
	selfOnce sync.Once
	selfPath string
	selfSum  string
	selfErr  error
)

// selfExecutable returns this executable and the hash of its content.
func selfExecutable() (string, string, error) {
	selfOnce.Do(func() {
		path, err := os.Executable()
		if err == nil {
			path, err = filepath.EvalSymlinks(path)
		}
		if err != nil {
			selfErr = err
			return
		}
		f, err := os.Open(path)
		if err != nil {
			selfErr = err
			return
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			selfErr = err
			return
		}
		selfPath, selfSum = path, hex.EncodeToString(h.Sum(nil))[:16]
	})
	return selfPath, selfSum, selfErr
}

// installToolGuard prepares workspace: the hook binary, hooks.json, and the decision records, dropping records and hook binaries older than a day or a week. Files are rewritten only when their content changes, atomically, so a running agy never reads half a file.
func installToolGuard(workspace string) error {
	exe, sum, err := selfExecutable()
	if err != nil {
		return fmt.Errorf("locate NeoSwitch: %w", err)
	}
	agents := filepath.Join(workspace, ".agents")
	records := filepath.Join(workspace, guardRecords)
	for _, dir := range []string{filepath.Join(agents, "bin"), records} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	name := "neoswitch-hook-" + sum
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	hook := filepath.Join(agents, "bin", name)
	if _, err := os.Stat(hook); err != nil {
		if err := copyFileAtomic(exe, hook, 0o700); err != nil {
			return fmt.Errorf("copy the tool guard: %w", err)
		}
	}
	command := "./bin/" + name
	if runtime.GOOS == "windows" {
		command = `bin\` + name
	}
	config, _ := json.MarshalIndent(map[string]any{"neoswitch-tool-guard": map[string]any{
		"PreToolUse": []any{map[string]any{
			"matcher": "*",
			"hooks":   []any{map[string]any{"type": "command", "command": command + " " + AgyHookCommand + " pre-tool", "timeout": 10}},
		}},
	}}, "", "  ")
	if err := writeIfChanged(filepath.Join(agents, "hooks.json"), append(config, '\n')); err != nil {
		return err
	}
	pruneOlder(records, guardRecordAge, "")
	pruneOlder(filepath.Join(agents, "bin"), 7*24*time.Hour, name)
	return nil
}

func copyFileAtomic(from, to string, mode os.FileMode) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	tmp, err := os.CreateTemp(filepath.Dir(to), ".tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), to)
}

func writeIfChanged(path string, data []byte) error {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// pruneOlder removes the files in dir not modified for age, except keep.
func pruneOlder(dir string, age time.Duration, keep string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.Name() == keep {
			continue
		}
		if info, err := entry.Info(); err == nil && !info.IsDir() && time.Since(info.ModTime()) > age {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

// guardSessionID is the shape of agy's conversation IDs; the hook writes nothing for anything else, so a payload cannot point it outside the records folder.
var guardSessionID = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// RunAgyHook is the agy hook (agy-hook pre-tool): it reads agy's PreToolUse payload from stdin and answers allow for search_web and deny for every other tool, or for anything it cannot read. It records the decision for the session first.
func RunAgyHook(args []string, stdin io.Reader, stdout io.Writer) error {
	decision := map[string]string{"decision": "deny", "reason": "NeoSwitch runs tools only in Claude Code, so Antigravity's own tools are switched off. Answer without them."}
	defer func() { _ = json.NewEncoder(stdout).Encode(decision) }()
	if len(args) != 1 || args[0] != "pre-tool" {
		return nil
	}
	var payload struct {
		ConversationID string `json:"conversationId"`
		ToolCall       struct {
			Name string `json:"name"`
		} `json:"toolCall"`
	}
	if err := json.NewDecoder(io.LimitReader(stdin, 16<<20)).Decode(&payload); err != nil {
		return nil
	}
	allow := payload.ToolCall.Name == agyAllowedTool
	if guardSessionID.MatchString(payload.ConversationID) {
		verdict := "deny"
		if allow {
			verdict = "allow"
		}
		if f, err := os.OpenFile(filepath.Join(guardRecordsFromHook, payload.ConversationID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			fmt.Fprintf(f, "%s %s\n", verdict, strings.ReplaceAll(payload.ToolCall.Name, "\n", ""))
			f.Close()
		}
	}
	if allow {
		decision = map[string]string{"decision": "allow"}
	}
	return nil
}

// errUnguarded is why the Antigravity route stopped: agy ran a tool without asking the guard.
var errUnguarded = errors.New("Antigravity ran one of its own tools without NeoSwitch's tool guard")

// guardDecisions counts the decisions the hook recorded for a session.
func guardDecisions(workspace, session string) int {
	if !guardSessionID.MatchString(session) {
		return 0
	}
	f, err := os.Open(filepath.Join(workspace, guardRecords, session))
	if err != nil {
		return 0
	}
	defer f.Close()
	n := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		n++
	}
	return n
}

// forgetDecisions removes a closed session's records.
func forgetDecisions(workspace, session string) {
	if guardSessionID.MatchString(session) {
		_ = os.Remove(filepath.Join(workspace, guardRecords, session))
	}
}
