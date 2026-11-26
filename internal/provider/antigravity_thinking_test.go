package provider

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/translate"
)

func TestAntigravityACPStreamThinkingAndSessionReuse(t *testing.T) {
	backend := newFakeACP(t, "thought")
	defer backend.Close()

	// Default (IncludeThoughts=false): thoughts are suppressed.
	reqHidden := translate.Request{
		Contents: []*genai.Content{genai.NewContentFromText("turn 1", genai.RoleUser)},
		Origin:   translate.Origin{Session: "sess-hidden", Class: "main"},
	}
	var hiddenThoughts, hiddenText strings.Builder
	for resp, err := range backend.Stream(context.Background(), reqHidden) {
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range resp.Candidates {
			if c.Content == nil {
				continue
			}
			for _, p := range c.Content.Parts {
				if p.Thought {
					hiddenThoughts.WriteString(p.Text)
				} else {
					hiddenText.WriteString(p.Text)
				}
			}
		}
	}
	if hiddenThoughts.Len() != 0 {
		t.Fatalf("expected no thoughts when IncludeThoughts=false, got %q", hiddenThoughts.String())
	}
	if hiddenText.String() == "" {
		t.Fatal("expected answer text when IncludeThoughts=false")
	}

	// Enabled (IncludeThoughts=true): thoughts stream before text, and session reuse still works on turn 2.
	req1 := translate.Request{
		Contents:        []*genai.Content{genai.NewContentFromText("hello", genai.RoleUser)},
		IncludeThoughts: true,
		Origin:          translate.Origin{Session: "sess-shown", Class: "main"},
	}
	var order []string
	var thoughts1, text1 strings.Builder
	for resp, err := range backend.Stream(context.Background(), req1) {
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range resp.Candidates {
			if c.Content == nil {
				continue
			}
			for _, p := range c.Content.Parts {
				if p.Thought {
					order = append(order, "thought")
					thoughts1.WriteString(p.Text)
				} else if p.Text != "" {
					order = append(order, "text")
					text1.WriteString(p.Text)
				}
			}
		}
	}
	if thoughts1.String() != "First thought. Second thought." {
		t.Fatalf("thoughts = %q", thoughts1.String())
	}
	if len(order) < 3 || order[0] != "thought" || order[1] != "thought" || order[2] != "text" {
		t.Fatalf("unexpected chunk order: %v", order)
	}
	fields1 := strings.Fields(text1.String())
	if len(fields1) != 2 || fields1[1] != "1" {
		t.Fatalf("turn 1 text = %q", text1.String())
	}

	// Turn 2 echoes the assistant text without thinking blocks (just as Claude Code + translate.ToGemini do).
	req2 := translate.Request{
		Contents: []*genai.Content{
			genai.NewContentFromText("hello", genai.RoleUser),
			genai.NewContentFromText(text1.String(), genai.RoleModel),
			genai.NewContentFromText("follow up", genai.RoleUser),
		},
		IncludeThoughts: true,
		Origin:          translate.Origin{Session: "sess-shown", Class: "main"},
	}
	resp2, err := backend.Generate(context.Background(), req2)
	if err != nil {
		t.Fatal(err)
	}
	parts := resp2.Candidates[0].Content.Parts
	if len(parts) != 2 || !parts[0].Thought || parts[0].Text != "First thought. Second thought." {
		t.Fatalf("turn 2 parts = %+v", parts)
	}
	fields2 := strings.Fields(parts[1].Text)
	if len(fields2) != 2 || fields2[0] != fields1[0] || fields2[1] != "2" {
		t.Fatalf("expected reused session %s prompt 2, got %q", fields1[0], parts[1].Text)
	}
}

func TestAntigravityACPStreamThinkingBeforeToolCall(t *testing.T) {
	backend := newFakeACP(t, "thought-tool")
	defer backend.Close()

	req := translate.Request{
		Contents:        []*genai.Content{genai.NewContentFromText("read a.go", genai.RoleUser)},
		IncludeThoughts: true,
	}
	var thoughts strings.Builder
	var toolName string
	for resp, err := range backend.Stream(context.Background(), req) {
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range resp.Candidates {
			if c.Content == nil {
				continue
			}
			for _, p := range c.Content.Parts {
				if p.Thought {
					thoughts.WriteString(p.Text)
				}
				if p.FunctionCall != nil {
					toolName = p.FunctionCall.Name
				}
			}
		}
	}
	if thoughts.String() != "Planning tool call." || toolName != "Read" {
		t.Fatalf("thoughts=%q tool=%q", thoughts.String(), toolName)
	}
}

func TestAntigravityACPSuppressesIntermediateMultiPromptThoughts(t *testing.T) {
	backend := newFakeACP(t, "thought-split")
	defer backend.Close()

	// > 150 KB forces acpPrompts to split into multiple prompts.
	big := strings.Repeat("a", 170_000)
	req := translate.Request{
		Contents:        []*genai.Content{genai.NewContentFromText(big, genai.RoleUser)},
		IncludeThoughts: true,
	}
	var thoughts, text strings.Builder
	for resp, err := range backend.Stream(context.Background(), req) {
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range resp.Candidates {
			if c.Content == nil {
				continue
			}
			for _, p := range c.Content.Parts {
				if p.Thought {
					thoughts.WriteString(p.Text)
				} else {
					text.WriteString(p.Text)
				}
			}
		}
	}
	if thoughts.String() != "FINAL_THOUGHT" {
		t.Fatalf("intermediate thoughts leaked or missing: %q", thoughts.String())
	}
	if text.String() != "split-done" {
		t.Fatalf("text = %q, want split-done", text.String())
	}
}

func TestGeminiGenerateConfigSetsIncludeThoughts(t *testing.T) {
	g := &Gemini{Model: DefaultModel}
	base := &genai.GenerateContentConfig{MaxOutputTokens: 128}

	cfg, changed := g.generateConfig(translate.Request{Config: base, IncludeThoughts: false})
	if changed || cfg.ThinkingConfig != nil {
		t.Fatalf("expected unchanged config without thoughts, got changed=%v thinking=%+v", changed, cfg.ThinkingConfig)
	}

	cfg, changed = g.generateConfig(translate.Request{Config: base, IncludeThoughts: true})
	if !changed || cfg.ThinkingConfig == nil || !cfg.ThinkingConfig.IncludeThoughts || cfg.ThinkingConfig.ThinkingLevel != "" {
		t.Fatalf("expected IncludeThoughts=true only, got changed=%v thinking=%+v", changed, cfg.ThinkingConfig)
	}
	if base.ThinkingConfig != nil {
		t.Fatal("generateConfig mutated shared base config")
	}
}
