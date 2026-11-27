package provider

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	acp "github.com/coder/acp-go-sdk"
	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/translate"
)

// ---- prompt ----

// acpPromptBudget is the most text NeoSwitch puts in one ACP prompt. agy keeps only the first 192,000 bytes of a prompt's text and replaces the rest with a "<truncated N bytes>" marker telling the model to read the full transcript with its own tools, which NeoSwitch refuses: the newest messages, which come last, would be lost without any error. The margin leaves room for the instructions that open every prompt.
const acpPromptBudget = 150_000

// acpSystemPromptBudget is the most text NeoSwitch places in _meta.antigravity.systemPrompt.append on session/new. Verified on agy 1.5.17 to accept >=420 KB without the 192 KB user-prompt truncation; anything larger overflows cleanly into prompt pieces.
const acpSystemPromptBudget = 380_000

// acpAnswerInstructions open the prompt the model answers (or the session system prompt when agy advertises _meta.antigravity.systemPrompt.append). agy keeps the start of a prompt, so they come first.
const acpAnswerInstructions = "Act as the language model for Claude Code. " + ModelIdentity + " Return exactly one JSON object and no markdown. For a normal answer use {\"text\":\"...\"}. To invoke one or more of the supplied Claude Code tools use {\"tool_uses\":[{\"name\":\"tool name\",\"input\":{...}},...]} (include multiple entries to run independent tools in parallel, or {\"tool_use\":{\"name\":\"tool name\",\"input\":{...}}} for a single call, optionally with \"text\"). Escape literal backslashes inside JSON strings as \\\\. Do not use ACP tools, edit files, or run commands yourself.\n\n"

// acpSystemUpdateNote tells the model that later continuation messages marked as updated or appended system instructions or tool definitions supersede or extend those pinned in session/new's systemPrompt.append.
const acpSystemUpdateNote = "If a later message begins with \"Updated system instructions, replacing the earlier ones:\" or \"Updated Claude Code tool definitions, replacing the earlier ones:\", those updated definitions replace the initial ones below. If it begins with \"Additional system instructions appended to the earlier ones:\", treat them as appended to the earlier system instructions.\n\n"

// acpPiece is prompt text with the images that belong to it.
type acpPiece struct {
	text   string
	images []acp.ContentBlock
}

// promptRenderer renders a request into prompt text. Images are numbered across the whole request.
type promptRenderer struct {
	acceptImages bool
	images       int
}

// rawContext renders the un-neutralized system instructions and tool definitions, each "" when the request has none. Hashing the raw text keeps session change detection independent of agyCommands growing after session/new.
func rawContext(req translate.Request) (system, tools string) {
	if req.Config == nil {
		return "", ""
	}
	if req.Config.SystemInstruction != nil {
		var out strings.Builder
		for _, part := range req.Config.SystemInstruction.Parts {
			if part != nil && part.Text != "" {
				out.WriteString(part.Text)
				out.WriteString("\n")
			}
		}
		system = out.String()
	}
	if len(req.Config.Tools) > 0 {
		declarations, _ := json.Marshal(req.Config.Tools)
		tools = string(declarations) + "\n"
	}
	return system, tools
}

// neutralizeContext escapes agy slash commands in system instructions and tool definitions.
func neutralizeContext(rawSystem, rawTools string) (system, tools string) {
	if rawSystem != "" {
		system = agyCommands.text(rawSystem)
	}
	if rawTools != "" {
		tools = agyCommands.json(rawTools)
	}
	return system, tools
}

// context renders the neutralized system instructions and tool definitions, each "" when the request has none.
func (r *promptRenderer) context(req translate.Request) (system, tools string) {
	rawSystem, rawTools := rawContext(req)
	return neutralizeContext(rawSystem, rawTools)
}

