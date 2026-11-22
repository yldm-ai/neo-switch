package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/translate"
)

type modelAnswer struct{ session, model, level string }

func askModel(t *testing.T, a *AntigravityACP, req translate.Request) modelAnswer {
	t.Helper()
	resp, err := a.Generate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var m modelAnswer
	if _, err := fmt.Sscanf(generatedText(t, resp), "%s %s %s", &m.session, &m.model, &m.level); err != nil {
		t.Fatalf("answer %q", generatedText(t, resp))
	}
	return m
}

func originRequest(text string, origin translate.Origin, tools bool) translate.Request {
	req := textRequest(text)
	req.Origin = origin
	if tools {
		req.Config = &genai.GenerateContentConfig{Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "Read"}}}}}
	}
	return req
}

func TestBackgroundRequestsUseTheBackgroundModel(t *testing.T) {
	a := newFakeACP(t, "model")
	a.background = "gemini-a-flash"
	for _, tc := range []struct {
		name   string
		origin translate.Origin
		tools  bool
		want   string
	}{
		{"main thread", translate.Origin{Session: "s", Class: "main", Kind: "main"}, true, DefaultModel},
		{"prompt suggestion", translate.Origin{Session: "s", Class: "auxiliary", Kind: "followup-generation"}, true, "gemini-a-flash"},
		{"session title", translate.Origin{Session: "s", Class: "auxiliary", Kind: "summary-extraction"}, false, "gemini-a-flash"},
		{"memory extraction", translate.Origin{Session: "s", Class: "auxiliary", Kind: "summary-extraction"}, true, DefaultModel},
		{"compaction", translate.Origin{Session: "s", Class: "compaction"}, false, DefaultModel},
		{"subagent", translate.Origin{Session: "s", Class: "subagent", Agent: "a1"}, false, DefaultModel},
		{"agent in the background", translate.Origin{Session: "s", Class: "auxiliary", Kind: "summary-extraction", Agent: "dream"}, false, DefaultModel},
		{"older Claude Code", translate.Origin{}, false, DefaultModel},
	} {
		if got := askModel(t, a, originRequest(tc.name, tc.origin, tc.tools)); got.model != tc.want {
			t.Errorf("%s: answered by %s, want %s", tc.name, got.model, tc.want)
		}
	}
}

// A session is continued only by a request for the model that answers it. Requests of one owner can ask for different models: a session title and memory extraction are both summary-extraction, without and with tools.
func TestSessionsAreNotContinuedAcrossModels(t *testing.T) {
	a := newFakeACP(t, "model")
	a.background = "gemini-a-flash"
	origin := translate.Origin{Session: "s", Class: "auxiliary", Kind: "summary-extraction"}
	title := originRequest("name this session", origin, false)
	resp, err := a.Generate(context.Background(), title)
	if err != nil {
		t.Fatal(err)
	}
	answer := generatedText(t, resp)
	memory := continueWith(title, answer, "now extract memories")
	memory.Origin = origin
	memory.Config = originRequest("", origin, true).Config
	if got := askModel(t, a, memory); got.model != DefaultModel || strings.HasPrefix(answer, got.session+" ") {
		t.Fatalf("memory extraction continued the flash session: %+v after %q", got, answer)
	}
}

// The thought levels a session offers depend on its model; a level the background model lacks is skipped, not sent.
func TestThoughtLevelFollowsTheSessionsModel(t *testing.T) {
	a := newFakeACP(t, "model")
	a.background = "gemini-a-flash"
	req := originRequest("suggest", translate.Origin{Session: "s", Class: "auxiliary", Kind: "followup-generation"}, true)
	req.ThinkingLevel = genai.ThinkingLevelHigh
	if got := askModel(t, a, req); got.model != "gemini-a-flash" || got.level != "unset" {
		t.Fatalf("got %+v; the flash model offers no high level", got)
	}
	main := originRequest("main", translate.Origin{Session: "s", Class: "main", Kind: "main"}, true)
	main.ThinkingLevel = genai.ThinkingLevelHigh
	if got := askModel(t, a, main); got.level != "high" {
		t.Fatalf("main thread level %+v", got)
	}
}

func TestBackgroundModelTheirPlanLacksUsesTheMainModel(t *testing.T) {
	a := newFakeACP(t, "model")
	a.background = "gemini-not-offered"
	suggestion := originRequest("suggest", translate.Origin{Session: "s", Class: "auxiliary", Kind: "followup-generation"}, true)
	for i := 0; i < 2; i++ {
		if got := askModel(t, a, suggestion); got.model != DefaultModel {
			t.Fatalf("request %d answered by %s", i, got.model)
		}
	}
}

func TestBackgroundModelSetting(t *testing.T) {
	for _, tc := range []struct {
		env        *string
		main, want string
	}{
		{nil, DefaultModel, DefaultBackgroundModel},
		{ptr("main"), DefaultModel, ""},
		{ptr("MAIN"), DefaultModel, ""},
		{ptr("gemini-other"), DefaultModel, "gemini-other"},
		{ptr(DefaultModel), DefaultModel, ""},
		// A main model the user chose is theirs: without the variable, background requests use it too.
		{nil, "gemini-chosen", ""},
		{ptr(DefaultBackgroundModel), "gemini-chosen", DefaultBackgroundModel},
	} {
		if tc.env != nil {
			t.Setenv("NEOSWITCH_BACKGROUND_MODEL", *tc.env)
		} else {
			unsetEnv(t, "NEOSWITCH_BACKGROUND_MODEL")
		}
		if got := backgroundModel(tc.main); got != tc.want {
			t.Errorf("env %v, main %s: %q, want %q", tc.env, tc.main, got, tc.want)
		}
	}
}

func ptr(s string) *string { return &s }

// unsetEnv removes a variable for the rest of the test; t.Setenv restores it afterwards.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	_ = os.Unsetenv(key)
}
