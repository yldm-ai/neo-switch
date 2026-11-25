package daemon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	envBaseURL   = "ANTHROPIC_BASE_URL"
	envAuthToken = "ANTHROPIC_AUTH_TOKEN"
	// envHeaders carries NeoSwitch's token. ANTHROPIC_AUTH_TOKEN would replace the user's Claude login in the session, which turns off claude.ai connectors and makes every request NeoSwitch's to answer; a header of its own leaves the login as it is.
	envHeaders = "ANTHROPIC_CUSTOM_HEADERS"
	// envExtraCAs makes the session trust NeoSwitch's local CA, so it can reach the daemon over TLS.
	envExtraCAs = "NODE_EXTRA_CA_CERTS"
	// EnvUseGateway is Claude Code's gateway mode switch, set by gateway launchers such as `everyapi use claude`. In gateway mode a session takes ANTHROPIC_BASE_URL from its process environment over project settings, so the project's proxy entries do not reach it: it keeps its gateway, which is what it should, because /neoswitch:on refuses gateway sessions and NeoSwitch could only pass their requests on to an upstream that does not know the gateway's credential. NeoSwitch versions up to 0.4.1 wrote gatewayOff into the project to force such sessions through the proxy, which sent their gateway credential to Anthropic; Enable, Disable and the SessionStart hook remove that entry.
	EnvUseGateway = "CLAUDE_CODE_USE_GATEWAY"
	gatewayOff    = "0"
	// envHintHeaders makes Claude Code say in headers which part of it sent each request: the main thread, a subagent, a compaction, or a background request such as a prompt suggestion that shares the main thread's history. The Antigravity route needs that to keep the main thread's ACP session for the main thread. Claude Code otherwise sends these headers only to its own API, depending on a remote flag. The headers are Claude Code's own hints for gateways, so passed-through requests keep working as before.
	envHintHeaders = "CLAUDE_CODE_GATEWAY_HINT_HEADERS"
	hintHeadersOn  = "1"
	// TokenHeader is the header that carries NeoSwitch's token.
	TokenHeader = "X-NeoSwitch-Token"
)

// settingsPath is the per-user, per-project Claude Code settings file. It is the narrowest scope that can set ANTHROPIC_BASE_URL: other projects and other people's checkouts keep talking to Anthropic.
func settingsPath(project string) string {
	return filepath.Join(project, ".claude", "settings.local.json")
}

// Route is what a switched session needs beyond the daemon's address: the CA file it trusts, and the custom headers of the user's environment that NeoSwitch's must not replace.
type Route struct {
	Bundle      string
	UserHeaders string
}

// Headers is the ANTHROPIC_CUSTOM_HEADERS value for a switched session: the user's own headers, then NeoSwitch's token.
func (r Route) Headers(s State) string {
	own := TokenHeader + ": " + s.Token
	if strings.TrimSpace(r.UserHeaders) == "" {
		return own
	}
	return strings.TrimRight(r.UserHeaders, "\n") + "\n" + own
}

// Enable points Claude Code sessions in project at the shared proxy by adding its env entries to settings.local.json. Every other key in the file is preserved, and entries an earlier NeoSwitch wrote are replaced.
func Enable(project string, s State, r Route) error {
	var saveErr error
	err := editSettings(project, true, func(env map[string]any) {
		// The user's own values go to the state folder before they are replaced, or are not replaced at all.
		if saveErr = saveReplaced(project, s, env); saveErr != nil {
			return
		}
		removeLegacy(env, s)
		clearGatewayOverride(env, s)
		env[envBaseURL] = s.BaseURL()
		env[envHeaders] = r.Headers(s)
		env[envExtraCAs] = r.Bundle
		if _, set := env[envHintHeaders]; !set {
			env[envHintHeaders] = hintHeadersOn
		}
	})
	if saveErr != nil {
		return fmt.Errorf("NeoSwitch could not keep a copy of this project's own %s settings, so it left the project as it was: %w", strings.Join(replaceable, " and "), saveErr)
	}
	return err
}

