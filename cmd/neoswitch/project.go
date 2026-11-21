package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yldm-ai/neo-switch/internal/daemon"
	"github.com/yldm-ai/neo-switch/internal/provider"
	"github.com/yldm-ai/neo-switch/internal/server"
)

func projectDir(args []string) (string, error) {
	dir := os.Getenv("CLAUDE_PROJECT_DIR")
	if len(args) > 0 {
		dir = args[0]
	}
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return "", err
		}
	}
	return filepath.Abs(dir)
}

// on routes Claude Code sessions in one project to the shared NeoSwitch daemon. Only the project's settings.local.json is touched, so other projects keep using Anthropic.
//
// A Claude Code session that is not in gateway mode applies env written to its project settings on its next request, without restarting, and entries removed later stay applied. So when on runs inside such a session (/neoswitch:on, or /neoswitch:login which ends with it), that session moves to the proxy straight away: the daemon is started and the session attached before the settings are written, because otherwise its next request would find nothing listening.
func on(args []string) error {
	project, err := projectDir(args)
	if err != nil {
		return err
	}
	return enableProject(project, currentSession(), nil)
}

// routeNotReadyError means the route cannot answer yet, for a reason the user can fix: the project stays on Claude.
type routeNotReadyError struct{ reason string }

func (e *routeNotReadyError) Error() string {
	return "NeoSwitch did not switch this project, so it keeps using Claude. " + e.reason
}

// preflightTimeout bounds the check of agy before a project is switched; a signed-in agy answers in about two seconds.
var preflightTimeout = 30 * time.Second

// antigravityReady checks that agy is signed in and offers the configured model, starting agy when models is nil. agy refusing to start counts as not ready, since every request would then fail the same way.
func antigravityReady(models []string) error {
	backend, err := provider.NewAntigravityACPFromEnv(context.Background())
	if err != nil {
		return &routeNotReadyError{reason: sentence(err.Error())}
	}
	if models == nil {
		ctx, cancel := context.WithTimeout(context.Background(), preflightTimeout)
		defer cancel()
		signed, offered, err := backend.SignedIn(ctx)
		switch {
		case err != nil:
			return &routeNotReadyError{reason: "Google's Antigravity command-line tool (agy) did not answer (" + err.Error() + "). Run /neoswitch:login, which repairs it."}
		case !signed:
			return &routeNotReadyError{reason: "You are not signed in to Google through agy. Run /neoswitch:login."}
		}
		models = offered
	}
	if len(models) > 0 && !slices.Contains(models, backend.Model()) {
		return &routeNotReadyError{reason: provider.ModelNotOfferedMessage(backend.Model(), models)}
	}
	return nil
}

// claudeSession is the Claude Code session a command runs in. A sign-in finished in the browser turns the project on from the sign-in helper, which records the session of the /neoswitch:login that started it.
type claudeSession struct {
	pid     int
	inside  bool
	gateway bool
}

func currentSession() claudeSession {
	pid, inside := callingSession()
	return claudeSession{pid: pid, inside: inside, gateway: gatewayModeOn()}
}

