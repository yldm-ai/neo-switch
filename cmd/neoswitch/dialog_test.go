package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// In dialog mode the plugin's mod shows the output itself and opens the dialog the closing line names, so the text points at that dialog rather than at a second command.
func TestSignInLinkInDialogMode(t *testing.T) {
	link := "https://accounts.google.com/o/oauth2/auth?state=x"
	for _, tc := range []struct {
		name               string
		withBrowser, again bool
		dialog             string
	}{
		{"code only", false, false, "code"},
		{"browser beside it", true, false, "browser"},
		{"switching accounts", false, true, "again"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(dialogEnv, "1")
			t.Setenv(pluginEnv, "1")
			var out bytes.Buffer
			printSignInLink(&out, link, false, tc.withBrowser, tc.again, false)
			text := out.String()
			lines := strings.Split(strings.TrimSpace(text), "\n")
			if lines[len(lines)-1] != dialogLine+tc.dialog {
				t.Fatalf("last line %q, want the %s dialog", lines[len(lines)-1], tc.dialog)
			}
			if !strings.Contains(text, "\n"+link+"\n") || !strings.Contains(text, "paste it into the dialog") || strings.Contains(text, "followed by the code") {
				t.Fatalf("dialog output:\n%s", text)
			}
		})
	}
	t.Run("skill keeps its instructions", func(t *testing.T) {
		t.Setenv(dialogEnv, "")
		t.Setenv(pluginEnv, "1")
		t.Setenv(claudeCodeExecEnv, "")
		var out bytes.Buffer
		printSignInLink(&out, link, false, false, false, false)
		if text := out.String(); strings.Contains(text, dialogLine) || !strings.Contains(text, "/neoswitch:login followed by the code") {
			t.Fatalf("skill output:\n%s", text)
		}
	})
}

// A code handed over by the dialog arrives on stdin and is never repeated: it may be anything the user pasted by mistake, and the error would put it in the conversation it was kept out of.
func TestPastedSignInCodeFromDialog(t *testing.T) {
	t.Setenv(dialogEnv, "1")
	if code, err := pastedSignInCode("-", strings.NewReader("  "+fakeCode+"\n")); err != nil || code != fakeCode {
		t.Fatalf("code %q, %v", code, err)
	}
	secret := "correct horse battery staple"
	if _, err := pastedSignInCode("-", strings.NewReader(secret)); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("a mistaken paste must fail without repeating it: %v", err)
	}
	if _, err := pastedSignInCode("-", strings.NewReader(strings.Repeat("4/", maxPastedCode))); err == nil {
		t.Fatal("an oversized paste was taken as a code")
	}
	t.Setenv(dialogEnv, "")
	if _, err := pastedSignInCode("-", strings.NewReader(fakeCode)); err == nil {
		t.Fatal("outside the dialog, - is not a code")
	}
}

// The dialog's code goes through the command, and a code meant for a sign-in another window replaced is refused before agy spends its one exchange on the wrong one.
func TestDialogCodeFinishesOnlyTheSignInItsLinkStarted(t *testing.T) {
	dir := fakeSignInEnv(t)
	project := t.TempDir()
	t.Setenv(dialogEnv, "1")
	t.Setenv("FAKE_LINK_STATE", "first")
	if err := antigravityLogin([]string{"--on", project}); err != nil {
		t.Fatal(err)
	}
	first := pendingSignInLink(dir)
	t.Setenv("FAKE_LINK_STATE", "second")
	if err := antigravityLogin([]string{"--on", project}); err != nil {
		t.Fatal(err)
	}
	if pendingSignInLink(dir) == first || first == "" {
		t.Fatalf("the second sign-in did not replace the first (%q)", first)
	}
	t.Setenv(shownLinkEnv, first)
	withStdin(t, fakeCode)
	if err := antigravityLogin([]string{"--on", project, "-"}); err == nil || !strings.Contains(err.Error(), "was replaced") || !strings.Contains(err.Error(), "not used") {
		t.Fatalf("a code for a replaced sign-in: %v", err)
	}
	if got := fakeSignedIn(t); got != "" {
		t.Fatalf("the replaced sign-in's code was spent: signed in by %q", got)
	}
	t.Setenv(shownLinkEnv, pendingSignInLink(dir))
	withStdin(t, fakeCode)
	if err := antigravityLogin([]string{"--on", project, "-"}); err != nil {
		t.Fatal(err)
	}
	if got := fakeSignedIn(t); got != "code" {
		t.Fatalf("signed in by %q", got)
	}
}

func withStdin(t *testing.T, text string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(text); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	saved := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = saved; _ = r.Close() })
}

// The skill tells the user that updating Claude Code shows the link at once, only when the running version is known to be older: where mods do not load at all, an update would not help.
func TestDialogVersionHint(t *testing.T) {
	bin := t.TempDir()
	script := func(name, output string) string {
		path := filepath.Join(bin, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho '"+output+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, tc := range []struct {
		name, exec, want string
	}{
		{"native install, older", "/home/u/.local/share/claude/versions/2.1.300", "This session runs 2.1.300"},
		{"native install, new enough", "/home/u/.local/share/claude/versions/2.1.326", ""},
		{"native install, newer minor", "/home/u/.local/share/claude/versions/2.2.0", ""},
		{"claude executable, older", script("claude", "2.1.200 (Claude Code)"), "This session runs 2.1.200"},
		{"node under npm", "/usr/local/bin/node", ""},
		{"unknown", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(claudeCodeExecEnv, tc.exec)
			got := dialogVersionHint()
			if tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("hint %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("claude executable that is not Claude Code", func(t *testing.T) {
		t.Setenv(claudeCodeExecEnv, script("claude", "1.0.0"))
		if got := dialogVersionHint(); got != "" {
			t.Fatalf("hint %q", got)
		}
	})
}
