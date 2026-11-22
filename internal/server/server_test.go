package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/translate"
)

type fakeBackend struct {
	response *genai.GenerateContentResponse
	chunks   []*genai.GenerateContentResponse
	err      error // returned by Generate/CountTokens, or yielded after chunks by Stream
	count    int
	got      translate.Request
}

func (f *fakeBackend) Generate(_ context.Context, req translate.Request) (*genai.GenerateContentResponse, error) {
	f.got = req
	return f.response, f.err
}

func (f *fakeBackend) Stream(_ context.Context, req translate.Request) iter.Seq2[*genai.GenerateContentResponse, error] {
	f.got = req
	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		for _, c := range f.chunks {
			if !yield(c, nil) {
				return
			}
		}
		if f.err != nil {
			yield(nil, f.err)
		}
	}
}

func (f *fakeBackend) CountTokens(_ context.Context, req translate.Request) (int, error) {
	f.got = req
	return f.count, f.err
}

const textRequest = `{"model":"claude-opus","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
const streamRequest = `{"model":"claude-opus","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`

func post(t *testing.T, handler http.Handler, path, body string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Del("Authorization")
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

type sseEvent struct {
	name string
	data map[string]any
}

func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var events []sseEvent
	scanner := bufio.NewScanner(strings.NewReader(body))
	var name string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			var data map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &data); err != nil {
				t.Fatalf("bad SSE data %q: %v", line, err)
			}
			events = append(events, sseEvent{name, data})
		}
	}
	return events
}

func names(events []sseEvent) string {
	var out []string
	for _, e := range events {
		out = append(out, e.name)
	}
	return strings.Join(out, ",")
}

func TestAuthAcceptsBearerAndAPIKey(t *testing.T) {
	backend := &fakeBackend{response: &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: genai.NewContentFromText("ok", genai.RoleModel)}}}}
	handler := NewHandler(Config{Token: "secret", Backend: backend})
	// Not 401, which Claude Code would blame on the user's Claude login.
	if rec := post(t, handler, "/v1/messages", textRequest, "Authorization", "Bearer wrong"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "/neoswitch:on") {
		t.Fatalf("wrong token: %d %s", rec.Code, rec.Body)
	}
	if rec := post(t, handler, "/v1/messages", textRequest); rec.Code != http.StatusOK {
		t.Fatalf("bearer: %d %s", rec.Code, rec.Body)
	}
	if rec := post(t, handler, "/v1/messages", textRequest, "X-Api-Key", "secret"); rec.Code != http.StatusOK {
		t.Fatalf("x-api-key: %d %s", rec.Code, rec.Body)
	}
}

