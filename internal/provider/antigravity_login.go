package provider

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// errNoTerminalSignIn means the installed agy predates sign-in by code.
var errNoTerminalSignIn = errors.New("this version of Google's agy cannot sign in with a pasted code. Update it with `agy update`, then try again")

// TerminalSignInCommand returns the command for agy's sign-in by code: agy prints a Google link, the user opens it on any device, and pastes the code the page shows back into agy. It is ACP's terminal authentication, which agy advertises only to clients that can run it, so the method and its arguments come from the handshake rather than being assumed. The browser sign-in of Login needs a browser on this computer; this one works over SSH.
//
// The command is not started. Its stdio is the caller's to connect: a terminal, or pipes for TerminalSignIn.
func (a *AntigravityACP) TerminalSignInCommand(ctx context.Context) (*exec.Cmd, error) {
	c, err := startACP(a.path, a.agent, a.cwd)
	if err != nil {
		return nil, err
	}
	init, err := c.initialize(ctx, acp.ClientCapabilities{Auth: acp.AuthCapabilities{Terminal: true}})
	_ = c.close()
	if err != nil {
		return nil, errors.New("Google's Antigravity command-line tool (agy) did not respond. Update or reinstall it, then run /neoswitch:login again")
	}
	for _, method := range init.AuthMethods {
		if method.Terminal == nil {
			continue
		}
		// Terminal authentication appends its arguments to the agent invocation the client uses, which is startACP's.
		cmd := exec.Command(a.path, append([]string{"acp", "--agent", a.agent}, method.Terminal.Args...)...)
		cmd.Dir = a.cwd
		cmd.Env = acpEnv(os.Environ())
		for key, value := range method.Terminal.Env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
		return cmd, nil
	}
	return nil, errNoTerminalSignIn
}

// SignedIn reports whether agy holds a sign-in it can use, and the models it then offers the account. A new session needs a sign-in, and agy refuses it with ACP's auth-required error otherwise; unlike Login, which returns at once for a signed-in agy but opens a browser for a signed-out one, the check has no effect beyond starting agy.
func (a *AntigravityACP) SignedIn(ctx context.Context) (signed bool, models []string, err error) {
	session, err := a.probeSession(ctx)
	var rpcErr *acp.RequestError
	if errors.As(err, &rpcErr) && rpcErr.Code == acpAuthRequired {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	return true, modelIDs(session.ConfigOptions), nil
}

// TerminalSignIn is a running sign-in by code that has shown its link and waits for the code. The code only works in the agy process that made the link, which holds the PKCE verifier, so the same TerminalSignIn must receive it.
type TerminalSignIn struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	link  string
	done  chan struct{}
	err   error // agy's exit status, set before done closes

	mu       sync.Mutex
	failure  string // agy's last "couldn't log in" line
	lastLine string
}

var (
	// signInLinkTimeout bounds how long agy may take to show the link. A working agy prints it at once; one waiting on a keyring or the network would otherwise hold the command silently.
	signInLinkTimeout = 45 * time.Second
	// signInExchangeTimeout bounds the code exchange with Google after the code was sent.
	signInExchangeTimeout = 2 * time.Minute
)

// StartTerminalSignIn starts agy's sign-in by code and returns once agy has shown the link.
func (a *AntigravityACP) StartTerminalSignIn(ctx context.Context) (*TerminalSignIn, error) {
	cmd, err := a.TerminalSignInCommand(ctx)
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("start the Google sign-in: %w", err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("start the Google sign-in: %w", err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start the Google sign-in: %w", err)
	}
	s := &TerminalSignIn{cmd: cmd, stdin: stdin, done: make(chan struct{})}
	links := make(chan string, 1)
	scanned := make(chan struct{})
	go func() {
		defer close(scanned)
		lines := bufio.NewScanner(out)
		lines.Buffer(make([]byte, 64<<10), 1<<20)
		for lines.Scan() {
			line := strings.TrimSpace(lines.Text())
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "https://") {
				select {
				case links <- line:
				default:
				}
				continue
			}
			s.mu.Lock()
			s.lastLine = line
			if strings.HasPrefix(line, "couldn't log in") {
				s.failure = line
			}
			s.mu.Unlock()
		}
	}()
	go func() {
		// Wait returns only after the scanner has read everything agy wrote, so the failure line is known by the time done closes.
		<-scanned
		s.err = cmd.Wait()
		close(s.done)
	}()
	timer := time.NewTimer(signInLinkTimeout)
	defer timer.Stop()
	select {
	case s.link = <-links:
		return s, nil
	case <-s.done:
		return nil, fmt.Errorf("Google's agy stopped before showing a sign-in link (%s)", s.lastOutput())
	case <-timer.C:
	case <-ctx.Done():
	}
	s.Close()
	return nil, errors.New("Google's agy did not show a sign-in link. Check the network, update agy with `agy update`, then try again")
}

// Link is the Google sign-in address to open in a browser.
func (s *TerminalSignIn) Link() string { return s.link }

// Done closes when agy has exited, after a code or on its own.
func (s *TerminalSignIn) Done() <-chan struct{} { return s.done }

// Submit gives agy the code from the sign-in page, or the full address of that page, and waits for agy to finish signing in. agy takes one code: it exits after the exchange either way, so a rejected code needs a new link.
func (s *TerminalSignIn) Submit(ctx context.Context, code string) error {
	// Closing stdin after the code ends the sign-in even if agy were to keep reading, so a finished exchange is never mistaken for a hang.
	_, werr := io.WriteString(s.stdin, code+"\n")
	_ = s.stdin.Close()
	timer := time.NewTimer(signInExchangeTimeout)
	defer timer.Stop()
	select {
	case <-s.done:
	case <-timer.C:
		s.Close()
		return errors.New("Google did not answer the sign-in in time. Run /neoswitch:login again for a new link")
	case <-ctx.Done():
		s.Close()
		return ctx.Err()
	}
	if s.err == nil && werr == nil {
		return nil
	}
	s.mu.Lock()
	failure := s.failure
	s.mu.Unlock()
	if failure == "" {
		failure = "agy ended the sign-in without saying why"
	}
	// agy's own line names the cause, such as Google answering invalid_grant for a mistyped, used or expired code. It holds no account details.
	return fmt.Errorf("the Google sign-in was not completed (%s). A code works once, for a few minutes, and only with the newest link", failure)
}

// Close stops agy if it is still waiting.
func (s *TerminalSignIn) Close() {
	select {
	case <-s.done:
		return
	default:
	}
	_ = s.cmd.Process.Kill()
	<-s.done
}

func (s *TerminalSignIn) lastOutput() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastLine == "" {
		return "no output"
	}
	return s.lastLine
}
