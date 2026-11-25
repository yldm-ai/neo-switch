//go:build e2e && unix

package e2e

import (
	"encoding/json"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// TestClaudeCode drives the real Claude Code CLI through NeoSwitch. Every scripted model turn checks what Claude Code sent back before answering, and the final answer is only reached when every check passed, so each scenario also asserts the final result text.
func TestClaudeCode(t *testing.T) {
	claudePath(t)
	t.Run("text", func(t *testing.T) {
		p := newProject(t)
		up, proxy := newUpstream(t, func(c *call) (reply, error) {
			if !strings.Contains(c.FirstUserText(), "Say the e2e greeting") {
				return reply{}, reject("prompt missing from the first user content: %q", c.FirstUserText())
			}
			if !c.HasTool("Read") || !c.HasTool("Bash") {
				return reply{}, reject("Claude Code tools missing: %v", c.Tools())
			}
			return text("plain text through NeoSwitch"), nil
		})
		p.proxy = proxy
		out := p.run("Say the e2e greeting")
		if out.Result != "plain text through NeoSwitch" {
			t.Fatalf("result = %q", out.Result)
		}
		if n := len(up.MainCalls()); n != 1 {
			t.Fatalf("main requests = %d, want 1", n)
		}
	})

	t.Run("read", func(t *testing.T) {
		p := newProject(t)
		_, proxy := newUpstream(t, func(c *call) (reply, error) {
			r := c.Response("Read")
			if r == nil {
				return calls(toolCall("Read", "sig-read", map[string]any{"file_path": p.path("notes.txt")})), nil
			}
			if err := replayed(c, "Read", "sig-read"); err != nil {
				return reply{}, err
			}
			if output, isErr := r.output(); isErr || !strings.Contains(output, "read-marker-4711") {
				return reply{}, reject("Read result = %q (error %v)", output, isErr)
			}
			return text("the file says read-marker-4711"), nil
		})
		p.proxy = proxy
		p.write("notes.txt", "read-marker-4711\n")
		out := p.run("What does notes.txt say?")
		if out.Result != "the file says read-marker-4711" {
			t.Fatalf("result = %q", out.Result)
		}
	})

	t.Run("edit and write", func(t *testing.T) {
		p := newProject(t)
		_, proxy := newUpstream(t, func(c *call) (reply, error) {
			// Edit requires the file to have been read in the session, as Claude Code enforces.
			switch {
			case c.Response("Read") == nil:
				return calls(toolCall("Read", "sig-edit-1", map[string]any{"file_path": p.path("app.txt")})), nil
			case c.Response("Edit") == nil:
				return calls(toolCall("Edit", "sig-edit-2", map[string]any{"file_path": p.path("app.txt"), "old_string": "version = 1", "new_string": "version = 2"})), nil
			case c.Response("Write") == nil:
				if output, isErr := c.Response("Edit").output(); isErr {
					return reply{}, reject("Edit failed: %s", output)
				}
				return calls(toolCall("Write", "sig-edit-3", map[string]any{"file_path": p.path("new/created.txt"), "content": "written by the model\n"})), nil
			}
			if output, isErr := c.Response("Write").output(); isErr {
				return reply{}, reject("Write failed: %s", output)
			}
			for i, s := range []string{"sig-edit-1", "sig-edit-2", "sig-edit-3"} {
				if got := c.Calls()[i].ThoughtSignature; got != sig(s) {
					return reply{}, reject("call %d replayed signature %q, want %q", i, got, sig(s))
				}
			}
			return text("edited"), nil
		})
		p.proxy = proxy
		p.write("app.txt", "name = demo\nversion = 1\n")
		out := p.run("Bump the version and add a file", "--permission-mode", "acceptEdits")
		if out.Result != "edited" {
			t.Fatalf("result = %q", out.Result)
		}
		if got := p.read("app.txt"); got != "name = demo\nversion = 2\n" {
			t.Fatalf("app.txt = %q", got)
		}
		if got := p.read("new/created.txt"); got != "written by the model\n" {
			t.Fatalf("created.txt = %q", got)
		}
	})

	t.Run("bash", func(t *testing.T) {
		p := newProject(t)
		_, proxy := newUpstream(t, func(c *call) (reply, error) {
			r := c.Response("Bash")
			if r == nil {
				return calls(toolCall("Bash", "sig-bash", map[string]any{"command": "echo bash-$((6*7))", "description": "Print a marker"})), nil
			}
			if err := replayed(c, "Bash", "sig-bash"); err != nil {
				return reply{}, err
			}
			if output, isErr := r.output(); isErr || !strings.Contains(output, "bash-42") {
				return reply{}, reject("Bash result = %q (error %v)", output, isErr)
			}
			return text("bash printed bash-42"), nil
		})
		p.proxy = proxy
		out := p.run("Run the marker command", "--allowedTools", "Bash(echo *)")
		if out.Result != "bash printed bash-42" {
			t.Fatalf("result = %q", out.Result)
		}
	})

	t.Run("parallel tool calls", func(t *testing.T) {
		p := newProject(t)
		_, proxy := newUpstream(t, func(c *call) (reply, error) {
			if len(c.Responses()) == 0 {
				// Gemini attaches the thought signature to the first function call of a parallel set only.
				return calls(
					toolCall("Read", "sig-parallel", map[string]any{"file_path": p.path("a.txt")}),
					geminiPart{FunctionCall: &functionCall{Name: "Read", Args: map[string]any{"file_path": p.path("b.txt")}}},
				), nil
			}
			last := c.Contents[len(c.Contents)-1]
			model := c.Contents[len(c.Contents)-2]
			if model.Role != "model" || len(model.Parts) != 2 || model.Parts[0].ThoughtSignature != sig("sig-parallel") || model.Parts[1].ThoughtSignature != "" {
				return reply{}, reject("parallel calls replayed as %+v", model)
			}
			if last.Role != "user" || len(last.Parts) != 2 || last.Parts[0].FunctionResponse == nil || last.Parts[1].FunctionResponse == nil {
				return reply{}, reject("parallel results sent as %+v", last)
			}
			first, _ := last.Parts[0].FunctionResponse.output()
			second, _ := last.Parts[1].FunctionResponse.output()
			if !strings.Contains(first, "alpha-file") || !strings.Contains(second, "beta-file") {
				return reply{}, reject("parallel results out of order: %q, %q", first, second)
			}
			return text("both files read"), nil
		})
		p.proxy = proxy
		p.write("a.txt", "alpha-file\n")
		p.write("b.txt", "beta-file\n")
		out := p.run("Read a.txt and b.txt")
		if out.Result != "both files read" {
			t.Fatalf("result = %q", out.Result)
		}
	})

	t.Run("permission denial", func(t *testing.T) {
		p := newProject(t)
		_, proxy := newUpstream(t, func(c *call) (reply, error) {
			r := c.Response("Bash")
			if r == nil {
				return calls(toolCall("Bash", "sig-deny", map[string]any{"command": "touch denied.txt", "description": "Create a file"})), nil
			}
			if err := replayed(c, "Bash", "sig-deny"); err != nil {
				return reply{}, err
			}
			if output, isErr := r.output(); !isErr {
				return reply{}, reject("denied Bash call reached the model as a successful result: %q", output)
			}
			return text("the command was refused"), nil
		})
		p.proxy = proxy
		// Nothing allows Bash and nobody can answer a prompt, so Claude Code must refuse the call.
		out := p.run("Create denied.txt", "--permission-prompts", "none")
		if out.Result != "the command was refused" {
			t.Fatalf("result = %q", out.Result)
		}
		if len(out.PermissionDenials) != 1 || out.PermissionDenials[0].ToolName != "Bash" {
			t.Fatalf("permission denials = %+v", out.PermissionDenials)
		}
		if _, err := os.Stat(p.path("denied.txt")); !os.IsNotExist(err) {
			t.Fatalf("the denied command ran: %v", err)
		}
	})

	t.Run("hooks", func(t *testing.T) {
		p := newProject(t)
		_, proxy := newUpstream(t, func(c *call) (reply, error) {
			if c.Response("Read") == nil {
				return calls(toolCall("Read", "sig-hook", map[string]any{"file_path": p.path("hooked.txt")})), nil
			}
			return text("hooked"), nil
		})
		p.proxy = proxy
		p.write("hooked.txt", "content\n")
		p.write(".claude/settings.json", `{"hooks":{
			"PreToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"cat > \"$CLAUDE_PROJECT_DIR/pre-tool-use.json\""}]}],
			"PostToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"cat > \"$CLAUDE_PROJECT_DIR/post-tool-use.json\""}]}]
		}}`)
		out := p.run("Read hooked.txt")
		if out.Result != "hooked" {
			t.Fatalf("result = %q", out.Result)
		}
		for _, name := range []string{"pre-tool-use.json", "post-tool-use.json"} {
			var input struct {
				HookEventName string `json:"hook_event_name"`
				ToolName      string `json:"tool_name"`
				ToolUseID     string `json:"tool_use_id"`
				ToolInput     struct {
					FilePath string `json:"file_path"`
				} `json:"tool_input"`
			}
			if err := json.Unmarshal([]byte(p.read(name)), &input); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if input.ToolName != "Read" || input.ToolInput.FilePath != p.path("hooked.txt") || !strings.HasPrefix(input.ToolUseID, "toolu_ns_") {
				t.Fatalf("%s = %+v", name, input)
			}
		}
	})

	t.Run("skill", func(t *testing.T) {
		p := newProject(t)
		_, proxy := newUpstream(t, func(c *call) (reply, error) {
			r := c.Response("Skill")
			if r == nil {
				// The skill must be advertised to the model before it can call it.
				if !strings.Contains(c.System()+c.Text(), "e2e-release-notes") {
					return reply{}, reject("project skill not advertised to the model")
				}
				return calls(toolCall("Skill", "sig-skill", map[string]any{"skill": "e2e-release-notes"})), nil
			}
			if err := replayed(c, "Skill", "sig-skill"); err != nil {
				return reply{}, err
			}
			if output, isErr := r.output(); isErr {
				return reply{}, reject("Skill failed: %s", output)
			}
			if !strings.Contains(c.Text(), "skill-body-marker-9931") {
				return reply{}, reject("skill instructions did not reach the model")
			}
			return text("skill applied"), nil
		})
		p.proxy = proxy
		p.write(".claude/skills/e2e-release-notes/SKILL.md", "---\nname: e2e-release-notes\ndescription: Write release notes for this project. Use when asked for release notes.\n---\n\nAlways start release notes with skill-body-marker-9931.\n")
		out := p.run("Write the release notes")
		if out.Result != "skill applied" {
			t.Fatalf("result = %q", out.Result)
		}
	})

	t.Run("resume", func(t *testing.T) {
		p := newProject(t)
		up, proxy := newUpstream(t, func(c *call) (reply, error) {
			if !strings.Contains(c.Text(), "second-turn-question") {
				if c.Response("Read") == nil {
					return calls(toolCall("Read", "sig-resume", map[string]any{"file_path": p.path("state.txt")})), nil
				}
				return text("first turn done"), nil
			}
			// The resumed conversation must carry the earlier tool call with its signature, its result and the earlier answer.
			if err := replayed(c, "Read", "sig-resume"); err != nil {
				return reply{}, err
			}
			if r := c.Response("Read"); r == nil {
				return reply{}, reject("resumed history lost the Read result")
			} else if output, _ := r.output(); !strings.Contains(output, "resume-marker-2024") {
				return reply{}, reject("resumed Read result = %q", output)
			}
			if !strings.Contains(c.Text(), "first turn done") {
				return reply{}, reject("resumed history lost the first answer")
			}
			return text("resumed with history"), nil
		})
		p.proxy = proxy
		p.write("state.txt", "resume-marker-2024\n")
		first := p.run("Read state.txt")
		if first.Result != "first turn done" || first.SessionID == "" {
			t.Fatalf("first run = %+v", first)
		}
		second := p.run("second-turn-question", "--resume", first.SessionID)
		if second.Result != "resumed with history" {
			t.Fatalf("result = %q", second.Result)
		}
		if n := len(up.MainCalls()); n != 3 {
			t.Fatalf("main requests = %d, want 3", n)
		}
	})

	t.Run("subagent", func(t *testing.T) {
		var subagentRequests atomic.Int32
		p := newProject(t)
		_, proxy := newUpstream(t, func(c *call) (reply, error) {
			if strings.Contains(c.FirstUserText(), "subagent-task-5150") {
				subagentRequests.Add(1)
				if strings.Contains(c.Text(), "Delegate the question") {
					return reply{}, reject("the subagent request carries the parent conversation")
				}
				return text("subagent-answer-8086"), nil
			}
			r := c.Response("Agent")
			if r == nil {
				return calls(toolCall("Agent", "sig-agent", map[string]any{"description": "Answer a question", "prompt": "subagent-task-5150: reply with your answer", "subagent_type": "general-purpose", "run_in_background": false})), nil
			}
			if err := replayed(c, "Agent", "sig-agent"); err != nil {
				return reply{}, err
			}
			if output, isErr := r.output(); isErr || !strings.Contains(output, "subagent-answer-8086") {
				return reply{}, reject("Agent result = %q (error %v)", output, isErr)
			}
			return text("the subagent answered"), nil
		})
		p.proxy = proxy
		out := p.run("Delegate the question")
		if out.Result != "the subagent answered" {
			t.Fatalf("result = %q", out.Result)
		}
		if subagentRequests.Load() == 0 {
			t.Fatal("the subagent never sent a request")
		}
	})

	t.Run("mcp", func(t *testing.T) {
		// Claude Code's own `claude mcp serve` is the MCP server: a real stdio MCP implementation that needs no extra dependency. Claude Code defers MCP tools, so the model first loads the tool's schema with ToolSearch, as it does against Anthropic.
		p := newProject(t)
		_, proxy := newUpstream(t, func(c *call) (reply, error) {
			switch {
			case c.Response("ToolSearch") == nil:
				if c.HasTool("mcp__cc__Read") {
					return reply{}, reject("deferred MCP tool declared before ToolSearch loaded it")
				}
				return calls(toolCall("ToolSearch", "sig-search", map[string]any{"query": "select:mcp__cc__Read", "max_results": 1})), nil
			case c.Response("mcp__cc__Read") == nil:
				if output, isErr := c.Response("ToolSearch").output(); isErr {
					return reply{}, reject("ToolSearch failed: %s", output)
				}
				if !c.HasTool("mcp__cc__Read") {
					return reply{}, reject("ToolSearch did not load the MCP tool: %v", c.Tools())
				}
				return calls(toolCall("mcp__cc__Read", "sig-mcp", map[string]any{"file_path": p.path("mcp.txt")})), nil
			}
			if err := replayed(c, "mcp__cc__Read", "sig-mcp"); err != nil {
				return reply{}, err
			}
			if output, isErr := c.Response("mcp__cc__Read").output(); isErr || !strings.Contains(output, "mcp-marker-6502") {
				return reply{}, reject("MCP result = %q (error %v)", output, isErr)
			}
			return text("read through MCP"), nil
		})
		p.proxy = proxy
		p.write("mcp.txt", "mcp-marker-6502\n")
		config, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"cc": map[string]any{"command": p.claude, "args": []string{"mcp", "serve"}}}})
		if err != nil {
			t.Fatal(err)
		}
		out := p.run("Read mcp.txt through MCP", "--mcp-config", string(config), "--strict-mcp-config", "--allowedTools", "mcp__cc__Read")
		if out.Result != "read through MCP" {
			t.Fatalf("result = %q", out.Result)
		}
	})

	t.Run("auto mode", func(t *testing.T) {
		t.Run("not_flagged runs bash without allowedTools", func(t *testing.T) {
			p := newProject(t)
			_, proxy := newUpstream(t, func(c *call) (reply, error) {
				if len(c.Tools()) == 0 && strings.Contains(c.System(), "security classifier") {
					return text(`{"verdicts":{"0":{"flagged":false,"explanation":""}}}`), nil
				}
				r := c.Response("Bash")
				if r == nil {
					return calls(toolCall("Bash", "sig-auto-ok", map[string]any{"command": "python3 -c 'print(54321)'", "description": "Print auto marker"})), nil
				}
				if output, isErr := r.output(); isErr || !strings.Contains(output, "54321") {
					return reply{}, reject("Bash result = %q (error %v)", output, isErr)
				}
				return text("auto mode ran 54321"), nil
			})
			p.proxy = proxy
			out := p.run("Print 54321 with python", "--permission-mode", "auto")
			if out.Result != "auto mode ran 54321" {
				t.Fatalf("result = %q", out.Result)
			}
		})

		t.Run("flagged blocks bash with classifier explanation", func(t *testing.T) {
			p := newProject(t)
			_, proxy := newUpstream(t, func(c *call) (reply, error) {
				if len(c.Tools()) == 0 && strings.Contains(c.System(), "security classifier") {
					return text(`{"verdicts":{"0":{"flagged":true,"explanation":"E2E blocked dangerous command"}}}`), nil
				}
				r := c.Response("Bash")
				if r == nil {
					return calls(toolCall("Bash", "sig-auto-block", map[string]any{"command": "python3 -c 'print(99999)'", "description": "Run blocked command"})), nil
				}
				output, isErr := r.output()
				if !isErr || !strings.Contains(output, "E2E blocked dangerous command") {
					return reply{}, reject("expected classifier block, got %q (error %v)", output, isErr)
				}
				return text("blocked as expected"), nil
			})
			p.proxy = proxy
			out := p.run("Run blocked command", "--permission-mode", "auto")
			if out.Result != "blocked as expected" {
				t.Fatalf("result = %q", out.Result)
			}
		})

		t.Run("malformed classifier verdict fails closed", func(t *testing.T) {
			p := newProject(t)
			_, proxy := newUpstream(t, func(c *call) (reply, error) {
				if len(c.Tools()) == 0 && strings.Contains(c.System(), "security classifier") {
					return text("not-valid-json"), nil
				}
				r := c.Response("Bash")
				if r == nil {
					return calls(toolCall("Bash", "sig-auto-err", map[string]any{"command": "python3 -c 'print(77777)'", "description": "Run command with broken classifier"})), nil
				}
				output, isErr := r.output()
				if !isErr || strings.Contains(output, "77777") {
					return reply{}, reject("expected fail-closed block without executing bash, got %q (error %v)", output, isErr)
				}
				return text("failed closed"), nil
			})
			p.proxy = proxy
			out := p.run("Run command with broken classifier", "--permission-mode", "auto")
			if out.Result != "failed closed" {
				t.Fatalf("result = %q", out.Result)
			}
		})
	})
}

// replayed checks that the history replays the named function call with its thought signature, which Gemini 3+ requires.
func replayed(c *call, name, signature string) error {
	for _, p := range c.Calls() {
		if p.FunctionCall.Name == name && p.ThoughtSignature == sig(signature) {
			return nil
		}
	}
	return reject("history does not replay %s with its thought signature (MISSING_THOUGHT_SIGNATURE): %+v", name, c.Calls())
}