// buildSystemPromptAppend packs acpAnswerInstructions plus system instructions and tool definitions into _meta.antigravity.systemPrompt.append up to acpSystemPromptBudget. Pieces that do not fit overflow into the returned slice so acpPrompts delivers them safely.
func buildSystemPromptAppend(system, tools string) (string, []acpPiece) {
	var out strings.Builder
	out.WriteString(acpAnswerInstructions)
	if system != "" || tools != "" {
		out.WriteString(acpSystemUpdateNote)
	}
	var overflow []acpPiece
	tryAppend := func(label, body string) {
		if body == "" {
			return
		}
		block := label + body + "\n"
		if out.Len()+len(block) <= acpSystemPromptBudget {
			out.WriteString(block)
			return
		}
		overflow = append(overflow, acpPiece{text: block})
	}
	tryAppend("System instructions:\n", system)
	tryAppend("Claude Code tool definitions for context only:\n", tools)
	return out.String(), overflow
}

// contextPieces labels the system instructions and tool definitions for the prompt; updated marks them as replacing those a session read before.
func contextPieces(system, tools string, updated bool) []acpPiece {
	systemLabel, toolsLabel := "System instructions:\n", "Claude Code tool definitions for context only:\n"
	if updated {
		systemLabel, toolsLabel = "Updated system instructions, replacing the earlier ones:\n", "Updated Claude Code tool definitions, replacing the earlier ones:\n"
	}
	var pieces []acpPiece
	if system != "" {
		pieces = append(pieces, acpPiece{text: systemLabel + system + "\n"})
	}
	if tools != "" {
		pieces = append(pieces, acpPiece{text: toolsLabel + tools + "\n"})
	}
	return pieces
}

// contextPiecesForContinuation labels changed system instructions and tool definitions for a continued session. When systemAppended is true, system holds only the suffix appended since the previous prompt rather than a full replacement.
func contextPiecesForContinuation(system string, systemAppended bool, tools string) []acpPiece {
	var pieces []acpPiece
	if system != "" {
		label := "Updated system instructions, replacing the earlier ones:\n"
		if systemAppended {
			label = "Additional system instructions appended to the earlier ones:\n"
		}
		pieces = append(pieces, acpPiece{text: label + system + "\n"})
	}
	if tools != "" {
		pieces = append(pieces, acpPiece{text: "Updated Claude Code tool definitions, replacing the earlier ones:\n" + tools + "\n"})
	}
	return pieces
}

// entry renders one message of the conversation.
func (r *promptRenderer) entry(content *genai.Content) acpPiece {
	p := r.builder()
	p.out.WriteString(strings.ToUpper(string(content.Role)))
	p.out.WriteString(":\n")
	p.writeParts(content.Parts)
	p.out.WriteString("\n\n")
	return acpPiece{text: p.out.String(), images: p.images}
}

func (r *promptRenderer) builder() *promptBuilder {
	return &promptBuilder{renderer: r}
}

// acpPrompt renders the whole request as one prompt text, used for token estimation and when agy does not advertise _meta.antigravity.systemPrompt.append. Images become ACP image blocks sent after the text when the agent accepts them (acceptImages); the text marks where each one belongs. Attachments ACP cannot carry are marked as omitted so the model knows they existed.
func acpPrompt(req translate.Request, acceptImages bool) (string, []acp.ContentBlock) {
	var out strings.Builder
	var images []acp.ContentBlock
	out.WriteString(acpAnswerInstructions)
	for _, piece := range conversationPieces(req, acceptImages) {
		out.WriteString(piece.text)
		images = append(images, piece.images...)
	}
	return out.String(), images
}

// conversationPieces renders the request for a session that holds nothing yet: system instructions, tool definitions, then every message. The tool definitions come before the messages so that the newest message ends the prompt.
func conversationPieces(req translate.Request, acceptImages bool) []acpPiece {
	r := &promptRenderer{acceptImages: acceptImages}
	system, tools := r.context(req)
	pieces := contextPieces(system, tools, false)
	for _, content := range req.Contents {
		pieces = append(pieces, r.entry(content))
	}
	return pieces
}

