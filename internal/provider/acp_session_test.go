package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	acp "github.com/coder/acp-go-sdk"
	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/anthropic"
	"github.com/yldm-ai/neo-switch/internal/translate"
)

// historyAnswer is the fake's answer in history mode.
type historyAnswer struct {
	session string
	prompts int
	bytes   int
}

func parseHistoryAnswer(t *testing.T, text string) historyAnswer {
	t.Helper()
	var h historyAnswer
	if _, err := fmt.Sscanf(text, "%s %d %d", &h.session, &h.prompts, &h.bytes); err != nil {
		t.Fatalf("unexpected answer %q", text)
	}
	return h
}

// continueWith is the next request of conv as Claude Code sends it: the answer echoed as a model turn, then a new user message.
func continueWith(req translate.Request, answer, next string) translate.Request {
	contents := append(append([]*genai.Content{}, req.Contents...),
		genai.NewContentFromText(answer, genai.RoleModel),
		genai.NewContentFromText(next, genai.RoleUser))
	return translate.Request{Contents: contents, Config: req.Config}
}

func toolsRequest(text string, tools ...string) translate.Request {
	req := textRequest(text)
	var declarations []*genai.FunctionDeclaration
	for _, name := range tools {
		declarations = append(declarations, &genai.FunctionDeclaration{Name: name, Description: strings.Repeat("d", 200)})
	}
	req.Config = &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText("You are Claude Code.", genai.RoleUser),
		Tools:             []*genai.Tool{{FunctionDeclarations: declarations}},
	}
	return req
}

