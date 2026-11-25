package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runFakePrintCommand(mode string) {
	// Verify Claude Code secrets are stripped from agy's environment.
	if os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("CLAUDE_CODE_SESSION_ID") != "" {
		fmt.Fprintln(os.Stderr, "leaked Claude Code env")
		os.Exit(9)
	}
	cmdArg := ""
	if len(os.Args) > 2 {
		cmdArg = os.Args[2]
	}
	switch mode {
	case "usage-ok":
		switch cmdArg {
		case "/usage":
			fmt.Print(`{"conversation_id":"","status":"SUCCESS","num_turns":0,"command":{"name":"usage","data":{"groups":[{"name":"Gemini Models","description":"Models within this group: Gemini 4 Neon, Gemini 3.8 Flash","buckets":[{"id":"gemini-weekly","name":"Weekly Limit Remaining","window":"weekly","remaining_fraction":0.94,"reset_time":"2026-12-02T07:04:22Z"},{"id":"gemini-5h","name":"Five Hour Limit Remaining","window":"5h","remaining_fraction":0.77,"reset_time":"2026-11-25T12:04:22Z"}]},{"name":"Claude and GPT models","description":"Models within this group: Claude Opus 5.5, GPT-OSS 120B","buckets":[{"id":"3p-weekly","name":"Weekly Limit Remaining","window":"weekly","remaining_fraction":1,"reset_time":"1970-01-01T00:00:00Z"}]}]}}}`)
		case "/credits":
			fmt.Print(`{"conversation_id":"","status":"SUCCESS","num_turns":0,"command":{"name":"credits","data":{"remaining_credits":12.5}}}`)
		default:
			os.Exit(2)
		}
	case "usage-old-agy":
		// Simulates an older agy that ran a model turn instead of a read-only slash command.
		fmt.Print(`{"conversation_id":"c1","status":"SUCCESS","response":"Sorry","num_turns":1}`)
	case "usage-error":
		fmt.Fprintln(os.Stderr, "secret-account@example.com /Users/private")
		fmt.Print(`{"status":"ERROR","error":"no account quota for API keys","num_turns":0}`)
		os.Exit(2)
	case "usage-slow":
		time.Sleep(10 * time.Second)
	case "credits-missing-field":
		fmt.Print(`{"status":"SUCCESS","num_turns":0,"command":{"name":"credits","data":{}}}`)
	default:
		os.Exit(1)
	}
}

func fakeUsageProvider(t *testing.T, mode string) *AntigravityACP {
	t.Helper()
	t.Setenv(fakeACPEnv, mode)
	return &AntigravityACP{
		path:  os.Args[0],
		agent: "bare-llm",
		model: DefaultModel,
		cwd:   filepath.Join(t.TempDir(), "agy-workspace"),
	}
}

func TestAntigravityUsageAndCredits(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-secret")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sess-secret")
	a := fakeUsageProvider(t, "usage-ok")

	report, err := a.Usage(context.Background())
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if len(report.Groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(report.Groups))
	}
	if report.Groups[0].Name != "Gemini Models" || len(report.Groups[0].Buckets) != 2 {
		t.Fatalf("unexpected first group: %+v", report.Groups[0])
	}
	if report.Groups[0].Buckets[1].Window != "5h" || report.Groups[0].Buckets[1].RemainingFraction != 0.77 {
		t.Fatalf("unexpected 5h bucket: %+v", report.Groups[0].Buckets[1])
	}
	// Tool guard must have been installed in a.cwd.
	if _, err := os.Stat(filepath.Join(a.cwd, ".agents", "hooks.json")); err != nil {
		t.Fatalf("expected tool guard in workspace: %v", err)
	}

	credits, err := a.Credits(context.Background())
	if err != nil {
		t.Fatalf("Credits: %v", err)
	}
	if credits != 12.5 {
		t.Fatalf("credits = %v, want 12.5", credits)
	}
}

func TestAntigravityUsageRejectsOldAgyTurn(t *testing.T) {
	a := fakeUsageProvider(t, "usage-old-agy")
	if _, err := a.Usage(context.Background()); err == nil {
		t.Fatal("expected error when agy ran a model turn")
	}
}

func TestAntigravityUsageErrorDoesNotLeakStderr(t *testing.T) {
	a := fakeUsageProvider(t, "usage-error")
	_, err := a.Usage(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no account quota") {
		t.Fatalf("expected envelope error, got %v", err)
	}
	if strings.Contains(err.Error(), "secret-account") {
		t.Fatalf("leaked stderr in error: %v", err)
	}
}

func TestAntigravityUsageTimeout(t *testing.T) {
	a := fakeUsageProvider(t, "usage-slow")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := a.Usage(ctx); err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestAntigravityCreditsMissingField(t *testing.T) {
	a := fakeUsageProvider(t, "credits-missing-field")
	if _, err := a.Credits(context.Background()); err == nil {
		t.Fatal("expected error for missing remaining_credits")
	}
}
