package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/yldm-ai/neo-switch/internal/translate"
)

// Web search.
//
// Claude Code's WebSearch tool asks Anthropic's API to run its web search server tool. Antigravity searches with Google instead: agy's own search_web tool, which runs without asking for permission because it touches nothing on this computer. ACP reports each search's query but not what it found, so the model lists the pages it was given, and NeoSwitch keeps only those it can check: a link of Google's grounding redirect that resolves to a page. A link the model wrote from memory, or copied wrongly, is dropped, so every link Claude Code receives came from the search.

// groundingRedirect is where Google's search grounding sends a link to a result. Only links under it are followed, and only one step: the links come from the model's text, which nothing else vouches for.
const groundingRedirect = "https://vertexaisearch.cloud.google.com/grounding-api-redirect/"

const (
	// searchResolveTimeout bounds the resolution of one link.
	searchResolveTimeout = 3 * time.Second
	// searchResolveParallel is how many links are resolved at once.
	searchResolveParallel = 8
)

// resolveGroundingLink returns the page a grounding redirect points to. It is a variable so tests need not reach Google.
var resolveGroundingLink = func(ctx context.Context, link string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, searchResolveTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return "", err
	}
	resp, err := groundingClient.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	target, err := url.Parse(resp.Header.Get("Location"))
	if resp.StatusCode < 300 || resp.StatusCode > 399 || err != nil || (target.Scheme != "https" && target.Scheme != "http") || target.Host == "" {
		return "", fmt.Errorf("grounding redirect answered %d", resp.StatusCode)
	}
	return target.String(), nil
}

// groundingClient reads the redirect without following it.
var groundingClient = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// searchAnswer is the JSON the search prompt asks for.
type searchAnswer struct {
	Text    string `json:"text"`
	Results []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	} `json:"results"`
}

// SearchWeb runs a web search for Claude Code's WebSearch tool in a session of its own, which is never kept: the search's own prompt is no conversation to continue. onQuery is called with each query agy searches, as it searches.
func (a *AntigravityACP) SearchWeb(ctx context.Context, req translate.Request, onQuery func(string)) (translate.SearchResult, error) {
	search := req.WebSearch
	if search == nil {
		return translate.SearchResult{}, errors.New("not a web search request")
	}
	if a.unguarded.Load() {
		return translate.SearchResult{}, unguardedError
	}
	c, err := a.connection(ctx)
	if err != nil {
		return translate.SearchResult{}, err
	}
	defer c.release()
	// The search answers the main thread's WebSearch tool, so the main model runs it.
	s, err := a.newSession(ctx, c, a.model, "")
	if err != nil {
		return translate.SearchResult{}, err
	}
	defer c.closeSessions([]*acpSession{s})

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu       sync.Mutex
		raw      strings.Builder
		seen     = map[acp.ToolCallId]bool{}
		queries  []string
		exceeded bool
	)
	// Queries reach onQuery on this goroutine; the SDK's notification goroutine only queues them.
	found := newChunkQueue[string]()
	onUpdate := func(update acp.SessionUpdate) {
		if chunk := update.AgentMessageChunk; chunk != nil && chunk.Content.Text != nil {
			mu.Lock()
			raw.WriteString(chunk.Content.Text.Text)
			mu.Unlock()
			return
		}
		query, id := searchQuery(update)
		if query == "" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if seen[id] {
			return
		}
		seen[id] = true
		if search.MaxUses > 0 && len(queries) >= search.MaxUses {
			exceeded = true
			cancel()
			return
		}
		queries = append(queries, query)
		found.push(query)
	}
	type outcome struct {
		result acp.PromptResponse
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := a.prompt(ctx, c, s, acpPiece{text: searchPrompt(req, c.imagePrompts)}, acp.SessionConfigValueId(strings.ToLower(string(req.ThinkingLevel))), onUpdate)
		done <- outcome{result, err}
	}()
	var out outcome
	for waiting := true; waiting; {
		select {
		case <-found.ready:
		case out = <-done:
			waiting = false
		}
		for _, query := range found.drain() {
			onQuery(query)
		}
	}
	mu.Lock()
	answer, ran, overLimit := raw.String(), queries, exceeded
	mu.Unlock()
	if overLimit {
		return translate.SearchResult{Queries: ran, ErrorCode: "max_uses_exceeded"}, nil
	}
	if out.err != nil {
		return translate.SearchResult{}, out.err
	}
	result := translate.SearchResult{Queries: ran, Usage: acpFinalResponse(nil, out.result).UsageMetadata}
	if len(ran) == 0 {
		// An answer without a search would reach Claude Code as search results.
		acpLogger.Warn("Antigravity answered a web search request without searching")
		result.ErrorCode = "unavailable"
		return result, nil
	}
	var parsed searchAnswer
	if err := decodeSearchAnswer(answer, &parsed); err != nil {
		acpLogger.Warn("Antigravity's web search answer could not be read", "error", err)
		result.ErrorCode = "unavailable"
		return result, nil
	}
	result.Links = resolveLinks(ctx, parsed, search)
	result.Summary = strings.TrimSpace(parsed.Text)
	if len(search.AllowedDomains) > 0 || len(search.BlockedDomains) > 0 {
		// Google's search cannot be held to the sites, only its links can, and the summary may quote any of them.
		result.Summary = "NeoSwitch left out the summary of these results: Antigravity's search cannot be limited to the requested sites, so only the links from allowed sites are listed."
	}
	return result, nil
}

