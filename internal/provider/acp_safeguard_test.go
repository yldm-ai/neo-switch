package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestACPSafeguardModelSelection(t *testing.T) {
	a := &AntigravityACP{model: DefaultModel, background: DefaultBackgroundModel}
	if got := a.safeguardModel(); got != DefaultBackgroundModel {
		t.Fatalf("default safeguardModel = %q, want %q", got, DefaultBackgroundModel)
	}
	t.Setenv("NEOSWITCH_SAFEGUARD_MODEL", "gemini-3.8-flash-lite")
	if got := a.safeguardModel(); got != "gemini-3.8-flash-lite" {
		t.Fatalf("custom safeguardModel = %q", got)
	}
	t.Setenv("NEOSWITCH_SAFEGUARD_MODEL", "main")
	if got := a.safeguardModel(); got != DefaultModel {
		t.Fatalf("main safeguardModel = %q", got)
	}
	t.Setenv("NEOSWITCH_SAFEGUARD_MODEL", "")
	a.backgroundMissing.Store(true)
	if got := a.safeguardModel(); got != DefaultModel {
		t.Fatalf("fallback safeguardModel = %q, want %q", got, DefaultModel)
	}
}

func TestACPJudgeSafeguardEphemeralAndBypassesEnvelope(t *testing.T) {
	a := newFakeACP(t, "text")
	raw, err := a.JudgeSafeguard(context.Background(), "System policy", "Classify /plan safely")
	if err != nil {
		t.Fatalf("JudgeSafeguard: %v", err)
	}
	if strings.TrimSpace(raw) == "" {
		t.Fatal("empty JudgeSafeguard output")
	}
	a.conn.mu.Lock()
	idle := len(a.conn.idle)
	a.conn.mu.Unlock()
	if idle != 0 {
		t.Fatalf("idle sessions after JudgeSafeguard = %d, want 0", idle)
	}
	a.unguarded.Store(true)
	if _, err := a.JudgeSafeguard(context.Background(), "System", "Prompt"); err != unguardedError {
		t.Fatalf("unguarded JudgeSafeguard err = %v, want unguardedError", err)
	}
}

func TestACPJudgeSafeguardUsesSystemPromptAppendWhenAdvertised(t *testing.T) {
	a := newFakeACP(t, "system-prompt-append")
	raw, err := a.JudgeSafeguard(context.Background(), "Policy rules", "Untrusted user command")
	if err != nil {
		t.Fatalf("JudgeSafeguard: %v", err)
	}
	var env struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal envelope %q: %v", raw, err)
	}
	var seen struct {
		Append string `json:"append"`
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(env.Text), &seen); err != nil {
		t.Fatalf("unmarshal payload %q: %v", env.Text, err)
	}
	if seen.Append != "Policy rules" {
		t.Fatalf("append = %q, want %q", seen.Append, "Policy rules")
	}
	if seen.Prompt != "Untrusted user command" {
		t.Fatalf("prompt = %q, want %q", seen.Prompt, "Untrusted user command")
	}
}
