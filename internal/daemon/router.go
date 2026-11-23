package daemon

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// router decides, for every request a routed session sends, whether Gemini answers it. Only the model requests of sessions that ran /neoswitch:on go to the Gemini proxy, except auto mode's safety check (autocheck.go); everything else, such as the request of another window that applied the project settings, or of a session that ran /neoswitch:off, passes through to Anthropic with the user's own credential, so it keeps answering with Claude.
type router struct {
	state       State
	sessions    *sessionSet
	gemini      http.Handler
	passthrough http.Handler
}

func (rt *router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// A page in the user's browser can reach a loopback port through a domain it controls (DNS rebinding); its requests name that domain as the host.
	if r.Host != rt.state.Listen {
		writeRouterError(w, "This request did not come from a Claude Code session switched by NeoSwitch.")
		return
	}
	token := strings.TrimSpace(r.Header.Get(TokenHeader))
	if subtle.ConstantTimeCompare([]byte(token), []byte(rt.state.Token)) != 1 {
		// Never 401: Claude Code would blame the user's Claude login.
		writeRouterError(w, "This session's NeoSwitch access key is out of date. Run /neoswitch:on to switch this session again, or run /neoswitch:off and start a new session to go back to Claude.")
		return
	}
	r.Header.Del(TokenHeader)
	if rt.sessions.sawRequest(r.Header.Get(sessionHeader)) && modelRequest(r) {
		if rt.passthrough != nil {
			if check, body := autoModeCheck(r); check {
				rt.judgeWithClaude(w, r, body)
				return
			}
		}
		// The user's Claude credential stays out of the Gemini side.
		r.Header.Del("Authorization")
		r.Header.Del("X-Api-Key")
		rt.gemini.ServeHTTP(w, r)
		return
	}
	if rt.passthrough == nil {
		writeRouterError(w, "NeoSwitch cannot pass this session's requests on to Claude. Run /neoswitch:off and start a new session to go back to Claude.")
		return
	}
	rt.passthrough.ServeHTTP(w, r)
}

// modelRequest is what the Gemini proxy answers: the messages API and its token count. Everything else a session asks its base URL for, such as account or usage endpoints, belongs to Anthropic.
func modelRequest(r *http.Request) bool {
	return r.Method == http.MethodPost && (r.URL.Path == "/v1/messages" || r.URL.Path == "/v1/messages/count_tokens")
}

func writeRouterError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]string{"type": "invalid_request_error", "message": message}})
}

// AnthropicAPI is where passthrough requests go unless the user's sessions had another ANTHROPIC_BASE_URL.
const AnthropicAPI = "https://api.anthropic.com"

// NewPassthrough forwards requests unchanged to upstream, Anthropic's API unless s.Upstream names another, such as the user's company gateway. It reads no body and logs nothing: the request carries the user's Claude credential and conversation, which only the upstream may see. Responses stream through as they arrive.
func NewPassthrough(s State) (http.Handler, error) {
	upstream := s.Upstream
	if upstream == "" {
		upstream = AnthropicAPI
	}
	target, err := url.Parse(upstream)
	if err != nil {
		return nil, err
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = target.Host
			r.Out.Header.Del(TokenHeader)
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// 529 lets Claude Code retry a network hiccup on the way to Anthropic.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(529)
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]string{"type": "overloaded_error", "message": "NeoSwitch could not reach " + target.Host + " to pass this request on. Check the network and try again."}})
		},
		ErrorLog: log.New(io.Discard, "", 0),
	}
	proxy.Transport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 10 * time.Minute,
		IdleConnTimeout:       90 * time.Second,
	}
	return proxy, nil
}