// enableProject turns project on for session. models are the models agy offers the account when the caller just checked them, or nil.
func enableProject(project string, session claudeSession, models []string) error {
	kind, _, err := resolveBackend()
	if err != nil {
		return err
	}
	// A session applies the settings on its next request and keeps them, so switching it to a route that cannot answer would leave it failing until it closes. The route is checked first.
	if kind == backendAntigravity {
		if err := antigravityReady(models); err != nil {
			return err
		}
	}
	dir, err := daemon.Dir()
	if err != nil {
		return err
	}
	state, err := daemon.EnsureState(dir)
	if err != nil {
		return err
	}
	pid, inside := session.pid, session.inside
	var notice string
	tracked := false
	if inside && !session.gateway && pid > 1 {
		if tracked, notice, err = serveSession(dir, &state, pid, projectKey(project)); err != nil {
			return fmt.Errorf("NeoSwitch was not turned on, so this session keeps using Claude: %w", err)
		}
	}
	if err := daemon.Enable(project, state); err != nil {
		return err
	}
	fmt.Printf("NeoSwitch is on for %s.\n", project)
	switch {
	case !inside:
		fmt.Println("New Claude Code sessions in this project use Gemini, until the last of them closes; the project then goes back to Claude. Sessions already open here that were not started through a gateway switch with their next message; run /neoswitch:on in them so NeoSwitch answers them.")
	case session.gateway:
		fmt.Println("This session was started through a gateway (" + daemon.EnvUseGateway + ") and keeps its connection. Start a new Claude Code session in this project to use Gemini.")
	case pid <= 1:
		fmt.Println("This Claude Code version does not tell NeoSwitch which session is running it, so this session cannot use Gemini. Start a new Claude Code session in this project.")
	case tracked:
		fmt.Println("This session uses Gemini from your next message, and so do new Claude Code sessions in this project while it is open. When the last of them closes, the project goes back to Claude; run /neoswitch:on to use Gemini again. Other Claude Code windows already open here switch too; run /neoswitch:on in each of them so NeoSwitch keeps answering them after this window closes.")
	default:
		// A daemon from before project tracking serves this session, so the project stays on until /neoswitch:off.
		fmt.Println("This session uses Gemini from your next message, and so do new Claude Code sessions in this project until you run /neoswitch:off. Other Claude Code windows already open here switch too; run /neoswitch:on in each of them so NeoSwitch keeps answering them after this window closes.")
	}
	if notice != "" {
		fmt.Println(notice)
	}
	// The settings file holds the proxy token. Excluding it in the clone's own exclude file keeps it out of commits without touching the shared .gitignore; the note is only for when that does not work.
	settings := filepath.Join(".claude", "settings.local.json")
	if ignored, known := gitIgnored(project, settings); known && !ignored {
		if err := gitExclude(project, settings); err != nil || !isGitIgnored(project, settings) {
			fmt.Println("Note: Git may include .claude/settings.local.json in commits. It holds NeoSwitch's local access key, so add it to .gitignore.")
		}
	}
	return nil
}

// callingSession reports whether the command runs inside a Claude Code session (a slash command, the Bash tool, or a process they started), and that session's process ID, which Claude Code passes to child processes as CLAUDE_PID. pid is 0 when the Claude Code version does not provide it.
func callingSession() (pid int, inside bool) {
	if os.Getenv("CLAUDECODE") != "1" {
		return 0, false
	}
	pid, _ = strconv.Atoi(strings.TrimSpace(os.Getenv("CLAUDE_PID")))
	return pid, true
}

// projectKey names a project the same way whichever path reached it, such as a symlink, so the daemon sees two sessions of one project as one.
func projectKey(project string) string {
	if resolved, err := filepath.EvalSymlinks(project); err == nil {
		return resolved
	}
	return project
}

// serveSession makes the shared daemon serve the Claude Code process pid, starting it with the configured backend when none is running. The notice is non-empty when an already running daemon serves a different setup than the one configured now.
// serveSession attaches pid to the daemon. A saved port that another program took is replaced in *state, so the settings written afterwards point at the new one.
func serveSession(dir string, state *daemon.State, pid int, project string) (tracked bool, notice string, err error) {
	kind, _, err := resolveBackend()
	if err != nil {
		return false, "", err
	}
	// Pinning the kind makes the daemon spawned below, whose environment drops Claude Code's variables, serve the backend checked here.
	_, model, label, err := configuredBackend(context.Background())
	if err != nil {
		return false, "", err
	}
	_ = os.Setenv(backendEnv, kind)
	spawn := func() error { return spawnDaemon(dir) }
	tracked, err = daemon.Attach(*state, pid, project, spawn)
	var busy *daemon.PortInUseError
	if errors.As(err, &busy) {
		moved, moveErr := daemon.MovePort(dir, *state)
		if moveErr != nil {
			return false, "", moveErr
		}
		*state = moved
		tracked, err = daemon.Attach(*state, pid, project, spawn)
	}
	if err != nil {
		return false, "", fmt.Errorf("NeoSwitch could not start (details are in %s)", filepath.Join(dir, "daemon.log"))
	}
	if info, running := daemon.Status(*state); running && (info.Backend != "" && info.Backend != label || info.Version != version) {
		return tracked, attachNotice("", model, label, info, running, false), nil
	}
	return tracked, "", nil
}

