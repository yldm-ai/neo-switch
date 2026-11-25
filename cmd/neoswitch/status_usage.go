package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yldm-ai/neo-switch/internal/provider"
)

const (
	// statusSignInTimeout bounds the ACP sign-in check in `neoswitch status`.
	// A signed-out agy answers in under 100ms and a signed-in agy in about 1-2s.
	statusSignInTimeout = 10 * time.Second
	// statusUsageTimeout bounds `agy -p /usage --output-format json` (typically ~3s).
	statusUsageTimeout = 10 * time.Second
	usageBarWidth      = 20
)

type antigravityStatus struct {
	checked    bool
	signedIn   bool
	models     []string
	signInErr  error
	usage      *provider.UsageReport
	usageErr   error
	hasCredits bool
	credits    float64
	creditsErr error
}

// checkAntigravityStatus checks whether agy holds a usable Google sign-in via ACP,
// and only once confirmed signed in (and offering targetModel when known) fetches
// quota usage and optional credits. Running `agy -p /usage` while signed out blocks
// for 60s waiting for OAuth, so SignedIn always runs first.
func checkAntigravityStatus(agy *provider.AntigravityACP, targetModel string, fetchCredits bool) antigravityStatus {
	st := antigravityStatus{checked: true}
	signCtx, cancelSign := context.WithTimeout(context.Background(), statusSignInTimeout)
	defer cancelSign()
	signed, models, err := agy.SignedIn(signCtx)
	st.signedIn = signed
	st.models = models
	st.signInErr = err
	if err != nil || !signed {
		return st
	}
	if targetModel != "" && len(models) > 0 && !slices.Contains(models, targetModel) {
		return st
	}
	usageCtx, cancelUsage := context.WithTimeout(context.Background(), statusUsageTimeout)
	defer cancelUsage()
	if !fetchCredits {
		st.usage, st.usageErr = agy.Usage(usageCtx)
		return st
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		st.usage, st.usageErr = agy.Usage(usageCtx)
	}()
	go func() {
		defer wg.Done()
		credits, cerr := agy.Credits(usageCtx)
		st.creditsErr = cerr
		if cerr == nil {
			st.hasCredits = true
			st.credits = credits
		}
	}()
	wg.Wait()
	return st
}

// writeUsageReport renders quota buckets with plain-text Unicode progress bars.
// It returns true when at least one bucket was written.
func writeUsageReport(w io.Writer, report *provider.UsageReport, model string, all bool, now time.Time) bool {
	groups := selectUsageGroups(report, model, all)
	printed := false
	for _, group := range groups {
		for _, bucket := range orderUsageBuckets(group.Buckets) {
			fmt.Fprintln(w)
			header := windowLabel(bucket)
			if group.Name != "" {
				header = group.Name + " — " + header
			}
			fmt.Fprintln(w, header)
			rem := bucket.RemainingFraction
			if rem < 0 {
				rem = 0
			} else if rem > 1 {
				rem = 1
			}
			used := 1 - rem
			pct := int(math.Round(used * 100))
			fmt.Fprintf(w, "%s  %d%% used\n", usageProgressBar(used, pct, usageBarWidth), pct)
			if reset := formatResetTime(bucket.ResetTime, now); reset != "" {
				fmt.Fprintf(w, "Resets %s\n", reset)
			}
			printed = true
		}
	}
	return printed
}

func usageProgressBar(used float64, pct, width int) string {
	filled := int(math.Round(used * float64(width)))
	if pct > 0 && filled == 0 {
		filled = 1
	}
	if pct < 100 && filled >= width {
		filled = width - 1
	}
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func windowLabel(bucket provider.UsageBucket) string {
	switch strings.ToLower(strings.TrimSpace(bucket.Window)) {
	case "5h":
		return "5-hour limit"
	case "weekly":
		return "Current week"
	}
	if name := strings.TrimSpace(bucket.Name); name != "" {
		return name
	}
	return strings.TrimSpace(bucket.Window)
}

func orderUsageBuckets(buckets []provider.UsageBucket) []provider.UsageBucket {
	ordered := slices.Clone(buckets)
	rank := func(window string) int {
		switch strings.ToLower(strings.TrimSpace(window)) {
		case "5h":
			return 0
		case "weekly":
			return 1
		default:
			return 2
		}
	}
	slices.SortStableFunc(ordered, func(a, b provider.UsageBucket) int {
		return rank(a.Window) - rank(b.Window)
	})
	return ordered
}

func selectUsageGroups(report *provider.UsageReport, model string, all bool) []provider.UsageGroup {
	if report == nil || len(report.Groups) == 0 {
		return nil
	}
	if all {
		return report.Groups
	}
	normModel := normalizeModelTokens(stripEffortSuffix(model))
	if normModel != "" {
		for _, g := range report.Groups {
			if strings.Contains(normalizeModelTokens(g.Description), normModel) {
				return []provider.UsageGroup{g}
			}
		}
	}
	lowerModel := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(lowerModel, "gemini") || strings.HasPrefix(lowerModel, "orchestrator"):
		for _, g := range report.Groups {
			if strings.Contains(strings.ToLower(g.Name), "gemini") || hasBucketPrefix(g.Buckets, "gemini-") {
				return []provider.UsageGroup{g}
			}
		}
	case strings.HasPrefix(lowerModel, "claude") || strings.HasPrefix(lowerModel, "gpt"):
		for _, g := range report.Groups {
			name := strings.ToLower(g.Name)
			if strings.Contains(name, "claude") || strings.Contains(name, "gpt") {
				return []provider.UsageGroup{g}
			}
		}
	}
	return report.Groups
}

func hasBucketPrefix(buckets []provider.UsageBucket, prefix string) bool {
	for _, b := range buckets {
		if strings.HasPrefix(strings.ToLower(b.ID), prefix) {
			return true
		}
	}
	return false
}

func stripEffortSuffix(model string) string {
	lower := strings.ToLower(strings.TrimSpace(model))
	for _, suffix := range []string{"-xhigh", "-high", "-medium", "-low"} {
		if strings.HasSuffix(lower, suffix) {
			return strings.TrimSuffix(lower, suffix)
		}
	}
	return lower
}

func normalizeModelTokens(s string) string {
	replacer := strings.NewReplacer("-", " ", "_", " ", ".", " ", "(", " ", ")", " ", ",", " ", ":", " ")
	return strings.Join(strings.Fields(strings.ToLower(replacer.Replace(s))), " ")
}

func formatResetTime(raw string, now time.Time) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || t.Unix() <= 0 {
		return ""
	}
	local := t.In(now.Location())
	clock := local.Format("3:04pm")
	if local.Minute() == 0 {
		clock = local.Format("3pm")
	}
	ny, nm, nd := now.Date()
	ly, lm, ld := local.Date()
	var day string
	switch {
	case ly == ny && lm == nm && ld == nd:
		day = "today"
	case ly == ny:
		day = local.Format("Jan 2")
	default:
		day = local.Format("Jan 2, 2006")
	}
	return fmt.Sprintf("%s at %s (%s)", day, clock, timezoneLabel(local))
}

func timezoneLabel(t time.Time) string {
	if loc := t.Location(); loc != nil && loc.String() != "" && loc.String() != "Local" {
		return loc.String()
	}
	if tz := strings.TrimPrefix(strings.TrimSpace(os.Getenv("TZ")), ":"); tz != "" {
		return tz
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, after, ok := strings.Cut(target, "zoneinfo/"); ok && after != "" {
			return after
		}
	}
	return t.Format("MST")
}