func TestMessagesJSON(t *testing.T) {
	backend := &fakeBackend{response: &genai.GenerateContentResponse{
		Candidates:    []*genai.Candidate{{Content: genai.NewContentFromText("hello", genai.RoleModel), FinishReason: genai.FinishReasonStop}},
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 3, CandidatesTokenCount: 4},
	}}
	rec := post(t, NewHandler(Config{Token: "secret", Backend: backend}), "/v1/messages?beta=true", textRequest)
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got["model"] != "claude-opus" || got["stop_reason"] != "end_turn" || got["content"].([]any)[0].(map[string]any)["text"] != "hello" {
		t.Fatalf("response = %v", got)
	}
	if rec := post(t, NewHandler(Config{Token: "secret", Backend: backend}), "/v1/messages", `{"model":"m","messages":[{"role":"user","content":"hi"}]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing max_tokens: %d", rec.Code)
	}
}

// The Antigravity route keeps a session per sender, so the request carries who in Claude Code sent it.
func TestRequestCarriesClaudeCodeHintHeaders(t *testing.T) {
	backend := &fakeBackend{response: &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{Content: genai.NewContentFromText("hello", genai.RoleModel), FinishReason: genai.FinishReasonStop}},
	}}
	rec := post(t, NewHandler(Config{Token: "secret", Backend: backend}), "/v1/messages", textRequest,
		"X-Claude-Code-Session-Id", "s1",
		"X-Claude-Code-Request-Class", "auxiliary",
		"X-Claude-1p-Request-Class", "followup-generation",
		"X-Claude-Code-Agent-Id", "a1",
		"Authorization", "Bearer secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if want := (translate.Origin{Session: "s1", Class: "auxiliary", Kind: "followup-generation", Agent: "a1"}); backend.got.Origin != want {
		t.Fatalf("origin %+v, want %+v", backend.got.Origin, want)
	}
}

func TestStreamMergesTextAndEmitsWholeToolCalls(t *testing.T) {
	backend := &fakeBackend{chunks: []*genai.GenerateContentResponse{
		{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "thinking...", Thought: true}, {Text: "Hel"}}}}},
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 9}},
		{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "lo"}}}}}},
		{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{Name: "Read", Args: map[string]any{"file_path": "/a"}}, ThoughtSignature: []byte("sig")},
		}}, FinishReason: genai.FinishReasonStop}},
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 9, CandidatesTokenCount: 6}},
	}}
	rec := post(t, NewHandler(Config{Token: "secret", Backend: backend}), "/v1/messages", streamRequest)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	events := parseSSE(t, rec.Body.String())
	want := "message_start,ping,content_block_start,content_block_delta,content_block_delta,content_block_stop,content_block_start,content_block_delta,content_block_stop,message_delta,message_stop"
	if names(events) != want {
		t.Fatalf("events = %s", names(events))
	}
	if events[3].data["delta"].(map[string]any)["text"] != "Hel" || events[4].data["delta"].(map[string]any)["text"] != "lo" {
		t.Fatalf("text deltas = %v %v", events[3].data, events[4].data)
	}
	toolStart := events[6].data["content_block"].(map[string]any)
	if toolStart["name"] != "Read" || events[6].data["index"] != float64(1) || !strings.HasPrefix(toolStart["id"].(string), "toolu_ns_") {
		t.Fatalf("tool start = %v", events[6].data)
	}
	if events[7].data["delta"].(map[string]any)["partial_json"] != `{"file_path":"/a"}` {
		t.Fatalf("tool args = %v", events[7].data)
	}
	delta := events[9].data
	if delta["delta"].(map[string]any)["stop_reason"] != "tool_use" || delta["usage"].(map[string]any)["output_tokens"] != float64(6) {
		t.Fatalf("message_delta = %v", delta)
	}
	if events[0].data["message"].(map[string]any)["usage"].(map[string]any)["input_tokens"] != float64(9) {
		t.Fatalf("message_start usage = %v", events[0].data)
	}
}

func TestStreamErrorBeforeFirstChunkKeepsHTTPStatus(t *testing.T) {
	backend := &fakeBackend{err: genai.APIError{Code: 429, Message: "quota", Status: "RESOURCE_EXHAUSTED"}}
	rec := post(t, NewHandler(Config{Token: "secret", Backend: backend}), "/v1/messages", streamRequest)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "rate_limit_error") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestStreamFailuresAfterStartBecomeErrorEvents(t *testing.T) {
	first := &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: genai.NewContentFromText("partial", genai.RoleModel)}}}
	for name, backend := range map[string]*fakeBackend{
		"transport": {chunks: []*genai.GenerateContentResponse{first}, err: errors.New("connection reset")},
		"finish":    {chunks: []*genai.GenerateContentResponse{first, {Candidates: []*genai.Candidate{{FinishReason: genai.FinishReasonMalformedFunctionCall}}}}},
	} {
		t.Run(name, func(t *testing.T) {
			rec := post(t, NewHandler(Config{Token: "secret", Backend: backend}), "/v1/messages", streamRequest)
			events := parseSSE(t, rec.Body.String())
			last := events[len(events)-1]
			if last.name != "error" || strings.Contains(names(events), "message_stop") {
				t.Fatalf("events = %s", names(events))
			}
			if last.data["error"].(map[string]any)["type"] != "api_error" {
				t.Fatalf("error event = %v", last.data)
			}
		})
	}
}

func TestUpstreamStatusMapping(t *testing.T) {
	for _, tc := range []struct {
		err     error
		status  int
		typ     string
		message string
	}{
		{genai.APIError{Code: 400, Message: "The input token count (2000000) exceeds the maximum number of tokens allowed (1048576)."}, 400, "invalid_request_error", "prompt is too long"},
		{genai.APIError{Code: 400, Message: "bad schema"}, 400, "invalid_request_error", "bad schema"},
		// Claude Code reads 401 and 403 as a broken Claude login and tells the user to run /login.
		{genai.APIError{Code: 403, Message: "no access"}, 400, "invalid_request_error", "no access"},
		{genai.APIError{Code: 401, Message: "bad key"}, 400, "invalid_request_error", "refused the credentials NeoSwitch uses"},
		{genai.APIError{Code: 404, Message: "model not found"}, 404, "not_found_error", "model not found"},
		{genai.APIError{Code: 503, Message: "overloaded"}, 529, "overloaded_error", "overloaded"},
		{genai.APIError{Code: 500, Message: "internal"}, 500, "api_error", "internal"},
		{genai.APIError{Code: 400, Message: "API key not valid. Please pass a valid API key.", Status: "INVALID_ARGUMENT"}, 400, "invalid_request_error", "Google rejected the Gemini API key"},
		{genai.APIError{Code: 400, Message: "bad key", Details: []map[string]any{{"reason": "API_KEY_INVALID"}}}, 400, "invalid_request_error", "Google rejected the Gemini API key"},
		{context.DeadlineExceeded, 504, "api_error", "did not answer within 10 minutes"},
		{errors.New("dial tcp: refused"), 502, "api_error", "refused"},
		{fmt.Errorf("start: %w", &translate.UnavailableError{Message: "Run /neoswitch:login to sign in again."}), 400, "invalid_request_error", "Run /neoswitch:login to sign in again."},
		{fmt.Errorf("prompt: %w", &translate.RetryableError{Message: "agy restarted"}), 529, "overloaded_error", "agy restarted"},
		{fmt.Errorf("prompt: %w", &translate.RateLimitError{Message: "slow down"}), 429, "rate_limit_error", "slow down"},
	} {
		status, typ, message := upstreamStatus(tc.err)
		if status != tc.status || typ != tc.typ || !strings.Contains(message, tc.message) {
			t.Errorf("%v -> %d %s %q", tc.err, status, typ, message)
		}
		if strings.Contains(message, "Gemini request failed") {
			t.Errorf("%v: message %q names Gemini for a failure on any backend", tc.err, message)
		}
	}
}

func TestUnavailableUpstreamIsReportedVerbatim(t *testing.T) {
	const message = "Antigravity is not responding. Run /neoswitch:login to sign in again."
	backend := &fakeBackend{err: &translate.UnavailableError{Message: message}}
	for _, body := range []string{textRequest, streamRequest} {
		rec := post(t, NewHandler(Config{Token: "secret", Backend: backend}), "/v1/messages", body)
		var got struct {
			Error struct{ Type, Message string } `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("%s: %v", rec.Body, err)
		}
		if rec.Code != http.StatusBadRequest || got.Error.Type != "invalid_request_error" || got.Error.Message != message {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	}
}