func off(args []string) error {
	project, err := projectDir(args)
	if err != nil {
		return err
	}
	dir, err := daemon.Dir()
	if err != nil {
		return err
	}
	// A sign-in still waiting would turn the project back on when it finishes in the browser. It is told first, so it cannot turn the project on after it was turned off.
	if socket, err := signInSocket(dir); err == nil {
		forgetPendingSignIn(socket, project)
	}
	state, ok, err := daemon.LoadState(dir)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Printf("NeoSwitch was never turned on; %s is unchanged.\n", project)
		return nil
	}
	if err := daemon.Disable(project, state); err != nil {
		return err
	}
	// A running session keeps env it already applied from the project settings after the entries are removed, so a session on NeoSwitch stays there; it stays attached to the daemon, which keeps answering it until it closes.
	fmt.Printf("NeoSwitch is off for %s. New Claude Code sessions here use Anthropic again; sessions already open keep their current connection until they are closed.\n", project)
	return nil
}

func status(args []string) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	verbose := flags.Bool("v", false, "also print addresses, the backend choice and the background service state")
	if err := flags.Parse(args); err != nil {
		return err
	}
	project, err := projectDir(flags.Args())
	if err != nil {
		return err
	}
	return printStatus(os.Stdout, project, *verbose)
}

// printStatus answers in two or three plain lines whether this project and this session use NeoSwitch, whether it can work, and the one command to run next. Addresses, the backend choice and the daemon state follow only when verbose.
func printStatus(w io.Writer, project string, verbose bool) error {
	dir, err := daemon.Dir()
	if err != nil {
		return err
	}
	state, ok, err := daemon.LoadState(dir)
	if err != nil {
		return err
	}
	enabled := false
	if ok {
		if enabled, err = daemon.Enabled(project, state); err != nil {
			return err
		}
	}
	// The session inherits the project's settings when it starts, so its ANTHROPIC_BASE_URL tells whether this session uses NeoSwitch, which can differ from the project right after on or off.
	session := ok && os.Getenv("ANTHROPIC_BASE_URL") == state.BaseURL()
	// configuredBackend checks credentials and agy locally without calling the model, so a missing sign-in is reported instead of a route that cannot work.
	_, model, label, backendErr := configuredBackend(context.Background())
	var info daemon.Info
	running := false
	if ok && (enabled || session) {
		info, running = daemon.Status(state)
	}
	switch {
	case !enabled && session:
		fmt.Fprintln(w, "NeoSwitch is off for this project, but this session still uses it.")
		fmt.Fprintln(w, "Start a new Claude Code session here to go back to Claude.")
	case !enabled && backendErr != nil:
		fmt.Fprintln(w, "This project uses Claude.")
		fmt.Fprintln(w, sentence(backendErr.Error()))
	case !enabled:
		fmt.Fprintln(w, "This project uses Claude.")
		fmt.Fprintf(w, "Run /neoswitch:on to switch it to %s.\n", modelName(model))
	case backendErr != nil:
		fmt.Fprintln(w, "NeoSwitch is on for this project, but it cannot reach a model.")
		fmt.Fprintln(w, sentence(backendErr.Error()))
		fmt.Fprintln(w, "Until then, run /neoswitch:off and start a new session to go back to Claude.")
	case !session:
		fmt.Fprintln(w, "NeoSwitch is on for this project, but this session still uses Claude.")
		fmt.Fprintf(w, "Run /neoswitch:on to use %s from this session's next message.\n", modelName(model))
	case !running:
		fmt.Fprintf(w, "This session is set to use %s, but NeoSwitch is not running, so it cannot answer.\n", modelName(model))
		fmt.Fprintln(w, "Run /neoswitch:on to restart it, or /neoswitch:off and a new session to go back to Claude.")
	default:
		fmt.Fprintf(w, "This session uses %s through %s, and NeoSwitch is running.\n", modelName(model), plainLabel(label))
		switch {
		case info.Backend != "" && info.Backend != label:
			fmt.Fprintf(w, "Other Claude Code windows still share the earlier setup (%s), so this session uses it too. Close all Claude Code windows, open Claude Code again and run /neoswitch:on to switch.\n", plainLabel(info.Backend))
		case info.Version != version:
			fmt.Fprintln(w, "NeoSwitch was updated, but this session still uses the earlier version. Close all Claude Code windows, open Claude Code again and run /neoswitch:on to use the update.")
		default:
			fmt.Fprintln(w, "Run /neoswitch:off to go back to Claude.")
		}
	}
	if notice := updateNotice(dir); notice != "" {
		fmt.Fprintln(w, notice)
	}
	if !verbose {
		return nil
	}
	fmt.Fprintf(w, "\nproject:  %s\n", project)
	if enabled {
		fmt.Fprintf(w, "routing:  NeoSwitch %s\n", state.BaseURL())
	} else {
		fmt.Fprintln(w, "routing:  Anthropic (NeoSwitch off)")
	}
	if session {
		fmt.Fprintln(w, "session:  NeoSwitch")
	} else {
		fmt.Fprintln(w, "session:  not through NeoSwitch")
	}
	if kind, reason, err := resolveBackend(); err != nil {
		fmt.Fprintf(w, "backend:  none: %v\n", err)
	} else if backendErr != nil {
		fmt.Fprintf(w, "backend:  %s (%s), not usable: %v\n", kind, reason, backendErr)
	} else {
		fmt.Fprintf(w, "backend:  %s (%s), %s, model %s\n", kind, reason, label, model)
	}
	if running {
		fmt.Fprintf(w, "daemon:   running %s on %s, %d Claude Code session(s) attached\n", versionOrUnknown(info.Version), info.Backend, info.Sessions)
	} else {
		fmt.Fprintln(w, "daemon:   not running")
	}
	fmt.Fprintf(w, "version:  %s\n", version)
	return nil
}

