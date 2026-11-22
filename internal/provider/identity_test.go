package provider

import (
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/translate"
)

// Claude Code's system prompt names the Claude model the session asks for; the model answering must hear first that this is only the session's label.
func TestModelIdentityOpensTheSystemInstructions(t *testing.T) {
	shared := &genai.GenerateContentConfig{SystemInstruction: &genai.Content{Parts: []*genai.Part{genai.NewPartFromText("You are powered by claude-opus-5-5.")}}}
	req := identified(translate.Request{Config: shared})
	parts := req.Config.SystemInstruction.Parts
	if len(parts) != 2 || parts[0].Text != ModelIdentity || parts[1].Text != "You are powered by claude-opus-5-5." {
		t.Fatalf("system instructions = %+v", parts)
	}
	if len(shared.SystemInstruction.Parts) != 1 {
		t.Fatal("the shared request config was modified")
	}
	if got := identified(translate.Request{}).Config.SystemInstruction.Parts; len(got) != 1 || got[0].Text != ModelIdentity {
		t.Fatalf("request without instructions: %+v", got)
	}
	if !strings.HasPrefix(acpAnswerInstructions, "Act as the language model for Claude Code. "+ModelIdentity) {
		t.Fatal("the Antigravity prompt does not carry the identity")
	}
}
