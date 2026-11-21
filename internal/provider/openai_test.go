package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yldm-ai/neo-switch/internal/server"
)

const openAITestKey = "sk-test-secret-0123456789"

// fakeOpenAI is a scripted Responses API. Each handler call receives the decoded request body and writes the reply; requests are recorded for assertions.
type fakeOpenAI struct {
	t        *testing.T
	mu       sync.Mutex
	requests []map[string]any
	paths    []string
	reply    func(w http.ResponseWriter, path string, body map[string]any)
}

func (f *fakeOpenAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+openAITestKey {
		f.t.Errorf("Authorization header = %q", r.Header.Get("Authorization"))
	}
	var body map[string]any
	if r.Body != nil {
		data, _ := io.ReadAll(r.Body)
		if len(data) > 0 {
			if err := json.Unmarshal(data, &body); err != nil {
				f.t.Errorf("request body is not JSON: %v", err)
			}
		}
	}
	f.mu.Lock()
	f.requests = append(f.requests, body)
	f.paths = append(f.paths, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
	f.reply(w, r.URL.Path, body)
}

func (f *fakeOpenAI) last() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

// newOpenAITest starts the fake upstream and builds the backend against it. Every variable the SDK reads is replaced, so a developer's real OPENAI_API_KEY can never reach the fake or OpenAI.
func newOpenAITest(t *testing.T, reply func(w http.ResponseWriter, path string, body map[string]any)) (*OpenAI, *fakeOpenAI) {
	t.Helper()
	fake := &fakeOpenAI{t: t, reply: reply}
	upstream := httptest.NewServer(fake)
	t.Cleanup(upstream.Close)
	for _, name := range []string{"OPENAI_ORG_ID", "OPENAI_PROJECT_ID", "OPENAI_ADMIN_KEY", "OPENAI_WEBHOOK_SECRET", "OPENAI_CUSTOM_HEADERS"} {
		t.Setenv(name, "")
	}
	t.Setenv("OPENAI_API_KEY", openAITestKey)
	t.Setenv("OPENAI_MODEL", "gpt-test")
	t.Setenv("OPENAI_BASE_URL", upstream.URL+"/v1")
	backend, err := NewOpenAIFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	return backend, fake
}

func writeJSONReply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func completedResponse(output string, usage string) string {
	return `{"id":"resp_1","object":"response","status":"completed","model":"gpt-test","output":[` + output + `],"usage":` + usage + `}`
}

const textUsage = `{"input_tokens":100,"input_tokens_details":{"cached_tokens":40},"output_tokens":30,"output_tokens_details":{"reasoning_tokens":10},"total_tokens":130}`

// post sends an Anthropic Messages request through the real server handler backed by the OpenAI backend.
func post(t *testing.T, backend server.Backend, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	handler := server.NewHandler(server.Config{Backend: backend, Model: "gpt-test"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return recorder
}

func TestOpenAITextRequestAndUsage(t *testing.T) {
	backend, fake := newOpenAITest(t, func(w http.ResponseWriter, path string, body map[string]any) {
		if path != "/v1/responses" {
			t.Errorf("path = %s", path)
		}
		writeJSONReply(w, http.StatusOK, completedResponse(`{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hello there","annotations":[]}]}`, textUsage))
	})
	recorder := post(t, backend, "/v1/messages", `{"model":"claude","max_tokens":321,"temperature":0.5,"top_p":0.9,"stop_sequences":["END"],
		"system":"Be brief.",
		"tools":[{"name":"Read","description":"Read a file","input_schema":{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"path":{"type":"string"}},"additionalProperties":false}}],
		"tool_choice":{"type":"tool","name":"Read"},
		"messages":[{"role":"system","content":"Env block"},{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGk="}}]}]}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	var response struct {
		Content []struct {
			Type, Text string
		}
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			CacheRead    int `json:"cache_read_input_tokens"`
		}
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Content) != 1 || response.Content[0].Text != "Hello there" || response.StopReason != "end_turn" {
		t.Fatalf("response = %+v", response)
	}
	if response.Usage.InputTokens != 60 || response.Usage.CacheRead != 40 || response.Usage.OutputTokens != 30 {
		t.Fatalf("usage = %+v", response.Usage)
	}

	body := fake.last()
	if body["model"] != "gpt-test" || body["instructions"] != "Be brief.\n\nEnv block" || body["store"] != false {
		t.Fatalf("model/instructions/store = %v / %q / %v", body["model"], body["instructions"], body["store"])
	}
	if body["max_output_tokens"] != float64(321) || body["temperature"] != 0.5 || body["top_p"] != 0.9 {
		t.Fatalf("sampling = %v %v %v", body["max_output_tokens"], body["temperature"], body["top_p"])
	}
	for _, unsupported := range []string{"stop", "reasoning", "top_k"} {
		if _, ok := body[unsupported]; ok {
			t.Errorf("%s must not be sent", unsupported)
		}
	}
	tool := body["tools"].([]any)[0].(map[string]any)
	parameters := tool["parameters"].(map[string]any)
	if tool["type"] != "function" || tool["name"] != "Read" || tool["description"] != "Read a file" || tool["strict"] != false || parameters["additionalProperties"] != false || parameters["$schema"] == nil {
		t.Fatalf("tool = %v", tool)
	}
	if choice := body["tool_choice"].(map[string]any); choice["type"] != "function" || choice["name"] != "Read" {
		t.Fatalf("tool_choice = %v", choice)
	}
	input := body["input"].([]any)
	message := input[0].(map[string]any)
	content := message["content"].([]any)
	if len(input) != 1 || message["role"] != "user" || len(content) != 2 {
		t.Fatalf("input = %v", input)
	}
	if text := content[0].(map[string]any); text["type"] != "input_text" || text["text"] != "hi" {
		t.Fatalf("text = %v", text)
	}
	if image := content[1].(map[string]any); image["type"] != "input_image" || image["image_url"] != "data:image/png;base64,aGk=" {
		t.Fatalf("image = %v", image)
	}
}

func TestOpenAIToolChoiceModes(t *testing.T) {
	backend, fake := newOpenAITest(t, func(w http.ResponseWriter, path string, body map[string]any) {
		writeJSONReply(w, http.StatusOK, completedResponse(``, textUsage))
	})
	for choice, want := range map[string]string{"auto": "auto", "any": "required", "none": "none"} {
		recorder := post(t, backend, "/v1/messages", `{"max_tokens":5,"tools":[{"name":"Read","input_schema":{"type":"object"}}],"tool_choice":{"type":"`+choice+`"},"messages":[{"role":"user","content":"hi"}]}`)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", choice, recorder.Code, recorder.Body)
		}
		if got := fake.last()["tool_choice"]; got != want {
			t.Errorf("tool_choice %s = %v, want %s", choice, got, want)
		}
	}
}

// TestOpenAIParallelToolRoundTrip checks the stateless call_id round trip: OpenAI's call_id is carried in the Anthropic tool_use ID and comes back on the function_call and function_call_output items of the next turn.
func TestOpenAIParallelToolRoundTrip(t *testing.T) {
	turn := 0
	backend, fake := newOpenAITest(t, func(w http.ResponseWriter, path string, body map[string]any) {
		turn++
		if turn == 1 {
			writeJSONReply(w, http.StatusOK, completedResponse(
				`{"type":"reasoning","id":"rs_1","summary":[]},
				{"type":"function_call","id":"fc_1","call_id":"call_alpha","name":"Read","arguments":"{\"path\":\"a.go\"}","status":"completed"},
				{"type":"function_call","id":"fc_2","call_id":"call_beta","name":"Bash","arguments":"{\"command\":\"ls\"}","status":"completed"}`, textUsage))
			return
		}
		writeJSONReply(w, http.StatusOK, completedResponse(`{"type":"message","id":"msg_2","role":"assistant","status":"completed","content":[{"type":"output_text","text":"done","annotations":[]}]}`, textUsage))
	})
	tools := `"tools":[{"name":"Read","input_schema":{"type":"object"}},{"name":"Bash","input_schema":{"type":"object"}}]`
	first := post(t, backend, "/v1/messages", `{"max_tokens":50,`+tools+`,"messages":[{"role":"user","content":"look"}]}`)
	if first.Code != http.StatusOK {
		t.Fatalf("status %d: %s", first.Code, first.Body)
	}
	var response struct {
		Content    []map[string]any
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.StopReason != "tool_use" || len(response.Content) != 2 {
		t.Fatalf("response = %+v", response)
	}
	ids := []string{response.Content[0]["id"].(string), response.Content[1]["id"].(string)}
	if ids[0] == ids[1] || !strings.HasPrefix(ids[0], "toolu_ns_") {
		t.Fatalf("tool IDs = %v", ids)
	}
	if input := response.Content[0]["input"].(map[string]any); response.Content[0]["name"] != "Read" || input["path"] != "a.go" {
		t.Fatalf("first tool_use = %v", response.Content[0])
	}

	assistant, _ := json.Marshal(response.Content)
	second := post(t, backend, "/v1/messages", `{"max_tokens":50,`+tools+`,"messages":[{"role":"user","content":"look"},
		{"role":"assistant","content":`+string(assistant)+`},
		{"role":"user","content":[
			{"type":"tool_result","tool_use_id":"`+ids[0]+`","content":"package main"},
			{"type":"tool_result","tool_use_id":"`+ids[1]+`","is_error":true,"content":[{"type":"text","text":"denied"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGk="}}]},
			{"type":"text","text":"continue"}]}]}`)
	if second.Code != http.StatusOK {
		t.Fatalf("status %d: %s", second.Code, second.Body)
	}
	input := fake.last()["input"].([]any)
	if len(input) != 6 {
		t.Fatalf("input has %d items: %v", len(input), input)
	}
	expect := []struct{ typ, callID string }{{"message", ""}, {"function_call", "call_alpha"}, {"function_call", "call_beta"}, {"function_call_output", "call_alpha"}, {"function_call_output", "call_beta"}, {"message", ""}}
	for i, want := range expect {
		item := input[i].(map[string]any)
		typ, _ := item["type"].(string)
		if typ == "" {
			typ = "message"
		}
		if typ != want.typ || (want.callID != "" && item["call_id"] != want.callID) {
			t.Fatalf("input[%d] = %v, want %s %s", i, item, want.typ, want.callID)
		}
	}
	if call := input[1].(map[string]any); call["name"] != "Read" || call["arguments"] != `{"path":"a.go"}` {
		t.Fatalf("replayed call = %v", call)
	}
	if output := input[3].(map[string]any); output["output"] != "package main" {
		t.Fatalf("text output = %v", output)
	}
	multimodal := input[4].(map[string]any)["output"].([]any)
	if len(multimodal) != 2 || multimodal[0].(map[string]any)["text"] != "denied" || multimodal[1].(map[string]any)["type"] != "input_image" {
		t.Fatalf("multimodal output = %v", multimodal)
	}
	if last := input[5].(map[string]any); last["role"] != "user" {
		t.Fatalf("trailing user text = %v", last)
	}
}

// TestOpenAIForeignToolIDs covers a session started on Anthropic and resumed here: its tool_use IDs carry no OpenAI call_id, so calls get synthetic call IDs that their results reuse.
func TestOpenAIForeignToolIDs(t *testing.T) {
	backend, fake := newOpenAITest(t, func(w http.ResponseWriter, path string, body map[string]any) {
		writeJSONReply(w, http.StatusOK, completedResponse(``, textUsage))
	})
	recorder := post(t, backend, "/v1/messages", `{"max_tokens":5,"messages":[{"role":"user","content":"x"},
		{"role":"assistant","content":[{"type":"text","text":"checking"},{"type":"tool_use","id":"toolu_01A","name":"Read","input":{}},{"type":"tool_use","id":"toolu_01B","name":"Read","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01A","content":"a"},{"type":"tool_result","tool_use_id":"toolu_01B","content":"b"}]}]}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	input := fake.last()["input"].([]any)
	if text := input[1].(map[string]any); text["role"] != "assistant" || text["content"] != "checking" {
		t.Fatalf("assistant text = %v", text)
	}
	a, b := input[2].(map[string]any)["call_id"], input[3].(map[string]any)["call_id"]
	if a == "" || a == b || input[4].(map[string]any)["call_id"] != a || input[5].(map[string]any)["call_id"] != b {
		t.Fatalf("synthetic call IDs do not pair: %v", input)
	}
}

func sse(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	for _, event := range events {
		var typed struct{ Type string }
		_ = json.Unmarshal([]byte(event), &typed)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typed.Type, event)
	}
}

func TestOpenAIStreamingTextAndToolCall(t *testing.T) {
	backend, fake := newOpenAITest(t, func(w http.ResponseWriter, path string, body map[string]any) {
		sse(w,
			`{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","object":"response","status":"in_progress","output":[]}}`,
			`{"type":"response.output_text.delta","sequence_number":1,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"Hel","logprobs":[]}`,
			`{"type":"response.output_text.delta","sequence_number":2,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"lo","logprobs":[]}`,
			`{"type":"response.output_item.done","sequence_number":3,"output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hello","annotations":[]}]}}`,
			`{"type":"response.function_call_arguments.delta","sequence_number":4,"item_id":"fc_1","output_index":1,"delta":"{\"path\""}`,
			`{"type":"response.output_item.done","sequence_number":5,"output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_stream","name":"Read","arguments":"{\"path\":\"a.go\"}","status":"completed"}}`,
			`{"type":"response.completed","sequence_number":6,"response":`+completedResponse(``, textUsage)+`}`,
		)
	})
	recorder := post(t, backend, "/v1/messages", `{"max_tokens":50,"stream":true,"tools":[{"name":"Read","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hi"}]}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	if fake.last()["stream"] != true {
		t.Fatal("stream was not requested upstream")
	}
	out := recorder.Body.String()
	for _, want := range []string{`"text":"Hel"`, `"text":"lo"`, `"type":"tool_use"`, `"partial_json":"{\"path\":\"a.go\"}"`, `"stop_reason":"tool_use"`, `"output_tokens":30`, `"cache_read_input_tokens":40`, "event: message_stop"} {
		if !strings.Contains(out, want) {
			t.Errorf("stream lacks %s:\n%s", want, out)
		}
	}
	if strings.Count(out, `"type":"text_delta"`) != 2 {
		t.Errorf("the finished message item must not repeat its text:\n%s", out)
	}
	// The streamed tool_use ID must carry the call_id like the non-streaming one.
	var id string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, `"type":"tool_use"`) {
			var event struct {
				ContentBlock struct{ ID string } `json:"content_block"`
			}
			_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event)
			id = event.ContentBlock.ID
		}
	}
	replay := translated(t, `{"max_tokens":5,"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":[{"type":"tool_use","id":"`+id+`","name":"Read","input":{}}]}]}`)
	if got := replay.Contents[1].Parts[0].FunctionCall.ID; got != "call_stream" {
		t.Fatalf("streamed tool_use ID carries call_id %q", got)
	}
}

func TestOpenAIMaxTokensAndRefusal(t *testing.T) {
	reply := ""
	backend, _ := newOpenAITest(t, func(w http.ResponseWriter, path string, body map[string]any) {
		writeJSONReply(w, http.StatusOK, reply)
	})
	cases := []struct{ reply, stop string }{
		{`{"id":"r","object":"response","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","id":"m","role":"assistant","status":"incomplete","content":[{"type":"output_text","text":"partial","annotations":[]}]}],"usage":` + textUsage + `}`, "max_tokens"},
		{`{"id":"r","object":"response","status":"incomplete","incomplete_details":{"reason":"content_filter"},"output":[],"usage":` + textUsage + `}`, "refusal"},
		{completedResponse(`{"type":"message","id":"m","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"I can't help with that."}]}`, textUsage), "refusal"},
	}
	for _, c := range cases {
		reply = c.reply
		recorder := post(t, backend, "/v1/messages", `{"max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`)
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"stop_reason":"`+c.stop+`"`) {
			t.Fatalf("want stop_reason %s, got %d %s", c.stop, recorder.Code, recorder.Body)
		}
	}
}

func TestOpenAIErrorStatusMappingDoesNotLeakKey(t *testing.T) {
	status, body := 0, ""
	backend, _ := newOpenAITest(t, func(w http.ResponseWriter, path string, _ map[string]any) {
		writeJSONReply(w, status, body)
	})
	cases := []struct {
		status   int
		body     string
		want     int
		wantType string
		contains string
	}{
		{401, `{"error":{"message":"Incorrect API key provided: ` + openAITestKey + `. You can find your API key at https://platform.openai.com/account/api-keys.","type":"invalid_request_error","param":null,"code":"invalid_api_key"}}`, 400, "invalid_request_error", "refused the credentials NeoSwitch uses"},
		{401, `{"error":{"message":"Incorrect API key provided: sk-test-********************6789.","type":"invalid_request_error","param":null,"code":"invalid_api_key"}}`, 400, "invalid_request_error", "refused the credentials NeoSwitch uses"},
		{429, `{"error":{"message":"Rate limit reached","type":"requests","param":null,"code":"rate_limit_exceeded"}}`, 429, "rate_limit_error", "Rate limit reached"},
		{400, `{"error":{"message":"Your input exceeds the context window of this model.","type":"invalid_request_error","param":"input","code":"context_length_exceeded"}}`, 400, "invalid_request_error", "prompt is too long"},
		{404, `{"error":{"message":"The model does not exist","type":"invalid_request_error","param":null,"code":"model_not_found"}}`, 404, "not_found_error", ""},
		{503, `{"error":{"message":"overloaded","type":"server_error","param":null,"code":null}}`, 529, "overloaded_error", ""},
	}
	for _, c := range cases {
		status, body = c.status, c.body
		for _, stream := range []string{"false", "true"} {
			recorder := post(t, backend, "/v1/messages", `{"max_tokens":5,"stream":`+stream+`,"messages":[{"role":"user","content":"hi"}]}`)
			out := recorder.Body.String()
			if recorder.Code != c.want || !strings.Contains(out, `"type":"`+c.wantType+`"`) || !strings.Contains(out, c.contains) {
				t.Errorf("upstream %d (stream %s): got %d %s", c.status, stream, recorder.Code, out)
			}
			if strings.Contains(out, "sk-test") || strings.Contains(out, "0123456789") {
				t.Errorf("upstream %d (stream %s): the key leaked: %s", c.status, stream, out)
			}
		}
	}
}

func TestOpenAIStreamFailureBeforeOutputKeepsStatus(t *testing.T) {
	backend, _ := newOpenAITest(t, func(w http.ResponseWriter, path string, body map[string]any) {
		sse(w,
			`{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","object":"response","status":"in_progress","output":[]}}`,
			`{"type":"response.failed","sequence_number":1,"response":{"id":"resp_1","object":"response","status":"failed","error":{"code":"rate_limit_exceeded","message":"slow down"},"output":[]}}`,
		)
	})
	recorder := post(t, backend, "/v1/messages", `{"max_tokens":5,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if recorder.Code != http.StatusTooManyRequests || !strings.Contains(recorder.Body.String(), "slow down") {
		t.Fatalf("got %d %s", recorder.Code, recorder.Body)
	}
}

func TestOpenAICountTokens(t *testing.T) {
	backend, fake := newOpenAITest(t, func(w http.ResponseWriter, path string, body map[string]any) {
		if path != "/v1/responses/input_tokens" {
			t.Errorf("path = %s", path)
		}
		writeJSONReply(w, http.StatusOK, `{"object":"response.input_tokens","input_tokens":42}`)
	})
	recorder := post(t, backend, "/v1/messages/count_tokens", `{"system":"Be brief.","tools":[{"name":"Read","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hi"}]}`)
	if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != `{"input_tokens":42}` {
		t.Fatalf("got %d %s", recorder.Code, recorder.Body)
	}
	body := fake.last()
	if body["model"] != "gpt-test" || body["instructions"] != "Be brief." || len(body["tools"].([]any)) != 1 || len(body["input"].([]any)) != 1 {
		t.Fatalf("count request = %v", body)
	}
}

func TestOpenAICheck(t *testing.T) {
	modelStatus := http.StatusOK
	backend, fake := newOpenAITest(t, func(w http.ResponseWriter, path string, body map[string]any) {
		switch path {
		case "/v1/models/gpt-test":
			if modelStatus != http.StatusOK {
				writeJSONReply(w, modelStatus, `{"error":{"message":"Incorrect API key provided: `+openAITestKey+`","type":"invalid_request_error","param":null,"code":"invalid_api_key"}}`)
				return
			}
			writeJSONReply(w, http.StatusOK, `{"id":"gpt-test","object":"model","created":0,"owned_by":"openai"}`)
		case "/v1/responses":
			writeJSONReply(w, http.StatusOK, completedResponse(`{"type":"message","id":"m","role":"assistant","status":"completed","content":[{"type":"output_text","text":"OK","annotations":[]}]}`, textUsage))
		default:
			t.Errorf("unexpected path %s", path)
		}
	})
	if err := backend.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fake.paths[0] != "GET /v1/models/gpt-test" || fake.paths[1] != "POST /v1/responses" {
		t.Fatalf("check calls = %v", fake.paths)
	}
	modelStatus = http.StatusUnauthorized
	err := backend.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "sk-test") || strings.Contains(fmt.Sprintf("%+v %#v", err, err), openAITestKey) {
		t.Fatalf("the key leaked: %v", err)
	}
}

func TestOpenAIRequiresKeyAndModel(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "gpt-test")
	if _, err := NewOpenAIFromEnv(); err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("got %v", err)
	}
	t.Setenv("OPENAI_API_KEY", openAITestKey)
	t.Setenv("OPENAI_MODEL", " ")
	_, err := NewOpenAIFromEnv()
	if err == nil || !strings.Contains(err.Error(), "OPENAI_MODEL") || strings.Contains(err.Error(), openAITestKey) {
		t.Fatalf("got %v", err)
	}
}

var _ server.Backend = (*OpenAI)(nil)

func TestOpenAIStreamingMaxTokens(t *testing.T) {
	backend, _ := newOpenAITest(t, func(w http.ResponseWriter, path string, body map[string]any) {
		sse(w,
			`{"type":"response.output_text.delta","sequence_number":0,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"partial","logprobs":[]}`,
			`{"type":"response.incomplete","sequence_number":1,"response":{"id":"r","object":"response","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[],"usage":`+textUsage+`}}`,
		)
	})
	recorder := post(t, backend, "/v1/messages", `{"max_tokens":5,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if out := recorder.Body.String(); recorder.Code != http.StatusOK || !strings.Contains(out, `"stop_reason":"max_tokens"`) || !strings.Contains(out, `"text":"partial"`) {
		t.Fatalf("got %d %s", recorder.Code, out)
	}
}

func TestOpenAIListModelsSortsIDs(t *testing.T) {
	backend, fake := newOpenAITest(t, func(w http.ResponseWriter, path string, _ map[string]any) {
		if path != "/v1/models" {
			t.Errorf("path = %s", path)
		}
		writeJSONReply(w, http.StatusOK, `{"object":"list","data":[{"id":"gpt-test","object":"model","created":1,"owned_by":"openai"},{"id":"gpt-a","object":"model","created":1,"owned_by":"openai"},{"id":"text-embedding-x","object":"model","created":1,"owned_by":"openai"}]}`)
	})
	ids, err := backend.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := "gpt-a,gpt-test,text-embedding-x"; strings.Join(ids, ",") != want {
		t.Fatalf("models = %v, want %s", ids, want)
	}
	if got := strings.Join(fake.paths, ","); got != "GET /v1/models" {
		t.Fatalf("requests = %s", got)
	}
}

func TestOpenAIListModelsNamesTheFailure(t *testing.T) {
	backend, _ := newOpenAITest(t, func(w http.ResponseWriter, _ string, _ map[string]any) {
		writeJSONReply(w, http.StatusUnauthorized, `{"error":{"message":"Incorrect API key provided: `+openAITestKey+`","type":"invalid_request_error","code":"invalid_api_key"}}`)
	})
	_, err := backend.ListModels(context.Background())
	if err == nil || !strings.Contains(err.Error(), "list models") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), openAITestKey) {
		t.Fatalf("error leaks the API key: %v", err)
	}
}
