package translate

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/anthropic"
)

func mustDecode(t *testing.T, body string) anthropic.MessagesRequest {
	t.Helper()
	var req anthropic.MessagesRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	return req
}

func TestToGeminiMapsSystemToolsChoiceAndMedia(t *testing.T) {
	req := mustDecode(t, `{
		"model":"claude-opus","max_tokens":64,"temperature":0.5,"top_k":3,
		"system":[{"type":"text","text":"be brief","cache_control":{"type":"ephemeral"}},{"type":"text","text":""}],
		"tool_choice":{"type":"tool","name":"Read"},
		"tools":[
			{"name":"Read","description":"read a file","input_schema":{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"path":{"type":"string"}},"additionalProperties":false}},
			{"name":"Bash","input_schema":{"type":"object","properties":{"timeout":{"type":"number","exclusiveMinimum":0}}}}
		],
		"messages":[{"role":"user","content":[
			{"type":"text","text":"look"},
			{"type":"text","text":""},
			{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}},
			{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"JVBERi0="}},
			{"type":"document","source":{"type":"text","media_type":"text/plain","data":"plain doc"}}
		]}]}`)
	got, err := ToGemini(req)
	if err != nil {
		t.Fatal(err)
	}
	c := got.Config
	if c.MaxOutputTokens != 64 || *c.Temperature != 0.5 || *c.TopK != 3 {
		t.Fatalf("generation config = %+v", c)
	}
	if len(c.SystemInstruction.Parts) != 1 || c.SystemInstruction.Parts[0].Text != "be brief" {
		t.Fatalf("system = %+v", c.SystemInstruction)
	}
	if len(c.Tools) != 1 || len(c.Tools[0].FunctionDeclarations) != 2 {
		t.Fatalf("all declarations must share one tool entry: %+v", c.Tools)
	}
	schema := c.Tools[0].FunctionDeclarations[0].ParametersJsonSchema.(map[string]any)
	if schema["additionalProperties"] != false || schema["$schema"] == nil {
		t.Fatalf("JSON Schema must pass through unmodified: %v", schema)
	}
	if c.Tools[0].FunctionDeclarations[0].Parameters != nil {
		t.Fatal("the OpenAPI-subset parameters field must stay unset")
	}
	fc := c.ToolConfig.FunctionCallingConfig
	if fc.Mode != genai.FunctionCallingConfigModeAny || len(fc.AllowedFunctionNames) != 1 || fc.AllowedFunctionNames[0] != "Read" {
		t.Fatalf("tool choice = %+v", fc)
	}
	parts := got.Contents[0].Parts
	if len(parts) != 4 {
		t.Fatalf("empty text must be dropped, got %d parts", len(parts))
	}
	if string(parts[1].InlineData.Data) != "hello" || parts[1].InlineData.MIMEType != "image/png" {
		t.Fatalf("image = %+v", parts[1].InlineData)
	}
	if parts[2].InlineData.MIMEType != "application/pdf" || parts[3].Text != "plain doc" {
		t.Fatalf("documents = %+v %+v", parts[2], parts[3])
	}
}