// messagePieces renders only the conversation messages (used when system instructions and tool definitions were sent in _meta.antigravity.systemPrompt.append).
func messagePieces(contents []*genai.Content, acceptImages bool) []acpPiece {
	r := &promptRenderer{acceptImages: acceptImages}
	pieces := make([]acpPiece, 0, len(contents))
	for _, content := range contents {
		pieces = append(pieces, r.entry(content))
	}
	return pieces
}

// acpPrompts packs pieces into the prompts that deliver them, none longer than acpPromptBudget. Each prompt but the last asks only for {"text":"ok"}; the last starts with instructions (empty when already pinned in systemPrompt.append) and continued, which tells a session that already holds the conversation what the new text is. A piece too long for one prompt is split, at a line break where there is one.
func acpPrompts(pieces []acpPiece, instructions, continued string) []acpPiece {
	const headroom = 1024
	capacity := acpPromptBudget - len(instructions) - len(continued) - headroom
	var bins []acpPiece
	var current acpPiece
	flush := func() {
		if current.text != "" || len(current.images) > 0 {
			bins = append(bins, current)
		}
		current = acpPiece{}
	}
	for _, piece := range pieces {
		for _, fragment := range splitPiece(piece, capacity) {
			if len(current.text)+len(fragment.text) > capacity {
				flush()
			}
			current.text += fragment.text
			current.images = append(current.images, fragment.images...)
		}
	}
	flush()
	if len(bins) == 0 {
		bins = []acpPiece{{}}
	}
	prompts := make([]acpPiece, len(bins))
	for i, bin := range bins {
		head := fmt.Sprintf("This is part %d of %d of a Claude Code conversation. Do not answer it yet: reply with exactly {\"text\":\"ok\"}. The conversation continues in the next message.\n\n", i+1, len(bins))
		if i == len(bins)-1 {
			head = instructions + continued
			if len(bins) > 1 {
				head += fmt.Sprintf("This is the last part (%d of %d) of the conversation; the earlier parts are in the previous messages of this session.\n\n", len(bins), len(bins))
			}
		}
		prompts[i] = acpPiece{text: head + bin.text, images: bin.images}
	}
	return prompts
}

// splitPiece cuts a piece into fragments of at most capacity bytes, at the last line break of each fragment when it is in the fragment's second half, otherwise at a character boundary. Its images go with the last fragment.
func splitPiece(piece acpPiece, capacity int) []acpPiece {
	if len(piece.text) <= capacity {
		return []acpPiece{piece}
	}
	var fragments []acpPiece
	text := piece.text
	for len(text) > capacity {
		cut := capacity
		if newline := strings.LastIndexByte(text[:cut], '\n'); newline >= capacity/2 {
			cut = newline + 1
		}
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		fragments = append(fragments, acpPiece{text: text[:cut]})
		text = text[cut:]
	}
	return append(fragments, acpPiece{text: text, images: piece.images})
}

type promptBuilder struct {
	out      strings.Builder
	renderer *promptRenderer
	images   []acp.ContentBlock
}

func (p *promptBuilder) writeParts(parts []*genai.Part) {
	for _, part := range parts {
		if part == nil {
			continue
		}
		if part.Text != "" {
			p.out.WriteString(agyCommands.text(part.Text))
			p.out.WriteString("\n")
		}
		if part.InlineData != nil {
			p.attach(part.InlineData.MIMEType, part.InlineData.Data)
		}
		if part.FunctionCall != nil {
			args, _ := json.Marshal(part.FunctionCall.Args)
			fmt.Fprintf(&p.out, "[tool call %s %s]\n", part.FunctionCall.Name, agyCommands.json(string(args)))
		}
		if part.FunctionResponse != nil {
			response, _ := json.Marshal(part.FunctionResponse.Response)
			fmt.Fprintf(&p.out, "[tool result %s %s]\n", part.FunctionResponse.Name, agyCommands.json(string(response)))
			for _, media := range part.FunctionResponse.Parts {
				if media != nil && media.InlineData != nil {
					p.attach(media.InlineData.MIMEType, media.InlineData.Data)
				}
			}
		}
	}
}

