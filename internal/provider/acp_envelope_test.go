package provider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/anthropic"
	"github.com/yldm-ai/neo-switch/internal/translate"
)

// Models write regular expressions and shell commands into JSON strings with single backslashes (issue #83). They must reach Claude Code as the tool call the model meant, not as raw JSON text that ends the turn.
func TestParseAnswerRepairsBackslashesInToolCalls(t *testing.T) {
	cases := []struct{ name, answer, command string }{
		{"regex dot", `{"tool_use":{"name":"Bash","input":{"command":"rg 'hooks\.json'"}}}`, `rg 'hooks\.json'`},
		{"grep alternation", `{"tool_use":{"name":"Bash","input":{"command":"grep -rn 'printStatus\|status\(' cmd"}}}`, `grep -rn 'printStatus\|status\(' cmd`},
		{"digits and dollar", `{"tool_use":{"name":"Bash","input":{"command":"rg '\d+\s\$'"}}}`, `rg '\d+\s\$'`},
		{"windows path", `{"tool_use":{"name":"Bash","input":{"command":"dir C:\users\x"}}}`, `dir C:\users\x`},
		{"valid escapes kept", `{"tool_use":{"name":"Bash","input":{"command":"printf \"a\\tb\u00e9\"\n"}}}`, "printf \"a\\tb\u00e9\"\n"},
		{"word boundary", `{"tool_use":{"name":"Bash","input":{"command":"rg 'foo\b' -e '\f'"}}}`, `rg 'foo\b' -e '\f'`},
		{"word boundary with bad escape", `{"tool_use":{"name":"Bash","input":{"command":"rg '\bfoo\.go'"}}}`, `rg '\bfoo\.go'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parts, exact, err := parseAnswer(tc.answer)
			if err != nil {
				t.Fatal(err)
			}
			if !exact || len(parts) != 1 || parts[0].FunctionCall == nil {
				t.Fatalf("exact=%v parts=%#v", exact, parts)
			}
			if got := parts[0].FunctionCall.Args["command"]; got != tc.command {
				t.Fatalf("command %q, want %q", got, tc.command)
			}
		})
	}
}

func TestParseAnswerRestoresBackslashLettersInNestedInput(t *testing.T) {
	parts, _, err := parseAnswer(`{"tool_use":{"name":"MultiEdit","input":{"edits":[{"old_string":"a\bb","n":1}],"content":"page\fbreak"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	args := parts[0].FunctionCall.Args
	if args["edits"].([]any)[0].(map[string]any)["old_string"] != `a\bb` {
		t.Fatalf("backspace not restored: %#v", args)
	}
	// A file can hold real form feeds; only a command cannot.
	if args["content"] != "page\fbreak" {
		t.Fatalf("form feed in content changed: %#v", args)
	}
}

func TestParseAnswerReportsAMalformedToolCall(t *testing.T) {
	for _, answer := range []string{
		`{"tool_use":{"name":"Bash","input":{"command":"echo "hi""}}}`,
		`{"text":"Running \"it\".", "tool_use":{"name":"Bash","input":{"command":"echo "hi""}}}`,
		"```json\n" + `{"tool_use":{"name":"Bash","input":{"command":"ls`,
	} {
		_, _, err := parseAnswer(answer)
		var malformed *malformedToolUseError
		if !errors.As(err, &malformed) {
			t.Fatalf("%q: error %v, want a malformed tool call", answer, err)
		}
	}
	// An answer that is not an envelope at all is still passed on as text.
	parts, exact, err := parseAnswer(`{"thinking": "unclosed`)
	if err != nil || exact || len(parts) != 1 || parts[0].Text != `{"thinking": "unclosed` {
		t.Fatalf("parts=%#v exact=%v err=%v", parts, exact, err)
	}
}

// The streamed text of an envelope with stray backslashes is what parseAnswer reads, so nothing is lost or repeated at the end.
func TestEnvelopeDecoderStreamsStrayBackslashes(t *testing.T) {
	for _, answer := range []string{
		`{"text":"use rg 'a\.b\|c' in C:\users and \uZZ or \u12"}`,
		`{"text":"tail \u00e9 \ud83d\ude00 \"q\" end\\"}`,
	} {
		var d envelopeDecoder
		var streamed strings.Builder
		for _, r := range answer {
			streamed.WriteString(d.feed(string(r)))
		}
		rest, err := d.finish(acp.PromptResponse{StopReason: acp.StopReasonEndTurn})
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range rest {
			streamed.WriteString(part.Text)
		}
		parts, _, _ := parseAnswer(answer)
		if streamed.String() != parts[0].Text {
			t.Fatalf("streamed %q, parsed %q", streamed.String(), parts[0].Text)
		}
		if len(rest) != 0 {
			t.Fatalf("%q: text left for the end: %#v", answer, rest)
		}
	}
}

func TestACPAsksTheSessionToRewriteAMalformedToolCall(t *testing.T) {
	check := func(t *testing.T, parts []*genai.Part) {
		t.Helper()
		// The fake answers the repair prompt with its session and how many prompts that session received: the same session, second prompt.
		if len(parts) != 1 || parts[0].FunctionCall == nil || parts[0].FunctionCall.Args["command"] != "sx 2" {
			t.Fatalf("unexpected parts: %#v", parts)
		}
	}
	t.Run("generate", func(t *testing.T) {
		a := newFakeACP(t, "tool-malformed-once")
		resp, err := a.Generate(context.Background(), textRequest("hi"))
		if err != nil {
			t.Fatal(err)
		}
		check(t, resp.Candidates[0].Content.Parts)
	})
	t.Run("stream", func(t *testing.T) {
		a := newFakeACP(t, "tool-malformed-once")
		chunks := collectStream(t, a, textRequest("hi"))
		if len(chunks) != 1 {
			t.Fatalf("got %d chunks", len(chunks))
		}
		check(t, chunks[0].Candidates[0].Content.Parts)
	})
}

func TestACPRetriesAToolCallMalformedTwice(t *testing.T) {
	isRetryable := func(t *testing.T, err error) {
		t.Helper()
		var retryable *translate.RetryableError
		if !errors.As(err, &retryable) {
			t.Fatalf("error %v, want one Claude Code retries", err)
		}
	}
	t.Run("generate", func(t *testing.T) {
		a := newFakeACP(t, "tool-malformed")
		_, err := a.Generate(context.Background(), textRequest("hi"))
		isRetryable(t, err)
	})
	t.Run("stream", func(t *testing.T) {
		a := newFakeACP(t, "tool-malformed")
		var errs []error
		for resp, err := range a.Stream(context.Background(), textRequest("hi")) {
			if resp != nil {
				t.Fatalf("nothing must reach Claude Code before the error: %#v", resp)
			}
			errs = append(errs, err)
		}
		if len(errs) != 1 {
			t.Fatalf("errors %v", errs)
		}
		isRetryable(t, errs[0])
	})
}

// Claude Code sends a tool call back as its tool read the input: its Edit tool adds "replace_all": false. The conversation must still match the session that wrote the call, or every edit would make the next request read the whole conversation again.
func TestHistoryKeyIgnoresTheInputClaudeCodeEchoesForAToolCall(t *testing.T) {
	parts, _, err := parseAnswer(`{"tool_use":{"name":"Edit","input":{"file_path":"/tmp/a.go","old_string":"x","new_string":"y"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := translate.Blocks(parts)
	if err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	_ = json.Unmarshal(blocks[0].Input, &input)
	input["replace_all"] = false
	blocks[0].Input, _ = json.Marshal(input)
	assistant, _ := json.Marshal(blocks)
	echoed, err := translate.ToGemini(anthropic.MessagesRequest{Messages: []anthropic.Message{
		{Role: "user", Content: json.RawMessage(`"edit it"`)},
		{Role: "assistant", Content: assistant},
	}})
	if err != nil {
		t.Fatal(err)
	}
	original := []*genai.Content{genai.NewContentFromText("edit it", genai.RoleUser), {Role: genai.RoleModel, Parts: parts}}
	if historyKey(echoed.Contents) != historyKey(original) {
		t.Fatal("the echoed edit does not match the conversation the session holds")
	}
}

// A tool call cut off by the output limit is not written again, which would run into the same limit: it is passed on as text with that stop reason, as before.
func TestACPAnswerPartsPassesOnAToolCallCutOffByTheOutputLimit(t *testing.T) {
	raw := `{"tool_use":{"name":"Write","input":{"content":"long`
	parts, exact, err := acpAnswerParts(raw, acp.PromptResponse{StopReason: acp.StopReasonMaxTokens})
	if err != nil || exact || len(parts) != 1 || parts[0].Text != raw {
		t.Fatalf("parts=%#v exact=%v err=%v", parts, exact, err)
	}
}

func TestACPRewritesAMalformedToolCallAfterText(t *testing.T) {
	t.Run("generate", func(t *testing.T) {
		a := newFakeACP(t, "text-tool-malformed-once")
		resp, err := a.Generate(context.Background(), textRequest("hi"))
		if err != nil {
			t.Fatal(err)
		}
		parts := resp.Candidates[0].Content.Parts
		if len(parts) != 2 || parts[0].Text != "Running it." || parts[1].FunctionCall == nil || parts[1].FunctionCall.Args["command"] != "sx 2" {
			t.Fatalf("unexpected parts: %#v", parts)
		}
	})
	t.Run("stream", func(t *testing.T) {
		a := newFakeACP(t, "text-tool-malformed-once")
		var text strings.Builder
		var call *genai.FunctionCall
		for _, chunk := range collectStream(t, a, textRequest("hi")) {
			for _, part := range chunk.Candidates[0].Content.Parts {
				text.WriteString(part.Text)
				if part.FunctionCall != nil {
					call = part.FunctionCall
				}
			}
		}
		// The text streamed before the malformed call is shown once, and the rewritten call follows it.
		if text.String() != "Running it." || call == nil || call.Args["command"] != "sx 2" {
			t.Fatalf("text %q, call %#v", text.String(), call)
		}
	})
}
