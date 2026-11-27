package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/genai"
)

func TestParseShowThinking(t *testing.T) {
	cases := []struct {
		in   string
		want ShowThinkingMode
	}{
		{"", ThinkingOff},
		{"off", ThinkingOff},
		{"false", ThinkingOff},
		{"0", ThinkingOff},
		{"no", ThinkingOff},
		{"bogus", ThinkingOff},
		{"on", ThinkingOn},
		{"ON", ThinkingOn},
		{"true", ThinkingOn},
		{"1", ThinkingOn},
		{"yes", ThinkingOn},
		{"auto", ThinkingOn},
		{"always", ThinkingAlways},
		{" ALWAYS ", ThinkingAlways},
	}
	for _, tc := range cases {
		if got := ParseShowThinking(tc.in); got != tc.want {
			t.Errorf("ParseShowThinking(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestStreamEmitsThinkingBeforeTextAndToolUse(t *testing.T) {
	backend := &fakeBackend{chunks: []*genai.GenerateContentResponse{
		{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "first ", Thought: true}}}}},
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 11}},
		{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "thought", Thought: true}}}}}},
		{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "Hello"}}}}}},
		{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{Name: "Read", Args: map[string]any{"file_path": "/a"}}},
		}}, FinishReason: genai.FinishReasonStop}},
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 11, CandidatesTokenCount: 7}},
	}}
	rec := post(t, NewHandler(Config{Backend: backend, ShowThinking: ThinkingOn}), "/v1/messages", streamRequest)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if !backend.got.IncludeThoughts {
		t.Fatal("expected IncludeThoughts=true on backend request")
	}
	events := parseSSE(t, rec.Body.String())
	want := strings.Join([]string{
		"message_start", "ping",
		"content_block_start", "content_block_delta", "content_block_delta", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	}, ",")
	if names(events) != want {
		t.Fatalf("events = %s\nwant   = %s", names(events), want)
	}
	thinkStart := events[2].data["content_block"].(map[string]any)
	if thinkStart["type"] != "thinking" || thinkStart["thinking"] != "" || thinkStart["signature"] != "" {
		t.Fatalf("thinking start = %v", thinkStart)
	}
	if events[3].data["delta"].(map[string]any)["type"] != "thinking_delta" || events[3].data["delta"].(map[string]any)["thinking"] != "first " {
		t.Fatalf("thinking delta 1 = %v", events[3].data)
	}
	if events[4].data["delta"].(map[string]any)["thinking"] != "thought" {
		t.Fatalf("thinking delta 2 = %v", events[4].data)
	}
	if events[5].data["delta"].(map[string]any)["type"] != "signature_delta" || events[5].data["delta"].(map[string]any)["signature"] != "" {
		t.Fatalf("signature delta = %v", events[5].data)
	}
	if events[7].data["index"] != float64(1) || events[8].data["delta"].(map[string]any)["text"] != "Hello" {
		t.Fatalf("text block = %v / %v", events[7].data, events[8].data)
	}
	if events[10].data["index"] != float64(2) || events[10].data["content_block"].(map[string]any)["type"] != "tool_use" {
		t.Fatalf("tool_use block = %v", events[10].data)
	}
}

func TestStreamThinkingDirectlyBeforeToolUse(t *testing.T) {
	backend := &fakeBackend{chunks: []*genai.GenerateContentResponse{
		{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "plan", Thought: true}}}}}},
		{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{Name: "Read", Args: map[string]any{"file_path": "/a"}}},
		}}, FinishReason: genai.FinishReasonStop}}},
	}}
	rec := post(t, NewHandler(Config{Backend: backend, ShowThinking: ThinkingOn}), "/v1/messages", streamRequest)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	events := parseSSE(t, rec.Body.String())
	want := "message_start,ping,content_block_start,content_block_delta,content_block_delta,content_block_stop,content_block_start,content_block_delta,content_block_stop,message_delta,message_stop"
	if names(events) != want {
		t.Fatalf("events = %s\nwant   = %s", names(events), want)
	}
	if events[6].data["index"] != float64(1) {
		t.Fatalf("tool block index = %v, want 1", events[6].data["index"])
	}
}

func TestShowThinkingModesRespectDisabledRequest(t *testing.T) {
	disabledReq := `{"model":"gemini-neon","max_tokens":64,"thinking":{"type":"disabled"},"messages":[{"role":"user","content":"hi"}]}`
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{
			{Text: "hidden reason", Thought: true},
			{Text: "answer"},
		}}, FinishReason: genai.FinishReasonStop}},
	}

	for _, tc := range []struct {
		name       string
		mode       ShowThinkingMode
		wantBlocks int
		wantInc    bool
	}{
		{"off", ThinkingOff, 1, false},
		{"on-respects-disabled", ThinkingOn, 1, false},
		{"always-overrides-disabled", ThinkingAlways, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeBackend{response: resp}
			rec := post(t, NewHandler(Config{Backend: backend, ShowThinking: tc.mode}), "/v1/messages", disabledReq)
			if rec.Code != http.StatusOK {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
			if backend.got.IncludeThoughts != tc.wantInc {
				t.Fatalf("IncludeThoughts = %v, want %v", backend.got.IncludeThoughts, tc.wantInc)
			}
			var out struct {
				Content []map[string]any `json:"content"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if len(out.Content) != tc.wantBlocks {
				t.Fatalf("content blocks = %d (%v), want %d", len(out.Content), out.Content, tc.wantBlocks)
			}
			if tc.wantBlocks == 2 {
				if out.Content[0]["type"] != "thinking" || out.Content[0]["thinking"] != "hidden reason" || out.Content[0]["signature"] != "" {
					t.Fatalf("thinking block = %v", out.Content[0])
				}
			}
		})
	}
}