func (p *promptBuilder) attach(mimeType string, data []byte) {
	if !p.renderer.acceptImages || !strings.HasPrefix(mimeType, "image/") {
		fmt.Fprintf(&p.out, "[%s attachment omitted: the Antigravity ACP route cannot send it]\n", mimeType)
		return
	}
	p.renderer.images++
	p.images = append(p.images, acp.ImageBlock(base64.StdEncoding.EncodeToString(data), mimeType))
	fmt.Fprintf(&p.out, "[image %d (%s) attached]\n", p.renderer.images, mimeType)
}

// ---- answer ----

var errEmptyACPAnswer = errors.New("Antigravity ACP returned an empty response")

// malformedToolUseError is an answer that starts a {"tool_use": ...} or {"tool_uses": ...} envelope but is not valid JSON even after repairJSONStrings, typically for an unescaped quote or a cut-off answer. Passing it on as text would end Claude Code's turn with the raw JSON on screen and no tool run, so the session is asked once to write it again (acpToolUseRepair).
type malformedToolUseError struct{ cause error }

func (e *malformedToolUseError) Error() string {
	return "Antigravity answered with a tool call that is not valid JSON: " + e.cause.Error()
}

// toolUseStart matches the start of an envelope with one or more tool calls once the code fence is stripped: "tool_use" / "tool_uses" first, or after the "text" the prompt allows with it.
var toolUseStart = regexp.MustCompile(`^\s*\{\s*(?:"text"\s*:\s*"(?:[^"\\]|\\.)*"\s*,\s*)?"tool_uses?"\s*:`)

// acpToolCall is one tool invocation inside an envelope.
type acpToolCall struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// acpEnvelope is the JSON object the prompt asks for.
type acpEnvelope struct {
	Text     string          `json:"text"`
	ToolUse  json.RawMessage `json:"tool_use"`
	ToolUses json.RawMessage `json:"tool_uses"`
}

// decodeEnvelopeToolCalls extracts all tool calls declared on a single envelope, accepting both "tool_use" (single object or array) and "tool_uses" (array or single object). Empty or null fields yield no calls.
func decodeEnvelopeToolCalls(envelope acpEnvelope) ([]acpToolCall, error) {
	var out []acpToolCall
	for _, raw := range []json.RawMessage{envelope.ToolUse, envelope.ToolUses} {
		calls, err := decodeRawToolCalls(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, calls...)
	}
	return out, nil
}

func decodeRawToolCalls(raw json.RawMessage) ([]acpToolCall, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	switch trimmed[0] {
	case '{':
		var call acpToolCall
		if err := json.Unmarshal([]byte(trimmed), &call); err != nil {
			return nil, err
		}
		return []acpToolCall{call}, nil
	case '[':
		var items []*acpToolCall
		if err := json.Unmarshal([]byte(trimmed), &items); err != nil {
			return nil, err
		}
		calls := make([]acpToolCall, 0, len(items))
		for _, item := range items {
			if item == nil {
				return nil, errors.New("null element in tool_uses")
			}
			calls = append(calls, *item)
		}
		return calls, nil
	default:
		return nil, fmt.Errorf("expected tool call object or array, got %q", trimmed)
	}
}