func versionOrUnknown(v string) string {
	if v == "" {
		return "an earlier version"
	}
	return v
}

// sentence turns an error message into a sentence for the user: capitalized and ending with a period.
func sentence(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return message
	}
	message = strings.ToUpper(message[:1]) + message[1:]
	if !strings.HasSuffix(message, ".") {
		message += "."
	}
	return message
}

// attach runs from the plugin's SessionStart hook. Claude Code waits for that hook before its first model request, so the daemon is up by the time the session needs it. Problems are reported to the user through the hook's systemMessage instead of failing silently.
func attach(args []string) error {
	flags := flag.NewFlagSet("attach", flag.ContinueOnError)
	pid := flags.Int("pid", 0, "Claude Code process to serve")
	if err := flags.Parse(args); err != nil {
		return err
	}
	dir, err := daemon.Dir()
	if err != nil {
		return hookMessage(err.Error())
	}
	state, ok, err := daemon.LoadState(dir)
	if err != nil {
		return hookMessage(err.Error())
	}
	// The hook inherits the session's settings env, so a matching base URL means this project is routed through NeoSwitch. It is the URL the session uses unless gateway mode overrides it, which is handled below.
	if !ok || os.Getenv("ANTHROPIC_BASE_URL") != state.BaseURL() {
		return nil
	}
	if *pid <= 1 {
		return hookMessage("NeoSwitch: the SessionStart hook did not pass the Claude Code process ID")
	}
	// Every routed session refreshes the update cache, including one blocked by a setup problem below: a fix may be what it needs.
	startUpdateCheck(dir)
	// Projects enabled before `neoswitch on` turned gateway mode off still let a gateway launcher's ANTHROPIC_BASE_URL win. The running session cannot be changed any more, so fix the project for the next one and say so instead of letting status claim a route the session may not use.
	var notice string
	if gatewayModeOn() {
		project, err := projectDir(nil)
		if err == nil {
			err = daemon.Enable(project, state)
		}
		if err != nil {
			return hookMessage("NeoSwitch: Claude Code was started in gateway mode (" + daemon.EnvUseGateway + "), which can send this session's requests to the gateway instead of NeoSwitch, and turning it off for this project failed: " + err.Error())
		}
		notice = "NeoSwitch: Claude Code was started in gateway mode (" + daemon.EnvUseGateway + "), which can send this session's requests to the gateway instead of Gemini. This project's .claude/settings.local.json now turns gateway mode off; start a new session and run /neoswitch:on in it to use NeoSwitch."
	}
	kind, _, err := resolveBackend()
	if err != nil {
		return hookMessage(sessionBlocked(err.Error()))
	}
	// configuredBackend also moves a Gemini key saved by an earlier plugin version into GEMINI_API_KEY. Pinning the kind makes the daemon spawned below, whose environment drops Claude Code's variables, serve the backend checked here.
	_, model, label, err := configuredBackend(context.Background())
	if err != nil {
		return hookMessage(sessionBlocked(err.Error()))
	}
	_ = os.Setenv(backendEnv, kind)
	// The hook runs in the session's project, which CLAUDE_PROJECT_DIR names.
	project := ""
	if path, err := projectDir(nil); err == nil {
		project = projectKey(path)
	}
	tracked, err := daemon.Attach(state, *pid, project, func() error { return spawnDaemon(dir) })
	if err != nil {
		var busy *daemon.PortInUseError
		if errors.As(err, &busy) {
			// This session already applied the old port and cannot answer. The project goes back to Claude for the next session, and NeoSwitch moves to a free port, which /neoswitch:on uses.
			_, port, _ := net.SplitHostPort(busy.Listen)
			if project != "" {
				_ = daemon.Disable(project, state)
			}
			_, _ = daemon.MovePort(dir, state)
			return hookMessage("NeoSwitch: another program on this computer took port " + port + ", which NeoSwitch used, so this session cannot answer. NeoSwitch moved to a new port: run /neoswitch:on to switch this session again, or start a new session, which uses Claude.")
		}
		return hookMessage("NeoSwitch could not start, so this session cannot answer. Start a new Claude Code session to try again, or run /neoswitch:off to go back to Claude. Details are in " + filepath.Join(dir, "daemon.log") + ".")
	}
	info, running := daemon.Status(state)
	return hookMessage(joinNotes(attachNotice(notice, model, label, info, running, tracked), updateNotice(dir)))
}

