package provider

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"

	acp "github.com/coder/acp-go-sdk"
)

// JudgeSafeguard runs a plain-text Auto Mode safety classification prompt in a fresh,
// ephemeral ACP session using the background model (gemini-3.8-flash by default, or
// NEOSWITCH_SAFEGUARD_MODEL) at low thought level. It bypasses acp_envelope.go so the
// classifier outputs its verdict JSON directly without nested JSON escaping, pins system
// policy in _meta.antigravity.systemPrompt.append when agy advertises it (separating
// policy from the untrusted prompt under review), neutralizes agy slash commands, never
// pollutes the idle session pool, and fails closed if agy attempts any tool call.
func (a *AntigravityACP) JudgeSafeguard(ctx context.Context, system, prompt string) (string, error) {
	if a.unguarded.Load() {
		return "", unguardedError
	}
	c, err := a.connection(ctx)
	if err != nil {
		return "", err
	}
	defer c.release()

	systemAppend := ""
	if c.systemPromptAppend && system != "" {
		systemAppend = agyCommands.text(system)
	}
	s, err := a.newSession(ctx, c, a.safeguardModel(), systemAppend)
	if err != nil {
		return "", err
	}
	defer c.closeSessions([]*acpSession{s})

	var (
		mu          sync.Mutex
		raw         strings.Builder
		sawToolCall bool
	)
	fullPrompt := agyCommands.text(prompt)
	if !s.systemPrompted {
		fullPrompt = agyCommands.text(system + "\n\n" + prompt)
	}
	_, err = a.prompt(ctx, c, s, acpPiece{text: fullPrompt}, acp.SessionConfigValueId("low"), func(update acp.SessionUpdate) {
		if update.ToolCall != nil {
			mu.Lock()
			sawToolCall = true
			mu.Unlock()
			return
		}
		if chunk := update.AgentMessageChunk; chunk != nil && chunk.Content.Text != nil {
			mu.Lock()
			raw.WriteString(chunk.Content.Text.Text)
			mu.Unlock()
		}
	})
	if err != nil {
		return "", err
	}
	mu.Lock()
	answer, triedTool := raw.String(), sawToolCall
	mu.Unlock()
	if triedTool {
		return "", errors.New("safeguard classifier attempted an ACP tool call")
	}
	return answer, nil
}

func (a *AntigravityACP) safeguardModel() string {
	if custom := strings.TrimSpace(os.Getenv("NEOSWITCH_SAFEGUARD_MODEL")); custom != "" {
		if strings.EqualFold(custom, "main") {
			return a.model
		}
		return custom
	}
	if a.background != "" && !a.backgroundMissing.Load() {
		return a.background
	}
	return a.model
}