// parseAnswer parses the JSON envelope the prompt asks for, and reports whether the answer was exactly one clean envelope. An answer that is not JSON at all is passed through as text, since a readable answer beats an error. Models sometimes write more than one top-level object, typically repeating a tool call; the objects are read in order, their texts joined, and only tool calls from the first object carrying calls are kept, because a second top-level object with a different call comes from a model that lost track. Exact duplicate calls within a single tool_uses array are also deduplicated and mark exact = false so the session is not reused with mismatched call counts. Multi-object or non-JSON answers are likewise not exact.
func parseAnswer(raw string) ([]*genai.Part, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, false, errEmptyACPAnswer
	}
	clean := stripCodeFence(raw)
	envelopes, complete, _ := decodeEnvelopes(clean)
	if len(envelopes) == 0 {
		// Models write literal newlines or tabs inside JSON strings, and backslashes that are no JSON escape (a regular expression's \. or \|, a Windows path); accept those before giving up on the envelope.
		repaired := repairJSONStrings(clean)
		var err error
		envelopes, complete, err = decodeEnvelopes(repaired)
		if len(envelopes) == 0 && (toolUseStart.MatchString(clean) || toolUseStart.MatchString(repaired)) {
			return nil, false, &malformedToolUseError{cause: err}
		}
	}
	if len(envelopes) == 0 {
		acpLogger.Warn("Antigravity answered without the JSON envelope; passing it on as text")
		return []*genai.Part{{Text: raw}}, false, nil
	}
	exact := complete && len(envelopes) == 1
	var texts []string
	var calls []*genai.Part
	for _, envelope := range envelopes {
		if envelope.Text != "" && !slices.Contains(texts, envelope.Text) {
			texts = append(texts, envelope.Text)
		}
		rawCalls, err := decodeEnvelopeToolCalls(envelope)
		if err != nil {
			return nil, false, &malformedToolUseError{cause: err}
		}
		if len(rawCalls) == 0 {
			continue
		}
		if len(calls) > 0 {
			for _, dropped := range rawCalls {
				var input map[string]any
				if len(dropped.Input) > 0 && string(dropped.Input) != "null" {
					_ = json.Unmarshal(dropped.Input, &input)
					restoreBackslashLetters(input)
				}
				if !hasMatchingCall(calls, dropped.Name, input) {
					acpLogger.Warn("Antigravity answered with tool calls in more than one JSON object; only the first object's calls are run", "first", calls[0].FunctionCall.Name, "dropped", dropped.Name)
				}
			}
			continue
		}
		for _, tc := range rawCalls {
			if strings.TrimSpace(tc.Name) == "" {
				return nil, false, errors.New("Antigravity ACP returned a tool call without a name")
			}
			var input map[string]any
			if len(tc.Input) > 0 && string(tc.Input) != "null" {
				if err := json.Unmarshal(tc.Input, &input); err != nil {
					return nil, false, fmt.Errorf("decode Antigravity ACP tool call: %w", err)
				}
				restoreBackslashLetters(input)
			}
			if hasMatchingCall(calls, tc.Name, input) {
				exact = false
				acpLogger.Warn("Antigravity repeated an identical tool call in tool_uses; duplicate dropped", "tool", tc.Name)
				continue
			}
			calls = append(calls, &genai.Part{FunctionCall: &genai.FunctionCall{ID: "acp_tool_" + rand.Text(), Name: tc.Name, Args: input}})
		}
	}
	if len(envelopes) > 1 {
		acpLogger.Warn("Antigravity answered with more than one JSON object; they were merged", "objects", len(envelopes))
	}
	var parts []*genai.Part
	if len(texts) > 0 {
		parts = append(parts, &genai.Part{Text: strings.Join(texts, "\n\n")})
	}
	parts = append(parts, calls...)
	if len(parts) == 0 {
		return nil, false, errors.New("Antigravity ACP returned neither text nor a tool call")
	}
	return parts, exact, nil
}

func hasMatchingCall(calls []*genai.Part, name string, input map[string]any) bool {
	for _, existing := range calls {
		if existing.FunctionCall != nil && existing.FunctionCall.Name == name && sameJSON(existing.FunctionCall.Args, input) {
			return true
		}
	}
	return false
}

// decodeEnvelopes reads the JSON objects at the start of s, one after another. complete is false when something other than another object follows them; err says what.
func decodeEnvelopes(s string) ([]acpEnvelope, bool, error) {
	decoder := json.NewDecoder(strings.NewReader(s))
	var envelopes []acpEnvelope
	for {
		var envelope acpEnvelope
		err := decoder.Decode(&envelope)
		if errors.Is(err, io.EOF) {
			return envelopes, true, nil
		}
		if err != nil {
			return envelopes, false, err
		}
		envelopes = append(envelopes, envelope)
	}
}

