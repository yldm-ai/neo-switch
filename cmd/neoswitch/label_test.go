package main

import (
	"strings"
	"testing"
)

// The label the mod pins under the prompt comes from the daemon, so it follows the session through /neoswitch:on and /neoswitch:off.
func TestLabelFollowsTheSession(t *testing.T) {
	isolateBackendEnv(t, true)
	inSession(t)
	project := t.TempDir()
	var out strings.Builder
	label(&out)
	if out.String() != "" {
		t.Fatalf("label before /neoswitch:on: %q", out.String())
	}
	mustOn(t, project)
	label(&out)
	if out.String() != "NeoSwitch: Gemini 4 Neon answers this session\n" {
		t.Fatalf("label after /neoswitch:on: %q", out.String())
	}
	if err := off([]string{project}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	label(&out)
	if out.String() != "" {
		t.Fatalf("label after /neoswitch:off: %q", out.String())
	}
}
