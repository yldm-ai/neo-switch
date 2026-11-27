package translate

import (
	"encoding/json"
	"testing"

	"google.golang.org/genai"
)

func TestFromGeminiWithOptionsIncludesThinkingBlocks(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			FinishReason: genai.FinishReasonStop,
			Content: &genai.Content{Parts: []*genai.Part{
				{Text: "step 1; step 2", Thought: true},
				{Text: "", Thought: true},
				{Text: "Final answer."},
			}},
		}},
	}

	hidden, err := FromGeminiWithOptions(resp, "gemini-neon", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(hidden.Content) != 1 || hidden.Content[0].Type != "text" || hidden.Content[0].Text != "Final answer." {
		t.Fatalf("hidden content = %+v", hidden.Content)
	}

	shown, err := FromGeminiWithOptions(resp, "gemini-neon", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(shown.Content) != 2 {
		t.Fatalf("shown content = %+v", shown.Content)
	}
	if shown.Content[0].Type != "thinking" || shown.Content[0].Thinking != "step 1; step 2" {
		t.Fatalf("thinking block = %+v", shown.Content[0])
	}
	if shown.Content[0].Signature == nil || *shown.Content[0].Signature != "" {
		t.Fatalf("thinking block must include empty signature, got %+v", shown.Content[0].Signature)
	}
	if shown.Content[1].Type != "text" || shown.Content[1].Text != "Final answer." || shown.Content[1].Signature != nil {
		t.Fatalf("text block = %+v", shown.Content[1])
	}

	// Verify JSON wire encoding: thinking carries "signature":"", text omits "signature".
	raw, err := json.Marshal(shown)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Content []map[string]any `json:"content"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if sig, ok := wire.Content[0]["signature"]; !ok || sig != "" {
		t.Fatalf("thinking wire JSON missing empty signature: %s", raw)
	}
	if _, ok := wire.Content[1]["signature"]; ok {
		t.Fatalf("text wire JSON must omit signature: %s", raw)
	}
}
