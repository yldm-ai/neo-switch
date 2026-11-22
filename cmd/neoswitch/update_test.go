package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestUpdateCheckDue(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name    string
		checked time.Time
		want    bool
	}{
		{"never checked", time.Time{}, true},
		{"just checked", now.Add(-time.Minute), false},
		{"interval passed", now.Add(-updateCheckInterval), true},
		{"clock moved back", now.Add(time.Hour), true},
	}
	for _, c := range cases {
		if got := updateCheckDue(updateCache{CheckedAt: c.checked}, now); got != c.want {
			t.Errorf("%s: updateCheckDue = %v, want %v", c.name, got, c.want)
		}
	}
}

func withVersion(t *testing.T, v string) {
	t.Helper()
	old := version
	version = v
	t.Cleanup(func() { version = old })
}

func TestUpdateNotice(t *testing.T) {
	dir := t.TempDir()
	withVersion(t, "v0.2.0+source.abc")
	t.Setenv(pluginEnv, "1")
	t.Setenv("NEOSWITCH_NO_UPDATE_CHECK", "")
	t.Setenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "")

	if got := updateNotice(dir); got != "" {
		t.Fatalf("notice without a cache = %q, want none", got)
	}
	if err := saveUpdateCache(dir, updateCache{CheckedAt: time.Now(), Latest: "v0.3.0"}); err != nil {
		t.Fatal(err)
	}
	got := updateNotice(dir)
	for _, want := range []string{"v0.3.0 is available", "this is v0.2.0.", "claude plugin update neoswitch@neoswitch", "close all Claude Code windows"} {
		if !strings.Contains(got, want) {
			t.Errorf("notice %q does not contain %q", got, want)
		}
	}

	t.Setenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1")
	if got := updateNotice(dir); got != "" {
		t.Errorf("notice with nonessential traffic disabled = %q, want none", got)
	}
	t.Setenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "")
	t.Setenv(pluginEnv, "")
	if got := updateNotice(dir); got != "" {
		t.Errorf("notice outside the plugin = %q, want none", got)
	}
}

func TestRunUpdateCheckRecordsLatest(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0.3.1\n"))
	}))
	defer server.Close()
	if err := runUpdateCheck(dir, server.URL); err != nil {
		t.Fatal(err)
	}
	cache := loadUpdateCache(dir)
	if cache.Latest != "v0.3.1" || cache.CheckedAt.IsZero() {
		t.Fatalf("cache = %+v, want latest v0.3.1 with a check time", cache)
	}
}

// 网络出错或返回的不是版本号时，保留已知的最新版本，已经显示的提示不会因为断网消失。
func TestRunUpdateCheckKeepsLatestOnFailure(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"server error": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusBadGateway) },
		"not a version": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("<html>captive portal</html>"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := saveUpdateCache(dir, updateCache{Latest: "v0.3.0"}); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(handler)
			defer server.Close()
			if err := runUpdateCheck(dir, server.URL); err == nil {
				t.Fatal("runUpdateCheck succeeded, want an error")
			}
			if got := loadUpdateCache(dir).Latest; got != "v0.3.0" {
				t.Fatalf("latest after a failed check = %q, want v0.3.0", got)
			}
		})
	}
}

// 测试进程不是 NeoSwitch，startUpdateCheck 绝不能在测试里拉起子进程；没有插件标记时它什么都不做。
func TestStartUpdateCheckOutsidePluginDoesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(pluginEnv, "")
	startUpdateCheck(dir)
	if cache := loadUpdateCache(dir); !cache.CheckedAt.IsZero() {
		t.Fatalf("cache = %+v, want no check recorded", cache)
	}
}

func TestJoinNotes(t *testing.T) {
	if got := joinNotes("a", "", "b"); got != "a b" {
		t.Fatalf("joinNotes = %q, want %q", got, "a b")
	}
	if got := joinNotes("a", ""); got != "a" {
		t.Fatalf("joinNotes = %q, want %q", got, "a")
	}
}
