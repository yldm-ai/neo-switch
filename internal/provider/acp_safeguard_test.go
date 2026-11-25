package provider

import (
	"context"
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
