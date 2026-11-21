package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yldm-ai/neo-switch/internal/daemon"
	"github.com/yldm-ai/neo-switch/internal/provider"
)

// A sign-in runs two of agy's sign-ins at once, as Claude Code's and Codex's own logins do: agy's browser sign-in, which opens a browser on this computer and finishes on its own, and its sign-in by code, whose link works in a browser on any device and whose page shows a code to paste back. The link is always shown, so a browser that opens where the user cannot see it, or not at all, never strands the sign-in; whichever finishes first wins and the other is stopped. The code goes back to the agy process that made the link, which holds the PKCE verifier.
//
// In a terminal the code is pasted into NeoSwitch. A slash command cannot wait for input, so the plugin splits the sign-in in two: /neoswitch:login starts a detached helper that runs both sign-ins and shows the link, and /neoswitch:login <code> hands the code to that helper over a socket only this user can reach. The code is single-use and worthless without the verifier inside agy, so its passing through argv and the conversation is harmless. A sign-in finished in the browser is finished by the helper, which then turns the project on for the session that asked.

var (
	// signInWindow is how long a sign-in waits. Google's codes expire within minutes, so a longer wait only keeps a dead link around.
	signInWindow = 10 * time.Minute
	// signInStartTimeout bounds how long /neoswitch:login waits for the helper to show the link; agy itself gives up on showing it sooner.
	signInStartTimeout = 60 * time.Second
	// signInReplyTimeout bounds how long a code waits for the helper's answer, which includes Google's code exchange.
	signInReplyTimeout = 3 * time.Minute
	// signedInCheckTimeout bounds the check for an agy that is signed in already, which takes about two seconds.
	signedInCheckTimeout = 30 * time.Second
)

// codeSignInSupported: the helper needs a detached process and a Unix socket, and NeoSwitch ships for macOS and Linux only.
const codeSignInSupported = runtime.GOOS != "windows"

const signInHelperCommand = "sign-in-helper"

var errNoPendingSignIn = errors.New("no sign-in is waiting for a code: a link works for 10 minutes and only until a newer one replaces it. If you signed in in the browser, you are signed in already, which /neoswitch:status shows")

// antigravityLogin signs in through agy and saves Antigravity OAuth as the backend, so a Gemini key exported for other tools does not take over afterwards. With --on it then turns that project on, which /neoswitch:login needs: the sign-in may finish only in a later invocation, and a project turned on before it would route the session to a signed-out agy.
//
// Without an argument it signs in, unless agy is signed in already; an argument is the code (or the address of the page that shows it) for the sign-in started before.
//
// A missing agy is installed first with Google's official installer, unless ANTIGRAVITY_CLI_PATH names a specific agy: that is the user's own choice, so NeoSwitch reports it instead of installing another copy elsewhere. The Terms warning follows, because it only matters once the sign-in can actually start; it is shown before every sign-in.
func antigravityLogin(args []string) error {
	flags := flag.NewFlagSet("neoswitch antigravity login", flag.ContinueOnError)
	projectFlag := flags.String("on", "", "turn this project on once signed in")
	if err := flags.Parse(args); err != nil {
		return err
	}
	turnOn := false
	flags.Visit(func(f *flag.Flag) { turnOn = turnOn || f.Name == "on" })
	if flags.NArg() > 1 {
		return errors.New("usage: neoswitch antigravity login [--on project] [code | cancel]")
	}
	project := ""
	if turnOn {
		var err error
		if project, err = projectDir([]string{*projectFlag}); err != nil {
			return err
		}
	}
	arg := strings.TrimSpace(flags.Arg(0))
	dir, err := daemon.Dir()
	if err != nil {
		return err
	}
	finish := func() error { return signedIn(dir, turnOn, project, currentSession()) }
	if arg == "cancel" {
		return cancelSignIn(dir)
	}
	if arg != "" {
		code, err := signInCode(arg)
		if err != nil {
			return err
		}
		if err := submitSignInCode(dir, code); err != nil {
			return err
		}
		return finish()
	}

	if _, err := provider.FindAntigravityCLI(); errors.Is(err, provider.ErrAntigravityCLIMissing) {
		if strings.TrimSpace(os.Getenv("ANTIGRAVITY_CLI_PATH")) != "" {
			return fmt.Errorf("%w: point ANTIGRAVITY_CLI_PATH at agy, or unset it so that /neoswitch:login installs agy, then run /neoswitch:login again", err)
		}
		if _, err := installAntigravityCLI(context.Background()); err != nil {
			return err
		}
	}
	backend, err := provider.NewAntigravityACPFromEnv(context.Background())
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, antigravityTermsWarning)
	// A sign-in still waiting is replaced, and stopped before the check below, so one it finished in the browser meanwhile counts as signed in.
	replaced := false
	if socket, err := signInSocket(dir); err == nil {
		replaced = cancelPendingSignIn(socket)
	}
	_ = os.Remove(signInLinkPath(dir))
	checkCtx, cancel := context.WithTimeout(context.Background(), signedInCheckTimeout)
	signed, models, _ := backend.SignedIn(checkCtx)
	cancel()
	if signed {
		return signInAgain(dir, backend, models, turnOn, project, replaced)
	}
	withBrowser := !remoteSession()
	switch {
	case isTerminal(os.Stdin) && isTerminal(os.Stdout):
		if _, err := signInHere(backend, withBrowser, false); err != nil {
			return err
		}
	case !codeSignInSupported:
		if err := backend.Login(context.Background()); err != nil {
			return err
		}
	default:
		link, err := startCodeSignIn(dir, helperArgs(withBrowser, turnOn, project, currentSession()))
		if err != nil {
			return err
		}
		printSignInLink(os.Stdout, link, replaced, withBrowser, false, false)
		return nil
	}
	return finish()
}

