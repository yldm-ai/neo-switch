package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/anthropic"
	"github.com/yldm-ai/neo-switch/internal/translate"
)

// upstreamTimeout bounds one Gemini call. It matches Claude Code's default API timeout (10 minutes): long agent turns that write large files legitimately run for several minutes.
const upstreamTimeout = 10 * time.Minute

type Backend interface {
	Generate(context.Context, translate.Request) (*genai.GenerateContentResponse, error)
	Stream(context.Context, translate.Request) iter.Seq2[*genai.GenerateContentResponse, error]
	CountTokens(context.Context, translate.Request) (int, error)
}

// WebSearcher is a backend that can run a web search itself, which answers Claude Code's WebSearch tool. onQuery is called with each query as the search runs it, on the caller's goroutine.
type WebSearcher interface {
	SearchWeb(ctx context.Context, req translate.Request, onQuery func(string)) (translate.SearchResult, error)
}

type Config struct {
	Backend Backend
	Model   string
	Token   string
}

func NewHandler(config Config) http.Handler {
	if config.Model == "" {
		config.Model = "gemini-4-neon"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{{
			"id": config.Model, "type": "model", "display_name": config.Model, "owned_by": "google",
		}}})
	})
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		request, translated, ok := decode(w, r, config)
		if !ok {
			return
		}
		if request.MaxTokens <= 0 {
			writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "max_tokens must be greater than zero")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
		defer cancel()
		if translated.WebSearch != nil {
			searchWeb(ctx, w, config.Backend, translated, request)
			return
		}
		if request.Stream {
			streamMessage(ctx, w, config.Backend, translated, request.Model)
			return
		}
		result, err := config.Backend.Generate(ctx, translated)
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		response, err := translate.FromGemini(result, request.Model)
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	})
	mux.HandleFunc("POST /v1/messages/count_tokens", func(w http.ResponseWriter, r *http.Request) {
		_, translated, ok := decode(w, r, config)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), upstreamTimeout)
		defer cancel()
		count, err := config.Backend.CountTokens(ctx, translated)
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, anthropic.CountTokensResponse{InputTokens: count})
	})
	return requestLogger(mux)
}

// decode authenticates, parses and translates a Messages-shaped request, writing the Anthropic error response itself when any step fails.
func decode(w http.ResponseWriter, r *http.Request, config Config) (anthropic.MessagesRequest, translate.Request, bool) {
	if config.Token != "" && !authorized(r, config.Token) {
		// Not 401: Claude Code would blame the user's Claude login. A session gets an outdated key when NeoSwitch's local state was recreated after the session applied it.
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "This session's NeoSwitch access key is out of date. Run /neoswitch:on to switch this session again, or run /neoswitch:off and start a new session to go back to Claude.")
		return anthropic.MessagesRequest{}, translate.Request{}, false
	}
	if config.Backend == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "NeoSwitch has no model to answer with. Run /neoswitch:login, or run /neoswitch:off to continue this session on Claude.")
		return anthropic.MessagesRequest{}, translate.Request{}, false
	}
	var request anthropic.MessagesRequest
	body := http.MaxBytesReader(w, r.Body, 32<<20)
	defer body.Close()
	if err := json.NewDecoder(body).Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("invalid JSON: %v", err))
		return anthropic.MessagesRequest{}, translate.Request{}, false
	}
	translated, err := translate.ToGemini(request)
	if err != nil {
		message := err.Error()
		var unsupported *translate.UnsupportedError
		if errors.As(err, &unsupported) {
			message = unsupported.Message
		}
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", message)
		return anthropic.MessagesRequest{}, translate.Request{}, false
	}
	translated.Origin = translate.Origin{
		Session: r.Header.Get("X-Claude-Code-Session-Id"),
		Class:   r.Header.Get("X-Claude-Code-Request-Class"),
		Kind:    r.Header.Get("X-Claude-1p-Request-Class"),
		Agent:   r.Header.Get("X-Claude-Code-Agent-Id"),
	}
	return request, translated, true
}

