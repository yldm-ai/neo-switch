package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/translate"
)

type fakeSearcher struct {
	fakeBackend
	result translate.SearchResult
	err    error
	// failAfterQuery fails the search after it reported its first query.
	failAfterQuery bool
}

func (f *fakeSearcher) SearchWeb(_ context.Context, req translate.Request, onQuery func(string)) (translate.SearchResult, error) {
	f.got = req
	for _, query := range f.result.Queries {
		onQuery(query)
		if f.failAfterQuery {
			return translate.SearchResult{}, errors.New("agy went away")
		}
	}
	return f.result, f.err
}

const searchBody = `{"model":"claude-opus","max_tokens":100,"stream":%s,"tools":[{"type":"web_search_20250305","name":"web_search","max_uses":8}],"tool_choice":{"type":"tool","name":"web_search"},"messages":[{"role":"user","content":"Perform a web search for the query: go 1.24"}]}`

func found() translate.SearchResult {
	return translate.SearchResult{
		Queries: []string{"go 1.24", "go 1.24 release"},
		Links:   []translate.SearchLink{{Title: "go.dev", URL: "https://go.dev/doc/go1.24"}},
		Summary: "Go 1.24 adds generic type aliases.",
		Usage:   &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 900, CandidatesTokenCount: 40},
	}
}

// Claude Code's WebSearch tool reads the server_tool_use blocks, the pages of the web_search_tool_result and the text; the stream must have the shapes Anthropic's API streams.
func TestStreamedWebSearchHasTheBlocksClaudeCodeReads(t *testing.T) {
	backend := &fakeSearcher{result: found()}
	rec := post(t, NewHandler(Config{Token: "secret", Backend: backend}), "/v1/messages", strings.Replace(searchBody, "%s", "true", 1))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if backend.got.WebSearch == nil || backend.got.WebSearch.MaxUses != 8 {
		t.Fatalf("search request %+v", backend.got.WebSearch)
	}
	events := parseSSE(t, rec.Body.String())
	var ids []string
	var resultFor, text, stop string
	var requests float64
	for _, e := range events {
		switch e.name {
		case "content_block_start":
			block := e.data["content_block"].(map[string]any)
			switch block["type"] {
			case "server_tool_use":
				ids = append(ids, block["id"].(string))
				if input := block["input"].(map[string]any); len(input) != 0 {
					t.Fatalf("server_tool_use input must arrive as a delta: %v", input)
				}
			case "web_search_tool_result":
				resultFor = block["tool_use_id"].(string)
				pages := block["content"].([]any)
				if len(pages) != 1 || pages[0].(map[string]any)["url"] != "https://go.dev/doc/go1.24" || pages[0].(map[string]any)["type"] != "web_search_result" {
					t.Fatalf("pages %v", pages)
				}
			}
		case "content_block_delta":
			delta := e.data["delta"].(map[string]any)
			if delta["type"] == "text_delta" {
				text += delta["text"].(string)
			}
			if delta["type"] == "input_json_delta" && !strings.Contains(delta["partial_json"].(string), `"query":"go 1.24`) {
				t.Fatalf("query delta %v", delta)
			}
		case "message_delta":
			stop = e.data["delta"].(map[string]any)["stop_reason"].(string)
			requests = e.data["usage"].(map[string]any)["server_tool_use"].(map[string]any)["web_search_requests"].(float64)
		}
	}
	if names(events) != "message_start,ping,content_block_start,content_block_delta,content_block_stop,content_block_start,content_block_delta,content_block_stop,content_block_start,content_block_stop,content_block_start,content_block_delta,content_block_stop,message_delta,message_stop" {
		t.Fatalf("events %s", names(events))
	}
	if len(ids) != 2 || resultFor != ids[1] || text != "Go 1.24 adds generic type aliases." || stop != "end_turn" || requests != 2 {
		t.Fatalf("ids %v result for %q text %q stop %q requests %v", ids, resultFor, text, stop, requests)
	}
}

func TestWebSearchAsJSON(t *testing.T) {
	backend := &fakeSearcher{result: found()}
	rec := post(t, NewHandler(Config{Token: "secret", Backend: backend}), "/v1/messages", strings.Replace(searchBody, "%s", "false", 1))
	var got struct {
		Content []struct {
			Type      string          `json:"type"`
			ID        string          `json:"id"`
			ToolUseID string          `json:"tool_use_id"`
			Input     json.RawMessage `json:"input"`
			Text      string          `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	c := got.Content
	if len(c) != 4 || c[0].Type != "server_tool_use" || string(c[0].Input) != `{"query":"go 1.24"}` || c[2].Type != "web_search_tool_result" || c[2].ToolUseID != c[1].ID || c[3].Text == "" || got.StopReason != "end_turn" {
		t.Fatalf("response %s", rec.Body)
	}
}

func TestWebSearchErrorReachesClaudeCodeAsASearchError(t *testing.T) {
	backend := &fakeSearcher{result: translate.SearchResult{ErrorCode: "unavailable"}}
	rec := post(t, NewHandler(Config{Token: "secret", Backend: backend}), "/v1/messages", strings.Replace(searchBody, "%s", "true", 1))
	if !strings.Contains(rec.Body.String(), `"content":{"error_code":"unavailable","type":"web_search_tool_result_error"}`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestWebSearchFailures(t *testing.T) {
	// Before the first query nothing was sent, so the failure keeps its HTTP status.
	early := &fakeSearcher{err: &translate.RateLimitError{Message: "slow down"}}
	if rec := post(t, NewHandler(Config{Token: "secret", Backend: early}), "/v1/messages", strings.Replace(searchBody, "%s", "true", 1)); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	late := &fakeSearcher{result: found(), failAfterQuery: true}
	rec := post(t, NewHandler(Config{Token: "secret", Backend: late}), "/v1/messages", strings.Replace(searchBody, "%s", "true", 1))
	if events := names(parseSSE(t, rec.Body.String())); !strings.HasSuffix(events, ",error") {
		t.Fatalf("a failure after the stream started must be an error event: %s", events)
	}
	// A backend that cannot search keeps saying so.
	rec = post(t, NewHandler(Config{Token: "secret", Backend: &fakeBackend{}}), "/v1/messages", strings.Replace(searchBody, "%s", "true", 1))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Web search (WebSearch) does not work with NeoSwitch") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