// signInAgain serves a /neoswitch:login run while agy is signed in: the project is turned on with the current sign-in, and a sign-in by code starts beside it, so the user can switch to another Google account. The current sign-in stays until the new one finishes, and cancel keeps it. agy's browser sign-in would only return the current sign-in, so it is not started.
func signInAgain(dir string, backend *provider.AntigravityACP, models []string, turnOn bool, project string, replaced bool) error {
	session := currentSession()
	if err := useAntigravity(dir, "ok: signed in to Google through Antigravity (agy) already", models, turnOn, project, session); err != nil {
		return err
	}
	fmt.Println()
	switch {
	case isTerminal(os.Stdin) && isTerminal(os.Stdout):
		kept, err := signInHere(backend, false, true)
		if err != nil || kept {
			return err
		}
		return signedIn(dir, false, "", session)
	case !codeSignInSupported:
		fmt.Println("To sign in with another Google account, run `neoswitch antigravity login` in a terminal.")
		return nil
	}
	link, err := startCodeSignIn(dir, helperArgs(false, false, "", session))
	if err != nil {
		return err
	}
	printSignInLink(os.Stdout, link, replaced, false, true, false)
	return nil
}

// cancelSignIn stops a sign-in still waiting; the current sign-in, if any, stays.
func cancelSignIn(dir string) error {
	socket, err := signInSocket(dir)
	if err != nil {
		return err
	}
	if !cancelPendingSignIn(socket) {
		fmt.Println("No sign-in was waiting.")
		return nil
	}
	fmt.Println("ok: the sign-in was cancelled. Its link no longer works, and the current sign-in, if there is one, stays.")
	return nil
}

// signedIn finishes a sign-in that just completed.
func signedIn(dir string, turnOn bool, project string, session claudeSession) error {
	// The daemon replaces agy processes older than this, which may still hold the account signed in before.
	if err := writePrivateFile(signedInPath(dir), time.Now().UTC().Format(time.RFC3339)+"\n"); err != nil {
		return err
	}
	return useAntigravity(dir, "ok: signed in to Google through Antigravity (agy)", nil, turnOn, project, session)
}

func signedInPath(dir string) string { return filepath.Join(dir, "signed-in") }