// authorized accepts the token the way Anthropic clients send it: ANTHROPIC_AUTH_TOKEN becomes a Bearer token, ANTHROPIC_API_KEY becomes x-api-key.
func authorized(r *http.Request, token string) bool {
	candidates := []string{r.Header.Get("X-NeoSwitch-Token"), r.Header.Get("X-Api-Key")}
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(auth, "Bearer ") {
		candidates = append(candidates, strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")))
	}
	for _, candidate := range candidates {
		if candidate != "" && subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1 {
			return true
		}
	}
	return false
}

// upstreamStatus maps an upstream failure onto the status and error type Claude Code reacts to: 429 and 529 trigger its backoff, 400 is shown as it is and not retried, and "prompt is too long" triggers its context-compaction hint.
//
// NeoSwitch never answers 401 or 403 for a failure of its own or of the Gemini side: Claude Code reads both as a problem with the user's Claude login and tells them to run /login, which would send them to fix a login that works.
func upstreamStatus(err error) (int, string, string) {
	var unavailable *translate.UnavailableError
	if errors.As(err, &unavailable) {
		return http.StatusBadRequest, "invalid_request_error", unavailable.Message
	}
	var retryable *translate.RetryableError
	if errors.As(err, &retryable) {
		return 529, "overloaded_error", retryable.Message
	}
	var rateLimited *translate.RateLimitError
	if errors.As(err, &rateLimited) {
		return http.StatusTooManyRequests, "rate_limit_error", rateLimited.Message
	}
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		message := "upstream returned HTTP " + fmt.Sprint(apiErr.Code) + ": " + apiErr.Message
		switch {
		case apiErr.Code == http.StatusBadRequest:
			lower := strings.ToLower(apiErr.Message)
			if strings.Contains(lower, "input token count") && strings.Contains(lower, "exceeds") {
				message = "prompt is too long: " + apiErr.Message
			}
			if invalidGeminiKey(apiErr) {
				message = "Google rejected the Gemini API key. Check that the key is correct and still active, then start a new Claude Code session."
			}
			return http.StatusBadRequest, "invalid_request_error", message
		case apiErr.Code == http.StatusUnauthorized || apiErr.Code == http.StatusForbidden:
			return http.StatusBadRequest, "invalid_request_error", "The model's provider refused the credentials NeoSwitch uses for it (" + message + "). Check that API key or account, then start a new Claude Code session, or run /neoswitch:off to continue this session on Claude."
		case apiErr.Code == http.StatusNotFound:
			return http.StatusNotFound, "not_found_error", message
		case apiErr.Code == http.StatusRequestEntityTooLarge:
			return http.StatusRequestEntityTooLarge, "request_too_large", message
		case apiErr.Code == http.StatusTooManyRequests:
			return http.StatusTooManyRequests, "rate_limit_error", message
		case apiErr.Code == http.StatusServiceUnavailable:
			return 529, "overloaded_error", message
		case apiErr.Code >= 500:
			return apiErr.Code, "api_error", message
		default:
			return http.StatusBadGateway, "api_error", message
		}
	}
	var translationErr *translate.UpstreamError
	if errors.As(err, &translationErr) {
		return http.StatusBadGateway, "api_error", translationErr.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, "api_error", fmt.Sprintf("The model did not answer within %d minutes. Try again.", int(upstreamTimeout.Minutes()))
	}
	return http.StatusBadGateway, "api_error", "NeoSwitch could not get an answer from the model: " + err.Error()
}

// invalidGeminiKey reports whether Gemini refused the request because of its API key. Gemini answers an invalid key with HTTP 400 and the API_KEY_INVALID reason.
func invalidGeminiKey(e genai.APIError) bool {
	for _, detail := range e.Details {
		if detail["reason"] == "API_KEY_INVALID" {
			return true
		}
	}
	return strings.Contains(strings.ToLower(e.Message), "api key not valid")
}

func writeUpstreamError(w http.ResponseWriter, err error) {
	status, typ, message := upstreamStatus(err)
	writeAPIError(w, status, typ, message)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, status int, typ, message string) {
	writeJSON(w, status, anthropic.ErrorResponse{Type: "error", Error: anthropic.Error{Type: typ, Message: message}})
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deliberately no prompt, response, header, or query logging.
		next.ServeHTTP(w, r)
	})
}

// ---- streaming ----

// streamMessage relays Gemini's server-sent stream as Anthropic SSE. Nothing is written until the first Gemini chunk arrives, so a request that fails up front still gets a real HTTP status Claude Code can retry on; failures after that point are reported with an SSE error event, as Anthropic does.
func streamMessage(ctx context.Context, w http.ResponseWriter, backend Backend, req translate.Request, model string) {
	next, stop := iter.Pull2(backend.Stream(ctx, req))
	defer stop()
	first, err, ok := next()
	if !ok {
		writeUpstreamError(w, &translate.UpstreamError{Reason: "upstream returned an empty stream"})
		return
	}
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	s := &sseWriter{w: w, model: model}
	s.flusher, _ = w.(http.Flusher)
	s.start(first)
	if !s.chunk(first) {
		return
	}
	for {
		resp, err, ok := next()
		if !ok {
			break
		}
		if err != nil {
			s.fail(err)
			return
		}
		if !s.chunk(resp) {
			return
		}
	}
	s.finish()
}