// restoreBackslashLetters turns control characters in a tool call's input that a model writes only by escaping too little back into the \b and \f it wrote. JSON reads \b as a backspace, which no tool input means, but a model writes it for a regular expression's word boundary. A form feed is restored only in a command, where Claude Code refuses it as hiding text from the approval dialog: a file can hold real form feeds, which an edit or a write must keep.
func restoreBackslashLetters(input map[string]any) {
	for key, value := range input {
		input[key] = restoreBackspaces(value)
	}
	if command, ok := input["command"].(string); ok {
		input["command"] = strings.ReplaceAll(command, "\f", `\f`)
	}
}

func restoreBackspaces(value any) any {
	switch v := value.(type) {
	case string:
		return strings.ReplaceAll(v, "\b", `\b`)
	case map[string]any:
		for key, item := range v {
			v[key] = restoreBackspaces(item)
		}
	case []any:
		for i, item := range v {
			v[i] = restoreBackspaces(item)
		}
	}
	return value
}

// sameJSON reports whether two decoded values encode to the same JSON; map keys are encoded in sorted order.
func sameJSON(a, b any) bool {
	x, errX := json.Marshal(a)
	y, errY := json.Marshal(b)
	return errX == nil && errY == nil && string(x) == string(y)
}

// acpAnswerParts is parseAnswer for a finished prompt: a refusal may legitimately come with no text, and a tool call cut off by the output limit is passed on as text with that stop reason, since writing it again would run into the same limit.
func acpAnswerParts(raw string, result acp.PromptResponse) ([]*genai.Part, bool, error) {
	parts, exact, err := parseAnswer(raw)
	if errors.Is(err, errEmptyACPAnswer) && result.StopReason == acp.StopReasonRefusal {
		return nil, false, nil
	}
	if malformed := (*malformedToolUseError)(nil); errors.As(err, &malformed) && result.StopReason == acp.StopReasonMaxTokens {
		acpLogger.Warn("Antigravity reached its output limit inside a tool call; passing it on as text")
		return []*genai.Part{{Text: raw}}, false, nil
	}
	return parts, exact, err
}

func stripCodeFence(raw string) string {
	clean := strings.TrimSpace(raw)
	if strings.HasPrefix(clean, "```") {
		clean = strings.TrimPrefix(clean, "```")
		if newline := strings.IndexByte(clean, '\n'); newline >= 0 {
			clean = clean[newline+1:]
		}
		clean = strings.TrimSuffix(strings.TrimSpace(clean), "```")
	}
	return clean
}

// repairJSONStrings makes the string literals of almost-JSON valid: it escapes raw control characters, and doubles every backslash that does not start a JSON escape, so that it reads as the backslash the model meant (a regular expression's \. or \|, a Windows path's C:\users). Valid escapes, and everything outside string literals, are left alone.
func repairJSONStrings(s string) string {
	var out strings.Builder
	inString := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inString && c == '\\':
			if n := jsonEscapeLength(s[i:]); n > 0 {
				out.WriteString(s[i : i+n])
				i += n - 1
				continue
			}
			out.WriteString(`\\`)
			continue
		case c == '"':
			inString = !inString
		case inString && c < 0x20:
			out.WriteString(controlEscape(c))
			continue
		}
		out.WriteByte(c)
	}
	return out.String()
}

// jsonEscapeLength returns the length of the JSON escape sequence at the start of s, or 0 when s does not start with one.
func jsonEscapeLength(s string) int {
	if len(s) < 2 {
		return 0
	}
	switch s[1] {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		return 2
	case 'u':
		if len(s) >= 6 && isHex(s[2:6]) {
			return 6
		}
	}
	return 0
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if !strings.ContainsRune("0123456789abcdefABCDEF", rune(s[i])) {
			return false
		}
	}
	return true
}