// lastSignIn is when a sign-in last finished, or the zero time.
func lastSignIn(dir string) time.Time {
	info, err := os.Stat(signedInPath(dir))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// useAntigravity saves Antigravity OAuth as the backend and, with turnOn, turns the project on, unless the account's plan lacks the model NeoSwitch asks for: every request of the project would then fail. models are the ones the account offers, or nil when they are still to be looked up.
func useAntigravity(dir, status string, models []string, turnOn bool, project string, session claudeSession) error {
	fmt.Println(status)
	if _, err := daemon.SaveBackend(dir, backendAntigravity); err != nil {
		return fmt.Errorf("save the Antigravity OAuth choice: %w", err)
	}
	fmt.Println("ok: NeoSwitch uses Antigravity OAuth for routed projects")
	printDaemonSwitchNote(backendAntigravity)
	if !turnOn {
		return nil
	}
	if problem := modelNotOffered(models); problem != "" {
		fmt.Println("NeoSwitch did not switch this project, so it keeps using Claude. " + problem)
		return nil
	}
	return enableProject(project, session)
}

// modelNotOffered explains why the account cannot use the configured model, or returns "" when it can or when that cannot be told: a check that fails does not block turning the project on.
func modelNotOffered(models []string) string {
	backend, err := provider.NewAntigravityACPFromEnv(context.Background())
	if err != nil {
		return ""
	}
	if models == nil {
		ctx, cancel := context.WithTimeout(context.Background(), signedInCheckTimeout)
		defer cancel()
		if _, models, err = backend.SignedIn(ctx); err != nil {
			return ""
		}
	}
	if len(models) == 0 || slices.Contains(models, backend.Model()) {
		return ""
	}
	return provider.ModelNotOfferedMessage(backend.Model(), models)
}

// loginCommand is the command that signs in where the user runs it, and finishes a sign-in with the code after it.
func loginCommand() string {
	if os.Getenv(pluginEnv) != "" {
		return "/neoswitch:login"
	}
	return "neoswitch antigravity login"
}

func statusCommand() string {
	if os.Getenv(pluginEnv) != "" {
		return "/neoswitch:status"
	}
	return "neoswitch status"
}

// printSignInLink shows the link. withBrowser says agy's browser sign-in runs beside it, again that agy is signed in already and this sign-in would replace that, and here that the code is pasted into this process rather than given to a second command.
func printSignInLink(w io.Writer, link string, replaced, withBrowser, again, here bool) {
	switch {
	case again:
		fmt.Fprintln(w, "To sign in again, such as with another Google account, open this link in a browser on any device:")
	case withBrowser:
		finish := "Sign in there, and NeoSwitch finishes on its own."
		if !here {
			finish = fmt.Sprintf("Sign in there, and NeoSwitch finishes on its own; %s then shows the result.", statusCommand())
		}
		fmt.Fprintln(w, "A Google sign-in page opens in a browser on this computer. "+finish)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "If no browser window opened, or to sign in on another device, such as your phone, open this link in any browser and sign in with Google:")
	default:
		fmt.Fprintln(w, "Open this link in a browser on any device, such as your own computer or phone, and sign in with Google:")
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, link)
	fmt.Fprintln(w)
	switch {
	case here && again:
		fmt.Fprintln(w, "The page then shows a code. Paste it here and press Enter, or press Enter alone to keep the current sign-in:")
		return
	case here:
		fmt.Fprintln(w, "The page then shows a code. Paste it here and press Enter:")
		return
	}
	fmt.Fprintf(w, "The page then shows a code. Copy it and run %s followed by the code, such as: %s 4/0Ab...\n", loginCommand(), loginCommand())
	if again {
		fmt.Fprintf(w, "The current sign-in stays until then. To keep it and drop this link, run %s cancel.\n", loginCommand())
	}
	fmt.Fprintln(w, "The link works for 10 minutes.")
	if replaced {
		fmt.Fprintln(w, "A link shown earlier no longer works; use this one.")
	}
}

// remoteSession reports whether this computer most likely cannot show the user a browser: the session came in over SSH, or a Linux or BSD desktop is not running. There the browser sign-in is not started, so no browser pops up on a screen the user cannot see, possibly in front of someone else; the link works either way.
func remoteSession() bool {
	for _, key := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if os.Getenv(key) != "" {
			return true
		}
	}
	switch runtime.GOOS {
	case "darwin", "windows":
		return false
	}
	return os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == ""
}

