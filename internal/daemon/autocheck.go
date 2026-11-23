package daemon

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
)

// Auto mode's safety check. With CLAUDE_CODE_AUTO_MODE_SERVER=0, Claude Code asks a Claude model, through the session's base URL, whether each action may run. In a session switched to Gemini that request would reach Gemini, which would then judge its own actions, and judge them badly: Claude Code reads the verdict by the reply format, stop sequence and thresholds of its Claude classifier. So the check passes through to Anthropic with the user's credential, as in a Claude session, and Gemini answers it only when Anthropic does not, for example once the user's Claude usage ran out. The server-side check, Claude Code's default, cannot work in such a session at all: its verdict comes with the answer of the model that chose the action, and Anthropic never sees an action Gemini chose. See docs/design-session-routing.md, "Auto mode".

// requestClassHeader is the header in which Claude Code names the kind of request; the safety check is an auxiliary request, never the main thread.
const requestClassHeader = "X-Claude-Code-Request-Class"

// maxCheckBody bounds the body read to recognize the check, as the Gemini proxy bounds what it accepts.
const maxCheckBody = 32 << 20

var routerLogger = slog.New(slog.NewTextHandler(os.Stderr, nil))

// autoModeCheck reports whether r is auto mode's safety check, and returns its body, which it read. It recognizes the check by its shape, never by its wording, which Anthropic changes remotely, nor by its model, which is the session's own when no other is configured: an auxiliary request without tools that wraps the conversation in a text block "<transcript>\n" and a later one "</transcript>\n". It errs towards missing the check, which leaves it with Gemini, as before.
func autoModeCheck(r *http.Request) (bool, []byte) {
	if r.URL.Path != "/v1/messages" || r.Header.Get(requestClassHeader) != "auxiliary" || r.Body == nil {
		return false, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCheckBody+1))
	if err != nil || len(body) > maxCheckBody {
		// Leave the request as it came: what was read goes first, the rest follows.
		r.Body = readCloser{io.MultiReader(bytes.NewReader(body), r.Body), r.Body}
		return false, nil
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	var request struct {
		Tools    []json.RawMessage `json:"tools"`
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &request) != nil || len(request.Tools) > 0 {
		return false, body
	}
	opened := false
	for _, message := range request.Messages {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(message.Content, &blocks) != nil {
			continue
		}
		for _, block := range blocks {
			if block.Type != "text" {
				continue
			}
			switch {
			case block.Text == "<transcript>\n":
				opened = true
			case opened && block.Text == "</transcript>\n":
				return true, body
			}
		}
	}
	return false, body
}

type readCloser struct {
	io.Reader
	io.Closer
}

// judgeWithClaude passes the safety check to Anthropic and, when Anthropic does not answer it, to Gemini.
func (rt *router) judgeWithClaude(w http.ResponseWriter, r *http.Request, body []byte) {
	held := &heldResponse{w: w, header: http.Header{}}
	rt.passthrough.ServeHTTP(held, r)
	if !held.failed {
		return
	}
	routerLogger.Warn("Anthropic did not answer auto mode's safety check, so Gemini answers it", "status", held.status)
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	// The user's Claude credential stays out of the Gemini side.
	r.Header.Del("Authorization")
	r.Header.Del("X-Api-Key")
	rt.gemini.ServeHTTP(w, r)
}

// heldResponse forwards a successful response as it arrives and holds back any other, so the request can go elsewhere.
type heldResponse struct {
	w       http.ResponseWriter
	header  http.Header
	status  int
	failed  bool
	started bool
}

func (h *heldResponse) Header() http.Header { return h.header }

func (h *heldResponse) WriteHeader(status int) {
	if h.started || h.failed {
		return
	}
	if status < 200 || status > 299 {
		h.status, h.failed = status, true
		return
	}
	for key, values := range h.header {
		h.w.Header()[key] = values
	}
	h.started = true
	h.w.WriteHeader(status)
}

func (h *heldResponse) Write(p []byte) (int, error) {
	if !h.started && !h.failed {
		h.WriteHeader(http.StatusOK)
	}
	if h.failed {
		return len(p), nil
	}
	return h.w.Write(p)
}

// FlushError lets a streamed reply through as it arrives.
func (h *heldResponse) FlushError() error {
	if !h.started {
		return nil
	}
	return http.NewResponseController(h.w).Flush()
}