func controlEscape(c byte) string {
	switch c {
	case '\n':
		return `\n`
	case '\r':
		return `\r`
	case '\t':
		return `\t`
	}
	return fmt.Sprintf(`\u%04x`, c)
}

// acpTextChunk is one streamed piece of answer text.
func acpTextChunk(text string, usage *genai.GenerateContentResponseUsageMetadata) *genai.GenerateContentResponse {
	return &genai.GenerateContentResponse{
		Candidates:    []*genai.Candidate{{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: text}}}}},
		UsageMetadata: usage,
	}
}

// acpThoughtChunk is one streamed piece of internal reasoning text.
func acpThoughtChunk(text string, usage *genai.GenerateContentResponseUsageMetadata) *genai.GenerateContentResponse {
	return &genai.GenerateContentResponse{
		Candidates:    []*genai.Candidate{{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: text, Thought: true}}}}},
		UsageMetadata: usage,
	}
}

// acpFinalResponse carries the remaining parts with agy's stop reason and token usage. ACP reports output tokens including reasoning (totalTokens = inputTokens + outputTokens), so they are passed as candidate tokens only.
func acpFinalResponse(parts []*genai.Part, result acp.PromptResponse) *genai.GenerateContentResponse {
	finish := genai.FinishReasonStop
	switch result.StopReason {
	case acp.StopReasonMaxTokens:
		finish = genai.FinishReasonMaxTokens
	case acp.StopReasonRefusal:
		finish = genai.FinishReasonSafety
	}
	resp := &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{
		Content:      &genai.Content{Role: genai.RoleModel, Parts: parts},
		FinishReason: finish,
	}}}
	if u := result.Usage; u != nil {
		meta := &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(promptTokens(u)),
			CandidatesTokenCount: int32(u.OutputTokens),
		}
		if u.CachedReadTokens != nil {
			meta.CachedContentTokenCount = int32(*u.CachedReadTokens)
		}
		resp.UsageMetadata = meta
	}
	return resp
}

// promptTokens is the whole prompt agy read. Its inputTokens leave out the tokens read from cache, which in a continued session are most of the conversation; Gemini's prompt token count, which Claude Code sizes its context from, includes them.
func promptTokens(u *acp.Usage) int64 {
	tokens := u.InputTokens
	if u.CachedReadTokens != nil {
		tokens += *u.CachedReadTokens
	}
	return tokens
}

// ---- streaming ----

// textEnvelopePrefix matches the start of a {"text": "..."} answer, optionally inside a code fence. partialTextEnvelope matches input that is still a prefix of such a start, so the decision waits for more chunks. Both are anchored RE2 expressions and run in linear time.
var (
	textEnvelopePrefix  = regexp.MustCompile("^\\s*(?:```(?:json)?\\s*)?\\{\\s*\"text\"\\s*:\\s*\"")
	partialTextEnvelope = regexp.MustCompile("^\\s*(?:`{1,3}(?:j(?:s(?:o(?:n)?)?)?)?\\s*)?(?:\\{\\s*(?:\"(?:t(?:e(?:x(?:t(?:\"\\s*(?::\\s*)?)?)?)?)?)?)?)?$")
)

type envelopeMode int

const (
	envelopeUndecided envelopeMode = iota
	// envelopeText streams the decoded string value of a {"text": ...} answer.
	envelopeText
	// envelopePlain streams an answer that ignored the JSON format verbatim, exactly as parseAnswer would return it.
	envelopePlain
	// envelopeBuffered holds the answer (a tool call, or a JSON shape that cannot be streamed safely) until the prompt ends.
	envelopeBuffered
	// envelopeDone means the text value has been streamed completely, or streaming stopped at malformed input.
	envelopeDone
)

// envelopeDecoder turns answer chunks into text deltas as early as the envelope allows. The complete answer is still parsed at the end, which stays authoritative for tool calls.
type envelopeDecoder struct {
	raw     strings.Builder
	mode    envelopeMode
	pos     int
	emitted strings.Builder
	answer  []*genai.Part
	exact   bool
}