// searchQuery returns the query of a search agy starts, and the tool call's ID. ACP marks searches with kind "search"; the query is in the call's raw input.
func searchQuery(update acp.SessionUpdate) (string, acp.ToolCallId) {
	call := update.ToolCall
	if call == nil || call.Kind != acp.ToolKindSearch {
		return "", ""
	}
	var input struct {
		Query string `json:"query"`
	}
	if json.Unmarshal(call.RawInput, &input) != nil {
		return "", ""
	}
	return strings.TrimSpace(input.Query), call.ToolCallId
}

// searchPrompt asks agy to search for what Claude Code's request asks, then list the pages.
func searchPrompt(req translate.Request, acceptImages bool) string {
	var b strings.Builder
	b.WriteString("Claude Code needs a web search. Search the web with your web search tool for what the request below asks; search more than once if one search is not enough. Use no other tool.")
	if domains := req.WebSearch.AllowedDomains; len(domains) > 0 {
		fmt.Fprintf(&b, " Search only these sites, adding site: to each query: %s.", strings.Join(domains, ", "))
	}
	if domains := req.WebSearch.BlockedDomains; len(domains) > 0 {
		fmt.Fprintf(&b, " Leave out pages from these sites: %s.", strings.Join(domains, ", "))
	}
	b.WriteString(` Then reply with exactly one JSON object and no markdown: {"text":"a short summary of what the pages say about the request","results":[{"title":"the page's title","url":"the page's link exactly as your search returned it"}]}. List every page your searches returned, and copy each link exactly.` + "\n\nRequest:\n")
	r := &promptRenderer{acceptImages: acceptImages}
	for _, content := range req.Contents {
		b.WriteString(r.entry(content).text)
	}
	return b.String()
}

// decodeSearchAnswer reads the first JSON object of the answer, accepting a code fence and raw control characters in strings.
func decodeSearchAnswer(raw string, into *searchAnswer) error {
	clean := stripCodeFence(raw)
	if start := strings.IndexByte(clean, '{'); start > 0 {
		clean = clean[start:]
	}
	err := json.NewDecoder(strings.NewReader(clean)).Decode(into)
	if err != nil {
		err = json.NewDecoder(strings.NewReader(repairJSONStrings(clean))).Decode(into)
	}
	return err
}

// resolveLinks keeps the pages the search returned: grounding redirects that resolve, at most once per page, on the allowed sites and off the blocked ones. A page the redirect does not name is shown by its host.
func resolveLinks(ctx context.Context, answer searchAnswer, search *translate.WebSearch) []translate.SearchLink {
	type candidate struct {
		title, link, page string
	}
	var candidates []*candidate
	for _, result := range answer.Results {
		link := strings.TrimSpace(result.URL)
		if !strings.HasPrefix(link, groundingRedirect) {
			continue
		}
		candidates = append(candidates, &candidate{title: strings.TrimSpace(result.Title), link: link})
	}
	if dropped := len(answer.Results) - len(candidates); dropped > 0 {
		acpLogger.Warn("Antigravity listed web search results that are not search links; they were left out", "dropped", dropped)
	}
	slots := make(chan struct{}, searchResolveParallel)
	var wg sync.WaitGroup
	for _, c := range candidates {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			if page, err := resolveGroundingLink(ctx, c.link); err == nil {
				c.page = page
			}
		}()
	}
	wg.Wait()
	var links []translate.SearchLink
	seen := map[string]bool{}
	unresolved := 0
	for _, c := range candidates {
		if c.page == "" {
			unresolved++
			continue
		}
		page, _ := url.Parse(c.page)
		host := strings.ToLower(page.Hostname())
		if seen[c.page] || (len(search.AllowedDomains) > 0 && !onSites(host, search.AllowedDomains)) || onSites(host, search.BlockedDomains) {
			continue
		}
		seen[c.page] = true
		title := c.title
		if title == "" {
			title = host
		}
		links = append(links, translate.SearchLink{Title: title, URL: c.page})
	}
	if unresolved > 0 {
		acpLogger.Warn("some web search links did not resolve; they were left out", "unresolved", unresolved)
	}
	return links
}

// onSites reports whether host is one of the domains or below one.
func onSites(host string, domains []string) bool {
	for _, domain := range domains {
		domain = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(domain), "."))
		if domain != "" && (host == domain || strings.HasSuffix(host, "."+domain)) {
			return true
		}
	}
	return false
}