type sseWriter struct {
	w            http.ResponseWriter
	flusher      http.Flusher
	model        string
	index        int
	textOpen     bool
	hasToolUse   bool
	refused      bool
	finishReason genai.FinishReason
	usage        anthropic.Usage
}

func (s *sseWriter) start(first *genai.GenerateContentResponse) {
	h := s.w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	s.usage = translate.Usage(first.UsageMetadata)
	s.event("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": translate.NewMessageID(), "type": "message", "role": "assistant", "model": s.model,
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": anthropic.Usage{InputTokens: s.usage.InputTokens, CacheReadInputTokens: s.usage.CacheReadInputTokens},
	}})
	s.event("ping", map[string]any{"type": "ping"})
}

// chunk emits the events for one Gemini chunk and reports whether streaming should continue.
func (s *sseWriter) chunk(resp *genai.GenerateContentResponse) bool {
	if resp.UsageMetadata != nil {
		s.usage = translate.Usage(resp.UsageMetadata)
	}
	if len(resp.Candidates) == 0 {
		if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
			s.refused = true
		}
		return true
	}
	candidate := resp.Candidates[0]
	if candidate.Content != nil {
		blocks, err := translate.Blocks(candidate.Content.Parts)
		if err != nil {
			s.fail(err)
			return false
		}
		for _, block := range blocks {
			switch block.Type {
			case "text":
				if !s.textOpen {
					s.event("content_block_start", map[string]any{"type": "content_block_start", "index": s.index, "content_block": map[string]any{"type": "text", "text": ""}})
					s.textOpen = true
				}
				s.event("content_block_delta", map[string]any{"type": "content_block_delta", "index": s.index, "delta": map[string]any{"type": "text_delta", "text": block.Text}})
			case "tool_use":
				// Gemini delivers each functionCall whole in one chunk (argument streaming is opt-in and not requested), so a tool call is emitted as one complete block and Claude Code never sees partial arguments.
				s.closeText()
				s.hasToolUse = true
				s.event("content_block_start", map[string]any{"type": "content_block_start", "index": s.index, "content_block": map[string]any{"type": "tool_use", "id": block.ID, "name": block.Name, "input": map[string]any{}}})
				s.event("content_block_delta", map[string]any{"type": "content_block_delta", "index": s.index, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(block.Input)}})
				s.event("content_block_stop", map[string]any{"type": "content_block_stop", "index": s.index})
				s.index++
			}
		}
	}
	if candidate.FinishReason != "" {
		s.finishReason = candidate.FinishReason
	}
	return true
}

func (s *sseWriter) closeText() {
	if !s.textOpen {
		return
	}
	s.event("content_block_stop", map[string]any{"type": "content_block_stop", "index": s.index})
	s.textOpen = false
	s.index++
}

