package daemon

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// checkBody is the shape of auto mode's safety check in Claude Code 2.1.330: stage one, with the transcript wrapped in text blocks of its own.
func checkBody(t *testing.T, mutate func(map[string]any)) string {
	t.Helper()
	body := map[string]any{
		"model":          "claude-sonnet-5",
		"max_tokens":     64,
		"stop_sequences": []string{"</block>"},
		"thinking":       map[string]any{"type": "disabled"},
		"system":         []map[string]any{{"type": "text", "text": "You judge actions."}},
		"messages": []map[string]any{{"role": "user", "content": []map[string]any{
			{"type": "text", "text": "<transcript>\n"},
			{"type": "text", "text": "User: clean up\nBash: rm -rf build\n"},
			{"type": "text", "text": "</transcript>\n"},
			{"type": "text", "text": "Answer with <block>yes</block> or <block>no</block>."},
		}}},
	}
	if mutate != nil {
		mutate(body)
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

type seen struct {
	auth, body string
}

func recordingHandler(list *[]seen, status int, reply string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*list = append(*list, seen{auth: r.Header.Get("Authorization"), body: string(body)})
		w.Header().Set("X-From", reply)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	})
}

func routeCheck(t *testing.T, rt *router, s State, class, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Host = s.Listen
	req.Header.Set("Authorization", "Bearer users-claude-login")
	req.Header.Set(TokenHeader, s.Token)
	req.Header.Set(sessionHeader, "on")
	if class != "" {
		req.Header.Set(requestClassHeader, class)
	}
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, req)
	return rec
}

func checkRouter(t *testing.T, passthrough http.Handler, gemini *[]seen) (*router, State) {
	t.Helper()
	s := State{Listen: "127.0.0.1:57002", Token: "token"}
	sessions := newSessionSet(t.TempDir(), func(string) {})
	sessions.add(attachRequest{PID: 100, Project: "/p", Session: "on", OptIn: true})
	return &router{state: s, sessions: sessions, gemini: recordingHandler(gemini, http.StatusOK, "gemini"), passthrough: passthrough}, s
}

func TestAutoModeCheckGoesToClaude(t *testing.T) {
	var gemini, claude []seen
	rt, s := checkRouter(t, recordingHandler(&claude, http.StatusOK, "claude"), &gemini)
	body := checkBody(t, nil)
	rec := routeCheck(t, rt, s, "auxiliary", body)
	if rec.Body.String() != "claude" || rec.Header().Get("X-From") != "claude" {
		t.Fatalf("answered by %q", rec.Body.String())
	}
	if len(gemini) != 0 || len(claude) != 1 {
		t.Fatalf("gemini %d, claude %d", len(gemini), len(claude))
	}
	if claude[0].auth != "Bearer users-claude-login" || claude[0].body != body {
		t.Fatalf("Claude saw %+v", claude[0])
	}
}

func TestAutoModeCheckFallsBackToGemini(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, 529, http.StatusInternalServerError, http.StatusUnauthorized, http.StatusBadRequest} {
		var gemini, claude []seen
		rt, s := checkRouter(t, recordingHandler(&claude, status, "claude refused"), &gemini)
		body := checkBody(t, nil)
		rec := routeCheck(t, rt, s, "auxiliary", body)
		if rec.Code != http.StatusOK || rec.Body.String() != "gemini" || rec.Header().Get("X-From") != "gemini" {
			t.Fatalf("status %d: answered %d %q, header %q", status, rec.Code, rec.Body.String(), rec.Header().Get("X-From"))
		}
		if len(claude) != 1 || len(gemini) != 1 || gemini[0].body != body || gemini[0].auth != "" {
			t.Fatalf("status %d: claude %d, gemini %+v", status, len(claude), gemini)
		}
	}
}

func TestOtherRequestsStayWithGemini(t *testing.T) {
	cases := map[string]struct{ class, body string }{
		"main thread": {"main", checkBody(t, nil)},
		"no class":    {"", checkBody(t, nil)},
		"with tools": {"auxiliary", checkBody(t, func(b map[string]any) {
			b["tools"] = []map[string]any{{"name": "Bash", "input_schema": map[string]any{"type": "object"}}}
		})},
		"transcript inside other text": {"auxiliary", checkBody(t, func(b map[string]any) {
			b["messages"] = []map[string]any{{"role": "user", "content": []map[string]any{{"type": "text", "text": "<transcript>\nhi\n</transcript>\n"}}}}
		})},
		"no closing block": {"auxiliary", checkBody(t, func(b map[string]any) {
			b["messages"] = []map[string]any{{"role": "user", "content": []map[string]any{{"type": "text", "text": "<transcript>\n"}, {"type": "text", "text": "hi"}}}}
		})},
		"closing before opening": {"auxiliary", checkBody(t, func(b map[string]any) {
			b["messages"] = []map[string]any{{"role": "user", "content": []map[string]any{{"type": "text", "text": "</transcript>\n"}, {"type": "text", "text": "<transcript>\n"}}}}
		})},
		"string content": {"auxiliary", checkBody(t, func(b map[string]any) {
			b["messages"] = []map[string]any{{"role": "user", "content": "<transcript>\n"}}
		})},
		"not json": {"auxiliary", "<transcript>\n</transcript>\n"},
	}
	for name, c := range cases {
		var gemini, claude []seen
		rt, s := checkRouter(t, recordingHandler(&claude, http.StatusOK, "claude"), &gemini)
		rec := routeCheck(t, rt, s, c.class, c.body)
		if rec.Body.String() != "gemini" || len(claude) != 0 {
			t.Fatalf("%s: answered by %q", name, rec.Body.String())
		}
		if len(gemini) != 1 || gemini[0].body != c.body || gemini[0].auth != "" {
			t.Fatalf("%s: gemini saw %+v", name, gemini)
		}
	}
}

func TestAutoModeCheckThroughThePassthrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer users-claude-login" || r.Header.Get(TokenHeader) != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []string{"event: a\n\n", "event: b\n\n"} {
			_, _ = io.WriteString(w, event)
			w.(http.Flusher).Flush()
		}
	}))
	defer upstream.Close()
	passthrough, err := NewPassthrough(State{Upstream: upstream.URL})
	if err != nil {
		t.Fatal(err)
	}
	var gemini []seen
	rt, s := checkRouter(t, passthrough, &gemini)
	rec := routeCheck(t, rt, s, "auxiliary", checkBody(t, nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "event: a\n\nevent: b\n\n" || rec.Header().Get("Content-Type") != "text/event-stream" || len(gemini) != 0 {
		t.Fatalf("answered %d %q %v, gemini %d", rec.Code, rec.Body.String(), rec.Header(), len(gemini))
	}
	if !rec.Flushed {
		t.Fatal("the reply was not streamed")
	}

	// Anthropic out of reach: Gemini answers.
	upstream.Close()
	rec = routeCheck(t, rt, s, "auxiliary", checkBody(t, nil))
	if rec.Body.String() != "gemini" || len(gemini) != 1 {
		t.Fatalf("unreachable Anthropic: answered %q", rec.Body.String())
	}
}