func TestToGeminiReplaysThoughtSignatureAndToolResults(t *testing.T) {
	id := newToolID([]byte("sig-bytes"), "call-7")
	body := `{"model":"m","max_tokens":10,"messages":[
		{"role":"user","content":"list files"},
		{"role":"assistant","content":[
			{"type":"thinking","thinking":"from an earlier Claude turn","signature":"x"},
			{"type":"text","text":"checking"},
			{"type":"tool_use","id":"` + id + `","name":"Bash","input":{"command":"ls"}},
			{"type":"tool_use","id":"toolu_from_anthropic","name":"Read","input":{}}
		]},
		{"role":"user","content":[
			{"type":"tool_result","tool_use_id":"` + id + `","content":"a.go\nb.go"},
			{"type":"tool_result","tool_use_id":"toolu_from_anthropic","is_error":true,"content":[
				{"type":"text","text":"line 1"},{"type":"text","text":"line 2"},
				{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"aGk="}}
			]},
			{"type":"text","text":"<system-reminder>r</system-reminder>"}
		]}]}`
	got, err := ToGemini(mustDecode(t, body))
	if err != nil {
		t.Fatal(err)
	}
	model := got.Contents[1]
	if model.Role != genai.RoleModel || len(model.Parts) != 3 {
		t.Fatalf("thinking must be dropped from the model turn: %+v", model.Parts)
	}
	call := model.Parts[1]
	if string(call.ThoughtSignature) != "sig-bytes" || call.FunctionCall.ID != "call-7" || call.FunctionCall.Args["command"] != "ls" {
		t.Fatalf("signature and call id must be replayed: %+v %+v", call, call.FunctionCall)
	}
	if model.Parts[2].ThoughtSignature != nil {
		t.Fatal("foreign tool IDs carry no signature")
	}
	results := got.Contents[2].Parts
	first := results[0].FunctionResponse
	if first.Name != "Bash" || first.ID != "call-7" || first.Response["output"] != "a.go\nb.go" {
		t.Fatalf("first result = %+v", first)
	}
	second := results[1].FunctionResponse
	if second.Name != "Read" || second.Response["error"] != "line 1\nline 2" || len(second.Parts) != 1 || string(second.Parts[0].InlineData.Data) != "hi" {
		t.Fatalf("error result with image = %+v", second)
	}
	if results[2].Text == "" {
		t.Fatal("text after tool results must be kept")
	}
}

// A conversation that ran on Anthropic before switching to NeoSwitch replays tool calls Claude made. Gemini 3+ rejects a step whose first function call is unsigned, so that call gets the documented placeholder; later parallel calls stay unsigned as Gemini expects.
func TestToGeminiSignsToolCallsMadeByClaude(t *testing.T) {
	body := `{"model":"m","max_tokens":10,"messages":[
		{"role":"user","content":"read both"},
		{"role":"assistant","content":[
			{"type":"text","text":"reading"},
			{"type":"tool_use","id":"toolu_01A","name":"Read","input":{"path":"a"}},
			{"type":"tool_use","id":"toolu_01B","name":"Read","input":{"path":"b"}}
		]},
		{"role":"user","content":[
			{"type":"tool_result","tool_use_id":"toolu_01A","content":"A"},
			{"type":"tool_result","tool_use_id":"toolu_01B","content":"B"}
		]}]}`
	got, err := ToGemini(mustDecode(t, body))
	if err != nil {
		t.Fatal(err)
	}
	parts := got.Contents[1].Parts
	if parts[0].ThoughtSignature != nil {
		t.Fatal("text parts must not be signed")
	}
	if string(parts[1].ThoughtSignature) != "skip_thought_signature_validator" {
		t.Fatalf("first call signature = %q", parts[1].ThoughtSignature)
	}
	if parts[2].ThoughtSignature != nil {
		t.Fatal("later parallel calls must stay unsigned")
	}
}

// Claude Code sends parallel tool results in the order the tools finished, not the order they were called. Gemini returns calls without IDs on the Developer API, so it can only pair responses with same-name calls by position: they must follow the call order.
func TestToGeminiOrdersParallelToolResultsLikeTheCalls(t *testing.T) {
	first, second := newToolID([]byte("sig"), ""), newToolID(nil, "")
	body := `{"model":"m","max_tokens":10,"messages":[
		{"role":"user","content":"read both"},
		{"role":"assistant","content":[
			{"type":"tool_use","id":"` + first + `","name":"Read","input":{"file_path":"a.txt"}},
			{"type":"tool_use","id":"` + second + `","name":"Read","input":{"file_path":"b.txt"}}
		]},
		{"role":"user","content":[
			{"type":"tool_result","tool_use_id":"` + second + `","content":"beta"},
			{"type":"text","text":"<system-reminder>r</system-reminder>"},
			{"type":"tool_result","tool_use_id":"` + first + `","content":"alpha"}
		]}]}`
	got, err := ToGemini(mustDecode(t, body))
	if err != nil {
		t.Fatal(err)
	}
	parts := got.Contents[2].Parts
	if len(parts) != 3 || parts[0].FunctionResponse.Response["output"] != "alpha" || parts[1].Text == "" || parts[2].FunctionResponse.Response["output"] != "beta" {
		t.Fatalf("results must follow the call order and other parts keep their place: %+v %+v %+v", parts[0], parts[1], parts[2])
	}
}