// replaceable are the entries Enable writes over that a user may have set in the project's settings: a CA file of their own (a company proxy's, for one), whose certificates the bundle includes, and headers of their own, which the token header joins. Disable puts the user's values back, so turning NeoSwitch off leaves the file as the user had it.
var replaceable = []string{envHeaders, envExtraCAs}

// replacedPath is where the user's values of replaceable entries in project are kept while NeoSwitch routes it.
func replacedPath(project string, s State) string {
	sum := sha256.Sum256([]byte(project))
	return filepath.Join(s.dir, "replaced", hex.EncodeToString(sum[:8])+".json")
}

// ownValue reports whether value of a replaceable entry is NeoSwitch's own.
func ownValue(key, value string, s State) bool {
	switch key {
	case envHeaders:
		return strings.Contains(value, TokenHeader+":")
	case envExtraCAs:
		return value == BundlePath(s.dir)
	}
	return false
}

// saveReplaced keeps the user's values of replaceable entries in env, when it has any; a later Enable finds NeoSwitch's values there and keeps the copy it has.
func saveReplaced(project string, s State, env map[string]any) error {
	if s.dir == "" {
		return nil
	}
	saved := map[string]string{}
	for _, key := range replaceable {
		if value, _ := env[key].(string); value != "" && !ownValue(key, value, s) {
			saved[key] = value
		}
	}
	if len(saved) == 0 {
		return nil
	}
	path := replacedPath(project, s)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, _ := json.Marshal(saved)
	return WriteFileAtomic(path, data, 0o600)
}

// loadReplaced returns the user's values Enable kept for project, if any.
func loadReplaced(project string, s State) map[string]string {
	saved := map[string]string{}
	if s.dir == "" {
		return saved
	}
	if data, err := os.ReadFile(replacedPath(project, s)); err == nil {
		_ = json.Unmarshal(data, &saved)
	}
	return saved
}

// ClearGatewayOverride removes the gateway mode override that versions up to 0.4.1 wrote with their proxy entries, and reports whether the project had one. A running session keeps env it already applied, so this fixes the sessions that start afterwards.
func ClearGatewayOverride(project string, s State) (bool, error) {
	cleared := false
	err := editSettings(project, false, func(env map[string]any) {
		cleared = clearGatewayOverride(env, s)
	})
	return cleared, err
}

// clearGatewayOverride removes gatewayOff when it sits next to this state's proxy address, where only NeoSwitch put it; a "0" the user set in a project NeoSwitch does not route is theirs and stays.
func clearGatewayOverride(env map[string]any, s State) bool {
	if env[EnvUseGateway] != gatewayOff || env[envBaseURL] != s.BaseURL() {
		return false
	}
	delete(env, EnvUseGateway)
	return true
}

// Disable removes the env entries Enable wrote, and those of NeoSwitch versions from before TLS. Entries the user changed to point elsewhere are left alone.
func Disable(project string, s State) error {
	saved := loadReplaced(project, s)
	restored := false
	// restore puts back the user's own value of key, or removes NeoSwitch's.
	restore := func(env map[string]any, key string) {
		if value, ok := saved[key]; ok {
			env[key] = value
			return
		}
		delete(env, key)
	}
	err := editSettings(project, false, func(env map[string]any) {
		removeLegacy(env, s)
		if env[envBaseURL] != s.BaseURL() {
			return
		}
		restored = true
		delete(env, envBaseURL)
		if headers, _ := env[envHeaders].(string); strings.Contains(headers, TokenHeader+": "+s.Token) {
			restore(env, envHeaders)
		}
		if cas, _ := env[envExtraCAs].(string); s.dir != "" && cas == BundlePath(s.dir) {
			restore(env, envExtraCAs)
		}
		if env[EnvUseGateway] == gatewayOff {
			delete(env, EnvUseGateway)
		}
		// Like gatewayOff, a "1" next to NeoSwitch's address is taken to be NeoSwitch's.
		if env[envHintHeaders] == hintHeadersOn {
			delete(env, envHintHeaders)
		}
	})
	if err == nil && restored && s.dir != "" {
		_ = os.Remove(replacedPath(project, s))
	}
	return err
}

