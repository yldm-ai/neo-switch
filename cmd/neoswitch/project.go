package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
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

// readyPath records that the last check of the route passed: the SessionStart hook offers /neoswitch:on in sessions that do not use NeoSwitch only then, so it never suggests a switch that would fail.
func readyPath(dir string) string { return filepath.Join(dir, "ready") }

func setReady(dir string, ready bool) {
	if !ready {
		_ = os.Remove(readyPath(dir))
		return
	}
	_ = os.MkdirAll(dir, 0o700)
	_ = daemon.WriteFileAtomic(readyPath(dir), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}

// declinedPath lists the projects where the user ran /neoswitch:off, one path per line; the start hint leaves them alone until /neoswitch:on runs there again.
func declinedPath(dir string) string { return filepath.Join(dir, "declined") }

func setDeclined(dir, project string, declined bool) {
	data, _ := os.ReadFile(declinedPath(dir))
	paths := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths[line] = true
		}
	}
	// The hook compares against CLAUDE_PROJECT_DIR as Claude Code gives it, which may be a symlink: both spellings are kept.
	for _, path := range []string{project, projectKey(project)} {
		if declined {
			paths[path] = true
		} else {
			delete(paths, path)
		}
	}
	var out strings.Builder
	for path := range paths {
		out.WriteString(path + "\n")
	}
	_ = os.MkdirAll(dir, 0o700)
	_ = daemon.WriteFileAtomic(declinedPath(dir), []byte(out.String()), 0o600)
}

// routeNotReadyError means the route cannot answer yet, for a reason the user can fix: the project stays on Claude.
type routeNotReadyError struct{ reason string }

func (e *routeNotReadyError) Error() string {
	return "NeoSwitch did not switch this project, so it keeps using Claude. " + e.reason
}

// preflightTimeout bounds the check of agy before a project is switched; a signed-in agy answers in about two seconds.
var preflightTimeout = 30 * time.Second

