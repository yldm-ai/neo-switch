package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// UsageReport is the structured payload returned by `agy -p /usage --output-format json`.
// agy 1.1.11+ answers read-only slash commands in print mode without starting an agent turn,
// spending quota, or leaving a conversation behind.
type UsageReport struct {
	Description string       `json:"description,omitempty"`
	Groups      []UsageGroup `json:"groups"`
}

// UsageGroup is one quota group (for example "Gemini Models" or "Claude and GPT models").
type UsageGroup struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Buckets     []UsageBucket `json:"buckets"`
}

// UsageBucket is one rolling quota window inside a UsageGroup (such as "5h" or "weekly").
type UsageBucket struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Description       string  `json:"description,omitempty"`
	Window            string  `json:"window"`
	RemainingFraction float64 `json:"remaining_fraction"`
	ResetTime         string  `json:"reset_time"`
}

type agyPrintEnvelope struct {
	Status   string `json:"status"`
	Error    string `json:"error"`
	NumTurns int    `json:"num_turns"`
	Command  *struct {
		Name string          `json:"name"`
		Data json.RawMessage `json:"data"`
	} `json:"command"`
}

// Usage fetches account quota information from agy without spending quota or starting a model turn.
// Callers must confirm agy is signed in first (via SignedIn): a signed-out agy blocks for 60s waiting
// for OAuth in print mode.
func (a *AntigravityACP) Usage(ctx context.Context) (*UsageReport, error) {
	raw, err := a.runPrintCommand(ctx, "usage")
	if err != nil {
		return nil, err
	}
	var report UsageReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, fmt.Errorf("decode Antigravity usage: %w", err)
	}
	return &report, nil
}

// Credits fetches the remaining AI credit balance from agy without spending quota or starting a model turn.
// Callers must confirm agy is signed in first (via SignedIn).
func (a *AntigravityACP) Credits(ctx context.Context) (float64, error) {
	raw, err := a.runPrintCommand(ctx, "credits")
	if err != nil {
		return 0, err
	}
	var data struct {
		RemainingCredits *float64 `json:"remaining_credits"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return 0, fmt.Errorf("decode Antigravity credits: %w", err)
	}
	if data.RemainingCredits == nil {
		return 0, errors.New("Antigravity credits payload omitted remaining_credits")
	}
	return *data.RemainingCredits, nil
}

// runPrintCommand runs a read-only agy slash command in print mode (`agy -p /<name> --output-format json`)
// and returns its structured command.data payload. It runs inside NeoSwitch's guarded workspace and
// rejects any envelope that started a turn or did not match the requested command, so older agy releases
// without non-interactive slash commands fail safely.
func (a *AntigravityACP) runPrintCommand(ctx context.Context, name string) (json.RawMessage, error) {
	if a.cwd != "" {
		if err := os.MkdirAll(a.cwd, 0o700); err != nil {
			return nil, fmt.Errorf("prepare Antigravity workspace: %w", err)
		}
		if err := installToolGuard(a.cwd); err != nil {
			return nil, fmt.Errorf("install Antigravity tool guard: %w", err)
		}
	}
	cmd := exec.CommandContext(ctx, a.path, "-p", "/"+name, "--output-format", "json")
	if a.cwd != "" {
		cmd.Dir = a.cwd
	}
	cmd.Env = acpEnv(os.Environ())
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	// Discard stderr: agy may log local paths or account details that NeoSwitch never echoes.
	runErr := cmd.Run()
	payload := bytes.TrimSpace(stdout.Bytes())
	var env agyPrintEnvelope
	if len(payload) > 0 && json.Unmarshal(payload, &env) == nil {
		if env.Error != "" {
			return nil, fmt.Errorf("agy /%s: %s", name, env.Error)
		}
		if runErr == nil {
			if env.Status != "SUCCESS" || env.NumTurns != 0 || env.Command == nil || env.Command.Name != name {
				return nil, fmt.Errorf("agy /%s returned an unexpected response", name)
			}
			return env.Command.Data, nil
		}
	}
	if runErr != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("agy /%s: %w", name, ctx.Err())
		}
		return nil, fmt.Errorf("agy /%s failed: %w", name, runErr)
	}
	return nil, fmt.Errorf("agy /%s returned invalid JSON", name)
}