// Claude Code 2.1.x sends its environment block as a role "system" message after the first user turn.
func TestToGeminiAppendsMidConversationSystemMessages(t *testing.T) {
	got, err := ToGemini(mustDecode(t, `{"system":"base","messages":[
		{"role":"user","content":"hi"},
		{"role":"system","content":[{"type":"text","text":"# Environment"}],"output_config":{}},
		{"role":"assistant","content":"ok"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	system := got.Config.SystemInstruction.Parts
	if len(system) != 2 || system[0].Text != "base" || system[1].Text != "# Environment" {
		t.Fatalf("system = %+v", system)
	}
	if len(got.Contents) != 2 || got.Contents[0].Role != genai.RoleUser || got.Contents[1].Role != genai.RoleModel {
		t.Fatalf("contents = %+v", got.Contents)
	}
}

func TestToGeminiRejectsWhatItCannotRepresent(t *testing.T) {
	cases := map[string]string{
		"unknown block":       `{"messages":[{"role":"user","content":[{"type":"text","text":"a"},{"type":"future_block"}]}]}`,
		"unknown tool_use_id": `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"nope","content":"x"}]}]}`,
		"server tool":         `{"tools":[{"type":"web_search_20250305","name":"web_search"}],"messages":[{"role":"user","content":"a"}]}`,
		"url image":           `{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://example.com/a.png"}}]}]}`,
		"bad base64":          `{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"%%%"}}]}]}`,
		"tool_use from user":  `{"messages":[{"role":"user","content":[{"type":"tool_use","id":"t","name":"x","input":{}}]}]}`,
		"only empty content":  `{"messages":[{"role":"user","content":[{"type":"text","text":""}]}]}`,
		"unknown role":        `{"messages":[{"role":"developer","content":"a"}]}`,
		"system image":        `{"messages":[{"role":"system","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGk="}}]},{"role":"user","content":"a"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ToGemini(mustDecode(t, body)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestToGeminiExplainsUnsupportedFeaturesPlainly(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"web search":    {`{"tools":[{"type":"web_search_20250305","name":"web_search"}],"messages":[{"role":"user","content":"a"}]}`, "Web search (WebSearch) does not work with NeoSwitch"},
		"other server":  {`{"tools":[{"type":"code_execution_20250522","name":"code_execution"}],"messages":[{"role":"user","content":"a"}]}`, `"code_execution_20250522" tool does not work with NeoSwitch`},
		"url image":     {`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://example.com/a.png"}}]}]}`, "cannot send images given as web links"},
		"url in result": {`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"t","name":"x","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":[{"type":"document","source":{"type":"url","url":"https://example.com/a.pdf"}}]}]}]}`, "cannot send documents given as web links"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ToGemini(mustDecode(t, tc.body))
			var unsupported *UnsupportedError
			if !errors.As(err, &unsupported) {
				t.Fatalf("expected an UnsupportedError, got %v", err)
			}
			if !strings.Contains(unsupported.Message, tc.want) || strings.Contains(unsupported.Message, "[") {
				t.Fatalf("message %q must contain %q and no field path", unsupported.Message, tc.want)
			}
		})
	}
}