// feed adds a chunk and returns the text that can be shown now.
func (d *envelopeDecoder) feed(chunk string) string {
	d.raw.WriteString(chunk)
	s := d.raw.String()
	if d.mode == envelopeUndecided {
		trimmed := strings.TrimLeftFunc(s, unicode.IsSpace)
		switch {
		case trimmed == "":
			return ""
		case trimmed[0] != '{' && trimmed[0] != '`':
			d.mode = envelopePlain
		case textEnvelopePrefix.MatchString(s):
			d.mode = envelopeText
			d.pos = textEnvelopePrefix.FindStringIndex(s)[1]
		case partialTextEnvelope.MatchString(s):
			return ""
		default:
			// A tool call, another key first, or a fence around something else.
			d.mode = envelopeBuffered
		}
	}
	var delta string
	switch d.mode {
	case envelopePlain:
		delta = s[d.pos:]
		d.pos = len(s)
	case envelopeText:
		delta = d.decodeText(s)
	}
	d.emitted.WriteString(delta)
	return delta
}

// decodeText decodes the string value from d.pos up to the last point where no escape sequence is cut off, and stops at its closing quote.
func (d *envelopeDecoder) decodeText(s string) string {
	end, closed := d.pos, false
scan:
	for end < len(s) {
		switch s[end] {
		case '"':
			closed = true
			break scan
		case '\\':
			n := escapeLength(s[end:])
			if n == 0 {
				break scan
			}
			end += n
		default:
			end++
		}
	}
	text, err := decodeJSONStringBody(s[d.pos:end])
	if err != nil {
		d.mode = envelopeDone
		return ""
	}
	d.pos = end
	if closed {
		d.mode = envelopeDone
	}
	return text
}

// escapeLength returns the length of the escape sequence at the start of s, or 0 when it is not complete yet. A backslash that starts no JSON escape counts as itself and the character after it, which decodeJSONStringBody keeps as written. A UTF-16 high surrogate is kept with the low surrogate that follows it so the pair decodes to one character.
func escapeLength(s string) int {
	if len(s) < 2 {
		return 0
	}
	if s[1] != 'u' {
		return 2
	}
	if !isHex(s[2:min(len(s), 6)]) {
		return 2
	}
	if len(s) < 6 {
		return 0
	}
	code, err := strconv.ParseUint(s[2:6], 16, 16)
	if err != nil || code < 0xD800 || code > 0xDBFF {
		return 6
	}
	if len(s) < 8 {
		return 0
	}
	if s[6:8] != `\u` {
		return 6
	}
	if len(s) < 12 {
		return 0
	}
	return 12
}

// decodeJSONStringBody decodes the inside of a JSON string literal the way parseAnswer does, accepting raw control characters and stray backslashes. body never holds an unescaped quote, because decodeText stops before the closing one.
func decodeJSONStringBody(body string) (string, error) {
	var text string
	err := json.Unmarshal([]byte(repairJSONStrings(`"`+body+`"`)), &text)
	return text, err
}

// finish parses the complete answer and returns the parts not streamed yet: the rest of the text, if any, and any tool calls. The whole answer is kept in answer, and exact reports whether it was one clean envelope.
func (d *envelopeDecoder) finish(result acp.PromptResponse) ([]*genai.Part, error) {
	parts, exact, err := acpAnswerParts(d.raw.String(), result)
	d.answer, d.exact = parts, exact
	if err != nil || d.emitted.Len() == 0 {
		return parts, err
	}
	emitted := d.emitted.String()
	var rest []*genai.Part
	for _, part := range parts {
		if part.FunctionCall != nil {
			rest = append(rest, part)
			continue
		}
		if tail, ok := strings.CutPrefix(part.Text, emitted); ok && tail != "" {
			rest = append(rest, &genai.Part{Text: tail})
		}
	}
	return rest, nil
}