func (s *sseWriter) finish() {
	s.closeText()
	stopReason := "refusal"
	if !s.refused {
		var err error
		if stopReason, err = translate.StopReason(s.finishReason, s.hasToolUse); err != nil {
			s.fail(err)
			return
		}
	}
	s.event("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil}, "usage": s.usage})
	s.event("message_stop", map[string]any{"type": "message_stop"})
}

func (s *sseWriter) fail(err error) {
	_, typ, message := upstreamStatus(err)
	s.event("error", anthropic.ErrorResponse{Type: "error", Error: anthropic.Error{Type: typ, Message: message}})
}

func (s *sseWriter) event(name string, value any) {
	data, _ := json.Marshal(value)
	_, _ = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, data)
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

// ---- web search ----

// searchWeb answers a request for Anthropic's web search server tool with the backend's own search, in the blocks that tool returns: a server_tool_use per query, a web_search_tool_result with the pages found, and the summary as text. Claude Code's WebSearch tool reads exactly these. A streamed answer starts with the first query, so Claude Code shows the search as it runs; a failure before that keeps its HTTP status.
func searchWeb(ctx context.Context, w http.ResponseWriter, backend Backend, req translate.Request, request anthropic.MessagesRequest) {
	searcher, ok := backend.(WebSearcher)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", translate.WebSearchUnsupported().Message)
		return
	}
	var ids []string
	var stream *sseWriter
	begin := func() {
		if stream == nil {
			stream = &sseWriter{w: w, model: request.Model}
			stream.flusher, _ = w.(http.Flusher)
			stream.start(&genai.GenerateContentResponse{})
		}
	}
	onQuery := func(query string) {
		ids = append(ids, "srvtoolu_ns_"+rand.Text())
		if !request.Stream {
			return
		}
		begin()
		stream.serverToolUse(ids[len(ids)-1], query)
	}
	result, err := searcher.SearchWeb(ctx, req, onQuery)
	if err != nil {
		if stream != nil {
			stream.fail(err)
			return
		}
		writeUpstreamError(w, err)
		return
	}
	for len(ids) < len(result.Queries) {
		ids = append(ids, "srvtoolu_ns_"+rand.Text())
	}
	toolUseID := "srvtoolu_ns_" + rand.Text()
	if len(ids) > 0 {
		toolUseID = ids[len(ids)-1]
	}
	found := searchResultBlock(result, toolUseID)
	usage := translate.Usage(result.Usage)
	usage.ServerToolUse = &anthropic.ServerToolUsage{WebSearchRequests: len(result.Queries)}
	if !request.Stream {
		var content []anthropic.ContentBlock
		for i, query := range result.Queries {
			input, _ := json.Marshal(map[string]string{"query": query})
			content = append(content, anthropic.ContentBlock{Type: "server_tool_use", ID: ids[i], Name: "web_search", Input: input})
		}
		content = append(content, found)
		if result.Summary != "" {
			content = append(content, anthropic.ContentBlock{Type: "text", Text: result.Summary})
		}
		writeJSON(w, http.StatusOK, anthropic.Response{
			ID: translate.NewMessageID(), Type: "message", Role: "assistant", Model: request.Model,
			Content: content, StopReason: "end_turn", Usage: usage,
		})
		return
	}
	begin()
	// The result arrives whole in content_block_start, as Anthropic sends it.
	stream.event("content_block_start", map[string]any{"type": "content_block_start", "index": stream.index, "content_block": found})
	stream.event("content_block_stop", map[string]any{"type": "content_block_stop", "index": stream.index})
	stream.index++
	if result.Summary != "" {
		stream.event("content_block_start", map[string]any{"type": "content_block_start", "index": stream.index, "content_block": map[string]any{"type": "text", "text": ""}})
		stream.event("content_block_delta", map[string]any{"type": "content_block_delta", "index": stream.index, "delta": map[string]any{"type": "text_delta", "text": result.Summary}})
		stream.event("content_block_stop", map[string]any{"type": "content_block_stop", "index": stream.index})
		stream.index++
	}
	stream.event("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": usage})
	stream.event("message_stop", map[string]any{"type": "message_stop"})
}

// serverToolUse emits a complete server_tool_use block the way Anthropic streams one: empty input at the start, then the input as one JSON delta.
func (s *sseWriter) serverToolUse(id, query string) {
	input, _ := json.Marshal(map[string]string{"query": query})
	s.event("content_block_start", map[string]any{"type": "content_block_start", "index": s.index, "content_block": map[string]any{"type": "server_tool_use", "id": id, "name": "web_search", "input": map[string]any{}}})
	s.event("content_block_delta", map[string]any{"type": "content_block_delta", "index": s.index, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(input)}})
	s.event("content_block_stop", map[string]any{"type": "content_block_stop", "index": s.index})
	s.index++
}

// searchResultBlock is the web_search_tool_result block for a search: the pages found, or the error Claude Code reports as "Web search error: <code>".
func searchResultBlock(result translate.SearchResult, toolUseID string) anthropic.ContentBlock {
	var content any
	if result.ErrorCode != "" {
		content = map[string]string{"type": "web_search_tool_result_error", "error_code": result.ErrorCode}
	} else {
		pages := make([]map[string]any, 0, len(result.Links))
		for _, link := range result.Links {
			pages = append(pages, map[string]any{"type": "web_search_result", "title": link.Title, "url": link.URL, "encrypted_content": "", "page_age": nil})
		}
		content = pages
	}
	encoded, _ := json.Marshal(content)
	return anthropic.ContentBlock{Type: "web_search_tool_result", ToolUseID: toolUseID, Content: encoded}
}
