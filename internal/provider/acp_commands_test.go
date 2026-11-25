package provider

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/translate"
)

// agyCommandAt is how agy finds a command in a prompt: its name after the start of the text or after whitespace (agy 1.5.17 read "Run /compact to" in a tool result, and not "`/compact`" or "plugins/swarm/").
func agyCommandAt(names ...string) *regexp.Regexp {
	return regexp.MustCompile(`(?:^|\s)/(?:` + strings.Join(names, "|") + `)\b`)
}

func TestSlashCommandsInTextAreNotReadAsCommands(t *testing.T) {
	commands := newSlashCommands(defaultAgyCommands)
	text := "/plan first\nThen Run /compact to shorten it, (/goal) and grill /grill-me.\nPaths stay: docs/plans /Users/sen/plan.md plugins/swarm/x /compactor"
	got := commands.text(text)
	if agyCommandAt("plan", "compact", "goal", "grill-me").MatchString(got) {
		t.Fatalf("agy would still read a command in %q", got)
	}
	if strings.ReplaceAll(got, wordJoiner, "") != text {
		t.Fatalf("text changed beyond the word joiners: %q", got)
	}
	for _, kept := range []string{"docs/plans", "/Users/sen/plan.md", "plugins/swarm/x", " /compactor"} {
		if !strings.Contains(got, kept) {
			t.Fatalf("%q changed: %q", kept, got)
		}
	}
}

// In JSON the slash becomes the escape \/, so the value decodes unchanged, also when the model copies it into a tool call.
func TestSlashCommandsInJSONDecodeUnchanged(t *testing.T) {
	commands := newSlashCommands(defaultAgyCommands)
	value := map[string]any{"output": "fmt.Sprintf(\"Run /compact to shorten the conversation.\")\n/plan mode"}
	encoded, _ := json.Marshal(value)
	got := commands.json(string(encoded))
	if agyCommandAt("compact", "plan").MatchString(got) || !strings.Contains(got, `Run \/compact`) {
		t.Fatalf("agy would still read a command in %s", got)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil || decoded["output"] != value["output"] {
		t.Fatalf("decoded %#v, %v", decoded, err)
	}
	parts, _, err := parseAnswer(`{"tool_use":{"name":"Edit","input":{"old_string":"Run \/compact to"}}}`)
	if err != nil || parts[0].FunctionCall.Args["old_string"] != "Run /compact to" {
		t.Fatalf("a copied escape must decode to the slash: %#v %v", parts, err)
	}
}

// The prompt NeoSwitch sends holds no command agy would read, wherever the conversation mentions one: in a message, a tool call, a tool result or the system instructions.
func TestACPPromptHoldsNoAgyCommand(t *testing.T) {
	req := translate.Request{
		Config: &genai.GenerateContentConfig{SystemInstruction: genai.NewContentFromText("Use /plan for big changes.", genai.RoleUser)},
		Contents: []*genai.Content{
			genai.NewContentFromText("please /compact later", genai.RoleUser),
			{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "c1", Name: "Bash", Args: map[string]any{"command": "echo run /goal"}}}}},
			{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "c1", Name: "Bash", Response: map[string]any{"output": "Run /compact to shorten the conversation."}}}}},
		},
	}
	text, _ := acpPrompt(req, false)
	if match := agyCommandAt(defaultAgyCommands...).FindString(text); match != "" {
		t.Fatalf("agy would read %q as a command in:\n%s", match, text)
	}
}

// agy also advertises the user's own commands; they are neutralized from the session's available_commands_update on.
func TestACPLearnsTheCommandsAgyAdvertises(t *testing.T) {
	const name = "neoswitch-test-review"
	if got := agyCommands.text("do /" + name); got != "do /"+name {
		t.Fatalf("an unknown command changed: %q", got)
	}
	c := &acpConn{}
	update := acp.SessionUpdate{AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{AvailableCommands: []acp.AvailableCommand{{Name: name}}}}
	if err := c.SessionUpdate(context.Background(), acp.SessionNotification{SessionId: "s", Update: update}); err != nil {
		t.Fatal(err)
	}
	if got := agyCommands.text("do /" + name); agyCommandAt(name).MatchString(got) {
		t.Fatalf("the advertised command is still read: %q", got)
	}
}
