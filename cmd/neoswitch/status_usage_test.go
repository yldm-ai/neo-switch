package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yldm-ai/neo-switch/internal/provider"
)

const sampleUsageJSON = `{"conversation_id":"","status":"SUCCESS","num_turns":0,"command":{"name":"usage","data":{"groups":[{"name":"Gemini Models","description":"Models within this group: Gemini 4 Neon, Gemini 3.8 Flash","buckets":[{"id":"gemini-weekly","name":"Weekly Limit Remaining","window":"weekly","remaining_fraction":0.938756,"reset_time":"2026-12-02T07:04:22Z"},{"id":"gemini-5h","name":"Five Hour Limit Remaining","window":"5h","remaining_fraction":0.77099,"reset_time":"2026-11-25T12:04:22Z"}]},{"name":"Claude and GPT models","description":"Models within this group: Claude Opus 5.5, GPT-OSS 120B","buckets":[{"id":"3p-weekly","name":"Weekly Limit Remaining","window":"weekly","remaining_fraction":1,"reset_time":"1970-01-01T00:00:00Z"}]}]}}}`

const sampleCreditsJSON = `{"conversation_id":"","status":"SUCCESS","num_turns":0,"command":{"name":"credits","data":{"remaining_credits":12.5}}}`

func TestWriteUsageReportFormattingAndSelection(t *testing.T) {
	loc := time.FixedZone("Asia/Saigon", 7*3600)
	now := time.Date(2026, 11, 25, 16, 0, 0, 0, loc)
	report := &provider.UsageReport{
		Groups: []provider.UsageGroup{
			{
				Name:        "Gemini Models",
				Description: "Models within this group: Gemini 4 Neon, Gemini 3.8 Flash",
				Buckets: []provider.UsageBucket{
					{ID: "gemini-weekly", Name: "Weekly Limit Remaining", Window: "weekly", RemainingFraction: 0.938756, ResetTime: "2026-12-02T04:00:00Z"},
					{ID: "gemini-5h", Name: "Five Hour Limit Remaining", Window: "5h", RemainingFraction: 0.77099, ResetTime: "2026-11-25T12:04:22Z"},
				},
			},
			{
				Name:        "Claude and GPT models",
				Description: "Models within this group: Claude Opus 5.5, GPT-OSS 120B",
				Buckets: []provider.UsageBucket{
					{ID: "3p-weekly", Name: "Weekly Limit Remaining", Window: "weekly", RemainingFraction: 1, ResetTime: "1970-01-01T00:00:00Z"},
				},
			},
		},
	}

	var out bytes.Buffer
	if !writeUsageReport(&out, report, "gemini-4-neon-high", false, now) {
		t.Fatal("expected writeUsageReport to print buckets")
	}
	got := out.String()
	i5h := strings.Index(got, "Gemini Models — 5-hour limit")
	iWeek := strings.Index(got, "Gemini Models — Current week")
	if i5h < 0 || iWeek < 0 || i5h > iWeek {
		t.Fatalf("expected 5h bucket before weekly bucket, got:\n%s", got)
	}
	for _, want := range []string{
		"█████░░░░░░░░░░░░░░░  23% used",
		"Resets today at 7:04pm (Asia/Saigon)",
		"█░░░░░░░░░░░░░░░░░░░  6% used",
		"Resets Dec 2 at 11am (Asia/Saigon)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Claude and GPT models") {
		t.Fatalf("non-verbose output should omit unrelated group:\n%s", got)
	}

	// Verbose (all=true) prints every group and omits epoch-0 reset timestamps.
	out.Reset()
	if !writeUsageReport(&out, report, "gemini-4-neon", true, now) {
		t.Fatal("expected writeUsageReport(all=true) to print buckets")
	}
	allOut := out.String()
	if !strings.Contains(allOut, "Claude and GPT models — Current week") || !strings.Contains(allOut, "░░░░░░░░░░░░░░░░░░░░  0% used") {
		t.Fatalf("verbose output lacks second group:\n%s", allOut)
	}
	if strings.Contains(allOut, "1970") {
		t.Fatalf("epoch-0 sentinel reset time must be omitted:\n%s", allOut)
	}

	// Selecting a Claude/GPT model picks the second group.
	out.Reset()
	writeUsageReport(&out, report, "claude-opus-5-5-high", false, now)
	if !strings.Contains(out.String(), "Claude and GPT models") || strings.Contains(out.String(), "Gemini Models") {
		t.Fatalf("unexpected group for claude model:\n%s", out.String())
	}
}

func TestUsageProgressBarEdges(t *testing.T) {
	cases := []struct {
		used float64
		pct  int
		want string
	}{
		{0, 0, "░░░░░░░░░░░░░░░░░░░░"},
		{0.01, 1, "█░░░░░░░░░░░░░░░░░░░"},
		{0.99, 99, "███████████████████░"},
		{1, 100, "████████████████████"},
	}
	for _, tc := range cases {
		if got := usageProgressBar(tc.used, tc.pct, 20); got != tc.want {
			t.Fatalf("usageProgressBar(%v, %d) = %q, want %q", tc.used, tc.pct, got, tc.want)
		}
	}
}

func TestFormatResetTime(t *testing.T) {
	loc := time.FixedZone("UTC", 0)
	now := time.Date(2026, 12, 30, 10, 0, 0, 0, loc)
	if got := formatResetTime("1970-01-01T00:00:00Z", now); got != "" {
		t.Fatalf("epoch 0 = %q, want empty", got)
	}
	if got := formatResetTime("not-a-time", now); got != "" {
		t.Fatalf("invalid = %q, want empty", got)
	}
	if got := formatResetTime("2027-01-03T15:30:00Z", now); got != "Jan 3, 2027 at 3:30pm (UTC)" {
		t.Fatalf("next year = %q", got)
	}
}