// isTerminal reports whether f is a terminal. /dev/null is a character device as well, and slash commands may run with it as stdin.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(info, null)
}

// signInCode takes the code from what the user pasted: the code itself, or the address of the page that showed it.
func signInCode(input string) (string, error) {
	input = strings.Trim(strings.TrimSpace(input), `"'`)
	if strings.HasPrefix(input, "https://") || strings.HasPrefix(input, "http://") {
		if u, err := url.Parse(input); err == nil {
			if code := u.Query().Get("code"); code != "" {
				input = code
			}
		}
	}
	// A code holds digits and slashes, such as 4/0Ab...; a plain word, such as "help", is a mistake, and sending it would spend the one code exchange agy allows.
	if input == "" || len(input) > 2048 || strings.ContainsFunc(input, func(r rune) bool { return r <= ' ' || r == 0x7f }) || strings.Contains(input, "://") || !strings.ContainsAny(input, "0123456789/") {
		return "", fmt.Errorf("%q is not a sign-in code. Paste only the code the Google sign-in page shows, such as 4/0Ab...", input)
	}
	return input, nil
}

func signInLinkPath(dir string) string { return filepath.Join(dir, "sign-in-link.txt") }

// signInSocket is where the waiting helper listens. Linux's per-user runtime directory is private to this computer and this boot, which keeps a home directory shared between computers from mixing up their sign-ins; elsewhere the state directory is used.
func signInSocket(dir string) (string, error) {
	path := filepath.Join(dir, "sign-in.sock")
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" && runtimeOS() == "linux" {
		path = filepath.Join(xdg, "neoswitch-sign-in.sock")
	}
	// Unix socket paths are limited to about 104 bytes.
	if len(path) > 100 {
		return "", fmt.Errorf("the folder %s has too long a path to sign in by code; set NEOSWITCH_STATE_DIR to a shorter folder", filepath.Dir(path))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	// Whoever can reach the socket can sign this user's agy in to their own Google account, so its folder must belong to this user alone.
	if err := privateDir(filepath.Dir(path)); err != nil {
		return "", err
	}
	return path, nil
}

// runtimeOS exists so tests can exercise the Linux socket location elsewhere.
var runtimeOS = func() string { return runtime.GOOS }

// pendingSignInLink returns the link of a sign-in by code that still waits for its code, or "".
func pendingSignInLink(dir string) string {
	path := signInLinkPath(dir)
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > signInWindow {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// helperArgs passes the helper what it needs to finish a sign-in done in the browser: the project to turn on and the Claude Code session that asked, since the helper's own environment no longer holds the session's variables.
func helperArgs(withBrowser, turnOn bool, project string, session claudeSession) []string {
	var args []string
	if withBrowser {
		args = append(args, "--browser")
	}
	if turnOn {
		args = append(args, "--on", project)
	}
	if session.inside {
		args = append(args, "--session", strconv.Itoa(session.pid))
	}
	if session.gateway {
		args = append(args, "--gateway")
	}
	return args
}

// startCodeSignIn starts the helper and returns the link once agy has shown it. A helper still waiting must have been cancelled before.
func startCodeSignIn(dir string, args []string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	logPath := filepath.Join(dir, "sign-in.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer logFile.Close()
	// The helper must not hold the slash command's output open, or Claude Code would wait for it to exit; stdin is /dev/null.
	cmd := exec.Command(self, append([]string{"antigravity", signInHelperCommand}, args...)...)
	cmd.Dir = dir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = withoutClaudeCode(os.Environ())
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start the Google sign-in: %w", err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	deadline := time.After(signInStartTimeout)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if link := pendingSignInLink(dir); link != "" {
			return link, nil
		}
		select {
		case <-tick.C:
		case <-exited:
			if link := pendingSignInLink(dir); link != "" {
				return link, nil
			}
			return "", fmt.Errorf("the Google sign-in did not start: %s", lastLogLine(logPath))
		case <-deadline:
			// The helper leads its own process group, so its agy processes go with it.
			killGroup(cmd)
			return "", fmt.Errorf("the Google sign-in did not start in time (details are in %s)", logPath)
		}
	}
}

// cancelPendingSignIn stops a helper still waiting and reports whether there was one. The helper removes its socket and link, and stops a browser sign-in in progress, before it answers, so nothing of it outlives the answer.
func cancelPendingSignIn(socket string) bool {
	return askPendingSignIn(socket, "cancel")
}

// forgetPendingSignIn tells a helper still waiting not to turn project on when its sign-in finishes in the browser: /neoswitch:off came after the /neoswitch:login that started it.
func forgetPendingSignIn(socket, project string) {
	askPendingSignIn(socket, "off "+project)
}

func askPendingSignIn(socket, request string) bool {
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		// Nobody listens: a helper that was killed left its socket behind.
		_ = os.Remove(socket)
		return false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	_, _ = fmt.Fprintln(conn, request)
	_, _ = bufio.NewReader(conn).ReadString('\n')
	return true
}

// submitSignInCode hands the code to the waiting helper and returns once agy has finished signing in.
func submitSignInCode(dir, code string) error {
	socket, err := signInSocket(dir)
	if err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		return fmt.Errorf("%w. Otherwise run %s again for a new link", errNoPendingSignIn, loginCommand())
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(signInReplyTimeout))
	if _, err := fmt.Fprintln(conn, "code "+code); err != nil {
		return fmt.Errorf("hand the code to the Google sign-in: %w", err)
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return fmt.Errorf("the Google sign-in stopped before it finished. Run %s again for a new link", loginCommand())
	}
	reply = strings.TrimSpace(reply)
	if reply == "ok" {
		return nil
	}
	return errors.New(strings.TrimPrefix(reply, "error "))
}

