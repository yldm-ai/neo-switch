package provider

import (
	"strings"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/translate"
)

func TestContinuationSendsOnlyAppendedSystemDelta(t *testing.T) {
	a := &AntigravityACP{model: DefaultModel}
	conn := &acpConn{}
	sess := &acpSession{id: "s-delta", model: DefaultModel, idleSince: time.Now()}

	req1 := translate.Request{
		Config: &genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: "BASE_INSTRUCTIONS_MARKER"}}},
		},
		Contents: []*genai.Content{
			{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "turn 1"}}},
		},
	}
	a.freshPieces(sess, req1, false)
	answer1 := []*genai.Part{{Text: "reply 1"}}
	sess.key = historyKey(append(req1.Contents, &genai.Content{Role: genai.RoleModel, Parts: answer1}))
	sess.idleSince = time.Now()
	conn.idle = []*acpSession{sess}

	continueReq := func(sysParts []string, contents []*genai.Content) string {
		var parts []*genai.Part
		for _, sp := range sysParts {
			parts = append(parts, &genai.Part{Text: sp})
		}
		req := translate.Request{
			Config:   &genai.GenerateContentConfig{SystemInstruction: &genai.Content{Parts: parts}},
			Contents: contents,
		}
		x := &acpExchange{c: conn, model: DefaultModel}
		pieces, _ := a.continuation(x, req, historyKeys(contents))
		if x.session == nil {
			t.Fatalf("expected session continuation, got nil")
		}
		var joined strings.Builder
		for _, p := range pieces {
			joined.WriteString(p.text)
		}
		sess.key = historyKey(append(contents, &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "ok"}}}))
		sess.idleSince = time.Now()
		conn.idle = []*acpSession{sess}
		return joined.String()
	}

	// Turn 2: unchanged system prompt -> no system header sent.
	conv := []*genai.Content{
		{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "turn 1"}}},
		{Role: genai.RoleModel, Parts: answer1},
		{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "turn 2"}}},
	}
	got2 := continueReq([]string{"BASE_INSTRUCTIONS_MARKER"}, conv)
	if strings.Contains(got2, "BASE_INSTRUCTIONS_MARKER") || strings.Contains(got2, "system instructions") {
		t.Fatalf("unchanged system prompt should not be resent: %q", got2)
	}

	// Turn 3: append-only extension -> sends only the new suffix, not BASE_INSTRUCTIONS_MARKER.
	conv = append(conv,
		&genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "ok"}}},
		&genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "turn 3"}}},
	)
	got3 := continueReq([]string{"BASE_INSTRUCTIONS_MARKER", "APPENDED_REMINDER_1"}, conv)
	if !strings.Contains(got3, "Additional system instructions appended to the earlier ones:") {
		t.Fatalf("expected appended header, got %q", got3)
	}
	if !strings.Contains(got3, "APPENDED_REMINDER_1") {
		t.Fatalf("expected appended suffix, got %q", got3)
	}
	if strings.Contains(got3, "BASE_INSTRUCTIONS_MARKER") {
		t.Fatalf("append-only update must not resend base system prompt: %q", got3)
	}

	// Turn 4: second append-only extension -> sends only APPENDED_HOOK_2, not earlier text.
	conv = append(conv,
		&genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "ok"}}},
		&genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "turn 4"}}},
	)
	got4 := continueReq([]string{"BASE_INSTRUCTIONS_MARKER", "APPENDED_REMINDER_1", "APPENDED_HOOK_2"}, conv)
	if !strings.Contains(got4, "APPENDED_HOOK_2") || strings.Contains(got4, "APPENDED_REMINDER_1") || strings.Contains(got4, "BASE_INSTRUCTIONS_MARKER") {
		t.Fatalf("second append should send only new suffix, got %q", got4)
	}

	// Turn 5: non-prefix replacement/shrinkage -> falls back to full replacement.
	conv = append(conv,
		&genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "ok"}}},
		&genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "turn 5"}}},
	)
	got5 := continueReq([]string{"COMPLETELY_NEW_SYSTEM"}, conv)
	if !strings.Contains(got5, "Updated system instructions, replacing the earlier ones:") || !strings.Contains(got5, "COMPLETELY_NEW_SYSTEM") {
		t.Fatalf("non-prefix change should send full replacement, got %q", got5)
	}
}