func TestStatusAntigravitySignInAndUsage(t *testing.T) {
	t.Run("on Claude, agy signed out does not invoke usage", func(t *testing.T) {
		isolateBackendEnv(t, false)
		t.Setenv("ANTHROPIC_BASE_URL", "")
		t.Setenv("ANTIGRAVITY_CLI_PATH", os.Args[0])
		t.Setenv(fakeAgyEnv, "1")
		t.Setenv("FAKE_SIGNED_IN", filepath.Join(t.TempDir(), "missing"))
		marker := filepath.Join(t.TempDir(), "usage-called")
		t.Setenv("FAKE_USAGE_CALLED", marker)
		t.Setenv("FAKE_USAGE", sampleUsageJSON)

		var out bytes.Buffer
		if err := printStatus(&out, t.TempDir(), false); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		for _, want := range []string{"This session uses Claude.", "Not signed in to Google (Antigravity)", "/neoswitch:login"} {
			if !strings.Contains(text, want) {
				t.Fatalf("output lacks %q:\n%s", want, text)
			}
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatal("signed-out status must not run agy -p /usage")
		}
	})

	t.Run("on Claude, agy signed in reports sign-in without invoking usage", func(t *testing.T) {
		isolateBackendEnv(t, false)
		t.Setenv("ANTHROPIC_BASE_URL", "")
		installAgy(t)
		marker := filepath.Join(t.TempDir(), "usage-called")
		t.Setenv("FAKE_USAGE_CALLED", marker)
		t.Setenv("FAKE_USAGE", sampleUsageJSON)

		var out bytes.Buffer
		if err := printStatus(&out, t.TempDir(), false); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		for _, want := range []string{"This session uses Claude.", "Signed in to Google (Antigravity), not active in this session.", "/neoswitch:on"} {
			if !strings.Contains(text, want) {
				t.Fatalf("output lacks %q:\n%s", want, text)
			}
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatal("inactive session status must not run agy -p /usage")
		}
	})

	t.Run("active on Gemini, agy signed out afterwards does not invoke usage", func(t *testing.T) {
		isolateBackendEnv(t, false)
		t.Setenv("ANTHROPIC_BASE_URL", "")
		installAgy(t)
		inSession(t)
		project := t.TempDir()
		mustOn(t, project)
		// Simulate signing out after switching on.
		if err := os.Remove(os.Getenv("FAKE_SIGNED_IN")); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(t.TempDir(), "usage-called")
		t.Setenv("FAKE_USAGE_CALLED", marker)
		t.Setenv("FAKE_USAGE", sampleUsageJSON)

		var out bytes.Buffer
		if err := printStatus(&out, project, false); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		for _, want := range []string{"not signed in to Google (Antigravity)", "/neoswitch:login", "/neoswitch:off"} {
			if !strings.Contains(text, want) {
				t.Fatalf("output lacks %q:\n%s", want, text)
			}
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatal("signed-out active status must not run agy -p /usage")
		}
	})

	t.Run("active and signed in prints quota bars and verbose adds all groups and credits", func(t *testing.T) {
		isolateBackendEnv(t, false)
		t.Setenv("ANTHROPIC_BASE_URL", "")
		installAgy(t)
		inSession(t)
		project := t.TempDir()
		mustOn(t, project)
		t.Setenv("FAKE_USAGE", sampleUsageJSON)
		t.Setenv("FAKE_CREDITS", sampleCreditsJSON)

		var out bytes.Buffer
		if err := printStatus(&out, project, false); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		for _, want := range []string{
			"This session uses Gemini 4 Neon",
			"Gemini Models — 5-hour limit",
			"23% used",
			"Gemini Models — Current week",
			"6% used",
			"Resets ",
			"/neoswitch:off",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("output lacks %q:\n%s", want, text)
			}
		}
		for _, notWant := range []string{"Claude and GPT models", "credits:", "sign-in:"} {
			if strings.Contains(text, notWant) {
				t.Fatalf("non-verbose output contains %q:\n%s", notWant, text)
			}
		}

		out.Reset()
		if err := printStatus(&out, project, true); err != nil {
			t.Fatal(err)
		}
		verbose := out.String()
		for _, want := range []string{
			"Gemini Models — 5-hour limit",
			"Claude and GPT models — Current week",
			"sign-in:  signed in",
			"credits:  12.5",
		} {
			if !strings.Contains(verbose, want) {
				t.Fatalf("verbose output lacks %q:\n%s", want, verbose)
			}
		}
	})

	t.Run("active and signed in degrades gracefully when usage fails", func(t *testing.T) {
		isolateBackendEnv(t, false)
		t.Setenv("ANTHROPIC_BASE_URL", "")
		installAgy(t)
		inSession(t)
		project := t.TempDir()
		mustOn(t, project)
		t.Setenv("FAKE_USAGE", "error")

		var out bytes.Buffer
		if err := printStatus(&out, project, false); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		for _, want := range []string{"This session uses Gemini 4 Neon", "NeoSwitch is running", "/neoswitch:off"} {
			if !strings.Contains(text, want) {
				t.Fatalf("output lacks %q:\n%s", want, text)
			}
		}
		if strings.Contains(text, "usage unavailable") {
			t.Fatalf("non-verbose output should stay clean on usage failure:\n%s", text)
		}

		out.Reset()
		if err := printStatus(&out, project, true); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "usage:    unavailable") {
			t.Fatalf("verbose output should report usage failure:\n%s", out.String())
		}
	})
}