func TestACPContinuesTheSessionOfItsConversation(t *testing.T) {
	a := newFakeACP(t, "history")
	first := toolsRequest(strings.Repeat("long context ", 2000), "Read", "Bash")
	resp, err := a.Generate(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	answer1 := generatedText(t, resp)
	h1 := parseHistoryAnswer(t, answer1)

	second := continueWith(first, answer1, "and now?")
	resp, err = a.Generate(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	answer2 := generatedText(t, resp)
	h2 := parseHistoryAnswer(t, answer2)
	if h2.session != h1.session || h2.prompts != 2 {
		t.Fatalf("second request did not continue the session: %+v after %+v", h2, h1)
	}
	if h2.bytes >= h1.bytes/4 {
		t.Fatalf("a continued session must read only what is new: %d bytes after %d", h2.bytes, h1.bytes)
	}

	// New tools reach the session as an update rather than a new session.
	third := continueWith(second, answer2, "with another tool")
	third.Config = toolsRequest("", "Read", "Bash", "Grep").Config
	resp, err = a.Generate(context.Background(), third)
	if err != nil {
		t.Fatal(err)
	}
	if h3 := parseHistoryAnswer(t, generatedText(t, resp)); h3.session != h1.session || h3.prompts != 3 {
		t.Fatalf("a tool change must not start a new session: %+v", h3)
	}

	// A conversation whose last answer differs from what the session holds, as after an edit or a rewind, gets a new session.
	diverged := continueWith(first, "something else", "and now?")
	resp, err = a.Generate(context.Background(), diverged)
	if err != nil {
		t.Fatal(err)
	}
	if h := parseHistoryAnswer(t, generatedText(t, resp)); h.session == h1.session || h.prompts != 1 {
		t.Fatalf("a different conversation reused the session: %+v", h)
	}
}

func TestACPSplitsAConversationTooLongForOnePrompt(t *testing.T) {
	a := newFakeACP(t, "history")
	// Multibyte text, so that a split in the middle of a character would show.
	req := toolsRequest(strings.Repeat("长对话 line\n", 30_000), "Read")
	resp, err := a.Generate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	answer := generatedText(t, resp)
	h := parseHistoryAnswer(t, answer)
	if h.prompts < 3 {
		t.Fatalf("a %d-byte conversation must arrive in several prompts, got %+v", 30_000*len("长对话 line\n"), h)
	}
	// The split session continues like any other.
	resp, err = a.Generate(context.Background(), continueWith(req, answer, "next"))
	if err != nil {
		t.Fatal(err)
	}
	if next := parseHistoryAnswer(t, generatedText(t, resp)); next.session != h.session || next.prompts != h.prompts+1 {
		t.Fatalf("the split session was not continued: %+v after %+v", next, h)
	}
}

func TestACPSplitsALongNewMessageInAContinuedSession(t *testing.T) {
	a := newFakeACP(t, "history")
	first := textRequest("hi")
	resp, err := a.Generate(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	answer := generatedText(t, resp)
	h1 := parseHistoryAnswer(t, answer)
	// Several large tool results in one message, as parallel reads return them.
	resp, err = a.Generate(context.Background(), continueWith(first, answer, strings.Repeat("result line\n", 40_000)))
	if err != nil {
		t.Fatal(err)
	}
	if h2 := parseHistoryAnswer(t, generatedText(t, resp)); h2.session != h1.session || h2.prompts < 3 {
		t.Fatalf("a long new message must be split within the session: %+v after %+v", h2, h1)
	}
}

func TestACPDoesNotContinueAnAnswerClaudeCodeDidNotGet(t *testing.T) {
	a := newFakeACP(t, "history")
	req := textRequest("hi")
	var text strings.Builder
	for chunk, err := range a.Stream(context.Background(), req) {
		if err != nil {
			t.Fatal(err)
		}
		if chunk.Candidates[0].FinishReason != "" {
			// Claude Code went away before it took the final chunk.
			break
		}
		for _, part := range chunk.Candidates[0].Content.Parts {
			text.WriteString(part.Text)
		}
	}
	h1 := parseHistoryAnswer(t, text.String())
	resp, err := a.Generate(context.Background(), continueWith(req, text.String(), "next"))
	if err != nil {
		t.Fatal(err)
	}
	if h2 := parseHistoryAnswer(t, generatedText(t, resp)); h2.session == h1.session {
		t.Fatalf("a session whose answer was not delivered was continued: %+v", h2)
	}
}

func TestACPStreamedAnswerContinuesItsSession(t *testing.T) {
	a := newFakeACP(t, "history")
	req := textRequest("hi")
	var text strings.Builder
	for _, chunk := range collectStream(t, a, req) {
		for _, part := range chunk.Candidates[0].Content.Parts {
			text.WriteString(part.Text)
		}
	}
	h1 := parseHistoryAnswer(t, text.String())
	resp, err := a.Generate(context.Background(), continueWith(req, text.String(), "next"))
	if err != nil {
		t.Fatal(err)
	}
	if h2 := parseHistoryAnswer(t, generatedText(t, resp)); h2.session != h1.session {
		t.Fatalf("a delivered streamed answer did not continue its session: %+v after %+v", h2, h1)
	}
}

// A tool call NeoSwitch returns reaches Claude Code as a tool_use block, and comes back translated from Claude Code's copy. Both must identify the same conversation, or no session would ever be continued after a tool call.
func TestHistoryKeySurvivesTheRoundTripThroughClaudeCode(t *testing.T) {
	parts, exact, err := parseAnswer(`{"text":"Reading it.","tool_use":{"name":"Read","input":{"file_path":"/tmp/a","limit":200,"opts":{"b":1,"a":[1,2]}}}}`)
	if err != nil || !exact {
		t.Fatalf("parse: %v exact=%v", err, exact)
	}
	blocks, err := translate.Blocks(parts)
	if err != nil {
		t.Fatal(err)
	}
	var toolID string
	for _, block := range blocks {
		if block.Type == "tool_use" {
			toolID = block.ID
		}
	}
	assistant, _ := json.Marshal(blocks)
	user, _ := json.Marshal([]map[string]any{{"type": "tool_result", "tool_use_id": toolID, "content": "file body"}})
	echoed, err := translate.ToGemini(anthropic.MessagesRequest{Messages: []anthropic.Message{
		{Role: "user", Content: json.RawMessage(`"read /tmp/a"`)},
		{Role: "assistant", Content: assistant},
		{Role: "user", Content: user},
	}})
	if err != nil {
		t.Fatal(err)
	}
	original := []*genai.Content{genai.NewContentFromText("read /tmp/a", genai.RoleUser), {Role: genai.RoleModel, Parts: parts}}
	if historyKey(echoed.Contents[:2]) != historyKey(original) {
		t.Fatal("the echoed conversation does not match the one the session holds")
	}
	other, _, _ := parseAnswer(`{"text":"Reading it.","tool_use":{"name":"Read","input":{"file_path":"/tmp/a","limit":200,"opts":{"b":1,"a":[1,2]}}}}`)
	if historyKey([]*genai.Content{original[0], {Role: genai.RoleModel, Parts: other}}) == historyKey(original) {
		t.Fatal("two separate tool calls must not identify the same conversation")
	}
}

func TestHistoryKeySurvivesParallelToolCallsRoundTripThroughClaudeCode(t *testing.T) {
	parts, exact, err := parseAnswer(`{"text":"Reading both.","tool_uses":[{"name":"Read","input":{"file_path":"/tmp/a"}},{"name":"Read","input":{"file_path":"/tmp/b"}}]}`)
	if err != nil || !exact {
		t.Fatalf("parse: %v exact=%v", err, exact)
	}
	blocks, err := translate.Blocks(parts)
	if err != nil {
		t.Fatal(err)
	}
	var toolIDs []string
	for _, block := range blocks {
		if block.Type == "tool_use" {
			toolIDs = append(toolIDs, block.ID)
		}
	}
	if len(toolIDs) != 2 || toolIDs[0] == toolIDs[1] {
		t.Fatalf("expected 2 distinct tool_use IDs, got %v", toolIDs)
	}
	assistant, _ := json.Marshal(blocks)
	// Return results out of order to verify translate.ToGemini reorders them cleanly.
	user, _ := json.Marshal([]map[string]any{
		{"type": "tool_result", "tool_use_id": toolIDs[1], "content": "body b"},
		{"type": "tool_result", "tool_use_id": toolIDs[0], "content": "body a"},
	})
	echoed, err := translate.ToGemini(anthropic.MessagesRequest{Messages: []anthropic.Message{
		{Role: "user", Content: json.RawMessage(`"read both"`)},
		{Role: "assistant", Content: assistant},
		{Role: "user", Content: user},
	}})
	if err != nil {
		t.Fatal(err)
	}
	original := []*genai.Content{genai.NewContentFromText("read both", genai.RoleUser), {Role: genai.RoleModel, Parts: parts}}
	if historyKey(echoed.Contents[:2]) != historyKey(original) {
		t.Fatal("parallel tool_uses conversation did not match the session key after round-trip")
	}
}

func TestACPPromptsStayWithinWhatAgyReads(t *testing.T) {
	pieces := []acpPiece{
		{text: "System instructions:\n" + strings.Repeat("s", 40_000)},
		{text: "USER:\n" + strings.Repeat("ü line\n", 60_000)},
		{text: "MODEL:\nshort\n\n"},
		{text: "USER:\n" + strings.Repeat("x", 200_000), images: []acp.ContentBlock{acp.ImageBlock("aGk=", "image/png")}},
	}
	prompts := acpPrompts(pieces, acpAnswerInstructions, "")
	if len(prompts) < 3 {
		t.Fatalf("expected several prompts, got %d", len(prompts))
	}
	var body strings.Builder
	images := 0
	for i, prompt := range prompts {
		if len(prompt.text) > acpPromptBudget {
			t.Fatalf("prompt %d is %d bytes", i, len(prompt.text))
		}
		if !utf8.ValidString(prompt.text) {
			t.Fatalf("prompt %d splits a character", i)
		}
		last := i == len(prompts)-1
		if strings.HasPrefix(prompt.text, acpAnswerInstructions) != last {
			t.Fatalf("prompt %d: only the last prompt asks for the answer", i)
		}
		if !last && !strings.Contains(prompt.text, `reply with exactly {"text":"ok"}`) {
			t.Fatalf("prompt %d does not ask to wait", i)
		}
		_, rest, _ := strings.Cut(prompt.text, "\n\n")
		if last {
			rest = strings.TrimPrefix(prompt.text, acpAnswerInstructions)
			_, rest, _ = strings.Cut(rest, "\n\n")
		}
		body.WriteString(rest)
		images += len(prompt.images)
	}
	var want strings.Builder
	for _, piece := range pieces {
		want.WriteString(piece.text)
	}
	if body.String() != want.String() {
		t.Fatal("the prompts do not carry the conversation exactly once, in order")
	}
	if images != 1 || len(prompts[len(prompts)-1].images) != 1 {
		t.Fatal("the image must travel with the end of its message")
	}
	if one := acpPrompts(pieces[2:3], acpAnswerInstructions, ""); len(one) != 1 || one[0].text != acpAnswerInstructions+pieces[2].text {
		t.Fatalf("a short conversation must be one prompt: %#v", one)
	}
	if pinned := acpPrompts(pieces[2:3], "", ""); len(pinned) != 1 || pinned[0].text != pieces[2].text {
		t.Fatalf("pinned system prompt must omit inline instructions: %#v", pinned)
	}
}

func TestParseAnswerWithSeveralObjects(t *testing.T) {
	for _, tc := range []struct {
		name, raw, text, tool string
	}{
		{"same tool call twice", `{"tool_use":{"name":"Read","input":{"file_path":"a","limit":2}}}` + "\n\n" + `{"tool_use":{"name":"Read","input":{"limit":2,"file_path":"a"}}}`, "", "Read"},
		{"a different second call is not run", `{"tool_use":{"name":"Read","input":{}}}{"tool_use":{"name":"Bash","input":{"command":"rm -rf /"}}}`, "", "Read"},
		{"text and tool call in two objects", `{"text":"Reading."}` + "\n" + `{"tool_use":{"name":"Read","input":{}}}`, "Reading.", "Read"},
		{"prose after the object", `{"text":"done"} I hope this helps.`, "done", ""},
		{"same text twice", `{"text":"hi"}{"text":"hi"}`, "hi", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parts, exact, err := parseAnswer(tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			if exact {
				t.Fatal("an answer of more than one object is not exact")
			}
			var text, tool string
			calls := 0
			for _, part := range parts {
				text += part.Text
				if part.FunctionCall != nil {
					tool = part.FunctionCall.Name
					calls++
				}
			}
			if text != tc.text || tool != tc.tool || calls > 1 {
				t.Fatalf("got text %q, tool %q (%d calls)", text, tool, calls)
			}
		})
	}
	if _, exact, _ := parseAnswer(`{"text":"hi"}`); !exact {
		t.Fatal("one object is exact")
	}
	if parts, exact, _ := parseAnswer("Just text."); exact || parts[0].Text != "Just text." {
		t.Fatal("plain text passes through and is not exact")
	}
}

func TestACPRefusalsReachClaudeCodeWithTheirReason(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want func(error) (string, bool)
		has  []string
	}{
		{"quota", func(err error) (string, bool) {
			var e *translate.UnavailableError
			if !errors.As(err, &e) {
				return "", false
			}
			return e.Message, true
		}, []string{"used up its Antigravity quota for " + DefaultModel, "Your quota resets in 4h.", "/neoswitch:off"}},
		{"rate-limit", func(err error) (string, bool) {
			var e *translate.RateLimitError
			if !errors.As(err, &e) {
				return "", false
			}
			return e.Message, true
		}, []string{"limiting how many requests", "RESOURCE_EXHAUSTED"}},
		{"too-long", func(err error) (string, bool) {
			var e *translate.UnavailableError
			if !errors.As(err, &e) {
				return "", false
			}
			return e.Message, true
		}, []string{"prompt is too long", "/compact"}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			a := newFakeACP(t, tc.mode)
			_, err := a.Generate(context.Background(), textRequest("hi"))
			message, ok := tc.want(err)
			if !ok {
				t.Fatalf("unexpected error %T %v", err, err)
			}
			for _, want := range tc.has {
				if !strings.Contains(message, want) {
					t.Fatalf("message %q lacks %q", message, want)
				}
			}
			if strings.ContainsAny(message, "{}") || strings.Contains(message, "-32") {
				t.Fatalf("message %q leaks protocol details", message)
			}
		})
	}
}

func from(req translate.Request, class, kind string) translate.Request {
	req.Origin = translate.Origin{Session: "cc1", Class: class, Kind: kind}
	return req
}

func historyOf(t *testing.T, a *AntigravityACP, req translate.Request) (historyAnswer, string) {
	t.Helper()
	resp, err := a.Generate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	text := generatedText(t, resp)
	return parseHistoryAnswer(t, text), text
}

// Claude Code's background requests at the end of a turn carry the main thread's history. They must not take the main thread's session, or the next message would read the whole conversation again.
func TestACPBackgroundRequestsLeaveTheMainSessionAlone(t *testing.T) {
	a := newFakeACP(t, "history")
	r1 := from(toolsRequest(strings.Repeat("context ", 3000), "Read"), "main", "main")
	h1, a1 := historyOf(t, a, r1)

	suggestion := from(continueWith(r1, a1, "[suggest the next prompt]"), "auxiliary", "followup-generation")
	hs1, as1 := historyOf(t, a, suggestion)
	if hs1.session == h1.session {
		t.Fatal("a background request took the main thread's session")
	}

	r2 := from(continueWith(r1, a1, "next message"), "main", "main")
	h2, a2 := historyOf(t, a, r2)
	if h2.session != h1.session || h2.prompts != 2 {
		t.Fatalf("the main thread lost its session: %+v after %+v", h2, h1)
	}

	// A turn later, the background session reads only what the main thread did since.
	_ = as1
	suggestion2 := from(continueWith(r2, a2, "[suggest the next prompt]"), "auxiliary", "followup-generation")
	hs2, _ := historyOf(t, a, suggestion2)
	if hs2.session != hs1.session || hs2.prompts != 2 {
		t.Fatalf("the background session was not continued from the main history: %+v after %+v", hs2, hs1)
	}
	if hs2.bytes >= hs1.bytes/4 {
		t.Fatalf("the background session read %d bytes after %d; it holds most of the history", hs2.bytes, hs1.bytes)
	}

	// Another kind of background request keeps a session of its own.
	memory := from(continueWith(r2, a2, "[extract memories]"), "auxiliary", "summary-extraction")
	if hm, _ := historyOf(t, a, memory); hm.session == hs1.session || hm.session == h1.session {
		t.Fatalf("a different kind of background request took another sender's session: %+v", hm)
	}
}

func TestACPCompactionReadsTheMainSessionAndEndsIt(t *testing.T) {
	a := newFakeACP(t, "history")
	r1 := from(toolsRequest(strings.Repeat("context ", 3000), "Read"), "main", "main")
	h1, a1 := historyOf(t, a, r1)
	compaction := from(continueWith(r1, a1, "Summarize the conversation."), "compaction", "")
	if hc, _ := historyOf(t, a, compaction); hc.session != h1.session || hc.prompts != 2 {
		t.Fatalf("compaction did not continue the main session: %+v after %+v", hc, h1)
	}
	a.conn.mu.Lock()
	defer a.conn.mu.Unlock()
	for _, idle := range a.conn.idle {
		if string(idle.id) == h1.session {
			t.Fatal("the session a compaction used must be closed")
		}
	}
}

func TestACPSubagentKeepsItsOwnSession(t *testing.T) {
	a := newFakeACP(t, "history")
	task := textRequest("explore the repo")
	task.Origin = translate.Origin{Session: "cc1", Class: "subagent", Agent: "agent-7"}
	h1, a1 := historyOf(t, a, task)
	// Another agent with the same history, as parallel agents given the same task have, starts its own session.
	other := continueWith(task, a1, "tool result")
	other.Origin = translate.Origin{Session: "cc1", Class: "subagent", Agent: "agent-8"}
	if h3, _ := historyOf(t, a, other); h3.session == h1.session {
		t.Fatal("one agent took another agent's session")
	}
	next := continueWith(task, a1, "tool result")
	next.Origin = task.Origin
	if h2, _ := historyOf(t, a, next); h2.session != h1.session {
		t.Fatalf("the subagent did not continue its session: %+v after %+v", h2, h1)
	}
}

func TestEvictionClosesMainThreadSessionsLast(t *testing.T) {
	now := time.Now()
	idle := []*acpSession{
		{id: "main-old", main: true, idleSince: now.Add(-time.Hour)},
		{id: "bg-new", idleSince: now},
		{id: "bg-old", idleSince: now.Add(-time.Minute)},
	}
	if got := idle[evictionChoice(idle)].id; got != "bg-old" {
		t.Fatalf("evicted %s", got)
	}
	if got := idle[evictionChoice(idle[:1])].id; got != "main-old" {
		t.Fatalf("evicted %s", got)
	}
}