// browserSignIn is agy's browser sign-in running beside the sign-in by code. A nil one was not started.
type browserSignIn struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error // Login's result, set before done closes
}

func startBrowserSignIn(ctx context.Context, backend *provider.AntigravityACP, start bool) *browserSignIn {
	if !start {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	b := &browserSignIn{cancel: cancel, done: make(chan struct{})}
	go func() {
		b.err = backend.Login(ctx)
		close(b.done)
	}()
	return b
}

func (b *browserSignIn) Done() <-chan struct{} {
	if b == nil {
		return nil
	}
	return b.done
}

// stop ends the browser sign-in, waiting until its agy has exited, and reports whether it had signed in. Waiting keeps it from storing a sign-in after the code's.
func (b *browserSignIn) stop() bool {
	if b == nil {
		return false
	}
	b.cancel()
	<-b.done
	return b.err == nil
}

type signInOutcome int

const (
	signInFailed signInOutcome = iota
	// signedInByBrowser: nobody asked for it, so whoever waits finishes the sign-in.
	signedInByBrowser
	// signedInByCode: the code's sender was told, and finishes the sign-in itself.
	signedInByCode
	signInReplaced
)

// signInRequest is a code to submit, or a cancel; reply answers its sender.
type signInRequest struct {
	cancel bool
	code   string
	reply  func(error)
}

// waitForSignIn waits until one of the two sign-ins finishes, the window ends, or a cancel arrives, and stops the other. Requests are handled one at a time, and a browser sign-in is stopped before a code is submitted, so only one of them ever stores a sign-in. stopRequests runs before anything is decided, so no second code can arrive for the single exchange agy allows.
func waitForSignIn(ctx context.Context, code *provider.TerminalSignIn, browser *browserSignIn, requests <-chan signInRequest, stopRequests func()) (signInOutcome, error) {
	end := func(err error) (signInOutcome, error) {
		stopRequests()
		signedIn := browser.stop()
		code.Close()
		if signedIn {
			return signedInByBrowser, nil
		}
		return signInFailed, err
	}
	browserDone := browser.Done()
	for {
		select {
		case <-ctx.Done():
			return end(errors.New("the sign-in link expired. Run " + loginCommand() + " again for a new one"))
		case <-code.Done():
			return end(errors.New("Google's agy stopped the sign-in by code. Run " + loginCommand() + " again for a new link"))
		case <-browserDone:
			if browser.err == nil {
				return end(nil)
			}
			// The link still works, so a browser that failed to open costs nothing.
			fmt.Fprintln(os.Stderr, "NeoSwitch: the browser sign-in stopped:", browser.err)
			browserDone = nil
		case r := <-requests:
			stopRequests()
			signedIn := browser.stop()
			if r.cancel {
				code.Close()
				r.reply(nil)
				return signInReplaced, nil
			}
			if signedIn {
				code.Close()
				r.reply(nil)
				return signedInByCode, nil
			}
			err := code.Submit(ctx, r.code)
			r.reply(err)
			if err != nil {
				return signInFailed, err
			}
			return signedInByCode, nil
		}
	}
}

// signInHere runs the sign-ins in this process, the code being pasted into the terminal. With again, agy is signed in already, and an empty line keeps that sign-in, which kept reports.
func signInHere(backend *provider.AntigravityACP, withBrowser, again bool) (kept bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), signInWindow)
	defer cancel()
	code, err := backend.StartTerminalSignIn(ctx)
	if err != nil {
		return false, err
	}
	// Started once the link is out, so the two agy processes do not reach for the keyring together.
	browser := startBrowserSignIn(ctx, backend, withBrowser)
	printSignInLink(os.Stdout, code.Link(), false, withBrowser, again, true)
	requests := make(chan signInRequest)
	go func() {
		lines := bufio.NewReader(os.Stdin)
		for {
			line, err := lines.ReadString('\n')
			if err != nil {
				return
			}
			if strings.TrimSpace(line) == "" {
				if again {
					requests <- signInRequest{cancel: true, reply: func(error) {}}
					return
				}
				continue
			}
			value, err := signInCode(line)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				continue
			}
			requests <- signInRequest{code: value, reply: func(error) {}}
			return
		}
	}()
	outcome, err := waitForSignIn(ctx, code, browser, requests, func() {})
	return outcome == signInReplaced, err
}

