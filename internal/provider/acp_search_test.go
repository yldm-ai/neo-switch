package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/yldm-ai/neo-switch/internal/translate"
)

// searchAnswerJSON is the fake's answer to a search: a link the search returned, the same page again, a link the model wrote from memory, and a search link that does not resolve.
const searchAnswerJSON = `{"text":"Go 1.24 adds generic type aliases.","results":[` +
	`{"title":"go.dev","url":"` + groundingRedirect + `go"},` +
	`{"title":"go.dev","url":"` + groundingRedirect + `go-again"},` +
	`{"title":"Invented","url":"https://made-up.example/page"},` +
	`{"title":"blog.example.com","url":"` + groundingRedirect + `blog"},` +
	`{"title":"broken","url":"` + groundingRedirect + `garbled"}]}`

func fakeGrounding(t *testing.T) {
	t.Helper()
	pages := map[string]string{
		groundingRedirect + "go":       "https://go.dev/doc/go1.24",
		groundingRedirect + "go-again": "https://go.dev/doc/go1.24",
		groundingRedirect + "blog":     "https://blog.example.com/go124",
	}
	previous := resolveGroundingLink
	resolveGroundingLink = func(_ context.Context, link string) (string, error) {
		// The links come from the model's text: anything else could point NeoSwitch at this computer or its network.
		if !strings.HasPrefix(link, groundingRedirect) {
			t.Errorf("resolved %s, which is not a link of Google's search", link)
		}
		if page, ok := pages[link]; ok {
			return page, nil
		}
		return "", errors.New("not a redirect")
	}
	t.Cleanup(func() { resolveGroundingLink = previous })
}

func searchRequest(search translate.WebSearch) translate.Request {
	req := textRequest("Perform a web search for the query: go 1.24")
	req.WebSearch = &search
	return req
}

func TestACPSearchWebReturnsOnlyPagesTheSearchFound(t *testing.T) {
	fakeGrounding(t)
	a := newFakeACP(t, "search-twice")
	var live []string
	result, err := a.SearchWeb(context.Background(), searchRequest(translate.WebSearch{}), func(q string) { live = append(live, q) })
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"query 1", "query 2"}; !slices.Equal(live, want) || !slices.Equal(result.Queries, want) {
		t.Fatalf("queries live %v, result %v", live, result.Queries)
	}
	want := []translate.SearchLink{{Title: "go.dev", URL: "https://go.dev/doc/go1.24"}, {Title: "blog.example.com", URL: "https://blog.example.com/go124"}}
	if !slices.Equal(result.Links, want) {
		t.Fatalf("links %+v", result.Links)
	}
	if result.Summary != "Go 1.24 adds generic type aliases." || result.ErrorCode != "" || result.Usage == nil {
		t.Fatalf("result %+v", result)
	}
	// The search session is never kept for reuse.
	a.conn.mu.Lock()
	defer a.conn.mu.Unlock()
	if len(a.conn.idle) != 0 {
		t.Fatalf("idle sessions after a search: %d", len(a.conn.idle))
	}
}

func TestACPSearchWebHoldsLinksToTheRequestedSites(t *testing.T) {
	fakeGrounding(t)
	a := newFakeACP(t, "search")
	result, err := a.SearchWeb(context.Background(), searchRequest(translate.WebSearch{AllowedDomains: []string{"example.com"}}), func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Links) != 1 || result.Links[0].URL != "https://blog.example.com/go124" {
		t.Fatalf("links %+v", result.Links)
	}
	if strings.Contains(result.Summary, "generic type aliases") {
		t.Fatal("the summary can quote any site, so it must be left out when sites are restricted")
	}
	result, err = a.SearchWeb(context.Background(), searchRequest(translate.WebSearch{BlockedDomains: []string{"go.dev"}}), func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Links) != 1 || result.Links[0].URL != "https://blog.example.com/go124" {
		t.Fatalf("links %+v", result.Links)
	}
}

func TestACPSearchWebWithoutASearchIsAnError(t *testing.T) {
	fakeGrounding(t)
	a := newFakeACP(t, "search-none")
	result, err := a.SearchWeb(context.Background(), searchRequest(translate.WebSearch{}), func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCode != "unavailable" || len(result.Links) != 0 || result.Summary != "" {
		t.Fatalf("an answer from memory must not pass for search results: %+v", result)
	}
}

func TestACPSearchWebStopsPastMaxUses(t *testing.T) {
	fakeGrounding(t)
	a := newFakeACP(t, "search-twice")
	result, err := a.SearchWeb(context.Background(), searchRequest(translate.WebSearch{MaxUses: 1}), func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCode != "max_uses_exceeded" || !slices.Equal(result.Queries, []string{"query 1"}) {
		t.Fatalf("result %+v", result)
	}
}

func TestOnSites(t *testing.T) {
	for host, want := range map[string]bool{"go.dev": true, "pkg.go.dev": true, "notgo.dev": false, "go.dev.evil.com": false} {
		if got := onSites(host, []string{".Go.dev"}); got != want {
			t.Errorf("%s: %v", host, got)
		}
	}
}

func TestResolveGroundingLinkReadsOneRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			http.Redirect(w, r, "https://go.dev/doc/go1.24", http.StatusFound)
		case "/script":
			w.Header().Set("Location", "javascript:alert(1)")
			w.WriteHeader(http.StatusFound)
		default:
			_, _ = w.Write([]byte("no redirect"))
		}
	}))
	defer srv.Close()
	if page, err := resolveGroundingLink(context.Background(), srv.URL+"/ok"); err != nil || page != "https://go.dev/doc/go1.24" {
		t.Fatalf("%q %v", page, err)
	}
	for _, path := range []string{"/script", "/page"} {
		if page, err := resolveGroundingLink(context.Background(), srv.URL+path); err == nil {
			t.Fatalf("%s resolved to %q", path, page)
		}
	}
}