// removeLegacy removes the entries of a NeoSwitch from before TLS: a plain-HTTP loopback address with this state's token.
func removeLegacy(env map[string]any, s State) {
	url, _ := env[envBaseURL].(string)
	if !strings.HasPrefix(url, "http://127.0.0.1:") || env[envAuthToken] != s.Token {
		return
	}
	delete(env, envBaseURL)
	delete(env, envAuthToken)
	if env[EnvUseGateway] == gatewayOff {
		delete(env, EnvUseGateway)
	}
}

func Enabled(project string, s State) (bool, error) {
	settings, err := readSettings(project)
	if err != nil {
		return false, err
	}
	env, _ := settings["env"].(map[string]any)
	return env[envBaseURL] == s.BaseURL(), nil
}

func readSettings(project string) (map[string]any, error) {
	data, err := os.ReadFile(settingsPath(project))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	settings := map[string]any{}
	if len(bytes.TrimSpace(data)) == 0 {
		return settings, nil
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, invalidSettings(settingsPath(project), data, err)
	}
	return settings, nil
}

// invalidSettings explains a settings file NeoSwitch cannot parse, pointing at the line of a syntax error, so the user can fix it without knowing JSON error messages. The file is never rewritten in that state.
func invalidSettings(path string, data []byte, err error) error {
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		offset := min(int(syntax.Offset), len(data))
		line := 1 + bytes.Count(data[:offset], []byte("\n"))
		return fmt.Errorf("NeoSwitch cannot read %s: line %d has a formatting mistake, such as a missing or extra comma. Fix that line and try again", path, line)
	}
	return fmt.Errorf("NeoSwitch cannot read %s: it must hold a single JSON object ({ ... }). Fix the file and try again", path)
}

// editSettings applies edit to the project's env and writes the file only when that changed something. Without create, a missing file is left missing: the daemon disables projects on its own, after the session ends, when a worktree may already be gone. Claude Code writes the same file, for example to save a permission rule, so a file that changed while being edited is read again once rather than overwritten.
func editSettings(project string, create bool, edit func(env map[string]any)) error {
	path := settingsPath(project)
	for attempt := 0; ; attempt++ {
		before, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) && !create {
			return nil
		}
		settings, err := readSettings(project)
		if err != nil {
			return err
		}
		original, _ := json.Marshal(settings)
		env, ok := settings["env"].(map[string]any)
		if !ok {
			if _, present := settings["env"]; present {
				return fmt.Errorf("NeoSwitch cannot change %s: its \"env\" entry is not a JSON object ({ ... }). Fix the file and try again", path)
			}
			env = map[string]any{}
		}
		edit(env)
		if len(env) == 0 {
			delete(settings, "env")
		} else {
			settings["env"] = env
		}
		if edited, _ := json.Marshal(settings); bytes.Equal(edited, original) && before != nil {
			return nil
		}
		if after, err := os.Stat(path); attempt == 0 && !sameFileState(before, after, err) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		data, _ := json.MarshalIndent(settings, "", "  ")
		return WriteFileAtomic(path, append(data, '\n'), 0o600)
	}
}

// sameFileState reports whether the file looks as it did when it was read: both missing, or the same size and modification time.
func sameFileState(before os.FileInfo, after os.FileInfo, afterErr error) bool {
	if before == nil {
		return errors.Is(afterErr, os.ErrNotExist)
	}
	return afterErr == nil && after.Size() == before.Size() && after.ModTime().Equal(before.ModTime())
}