// antigravityReady checks that agy is signed in and offers model, or the configured model when model is "", starting agy when models is nil. agy refusing to start counts as not ready, since every request would then fail the same way.
func antigravityReady(model string, models []string) error {
	backend, err := provider.NewAntigravityACPFromEnv(context.Background())
	if err != nil {
		return &routeNotReadyError{reason: sentence(err.Error())}
	}
	shared := model != "" && model != backend.Model()
	if model == "" {
		model = backend.Model()
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
	if len(models) > 0 && !slices.Contains(models, model) {
		if shared {
			// GEMINI_MODEL cannot help while the running daemon answers every switched session with its own model.
			return &routeNotReadyError{reason: "Other Claude Code windows started NeoSwitch with " + model + ", which your Google account's Antigravity plan does not offer (it offers " + strings.Join(models, ", ") + "). Close all Claude Code windows, open Claude Code again with GEMINI_MODEL set to one of those, and run /neoswitch:on."}
		}
		return &routeNotReadyError{reason: provider.ModelNotOfferedMessage(model, models)}
	}
	return nil
}

// claudeSession is the Claude Code session a command runs in. A sign-in finished in the browser turns the project on from the sign-in helper, which records the session of the /neoswitch:login that started it.
type claudeSession struct {
	pid     int
	inside  bool
	gateway bool
	// id is the session id Claude Code sends with every request, which the daemon routes by.
	id string
}

func currentSession() claudeSession {
	pid, inside := callingSession()
	return claudeSession{pid: pid, inside: inside, gateway: gatewayModeOn(), id: strings.TrimSpace(os.Getenv("CLAUDE_CODE_SESSION_ID"))}
}

// callingSession reports whether the command runs inside a Claude Code session (a slash command, the Bash tool, or a process they started), and that session's process ID, which Claude Code passes to child processes as CLAUDE_PID. pid is 0 when the Claude Code version does not provide it.
func callingSession() (pid int, inside bool) {
	if os.Getenv("CLAUDECODE") != "1" {
		return 0, false
	}
	pid, _ = strconv.Atoi(strings.TrimSpace(os.Getenv("CLAUDE_PID")))
	return pid, true
}

// errOutsideSession: switching is per session, so it needs the session.
var errOutsideSession = errors.New("NeoSwitch switches one Claude Code session at a time: run /neoswitch:on inside the session that should answer with Gemini. From a terminal, `neoswitch claude` starts Claude Code on Gemini")

// enableProject switches session to Gemini. The project's settings point every session in it at the daemon; only this one, and others that opt in themselves, get Gemini, while the rest pass through to Claude. models are the models agy offers the account when the caller just checked them, or nil.
func enableProject(project string, session claudeSession, models []string) error {
	switch {
	case !session.inside:
		return errOutsideSession
	case session.gateway:
		return errors.New("this session was started through a gateway (" + daemon.EnvUseGateway + "), which keeps its own connection, so NeoSwitch cannot switch it. Start Claude Code without the gateway, then run /neoswitch:on")
	case session.pid <= 1 || session.id == "":
		return errors.New("this Claude Code version does not tell NeoSwitch which session is running it, so it cannot switch it. Update Claude Code, then run /neoswitch:on")
	case envTrue("CLAUDE_CODE_USE_BEDROCK") || envTrue("CLAUDE_CODE_USE_VERTEX"):
		return errors.New("this session reaches Claude through Amazon Bedrock or Google Vertex AI, which do not use ANTHROPIC_BASE_URL, so NeoSwitch cannot switch it")
	}
	kind, _, err := resolveBackend()
	if err != nil {
		return err
	}
	dir, err := daemon.Dir()
	if err != nil {
		return err
	}
	// A session applies the settings on its next request and keeps them, so switching it to a route that cannot answer would leave it failing until it closes. The route is checked first, and its result kept for the start hint.
	if kind == backendAntigravity {
		if err := antigravityReady(runningModel(dir), models); err != nil {
			setReady(dir, false)
			return err
		}
	}
	setReady(dir, true)
	state, err := daemon.EnsureState(dir)
	if err != nil {
		return err
	}
	// The project's other sessions pass through to where they went before: Anthropic, or the gateway the user's environment names.
	if base := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")); !strings.HasPrefix(base, "http://127.0.0.1:") && !strings.HasPrefix(base, "https://127.0.0.1:") {
		if state, err = daemon.SaveUpstream(dir, state, base); err != nil {
			return err
		}
	}
	key := projectKey(project)
	notice, err := serveSession(dir, &state, daemon.Session{PID: session.pid, Project: key, ID: session.id, OptIn: true})
	if err != nil {
		return fmt.Errorf("NeoSwitch was not turned on, so this session keeps using Claude: %w", err)
	}
	// Registered before written: the daemon removes the settings only when no registered session needs them.
	bundle, err := daemon.WriteBundle(dir, userExtraCAs(dir))
	if err != nil {
		return err
	}
	if err := daemon.Enable(key, state, daemon.Route{Bundle: bundle, UserHeaders: userHeaders()}); err != nil {
		return err
	}
	setDeclined(dir, project, false)
	// The plugin's dialog answers without a model reply, so there is no reply to warn about.
	replyNote := " (this reply may still come from Claude)"
	if dialogMode() {
		replyNote = ""
	}
	fmt.Println("This session uses Gemini from your next message" + replyNote + ". Other sessions keep using Claude: windows open in this project and new ones pass their requests through NeoSwitch to Claude unchanged while this session is open. /neoswitch:off returns this session to Claude; when it closes, the project goes back to how it was.")
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

// runningModel is the model of the daemon already running, which answers this session whatever GEMINI_MODEL says, or "" when none runs or it does not report one.
func runningModel(dir string) string {
	state, ok, err := daemon.LoadState(dir)
	if err != nil || !ok {
		return ""
	}
	info, running := daemon.Status(state)
	if !running {
		return ""
	}
	return info.Model
}

// userExtraCAs is the CA file the user's environment adds, which NeoSwitch's bundle keeps, or "" when that is NeoSwitch's own bundle.
func userExtraCAs(dir string) string {
	path := strings.TrimSpace(os.Getenv("NODE_EXTRA_CA_CERTS"))
	if path == daemon.BundlePath(dir) {
		return ""
	}
	return path
}

// userHeaders are the custom headers of the user's environment, which NeoSwitch's token header must not replace; "" when they are NeoSwitch's own.
func userHeaders() string {
	headers := os.Getenv("ANTHROPIC_CUSTOM_HEADERS")
	if strings.Contains(headers, daemon.TokenHeader+":") {
		return ""
	}
	return headers
}

// projectKey names a project the same way whichever path reached it, such as a symlink, so the daemon sees two sessions of one project as one.
func projectKey(project string) string {
	if resolved, err := filepath.EvalSymlinks(project); err == nil {
		return resolved
	}
	return project
}

// serveSession makes the shared daemon serve the Claude Code process pid, starting it with the configured backend when none is running. The notice is non-empty when an already running daemon serves a different setup than the one configured now.
// serveSession attaches session to the daemon, starting it when needed. A saved port that another program took is replaced in *state, so the settings written afterwards point at the new one.
func serveSession(dir string, state *daemon.State, session daemon.Session) (notice string, err error) {
	kind, _, err := resolveBackend()
	if err != nil {
		return "", err
	}
	// Pinning the kind makes the daemon spawned below, whose environment drops Claude Code's variables, serve the backend checked here.
	_, model, label, err := configuredBackend(context.Background())
	if err != nil {
		return "", err
	}
	_ = os.Setenv(backendEnv, kind)
	spawn := func() error { return spawnDaemon(dir) }
	_, err = daemon.AttachSession(*state, session, spawn)
	var busy *daemon.PortInUseError
	if errors.As(err, &busy) {
		moved, moveErr := daemon.MovePort(dir, *state)
		if moveErr != nil {
			return "", moveErr
		}
		*state = moved
		_, err = daemon.AttachSession(*state, session, spawn)
	}
	if err != nil {
		return "", fmt.Errorf("NeoSwitch could not start (details are in %s)", filepath.Join(dir, "daemon.log"))
	}
	info, running := daemon.Status(*state)
	if !running {
		return "", nil
	}
	// The user asked for this setup and version here, so the open windows move to it.
	switch planTakeover(dir, info, label, model, true) {
	case handOver:
		if startTakeover(dir, true) {
			return movingNote(), nil
		}
	case replaceLegacy:
		stopErr := stopLegacyDaemon(*state)
		if stopErr != nil {
			return "NeoSwitch: other Claude Code windows still use " + versionOrUnknown(info.Version) + " of NeoSwitch, which could not be stopped (" + strings.TrimSuffix(stopErr.Error(), ".") + "), so this session uses it too. " + closeWindowsHint("NeoSwitch "+version), nil
		}
		if _, err := daemon.AttachSession(*state, session, spawn); err != nil {
			return "", fmt.Errorf("NeoSwitch could not start after stopping the earlier version (details are in %s)", filepath.Join(dir, "daemon.log"))
		}
		return "NeoSwitch: every open window now uses " + version + ". A reply that was still in progress in another window stopped; ask again there. Each Gemini conversation reads itself again on its next reply, which takes longer once.", nil
	}
	if info.Backend != "" && info.Backend != label || daemon.NewerVersion(version, info.Version) || info.Version == "" || info.Model != "" && info.Model != model {
		return attachNotice("", model, label, info, running, false), nil
	}
	return "", nil
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
		fmt.Println("This session uses Claude; NeoSwitch was never turned on.")
		return nil
	}
	// The session goes back to Claude from its next request: the daemon passes it through with the user's own login. The project settings stay only while another session of the project still uses Gemini.
	session := currentSession()
	wasOn, running := false, false
	if session.id != "" {
		wasOn, running = daemon.OptOut(state, session.id)
	}
	if !running {
		// No daemon to ask: no session of the project can still be on Gemini.
		if err := daemon.Disable(projectKey(project), state); err != nil {
			return err
		}
	}
	setDeclined(dir, project, true)
	switch {
	case wasOn:
		fmt.Println("This session uses Claude from your next message, with your own Claude login.")
	case os.Getenv("ANTHROPIC_BASE_URL") == state.BaseURL() && !running:
		fmt.Println("NeoSwitch is not running, so this session cannot answer. Start a new Claude Code session: it uses Claude.")
	default:
		fmt.Println("This session already uses Claude.")
	}
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
	// The daemon routes by session, so it is the one that knows whether this session uses Gemini; its env only tells whether its requests pass through NeoSwitch at all.
	through := ok && os.Getenv("ANTHROPIC_BASE_URL") == state.BaseURL() && !gatewayModeOn()
	// configuredBackend checks credentials and agy locally without calling the model, so a missing sign-in is reported instead of a route that cannot work.
	_, model, label, backendErr := configuredBackend(context.Background())
	var info daemon.Info
	running := false
	if ok {
		info, running = daemon.StatusOf(state, currentSession().id)
	}
	session := info.OptedIn && !gatewayModeOn()
	switch {
	case !session && through && !running:
		fmt.Fprintln(w, "This session's requests go through NeoSwitch, which is not running, so it cannot answer.")
		fmt.Fprintln(w, "Start a new Claude Code session: it uses Claude.")
	case !session && backendErr != nil:
		fmt.Fprintln(w, "This session uses Claude.")
		fmt.Fprintln(w, sentence(backendErr.Error()))
	case !session:
		fmt.Fprintln(w, "This session uses Claude.")
		fmt.Fprintf(w, "Run /neoswitch:on to answer with %s from this session's next message.\n", modelName(model))
	case backendErr != nil:
		fmt.Fprintln(w, "This session is switched to NeoSwitch, but NeoSwitch cannot reach a model.")
		fmt.Fprintln(w, sentence(backendErr.Error()))
		fmt.Fprintln(w, "Until then, run /neoswitch:off to continue on Claude in this session.")
	default:
		fmt.Fprintf(w, "This session uses %s through %s, and NeoSwitch is running.\n", modelName(model), plainLabel(label))
		switch {
		case info.Backend != "" && info.Backend != label:
			fmt.Fprintf(w, "Other Claude Code windows still share the earlier setup (%s), so this session uses it too. %s\n", plainLabel(info.Backend), moveWindowsHint(info, modelName(model)+" through "+plainLabel(label)))
		case info.Version == "" || daemon.NewerVersion(version, info.Version):
			fmt.Fprintf(w, "NeoSwitch was updated, but the open windows, this session included, still use %s. %s\n", versionOrUnknown(info.Version), moveWindowsHint(info, "NeoSwitch "+version))
		default:
			fmt.Fprintln(w, "Run /neoswitch:off to continue on Claude in this session.")
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
	session := flags.String("session", "", "its session id")
	quiet := flags.Bool("quiet", false, "only make sure the daemon runs, as before every prompt")
	if err := flags.Parse(args); err != nil {
		return err
	}
	message := hookMessage
	if *quiet {
		// Before a prompt nothing is shown: the hook only keeps the route answering.
		message = func(string) error { return nil }
	}
	dir, err := daemon.Dir()
	if err != nil {
		return message(err.Error())
	}
	state, ok, err := daemon.LoadState(dir)
	if err != nil || !ok {
		return nil
	}
	// Versions up to 0.4.1 turned gateway mode off for the whole project, which sends sessions started through a gateway to Anthropic with the gateway's credential. This session already applied the override and keeps it until it closes; removing it fixes the sessions that start afterwards.
	leftover := ""
	if path, err := projectDir(nil); err == nil {
		if cleared, _ := daemon.ClearGatewayOverride(projectKey(path), state); cleared {
			leftover = "NeoSwitch: an earlier version turned off gateway mode for this project, so sessions started through a gateway, such as `everyapi use claude`, could not reach it. New sessions reach their gateway again; if this session was started through one, close it and start it again."
		}
	}
	base := os.Getenv("ANTHROPIC_BASE_URL")
	opted := false
	if *session != "" {
		info, _ := daemon.StatusOf(state, *session)
		// /neoswitch:on refuses gateway sessions, and a gateway session's requests never reach NeoSwitch, so an opt-in left from before, such as of a session resumed through a gateway, does not make it use Gemini.
		opted = info.OptedIn && !gatewayModeOn()
	}
	// A gateway session keeps the gateway's ANTHROPIC_BASE_URL whatever the project settings, which are applied to this hook's environment, say: its requests never reach NeoSwitch, so there is nothing to attach or announce.
	routed := base == state.BaseURL() && !gatewayModeOn()
	if !routed && !opted {
		if legacyRoute(base, os.Getenv("ANTHROPIC_AUTH_TOKEN"), state) && !*quiet {
			// Settings of a NeoSwitch from before TLS that no daemon serves any more: this session cannot answer, and the next one should not inherit them.
			if project, err := projectDir(nil); err == nil {
				_ = daemon.Disable(projectKey(project), state)
			}
			if _, running := daemon.Status(state); !running {
				return message("NeoSwitch was updated, and this session still points at the earlier version, which is no longer running, so it cannot answer. Start a new session: it uses Claude, and /neoswitch:on switches it to Gemini.")
			}
		}
		return nil
	}
	if *pid <= 1 {
		return message("NeoSwitch: the SessionStart hook did not pass the Claude Code process ID")
	}
	if !*quiet {
		// Every routed session refreshes the update cache: a fix may be what it needs.
		startUpdateCheck(dir)
	}
	// Sessions that only pass through to Claude need the daemon as much as the opted-in ones, and do not need a model: a backend problem must not take their Claude away.
	if kind, _, err := resolveBackend(); err == nil {
		_ = os.Setenv(backendEnv, kind)
	}
	project := ""
	if path, err := projectDir(nil); err == nil {
		project = projectKey(path)
	}
	_, err = daemon.AttachSession(state, daemon.Session{PID: *pid, ID: *session}, func() error { return spawnDaemon(dir) })
	if err != nil {
		var busy *daemon.PortInUseError
		if errors.As(err, &busy) {
			// This session already applied the old port and cannot answer. The project goes back to Claude for the next session, and NeoSwitch moves to a free port, which /neoswitch:on uses.
			_, port, _ := net.SplitHostPort(busy.Listen)
			if project != "" {
				_ = daemon.Disable(project, state)
			}
			_, _ = daemon.MovePort(dir, state)
			return message("NeoSwitch: another program on this computer took port " + port + ", which NeoSwitch used, so this session cannot answer. Start a new session, which uses Claude, and run /neoswitch:on in it to use Gemini.")
		}
		return message("NeoSwitch could not start, so this session cannot answer. Start a new Claude Code session, which uses Claude. Details are in " + filepath.Join(dir, "daemon.log") + ".")
	}
	// The first session of a new version moves the open windows to it. The hook before every prompt does it too, in windows whose own binary is gone from the cache, which then run the latest one cached (neoswitch.sh --cached).
	moving := ""
	if info, running := daemon.Status(state); running && (info.Version == "" || daemon.NewerVersion(version, info.Version)) {
		if _, model, label, err := configuredBackend(context.Background()); err == nil && planTakeover(dir, info, label, model, false) == handOver && startTakeover(dir, false) {
			moving = movingNote()
		}
	}
	if *quiet {
		return nil
	}
	if !opted {
		if leftover != "" {
			// Whether this session was started through a gateway is hidden by the override it applied, so the usual line about how it reaches Claude may be wrong.
			return message(joinNotes(leftover, updateNotice(dir)))
		}
		return message(joinNotes("NeoSwitch: another session in this project uses Gemini, so this one reaches Claude through NeoSwitch, unchanged. Run /neoswitch:on to switch this one to Gemini too.", moving, updateNotice(dir)))
	}
	_, model, label, err := configuredBackend(context.Background())
	if err != nil {
		return message(sessionBlocked(err.Error()))
	}
	if moving != "" {
		return message(joinNotes(moving, attachNotice("", model, label, daemon.Info{Backend: label, Version: version, Model: model}, true, true), leftover, updateNotice(dir)))
	}
	info, running := daemon.Status(state)
	return message(joinNotes(attachNotice("", model, label, info, running, true), leftover, updateNotice(dir)))
}

// legacyRoute reports a base URL written by a NeoSwitch from before TLS.
func legacyRoute(base, token string, s daemon.State) bool {
	// Other local proxies, such as a company or account gateway on this computer, use the same form; only NeoSwitch's own route carries its token.
	return strings.HasPrefix(base, "http://127.0.0.1:") && token != "" && token == s.Token
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
		notes = append(notes, "NeoSwitch: other Claude Code windows still share the earlier setup ("+plainLabel(info.Backend)+"), so this session uses it too. "+moveWindowsHint(info, modelName(model)+" through "+plainLabel(label)))
	case running && info.Model != "" && info.Model != model:
		notes = append(notes, "NeoSwitch: other Claude Code windows started NeoSwitch with "+modelName(info.Model)+", so this session uses it too. "+moveWindowsHint(info, modelName(model)))
	case running && (info.Version == "" || daemon.NewerVersion(version, info.Version)):
		notes = append(notes, "NeoSwitch: other Claude Code windows still use "+versionOrUnknown(info.Version)+" of NeoSwitch, so this session does too. "+moveWindowsHint(info, "NeoSwitch "+version))
	case notice == "":
		notes = append(notes, "NeoSwitch: this session uses "+modelName(model)+" through "+plainLabel(label)+". Ignore the Claude model name in the header. The first reply can take a while.")
		if tracked {
			notes = append(notes, "/neoswitch:off continues this session on Claude.")
		}
	}
	return strings.Join(notes, " ")
}

// gatewayModeOn reports whether the session runs in Claude Code's gateway mode, in which its requests go to the gateway its launcher named, not to the project's ANTHROPIC_BASE_URL.
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

// spawnDaemon starts `neoswitch daemon` in its own session so it survives the hook process, with a scrubbed environment: Claude Code's API settings and plugin options other than the Gemini key stay behind. args are the daemon's flags, such as --take-over.
func spawnDaemon(dir string, args ...string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(dir, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(self, append([]string{"daemon"}, args...)...)
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

// runDaemon serves the shared daemon. With --take-over it replaces the one running (planTakeover decided it may); --explicit marks a takeover the user asked for with /neoswitch:on.
func runDaemon(args []string) error {
	flags := flag.NewFlagSet("daemon", flag.ContinueOnError)
	takeOver := flags.Bool("take-over", false, "take the port over from the running daemon")
	explicit := flags.Bool("explicit", false, "the user asked for the takeover")
	if err := flags.Parse(args); err != nil {
		return err
	}
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
	// The sessions that pass through to Claude need the daemon whatever happened to the model side, so a backend problem does not stop it: the opted-in sessions get an answer saying what to fix.
	backend, model, backendName, err := configuredBackend(context.Background())
	if err != nil {
		if *takeOver {
			// The running daemon may be able to answer; this one could not.
			return fmt.Errorf("NeoSwitch %s did not take over: no model: %w", version, err)
		}
		fmt.Fprintf(os.Stderr, "NeoSwitch daemon: no model: %v\n", err)
		backend, backendName = nil, "no model"
	}
	if *takeOver && !*explicit {
		// The setup is checked again here, where it is final: an update must not change the model or account the open windows use.
		if info, running := daemon.Status(state); running && (info.Backend != "" && info.Backend != backendName || info.Model != "" && info.Model != model) {
			return fmt.Errorf("NeoSwitch %s did not take over: the running daemon serves %s with %s, and this one would serve %s with %s", version, info.Backend, info.Model, backendName, model)
		}
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
	config := server.Config{Model: model, Token: state.Token}
	if backend != nil {
		config.Backend = backend
	}
	var passthrough http.Handler
	if state.TLS {
		// The router checks NeoSwitch's token header and strips the user's credential before the proxy sees a request.
		config.Token = ""
		if passthrough, err = daemon.NewPassthrough(state); err != nil {
			return err
		}
	}
	handler := server.NewHandler(config)
	// Routing lasts as long as the sessions using it: a project no attached session uses goes back to Claude, so the next session there, and its claude.ai connectors, start as before.
	release := func(project string) {
		if err := daemon.Disable(project, state); err != nil {
			fmt.Fprintf(os.Stderr, "NeoSwitch could not turn %s back to Claude: %v\n", project, err)
			return
		}
		fmt.Fprintf(os.Stderr, "NeoSwitch: %s is back on Claude\n", project)
	}
	if *takeOver {
		fmt.Fprintf(os.Stderr, "NeoSwitch %s taking over %s\n", version, state.BaseURL())
	}
	err = daemon.Run(ctx, state, handler, daemon.Options{Backend: backendName, Version: version, Model: model, Release: release, Passthrough: passthrough, TakeOver: *takeOver, TakeOverExplicit: *explicit})
	switch {
	case errors.Is(err, daemon.ErrHandedOver):
		fmt.Fprintln(os.Stderr, "NeoSwitch daemon stopped: the NeoSwitch that took over serves the open windows now")
		return nil
	case errors.Is(err, daemon.ErrTakeoverBusy), errors.Is(err, daemon.ErrHandoverRefused):
		fmt.Fprintf(os.Stderr, "NeoSwitch %s did not take over: %v\n", version, err)
		return nil
	case err != nil:
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

// label prints the line the plugin's mod pins under the prompt while this session's model requests go to Gemini, and nothing otherwise: Claude Code's header keeps naming the Claude model the session asks for. It asks the daemon, which decides where each session's requests go, so the line stays true after a switch made any other way, and it starts nothing.
func label(w io.Writer) {
	session := currentSession()
	if session.id == "" || session.gateway {
		return
	}
	dir, err := daemon.Dir()
	if err != nil {
		return
	}
	state, ok, err := daemon.LoadState(dir)
	if err != nil || !ok {
		return
	}
	info, running := daemon.StatusOf(state, session.id)
	if !running || !info.OptedIn {
		return
	}
	name := "Gemini"
	if info.Model != "" {
		name = modelName(info.Model)
	}
	// Claude Code shows the line under the plugin's name, so it does not repeat it.
	fmt.Fprintf(w, "%s answers this session\n", name)
}