// runSignInHelper is the detached process behind the plugin's sign-in: it runs both sign-ins, publishes the link, and serves requests on the socket. A sign-in finished in the browser is finished here, and the project turned on, since no command waits for it. Its output goes to sign-in.log.
func runSignInHelper(args []string) error {
	flags := flag.NewFlagSet(signInHelperCommand, flag.ContinueOnError)
	withBrowser := flags.Bool("browser", false, "also run the browser sign-in")
	project := flags.String("on", "", "turn this project on once signed in in the browser")
	pid := flags.Int("session", 0, "the Claude Code session to switch")
	gateway := flags.Bool("gateway", false, "the session runs through a gateway")
	if err := flags.Parse(args); err != nil {
		return err
	}
	session := claudeSession{pid: *pid, gateway: *gateway}
	flags.Visit(func(f *flag.Flag) { session.inside = session.inside || f.Name == "session" })

	dir, err := daemon.Dir()
	if err != nil {
		return err
	}
	socket, err := signInSocket(dir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), signInWindow)
	defer cancel()
	backend, err := provider.NewAntigravityACPFromEnv(ctx)
	if err != nil {
		return err
	}
	code, err := backend.StartTerminalSignIn(ctx)
	if err != nil {
		return err
	}
	defer code.Close()
	_ = os.Remove(socket)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("listen for the sign-in code: %w", err)
	}
	var once sync.Once
	stopRequests := func() {
		once.Do(func() {
			_ = listener.Close()
			_ = os.Remove(socket)
			_ = os.Remove(signInLinkPath(dir))
		})
	}
	defer stopRequests()
	if err := os.Chmod(socket, 0o600); err != nil {
		return err
	}
	browser := startBrowserSignIn(ctx, backend, *withBrowser)
	defer browser.stop()
	if err := writePrivateFile(signInLinkPath(dir), code.Link()+"\n"); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "NeoSwitch sign-in: waiting for the browser or the code")

	// turnOn is cleared by a /neoswitch:off for the project; mu also keeps that off from passing the turning on.
	var mu sync.Mutex
	turnOn := *project != ""
	requests := make(chan signInRequest)
	go serveSignInRequests(listener, requests, func(off string) {
		mu.Lock()
		defer mu.Unlock()
		if off == *project {
			turnOn = false
		}
	})
	outcome, err := waitForSignIn(ctx, code, browser, requests, stopRequests)
	switch outcome {
	case signedInByBrowser:
		fmt.Fprintln(os.Stderr, "NeoSwitch sign-in: signed in through the browser")
		mu.Lock()
		defer mu.Unlock()
		// Routing lasts as long as the session that asked for it. One that closed meanwhile asked for nothing any more: the sign-in stands, and the project stays on Claude.
		if session.inside && !processAlive(session.pid) {
			turnOn = false
		}
		return signedIn(dir, turnOn, *project, session)
	case signedInByCode:
		fmt.Fprintln(os.Stderr, "NeoSwitch sign-in: signed in with the code")
	case signInReplaced:
		fmt.Fprintln(os.Stderr, "NeoSwitch sign-in: replaced by a newer sign-in")
	default:
		fmt.Fprintln(os.Stderr, "NeoSwitch sign-in:", err)
	}
	return nil
}

