package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/yldm-ai/neo-switch/internal/provider"
	"github.com/yldm-ai/neo-switch/internal/server"
)

// version is set by the release build with -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve()
	case "claude":
		err = claude(os.Args[2:])
	case "antigravity":
		err = antigravity(os.Args[2:])
	case "doctor":
		err = doctor()
	case "models":
		err = models()
	case "on":
		err = on(os.Args[2:])
	case "off":
		err = off(os.Args[2:])
	case "status":
		err = status(os.Args[2:])
	case "label":
		label(os.Stdout)
		return
	case "backend":
		err = backendCommand(os.Args[2:])
	case "sign-in-check":
		err = checkRelayedLink(os.Stdin, os.Stdout)
	case "detach":
		err = endSession(os.Args[2:])
	case "attach":
		err = attach(os.Args[2:])
	case "daemon":
		err = runDaemon()
	case provider.AgyHookCommand:
		// agy's tool guard: see provider/acp_guard.go. It never fails: anything wrong is a deny.
		_ = provider.RunAgyHook(os.Args[2:], os.Stdin, os.Stdout)
		return
	case "update-check":
		err = updateCheckCommand()
	case "version", "--version":
		fmt.Println("neoswitch", version)
		return
	case "help", "--help", "-h":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "neoswitch:", err)
		os.Exit(1)
	}
}