// sessionBlocked reports a setup problem found at session start, followed by the way back to Claude, because the session cannot answer until it is fixed.
func sessionBlocked(problem string) string {
	return withPrefix(strings.TrimSuffix(sentence(problem), ".") + ". Until then this session cannot answer; run /neoswitch:off and start a new session to go back to Claude.")
}

// withPrefix names NeoSwitch at the start of a hook message unless the message already starts with it.
func withPrefix(message string) string {
	if strings.HasPrefix(message, "NeoSwitch") {
		return message
	}
	return "NeoSwitch: " + message
}

// joinNotes joins the non-empty notes of a hook message.
func joinNotes(notes ...string) string {
	var kept []string
	for _, note := range notes {
		if note != "" {
			kept = append(kept, note)
		}
	}
	return strings.Join(kept, " ")
}

// attachNotice is the message a session sees after it attached. Without a problem to report it confirms the model, because Claude Code's header keeps showing the Claude model the session asks for.
func attachNotice(notice, model, label string, info daemon.Info, running, tracked bool) string {
	var notes []string
	if notice != "" {
		notes = append(notes, notice)
	}
	switch {
	case running && info.Backend != "" && info.Backend != label:
		notes = append(notes, "NeoSwitch: other Claude Code windows still share the earlier setup ("+plainLabel(info.Backend)+"), so this session uses it too. Close all Claude Code windows, open Claude Code again and run /neoswitch:on to switch to "+modelName(model)+" through "+plainLabel(label)+".")
	case running && info.Version != version:
		notes = append(notes, "NeoSwitch: other Claude Code windows still use the earlier NeoSwitch version, so this session does too. Close all Claude Code windows, open Claude Code again and run /neoswitch:on to use the update.")
	case notice == "":
		notes = append(notes, "NeoSwitch: this session uses "+modelName(model)+" through "+plainLabel(label)+". Ignore the Claude model name in the header. The first reply can take a while.")
		if tracked {
			notes = append(notes, "When the last session here using NeoSwitch closes, this project goes back to Claude.")
		}
	}
	return strings.Join(notes, " ")
}

// gatewayModeOn reports whether the session runs in Claude Code's gateway mode. Project settings that turn it off are already applied to the hook's environment.
func gatewayModeOn() bool {
	return envTrue(daemon.EnvUseGateway)
}

// endSession is the SessionEnd hook: the session ended, so the daemon turns its projects off at once. A /clear ends a conversation but not the process, which goes on in the same session.
func endSession(args []string) error {
	flags := flag.NewFlagSet("detach", flag.ContinueOnError)
	pid := flags.Int("pid", 0, "Claude Code process that ended")
	if err := flags.Parse(args); err != nil {
		return err
	}
	var input struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(os.Stdin).Decode(&input)
	if input.Reason == "clear" || *pid <= 1 {
		return nil
	}
	dir, err := daemon.Dir()
	if err != nil {
		return nil
	}
	if state, ok, err := daemon.LoadState(dir); err == nil && ok {
		daemon.Detach(state, *pid)
	}
	return nil
}