// serveSignInRequests reads one request per connection: an off is answered here, and a code or a cancel goes to waitForSignIn, which replies.
func serveSignInRequests(listener net.Listener, requests chan<- signInRequest, off func(project string)) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_ = conn.SetDeadline(time.Now().Add(signInReplyTimeout))
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			_ = conn.Close()
			continue
		}
		request := strings.TrimSpace(line)
		reply := func(err error) {
			if err != nil {
				_, _ = fmt.Fprintln(conn, "error "+strings.ReplaceAll(err.Error(), "\n", " "))
			} else {
				_, _ = fmt.Fprintln(conn, "ok")
			}
			_ = conn.Close()
		}
		switch {
		case request == "cancel":
			requests <- signInRequest{cancel: true, reply: reply}
		case strings.HasPrefix(request, "code "):
			requests <- signInRequest{code: strings.TrimPrefix(request, "code "), reply: reply}
		case strings.HasPrefix(request, "off "):
			off(strings.TrimPrefix(request, "off "))
			reply(nil)
		default:
			reply(errors.New("unknown request"))
		}
	}
}

// checkRelayedLink is the plugin's Stop hook. The model relays the sign-in link to the user and can change a character of it, which breaks the sign-in, and Claude Code offers a plugin no other way to show the user text that can be clicked. So a reply that shows a sign-in link is checked against the link waiting for its code: a wrong one is sent back to the model with the exact link, which the user sees in the hook's notice as well; a second wrong one leaves the exact link in a notice.
func checkRelayedLink(in io.Reader, out io.Writer) error {
	var input struct {
		Message string `json:"last_assistant_message"`
		Retried bool   `json:"stop_hook_active"`
	}
	if json.NewDecoder(in).Decode(&input) != nil || !relaysSignInLink(input.Message) {
		return nil
	}
	dir, err := daemon.Dir()
	if err != nil {
		return nil
	}
	link := pendingSignInLink(dir)
	if link == "" || strings.Contains(input.Message, link) {
		return nil
	}
	if !input.Retried {
		return json.NewEncoder(out).Encode(map[string]string{
			"decision": "block",
			"reason":   "NeoSwitch: the sign-in link in the reply above differs from the one Google made, so it would not work. Tell the user that link is wrong, then show this one, copied exactly, alone on its own line:\n\n" + link,
		})
	}
	return json.NewEncoder(out).Encode(map[string]string{"systemMessage": "NeoSwitch: the sign-in link above is not exact, so it would not work. Use this one:\n\n" + link})
}

// relaysSignInLink reports whether a reply shows agy's Google sign-in link, even a changed one: Google's sign-in host, and agy's redirect back to antigravity.google.
func relaysSignInLink(message string) bool {
	return strings.Contains(message, "accounts.google.com") && strings.Contains(message, "antigravity")
}

func writePrivateFile(path, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sign-in-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func lastLogLine(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "no details"
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	last = strings.TrimPrefix(last, "neoswitch: ")
	if last == "" {
		return "no details"
	}
	return last
}