func antigravity(args []string) error {
	if len(args) > 0 && (args[0] == "login" || args[0] == "oauth") {
		return antigravityLogin(args[1:])
	}
	if len(args) > 0 && args[0] == signInHelperCommand {
		return runSignInHelper(args[1:])
	}
	if len(args) != 1 || args[0] != "probe" {
		return fmt.Errorf("usage: neoswitch antigravity {login [--on project] [code] | probe}")
	}
	path, err := provider.FindAntigravityCLI()
	if err != nil {
		return fmt.Errorf("%w: run `neoswitch antigravity login`, which installs it", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path,
		"-p", "Reply with exactly: neoswitch-ok",
		"--output-format", "json",
		"--disable-slash-commands",
		"--print-timeout", "30s",
	)
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("Antigravity probe timed out; run %q interactively to re-authenticate", path)
		}
		// Do not echo stderr: it may contain account or local-path details.
		return fmt.Errorf("Antigravity CLI probe failed; run %q interactively to inspect authentication", path)
	}
	var result struct {
		Status   string `json:"status"`
		Response string `json:"response"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		return fmt.Errorf("Antigravity returned an unexpected JSON envelope")
	}
	if result.Status != "SUCCESS" || result.Error != "" || strings.TrimSpace(result.Response) != "neoswitch-ok" {
		return fmt.Errorf("Antigravity session probe was not successful; run %q interactively to inspect authentication", path)
	}
	fmt.Println("ok: Antigravity CLI session is usable")
	return nil
}

func serve() error {
	backend, model, _, err := configuredBackend(context.Background())
	if err != nil {
		return err
	}
	listen := os.Getenv("NEOSWITCH_LISTEN")
	if listen == "" {
		listen = "127.0.0.1:8080"
	}
	token := os.Getenv("NEOSWITCH_AUTH_TOKEN")
	if token == "" && !loopbackAddress(listen) {
		return fmt.Errorf("NEOSWITCH_LISTEN=%s accepts connections from other computers, which could then use your Google account or API key: set NEOSWITCH_AUTH_TOKEN, or listen on 127.0.0.1", listen)
	}
	return serveWithListener(backend, model, listen, token)
}

// loopbackAddress reports whether a listen address only accepts connections from this computer. An empty host listens on every interface.
func loopbackAddress(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil || host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func claude(args []string) error {
	backend, model, _, err := configuredBackend(context.Background())
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("allocate local proxy port: %w", err)
	}
	token, err := randomToken()
	if err != nil {
		_ = listener.Close()
		return err
	}
	handler := server.NewHandler(server.Config{Backend: backend, Model: model, Token: token})
	httpServer := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = httpServer.Serve(listener) }()
	defer func() { _ = httpServer.Shutdown(context.Background()) }()

	claudePath := os.Getenv("CLAUDE_CLI_PATH")
	if claudePath == "" {
		claudePath = "claude"
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	child := exec.Command(claudePath, args...)
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	child.Env = append(os.Environ(),
		"ANTHROPIC_BASE_URL=http://"+listener.Addr().String(),
		"ANTHROPIC_AUTH_TOKEN="+token,
		"ANTHROPIC_API_KEY=",
	)
	return child.Run()
}

func serveWithListener(backend server.Backend, model, listen, token string) error {
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", listen, err)
	}
	defer listener.Close()
	httpServer := &http.Server{Handler: server.NewHandler(server.Config{Backend: backend, Model: model, Token: token}), ReadHeaderTimeout: 10 * time.Second}
	fmt.Fprintf(os.Stderr, "NeoSwitch listening on http://%s (model %s)\n", listener.Addr(), model)
	err = httpServer.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// doctor checks the whole path a request takes: the Claude CLI, the configured credentials and whether they can actually use the model.
func doctor() error {
	claudePath := os.Getenv("CLAUDE_CLI_PATH")
	if claudePath == "" {
		claudePath = "claude"
	}
	if _, err := exec.LookPath(claudePath); err != nil {
		return fmt.Errorf("Claude CLI %q was not found: %w", claudePath, err)
	}
	fmt.Printf("ok: Claude CLI %s\n", claudePath)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	backend, model, backendName, err := configuredBackend(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("ok: %s credentials configured\n", backendName)
	switch backend := backend.(type) {
	case *provider.Gemini:
		if err := backend.Check(ctx); err != nil {
			return err
		}
		fmt.Printf("ok: %s can use %s\n", backendName, model)
	case *provider.OpenAI:
		if err := backend.Check(ctx); err != nil {
			return err
		}
		fmt.Printf("ok: %s can use %s\n", backendName, model)
	case *provider.AntigravityACP:
		fmt.Fprintln(os.Stderr, antigravityTermsWarning)
		return antigravity([]string{"probe"})
	}
	return nil
}

// antigravityTermsWarning is shown wherever NeoSwitch starts or checks the Antigravity OAuth route.
const antigravityTermsWarning = "warning: the Google Antigravity Terms of Service forbid accessing Antigravity through third-party tools, and Google may suspend Antigravity accounts that do. Routing Claude Code through Antigravity OAuth falls under that rule; use a Gemini API key or Vertex AI instead."

// models lists the models the configured credentials can use, marking the configured one.
func models() error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	backend, model, _, err := configuredBackend(ctx)
	if err != nil {
		return err
	}
	var ids []string
	switch backend := backend.(type) {
	case *provider.Gemini:
		ids, err = backend.ListModels(ctx)
	case *provider.AntigravityACP:
		fmt.Fprintln(os.Stderr, antigravityTermsWarning)
		ids, err = backend.ListModels(ctx)
	case *provider.OpenAI:
		ids, err = backend.ListModels(ctx)
	default:
		return fmt.Errorf("listing models is not supported for this backend")
	}
	if err != nil {
		return err
	}
	printModels(os.Stdout, os.Stderr, ids, model)
	return nil
}

// printModels writes one model per line, the configured one prefixed with "* ", and warns on errOut when the configured model is not among them.
func printModels(out, errOut io.Writer, ids []string, configured string) {
	found := false
	for _, id := range ids {
		marker := "  "
		if id == configured {
			marker = "* "
			found = true
		}
		fmt.Fprintln(out, marker+id)
	}
	if !found {
		fmt.Fprintf(errOut, "warning: the configured model %s is not in this list; set GEMINI_MODEL to one of these\n", configured)
	}
}

func randomToken() (string, error) {
	var data [24]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("generate local auth token: %w", err)
	}
	return hex.EncodeToString(data[:]), nil
}

func usage() {
	fmt.Println(`NeoSwitch — local Claude Code to Gemini bridge

Usage:
  neoswitch claude [-- claude arguments...]   start Claude Code through a local proxy
  neoswitch serve                             run the Anthropic-compatible proxy
  neoswitch antigravity login                 sign in with Google through the official agy: a browser opens, and a printed link works on any device
  neoswitch antigravity login cancel          drop a sign-in still waiting for its code; the current sign-in stays
  neoswitch antigravity probe                 verify the official agy session without reading its token
  neoswitch on [project]                      inside a Claude Code session: answer it with Gemini from its next message
  neoswitch off [project]                     inside a Claude Code session: switch it back to Claude
  neoswitch status [-v] [project]             show whether a project uses NeoSwitch and what to do next (-v adds details)
  neoswitch backend [auto|antigravity|gemini|openai]  show or choose the upstream (openai is opt-in and experimental)
  neoswitch doctor                            check that the credentials can use the model
  neoswitch models                            list the models the credentials can use (* = configured)
  neoswitch version                           print the NeoSwitch version

Environment:
  NEOSWITCH_BACKEND    antigravity, gemini, openai or auto; overrides the saved choice
  GEMINI_API_KEY       Gemini Developer API key
  GOOGLE_GENAI_USE_VERTEXAI=true  use Vertex AI with Application Default Credentials; also needs GOOGLE_CLOUD_PROJECT and GOOGLE_CLOUD_LOCATION
  GEMINI_MODEL         backend model (default: gemini-4-neon)
  GEMINI_BASE_URL      Gemini API base URL
  OPENAI_API_KEY       OpenAI API key (experimental openai backend)
  OPENAI_MODEL         OpenAI model, required for the openai backend
  OPENAI_BASE_URL      OpenAI API base URL including /v1 (default: https://api.openai.com/v1)
  CLAUDE_CLI_PATH      Claude executable (default: claude)
  ANTIGRAVITY_CLI_PATH  Antigravity executable (default: agy on PATH, then ~/.local/bin/agy)
  NEOSWITCH_LISTEN     serve listen address (default: 127.0.0.1:8080)
  NEOSWITCH_AUTH_TOKEN token for standalone serve, required off loopback`)
}
