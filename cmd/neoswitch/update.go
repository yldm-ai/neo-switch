package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yldm-ai/neo-switch/internal/daemon"
)

// Claude Code turns auto-update off for third-party marketplaces such as NeoSwitch's, and marketplace.json cannot turn it on, so most users never receive a fix unless something tells them. The session note does: attach shows a notice when the cached latest version is newer than this binary, and starts a background check when the cache is stale. The check runs in a detached process because Claude Code waits for the SessionStart hook, so a slow network must never delay a session; the notice therefore appears from the session after the check.
//
// The latest version is main's VERSION file rather than GitHub's latest release: a plugin update delivers the release tag that main's .claude-plugin/marketplace.json pins, and the released-version check keeps main's VERSION equal to that pin and the pin on a published release (see CONTRIBUTING.md), so main's VERSION is exactly what updating gets the user.

const (
	updateCheckURL      = "https://raw.githubusercontent.com/yldm-ai/neo-switch/main/VERSION"
	updateCheckInterval = 6 * time.Hour
	updateCheckTimeout  = 15 * time.Second
	// pluginEnv is set by plugin/neoswitch.sh. The notice tells the user to update the plugin, which is wrong advice for a binary installed another way, and tests never set it, so they make no network call.
	pluginEnv = "NEOSWITCH_PLUGIN"
)

// updateCache records the last check. CheckedAt is written before the network call, so sessions opened together, or a check killed before it finishes, do not start another check until the interval has passed.
type updateCache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest,omitempty"`
}

func updateCachePath(dir string) string { return filepath.Join(dir, "update-check.json") }

func loadUpdateCache(dir string) updateCache {
	var cache updateCache
	if data, err := os.ReadFile(updateCachePath(dir)); err == nil {
		_ = json.Unmarshal(data, &cache)
	}
	return cache
}

func saveUpdateCache(dir string, cache updateCache) error {
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return daemon.WriteFileAtomic(updateCachePath(dir), data, 0o600)
}

// updateCheckEnabled reports whether this binary runs from the plugin and the user has not opted out of the network call. Claude Code's DISABLE_AUTOUPDATER is deliberately not an opt-out: people who update by hand are the ones who need the notice.
func updateCheckEnabled() bool {
	if os.Getenv(pluginEnv) != "1" {
		return false
	}
	return !envTrue("NEOSWITCH_NO_UPDATE_CHECK") && !envTrue("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC")
}

func envTrue(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// updateCheckDue reports whether the cache is old enough for a new check. A check time in the future, after a clock change, also counts as due.
func updateCheckDue(cache updateCache, now time.Time) bool {
	age := now.Sub(cache.CheckedAt)
	return age < 0 || age >= updateCheckInterval
}

// startUpdateCheck claims the next check by recording the time, then runs `neoswitch update-check` in its own session. The child gets no stdin, stdout or stderr: session-start.sh reads attach's output until every holder of the pipe exits, so an inherited pipe would make the hook wait for the network after all.
func startUpdateCheck(dir string) {
	if !updateCheckEnabled() {
		return
	}
	cache := loadUpdateCache(dir)
	now := time.Now()
	if !updateCheckDue(cache, now) {
		return
	}
	cache.CheckedAt = now
	if err := saveUpdateCache(dir, cache); err != nil {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(self, "update-check")
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "ANTHROPIC_") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	detach(cmd)
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

// runUpdateCheck fetches the latest version and records it. A failed check keeps the previously known latest version, so a notice already shown does not disappear while the network is down.
func runUpdateCheck(dir, url string) error {
	client := &http.Client{Timeout: updateCheckTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update check: %s returned %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return err
	}
	latest := "v" + strings.TrimSpace(string(body))
	if _, ok := daemon.ParseVersion(latest); !ok {
		return fmt.Errorf("update check: %s returned %q, which is not a version", url, latest)
	}
	cache := loadUpdateCache(dir)
	cache.CheckedAt = time.Now()
	cache.Latest = latest
	return saveUpdateCache(dir, cache)
}

// updateCheckCommand is the hidden `neoswitch update-check` that attach starts in the background.
func updateCheckCommand() error {
	dir, err := daemon.Dir()
	if err != nil {
		return err
	}
	return runUpdateCheck(dir, updateCheckURL)
}

// updateNotice is the note shown while a newer version is known, empty otherwise. It reads only the cache and makes no network call.
func updateNotice(dir string) string {
	if !updateCheckEnabled() {
		return ""
	}
	latest := loadUpdateCache(dir).Latest
	if !daemon.NewerVersion(latest, version) {
		return ""
	}
	current, _, _ := strings.Cut(version, "+")
	return "NeoSwitch " + latest + " is available; this is " + current + ". To update, run `claude plugin marketplace update neoswitch` and then `claude plugin update neoswitch@neoswitch` in a terminal, then close all Claude Code windows and open them again. To get updates automatically, run /plugin, open Marketplaces, choose neoswitch and turn on auto-update."
}