func hookMessage(message string) error {
	return json.NewEncoder(os.Stdout).Encode(map[string]string{"systemMessage": message})
}

// spawnDaemon starts `neoswitch daemon` in its own session so it survives the hook process, with a scrubbed environment: Claude Code's API settings and plugin options other than the Gemini key stay behind.
func spawnDaemon(dir string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(dir, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(self, "daemon")
	cmd.Dir = dir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = withoutClaudeCode(os.Environ())
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// withoutClaudeCode drops Claude Code's API settings and session variables from environ, for the background processes NeoSwitch starts from a session.
func withoutClaudeCode(environ []string) []string {
	var env []string
	for _, kv := range environ {
		if strings.HasPrefix(kv, "ANTHROPIC_") || strings.HasPrefix(kv, "CLAUDE_") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func runDaemon() error {
	dir, err := daemon.Dir()
	if err != nil {
		return err
	}
	state, ok, err := daemon.LoadState(dir)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("NeoSwitch was never turned on; run /neoswitch:on first")
	}
	backend, model, backendName, err := configuredBackend(context.Background())
	if err != nil {
		return err
	}
	if closer, ok := backend.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}
	if acp, ok := backend.(*provider.AntigravityACP); ok {
		acp.PendingSignIn = func() string { return pendingSignInLink(dir) }
		acp.SignedInAt = func() time.Time { return lastSignIn(dir) }
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(os.Stderr, "NeoSwitch daemon on %s (%s, model %s)\n", state.BaseURL(), backendName, model)
	handler := server.NewHandler(server.Config{Backend: backend, Model: model, Token: state.Token})
	// Routing lasts as long as the sessions using it: a project no attached session uses goes back to Claude, so the next session there, and its claude.ai connectors, start as before.
	release := func(project string) {
		if err := daemon.Disable(project, state); err != nil {
			fmt.Fprintf(os.Stderr, "NeoSwitch could not turn %s back to Claude: %v\n", project, err)
			return
		}
		fmt.Fprintf(os.Stderr, "NeoSwitch: %s is back on Claude\n", project)
	}
	if err := daemon.Run(ctx, state, handler, daemon.Options{Backend: backendName, Version: version, Release: release}); err != nil {
		return err
	}
	if ctx.Err() != nil {
		fmt.Fprintln(os.Stderr, "NeoSwitch daemon stopped by a signal")
	} else {
		fmt.Fprintln(os.Stderr, "NeoSwitch daemon stopped: no Claude Code session attached")
	}
	return nil
}

// isGitIgnored reports whether path is known to be ignored in project's git repository.
func isGitIgnored(project, path string) bool {
	ignored, _ := gitIgnored(project, path)
	return ignored
}

// gitExclude adds path, relative to project, to the repository's local exclude file (normally .git/info/exclude), which only affects this clone. git resolves the file, because in a worktree .git is a file rather than a directory.
func gitExclude(project, path string) error {
	revParse := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", project, "rev-parse"}, args...)...).Output()
		return strings.TrimSpace(string(out)), err
	}
	exclude, err := revParse("--git-path", "info/exclude")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(exclude) {
		exclude = filepath.Join(project, exclude)
	}
	// The project can be a subdirectory of the repository; exclude patterns are relative to its top level.
	prefix, err := revParse("--show-prefix")
	if err != nil {
		return err
	}
	pattern := "/" + filepath.ToSlash(filepath.Join(prefix, path))
	data, err := os.ReadFile(exclude)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == pattern {
			return nil
		}
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		pattern = "\n" + pattern
	}
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(exclude, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(pattern + "\n"); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// gitIgnored reports whether path is ignored in project's git repository; known is false outside a repository or without git.
func gitIgnored(project, path string) (ignored, known bool) {
	cmd := exec.Command("git", "-C", project, "check-ignore", "-q", path)
	err := cmd.Run()
	if err == nil {
		return true, true
	}
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
		return false, true
	}
	return false, false
}