func TestUnsupportedFeatureHasNoFieldPath(t *testing.T) {
	body := `{"model":"m","max_tokens":10,"tools":[{"type":"web_search_20250305","name":"web_search"}],"messages":[{"role":"user","content":"hi"}]}`
	rec := post(t, NewHandler(Config{Token: "secret", Backend: &fakeBackend{}}), "/v1/messages", body)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Web search (WebSearch) does not work with NeoSwitch") || strings.Contains(rec.Body.String(), "tools[0]") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestCountTokens(t *testing.T) {
	backend := &fakeBackend{count: 42}
	// count_tokens requests carry no max_tokens.
	rec := post(t, NewHandler(Config{Token: "secret", Backend: backend}), "/v1/messages/count_tokens", `{"model":"m","system":"s","messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"input_tokens":42}` {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if backend.got.Config.SystemInstruction == nil {
		t.Fatal("system prompt must be counted")
	}
}

func TestMethodsAreEnforced(t *testing.T) {
	handler := NewHandler(Config{Backend: &fakeBackend{}})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/messages", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /v1/messages = %d", rec.Code)
	}
}

// No answer of NeoSwitch's own may be a 401 or 403: Claude Code shows both as a broken Claude login.
func TestNoOwnFailureLooksLikeAClaudeLoginProblem(t *testing.T) {
	for _, err := range []error{
		&translate.UnavailableError{Message: "x"},
		&translate.RetryableError{Message: "x"},
		&translate.UpstreamError{Reason: "x"},
		genai.APIError{Code: 401, Message: "x"},
		genai.APIError{Code: 403, Message: "x"},
		errors.New("x"),
	} {
		if status, _, _ := upstreamStatus(err); status == http.StatusUnauthorized || status == http.StatusForbidden {
			t.Errorf("%T -> %d", err, status)
		}
	}
	rec := post(t, NewHandler(Config{Token: "secret"}), "/v1/messages", textRequest)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no backend: %d", rec.Code)
	}
}
