package provider

import (
	"regexp"
	"slices"
	"strings"
	"sync"
)

// agy reads its own slash commands (/compact, /plan, /goal, ...) out of the prompt text, not only at its start: a "/compact" after a space anywhere in the prompt, such as in a tool result quoting source code that says "Run /compact", is taken as a command the user typed and added to the message. The model then answers that command instead of the conversation; it told the user "the /compact you sent came in as a queued message" (seen with agy 1.5.17, whose ACP mode has no switch to turn this off). So NeoSwitch writes every command name in a form agy does not read as a command but the model reads unchanged:
//   - in JSON (tool calls, tool results, tool definitions), the slash is written as the escape \/, which decodes to the same text, also when the model copies it into a tool call of its own;
//   - in plain text, an invisible word joiner (U+2060) goes before the slash, so that no space precedes it.

// defaultAgyCommands are the commands agy 1.5.17 advertises in every session. agy also advertises the user's own commands and skills, which slashCommands learns from each session's available_commands_update.
var defaultAgyCommands = []string{"goal", "compact", "schedule", "browser", "plan", "grill-me", "teamwork", "learn", "boost", "agy-customizations", "antigravity-guide", "run-workflow", "swarm"}

// agyCommands are the slash commands of the agy NeoSwitch runs.
var agyCommands = newSlashCommands(defaultAgyCommands)

// wordJoiner is invisible and is not whitespace, so a slash after it does not start a command for agy.
const wordJoiner = "\u2060"

type slashCommands struct {
	mu      sync.RWMutex
	names   []string
	pattern *regexp.Regexp
}

func newSlashCommands(names []string) *slashCommands {
	s := &slashCommands{}
	s.add(names)
	return s
}

// add learns command names agy advertised.
func (s *slashCommands) add(names []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, name := range names {
		name = strings.TrimPrefix(strings.TrimSpace(name), "/")
		if name != "" && !slices.Contains(s.names, name) {
			s.names = append(s.names, name)
			changed = true
		}
	}
	if !changed {
		return
	}
	quoted := make([]string, len(s.names))
	for i, name := range s.names {
		quoted[i] = regexp.QuoteMeta(name)
	}
	// Longer names first, so that /plan does not stop the match of a longer name it starts. A slash right after a word character, a dot, a slash, a hyphen or a backslash belongs to a path or an escape, which agy does not read as a command.
	slices.SortFunc(quoted, func(a, b string) int { return len(b) - len(a) })
	s.pattern = regexp.MustCompile(`(^|[^\w./\\-])/(?:` + strings.Join(quoted, "|") + `)\b`)
}

// neutralize rewrites each command in s with the slash written as slash.
func (s *slashCommands) neutralize(text, slash string) string {
	s.mu.RLock()
	pattern := s.pattern
	s.mu.RUnlock()
	if pattern == nil || !strings.Contains(text, "/") {
		return text
	}
	return pattern.ReplaceAllStringFunc(text, func(match string) string {
		cut := strings.IndexByte(match, '/')
		return match[:cut] + slash + match[cut+1:]
	})
}

// text keeps agy from reading commands in plain prompt text.
func (s *slashCommands) text(text string) string {
	return s.neutralize(text, wordJoiner+"/")
}

// json keeps agy from reading commands in encoded JSON, where every slash is inside a string and \/ is the same slash.
func (s *slashCommands) json(encoded string) string {
	return s.neutralize(encoded, `\/`)
}