func TestToGeminiLeavesOutTheAdvisorTool(t *testing.T) {
	// The advisor entry is what Claude Code sends on every request when the advisor is enabled.
	const advisor = `{"type":"advisor_20260301","name":"advisor","model":"claude-opus-5-5","defer_loading":true}`

	got, err := ToGemini(mustDecode(t, `{"tools":[`+advisor+`,{"name":"Read","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"a"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Config.Tools) != 1 || len(got.Config.Tools[0].FunctionDeclarations) != 1 || got.Config.Tools[0].FunctionDeclarations[0].Name != "Read" {
		t.Fatalf("tools = %+v", got.Config.Tools)
	}

	got, err = ToGemini(mustDecode(t, `{"tools":[`+advisor+`],"messages":[{"role":"user","content":"a"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.Tools != nil {
		t.Fatalf("a request whose only tool is the advisor must send no tools, got %+v", got.Config.Tools)
	}
}

func TestToGeminiDropsAdvisorExchangesFromHistory(t *testing.T) {
	got, err := ToGemini(mustDecode(t, `{"messages":[
		{"role":"user","content":"a"},
		{"role":"assistant","content":[
			{"type":"server_tool_use","id":"srvtoolu_1","name":"advisor","input":{}},
			{"type":"advisor_tool_result","tool_use_id":"srvtoolu_1","content":{"type":"advisor_result","text":"advice"}},
			{"type":"text","text":"answer"}
		]},
		{"role":"user","content":"b"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Contents) != 3 {
		t.Fatalf("contents = %+v", got.Contents)
	}
	model := got.Contents[1]
	if len(model.Parts) != 1 || model.Parts[0].Text != "answer" {
		t.Fatalf("model turn must keep only its text: %+v", model.Parts)
	}

	if _, err := ToGemini(mustDecode(t, `{"messages":[{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_2","name":"web_search","input":{"query":"q"}}]},{"role":"user","content":"a"}]}`)); err == nil {
		t.Fatal("server_tool_use for tools other than the advisor must still be rejected")
	}
}

// TestToGeminiAcceptsClaudeCodeToolSchemas runs every built-in tool schema from a captured Claude Code request through the translator and checks they reach Gemini as JSON Schema, byte-for-byte equivalent.
func TestToGeminiAcceptsClaudeCodeToolSchemas(t *testing.T) {
	data, err := os.ReadFile("testdata/claude-code-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Tools []anthropic.Tool `json:"tools"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Tools) < 10 {
		t.Fatalf("fixture has %d tools", len(fixture.Tools))
	}
	req := anthropic.MessagesRequest{MaxTokens: 1, Tools: fixture.Tools, Messages: []anthropic.Message{{Role: "user", Content: json.RawMessage(`"hi"`)}}}
	got, err := ToGemini(req)
	if err != nil {
		t.Fatal(err)
	}
	declarations := got.Config.Tools[0].FunctionDeclarations
	for i, tool := range fixture.Tools {
		want, _ := json.Marshal(mustAny(t, tool.InputSchema))
		have, _ := json.Marshal(declarations[i].ParametersJsonSchema)
		if string(want) != string(have) {
			t.Fatalf("%s schema changed in translation", tool.Name)
		}
	}
}

func mustAny(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestFromGeminiMapsTextToolUseAndUsage(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			FinishReason: genai.FinishReasonStop,
			Content: &genai.Content{Parts: []*genai.Part{
				{Text: "internal reasoning", Thought: true},
				{Text: "I'll look."},
				{FunctionCall: &genai.FunctionCall{Name: "Read", Args: map[string]any{"path": "a"}}, ThoughtSignature: []byte{0, 1, 255}},
				{FunctionCall: &genai.FunctionCall{Name: "Glob"}},
			}},
		}},
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 100, CachedContentTokenCount: 40, CandidatesTokenCount: 7, ThoughtsTokenCount: 5},
	}
	got, err := FromGemini(resp, "claude-opus")
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "claude-opus" || got.StopReason != "tool_use" || len(got.Content) != 3 {
		t.Fatalf("response = %+v", got)
	}
	if got.Content[0].Text != "I'll look." {
		t.Fatalf("thought parts must not leak into text: %+v", got.Content)
	}
	use := got.Content[1]
	if !strings.HasPrefix(use.ID, toolIDPrefix) || string(use.Input) != `{"path":"a"}` {
		t.Fatalf("tool_use = %+v", use)
	}
	if meta := parseToolID(use.ID); string(meta.Signature) != string([]byte{0, 1, 255}) {
		t.Fatalf("signature not embedded: %+v", meta)
	}
	if string(got.Content[2].Input) != "{}" || got.Content[2].ID == use.ID {
		t.Fatalf("argument-less call = %+v", got.Content[2])
	}
	if got.Usage != (anthropic.Usage{InputTokens: 60, OutputTokens: 12, CacheReadInputTokens: 40}) {
		t.Fatalf("usage = %+v", got.Usage)
	}
}

func TestFromGeminiFinishReasons(t *testing.T) {
	text := &genai.Content{Parts: []*genai.Part{{Text: "x"}}}
	for reason, want := range map[genai.FinishReason]string{
		genai.FinishReasonStop:      "end_turn",
		genai.FinishReasonMaxTokens: "max_tokens",
		genai.FinishReasonSafety:    "refusal",
	} {
		got, err := FromGemini(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: text, FinishReason: reason}}}, "m")
		if err != nil || got.StopReason != want {
			t.Fatalf("%s: stop = %q err = %v", reason, got.StopReason, err)
		}
	}
	for _, reason := range []genai.FinishReason{genai.FinishReasonMalformedFunctionCall, genai.FinishReasonMissingThoughtSignature, genai.FinishReasonOther} {
		_, err := FromGemini(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{{FinishReason: reason}}}, "m")
		var upstream *UpstreamError
		if !errors.As(err, &upstream) || !strings.Contains(err.Error(), string(reason)) {
			t.Fatalf("%s must fail the turn, got %v", reason, err)
		}
	}
	blocked, err := FromGemini(&genai.GenerateContentResponse{PromptFeedback: &genai.GenerateContentResponsePromptFeedback{BlockReason: genai.BlockedReasonSafety}}, "m")
	if err != nil || blocked.StopReason != "refusal" || blocked.Content == nil {
		t.Fatalf("blocked prompt = %+v %v", blocked, err)
	}
	if _, err := FromGemini(&genai.GenerateContentResponse{}, "m"); err == nil {
		t.Fatal("no candidates without a block reason is an upstream error")
	}
}

func TestToolIDIsAnthropicCompatible(t *testing.T) {
	id := newToolID([]byte(strings.Repeat("\xff", 300)), "c")
	for _, r := range id {
		if !(r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			t.Fatalf("id contains %q", r)
		}
	}
	if parseToolID("toolu_ns_not-base64!").Signature != nil {
		t.Fatal("corrupt IDs must decode to no metadata")
	}
}

func TestToGeminiMapsEffortToThinkingLevel(t *testing.T) {
	cases := []struct {
		outputConfig string
		want         genai.ThinkingLevel
	}{
		{`,"output_config":{"effort":"low"}`, genai.ThinkingLevelLow},
		{`,"output_config":{"effort":"medium"}`, genai.ThinkingLevelMedium},
		{`,"output_config":{"effort":"high"}`, genai.ThinkingLevelHigh},
		{`,"output_config":{"effort":"xhigh"}`, genai.ThinkingLevelXhigh},
		{`,"output_config":{"effort":"max"}`, genai.ThinkingLevelXhigh},
		{`,"output_config":{"effort":"turbo"}`, ""},
		{`,"output_config":{}`, ""},
		{``, ""},
	}
	for _, tc := range cases {
		// Captured from Claude Code 2.1.329: adaptive thinking plus output_config.effort on every request.
		req := mustDecode(t, `{"model":"claude-opus-5-5","max_tokens":128000,"thinking":{"type":"adaptive","display":"omitted"}`+tc.outputConfig+`,"messages":[{"role":"user","content":"hi"}]}`)
		got, err := ToGemini(req)
		if err != nil {
			t.Fatalf("%s: %v", tc.outputConfig, err)
		}
		if got.ThinkingLevel != tc.want {
			t.Fatalf("%s: thinking level %q, want %q", tc.outputConfig, got.ThinkingLevel, tc.want)
		}
		// The Gemini API request itself is unchanged until per-model support for each level is verified.
		if got.Config.ThinkingConfig != nil {
			t.Fatalf("%s: unexpected Gemini thinking config %+v", tc.outputConfig, got.Config.ThinkingConfig)
		}
	}
}
