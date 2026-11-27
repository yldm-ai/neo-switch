package translate

import (
	"testing"

	"google.golang.org/genai"
)

func TestToGeminiMapsThinkingBudgetToLevel(t *testing.T) {
	cases := []struct {
		name  string
		extra string
		want  genai.ThinkingLevel
	}{
		{"low tier (4000)", `,"thinking":{"type":"enabled","budget_tokens":4000}`, genai.ThinkingLevelLow},
		{"min anthropic (1024)", `,"thinking":{"type":"enabled","budget_tokens":1024}`, genai.ThinkingLevelLow},
		{"boundary below medium (5999)", `,"thinking":{"type":"enabled","budget_tokens":5999}`, genai.ThinkingLevelLow},
		{"boundary medium (6000)", `,"thinking":{"type":"enabled","budget_tokens":6000}`, genai.ThinkingLevelMedium},
		{"medium tier (8000)", `,"thinking":{"type":"enabled","budget_tokens":8000}`, genai.ThinkingLevelMedium},
		{"boundary below high (11999)", `,"thinking":{"type":"enabled","budget_tokens":11999}`, genai.ThinkingLevelMedium},
		{"boundary high (12000)", `,"thinking":{"type":"enabled","budget_tokens":12000}`, genai.ThinkingLevelHigh},
		{"high tier (16000)", `,"thinking":{"type":"enabled","budget_tokens":16000}`, genai.ThinkingLevelHigh},
		{"boundary below xhigh (23999)", `,"thinking":{"type":"enabled","budget_tokens":23999}`, genai.ThinkingLevelHigh},
		{"boundary xhigh (24000)", `,"thinking":{"type":"enabled","budget_tokens":24000}`, genai.ThinkingLevelXhigh},
		{"xhigh tier (32000)", `,"thinking":{"type":"enabled","budget_tokens":32000}`, genai.ThinkingLevelXhigh},
		{"zero budget ignored", `,"thinking":{"type":"enabled","budget_tokens":0}`, ""},
		{"disabled ignores budget", `,"thinking":{"type":"disabled","budget_tokens":16000}`, ""},
		{"adaptive without effort stays empty", `,"thinking":{"type":"adaptive","budget_tokens":16000}`, ""},
		{"output_config.effort wins over budget", `,"thinking":{"type":"enabled","budget_tokens":32000},"output_config":{"effort":"low"}`, genai.ThinkingLevelLow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := mustDecode(t, `{"model":"gemini-neon","max_tokens":32000`+tc.extra+`,"messages":[{"role":"user","content":"hi"}]}`)
			got, err := ToGemini(req)
			if err != nil {
				t.Fatal(err)
			}
			if got.ThinkingLevel != tc.want {
				t.Fatalf("ThinkingLevel = %q, want %q", got.ThinkingLevel, tc.want)
			}
		})
	}
}
