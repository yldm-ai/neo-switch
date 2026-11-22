package translate

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/anthropic"
)

// Request is a translated Anthropic Messages request, ready for genai's Models.GenerateContent / GenerateContentStream / CountTokens.
type Request struct {
	Contents []*genai.Content
	Config   *genai.GenerateContentConfig
	// ThinkingLevel is the request's effort mapped onto Gemini's thinking levels, empty when the request names none or an effort NeoSwitch does not know. It is kept out of Config so each backend decides whether to apply it: the Antigravity ACP route selects agy's thought level from it, and the Gemini API route sends it as thinkingConfig.thinkingLevel, dropping it for the rest of the backend's life if the model rejects it, because per-model support for each level has not been verified against the live API.
	ThinkingLevel genai.ThinkingLevel
	// WebSearch is set when the request offers nothing but Anthropic's web search server tool, which is how Claude Code's WebSearch tool asks for one search. A backend that can search answers it with its own search (server.WebSearcher); for the others it stays unsupported (WebSearchUnsupported).
	WebSearch *WebSearch
	// Origin is who in Claude Code sent the request, empty when Claude Code did not say. Backends that keep state per conversation use it to tell the main thread from requests that share its history; the Gemini API route, which keeps none, ignores it.
	Origin Origin
}

// WebSearch is the configuration of a web search request.
type WebSearch struct {
	AllowedDomains []string
	BlockedDomains []string
	// MaxUses is how many searches the request allows, 0 for no limit.
	MaxUses int
}

// SearchResult is what a web search found, for the response NeoSwitch builds in the shape of Anthropic's web search server tool.
type SearchResult struct {
	// Queries are the searches run, in order.
	Queries []string
	Links   []SearchLink
	// Summary is the searching model's account of the results, "" when there is none.
	Summary string
	// ErrorCode is set when the search failed in a way Claude Code reports as a web search error: "unavailable" or "max_uses_exceeded".
	ErrorCode string
	Usage     *genai.GenerateContentResponseUsageMetadata
}

// SearchLink is one page a search found.
type SearchLink struct {
	Title, URL string
}

// WebSearchUnsupported is the error for a web search request on a backend that cannot search.
func WebSearchUnsupported() *UnsupportedError {
	return unsupportedServerTool("web_search").(*UnsupportedError)
}

// Origin is what Claude Code's hint headers say about a request. It sends them only when CLAUDE_CODE_GATEWAY_HINT_HEADERS is set, which NeoSwitch does for the projects it switches.
type Origin struct {
	// Session is the Claude Code session (X-Claude-Code-Session-Id).
	Session string
	// Class is main, subagent, workflow, compaction, auxiliary or side_reply (x-claude-code-request-class).
	Class string
	// Kind narrows a background request down, such as followup-generation or summary-extraction (x-claude-1p-request-class).
	Kind string
	// Agent is the subagent or workflow agent that sent the request (x-claude-code-agent-id).
	Agent string
}

// thinkingLevels maps Claude Code's effort onto Gemini's thinking levels. Gemini has no level above XHIGH, so max shares it.
var thinkingLevels = map[string]genai.ThinkingLevel{
	"low":    genai.ThinkingLevelLow,
	"medium": genai.ThinkingLevelMedium,
	"high":   genai.ThinkingLevelHigh,
	"xhigh":  genai.ThinkingLevelXhigh,
	"max":    genai.ThinkingLevelXhigh,
}

type toolCall struct {
	name   string
	callID string
	// order is the position of the call in the conversation, used to put parallel results back in call order.
	order int
}

// ToGemini translates an Anthropic Messages request. It does not require max_tokens so that /v1/messages/count_tokens can reuse it; the /v1/messages handler enforces that field itself.
func ToGemini(in anthropic.MessagesRequest) (Request, error) {
	if len(in.Messages) == 0 {
		return Request{}, fmt.Errorf("messages must not be empty")
	}
	config := &genai.GenerateContentConfig{
		MaxOutputTokens: int32(in.MaxTokens),
		Temperature:     toFloat32(in.Temperature),
		TopP:            toFloat32(in.TopP),
		StopSequences:   in.Stop,
	}
	if in.TopK != nil {
		k := float32(*in.TopK)
		config.TopK = &k
	}

	system, err := systemInstruction(in.System)
	if err != nil {
		return Request{}, err
	}
	config.SystemInstruction = system

	search := soleWebSearch(in.Tools)
	if search == nil {
		if config.Tools, err = tools(in.Tools); err != nil {
			return Request{}, err
		}
		if config.ToolConfig, err = toolConfig(in.ToolChoice); err != nil {
			return Request{}, err
		}
	}

	calls := map[string]toolCall{}
	var contents []*genai.Content
	for i, message := range in.Messages {
		if message.Role == "system" {
			// Claude Code inserts mid-conversation system messages (for example its environment block). Gemini's only system channel is systemInstruction, so their text is appended there in order.
			parts, err := systemParts(message.Content)
			if err != nil {
				return Request{}, fmt.Errorf("messages[%d]: %w", i, err)
			}
			if len(parts) > 0 {
				if config.SystemInstruction == nil {
					config.SystemInstruction = &genai.Content{}
				}
				config.SystemInstruction.Parts = append(config.SystemInstruction.Parts, parts...)
			}
			continue
		}
		if message.Role != "user" && message.Role != "assistant" {
			return Request{}, fmt.Errorf("messages[%d]: unsupported role %q", i, message.Role)
		}
		blocks, err := decodeBlocks(message.Content)
		if err != nil {
			return Request{}, fmt.Errorf("messages[%d]: %w", i, err)
		}
		content := &genai.Content{Role: geminiRole(message.Role)}
		var results []toolResultSlot
		for j, block := range blocks {
			parts, err := blockToParts(block, message.Role, calls)
			if err != nil {
				return Request{}, fmt.Errorf("messages[%d].content[%d]: %w", i, j, err)
			}
			if block.Type == "tool_result" {
				results = append(results, toolResultSlot{index: len(content.Parts), order: calls[block.ToolUseID].order})
			}
			content.Parts = append(content.Parts, parts...)
		}
		orderToolResults(content.Parts, results)
		if message.Role == "assistant" {
			signUnsignedStep(content.Parts)
		}
		// A message can legitimately translate to nothing (an assistant turn holding only Claude thinking blocks, or an empty text block). Gemini rejects contents without parts, so such turns are dropped rather than sent.
		if len(content.Parts) > 0 {
			contents = append(contents, content)
		}
	}
	if len(contents) == 0 {
		return Request{}, fmt.Errorf("messages contain no content Gemini can accept")
	}
	out := Request{Contents: contents, Config: config, WebSearch: search}
	if in.OutputConfig != nil {
		// An unknown effort is a hint NeoSwitch cannot map, not an invalid request: newer Claude Code versions may add levels.
		out.ThinkingLevel = thinkingLevels[strings.ToLower(strings.TrimSpace(in.OutputConfig.Effort))]
	}
	return out, nil
}

func systemInstruction(raw json.RawMessage) (*genai.Content, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	parts, err := systemParts(raw)
	if err != nil {
		return nil, fmt.Errorf("system: %w", err)
	}
	if len(parts) == 0 {
		return nil, nil
	}
	return &genai.Content{Parts: parts}, nil
}

// systemParts decodes system content (a string or text blocks) into non-empty text parts.
func systemParts(raw json.RawMessage) ([]*genai.Part, error) {
	blocks, err := decodeBlocks(raw)
	if err != nil {
		return nil, err
	}
	var parts []*genai.Part
	for i, b := range blocks {
		if b.Type != "text" {
			return nil, fmt.Errorf("system content[%d]: block type %q is not supported", i, b.Type)
		}
		if b.Text != "" {
			parts = append(parts, &genai.Part{Text: b.Text})
		}
	}
	return parts, nil
}

// soleWebSearch returns the web search configuration of a request whose only tool is Anthropic's web search server tool, and nil otherwise. A web search offered next to other tools stays unsupported: the model would have to choose between them, and only Anthropic's API runs that tool inside a turn.
func soleWebSearch(in []anthropic.Tool) *WebSearch {
	if len(in) != 1 || !strings.HasPrefix(in[0].Type, "web_search_") {
		return nil
	}
	return &WebSearch{AllowedDomains: in[0].AllowedDomains, BlockedDomains: in[0].BlockedDomains, MaxUses: in[0].MaxUses}
}

func tools(in []anthropic.Tool) ([]*genai.Tool, error) {
	if len(in) == 0 {
		return nil, nil
	}
	declarations := make([]*genai.FunctionDeclaration, 0, len(in))
	for i, tool := range in {
		if strings.HasPrefix(tool.Type, "advisor_") {
			// The advisor tool only offers the model an optional second opinion from another Claude model. Leaving it out keeps every request answerable; the routed model simply never consults an advisor.
			continue
		}
		if tool.Type != "" && tool.Type != "custom" {
			return nil, fmt.Errorf("tools[%d]: %w", i, unsupportedServerTool(tool.Type))
		}
		if strings.TrimSpace(tool.Name) == "" {
			return nil, fmt.Errorf("tools[%d]: name must not be empty", i)
		}
		declaration := &genai.FunctionDeclaration{Name: tool.Name, Description: tool.Description}
		if len(bytes.TrimSpace(tool.InputSchema)) > 0 {
			var schema any
			if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
				return nil, fmt.Errorf("tools[%d] %q input_schema: %w", i, tool.Name, err)
			}
			// parametersJsonSchema takes JSON Schema as-is. The older `parameters` field only accepts an OpenAPI subset and rejects keywords Claude Code and MCP schemas routinely carry ($schema, additionalProperties, propertyNames, ...).
			declaration.ParametersJsonSchema = schema
		}
		declarations = append(declarations, declaration)
	}
	if len(declarations) == 0 {
		return nil, nil
	}
	// All functions go in one Tool: the Gemini API documents function declarations as a list inside a single tool entry.
	return []*genai.Tool{{FunctionDeclarations: declarations}}, nil
}

func toolConfig(choice *anthropic.ToolChoice) (*genai.ToolConfig, error) {
	if choice == nil {
		return nil, nil
	}
	config := &genai.FunctionCallingConfig{}
	switch choice.Type {
	case "auto":
		config.Mode = genai.FunctionCallingConfigModeAuto
	case "any":
		config.Mode = genai.FunctionCallingConfigModeAny
	case "none":
		config.Mode = genai.FunctionCallingConfigModeNone
	case "tool":
		if strings.TrimSpace(choice.Name) == "" {
			return nil, fmt.Errorf("tool_choice of type tool requires name")
		}
		config.Mode = genai.FunctionCallingConfigModeAny
		config.AllowedFunctionNames = []string{choice.Name}
	default:
		return nil, fmt.Errorf("tool_choice type %q is not supported", choice.Type)
	}
	return &genai.ToolConfig{FunctionCallingConfig: config}, nil
}

// toolResultSlot is the position of a translated tool_result part in its content and the order of the call it answers.
type toolResultSlot struct {
	index int
	order int
}

// orderToolResults puts function responses into the order of the calls they answer, leaving other parts where they are. Claude Code sends parallel tool results in the order the tools finished; the Gemini Developer API returns calls without IDs, so Gemini pairs responses with same-name calls by position and would otherwise hand the model one file's content as another's.
func orderToolResults(parts []*genai.Part, slots []toolResultSlot) {
	if len(slots) < 2 {
		return
	}
	sorted := slices.Clone(slots)
	slices.SortStableFunc(sorted, func(a, b toolResultSlot) int { return a.order - b.order })
	responses := make([]*genai.Part, len(sorted))
	for i, slot := range sorted {
		responses[i] = parts[slot.index]
	}
	for i, slot := range slots {
		parts[slot.index] = responses[i]
	}
}

func blockToParts(block anthropic.ContentBlock, role string, calls map[string]toolCall) ([]*genai.Part, error) {
	switch block.Type {
	case "text":
		if block.Text == "" {
			// Gemini rejects a part with no data; an empty Anthropic text block carries nothing.
			return nil, nil
		}
		return []*genai.Part{{Text: block.Text}}, nil
	case "thinking", "redacted_thinking":
		// Claude's reasoning is signed for Anthropic and has no Gemini representation. It appears in history only when a session started on Claude is resumed here.
		return nil, nil
	case "advisor_tool_result":
		// Advisor consultations run on Anthropic's servers and show up in history only when a session started on Claude is resumed here. The routed model never had the advisor tool, so the exchange is dropped like Claude's thinking.
		return nil, nil
	case "server_tool_use":
		if block.Name == "advisor" {
			return nil, nil
		}
		return nil, fmt.Errorf("content block type %q is not supported", block.Type)
	case "image", "document":
		part, err := mediaPart(block)
		if err != nil {
			return nil, err
		}
		return []*genai.Part{part}, nil
	case "tool_use":
		if role != "assistant" {
			return nil, fmt.Errorf("tool_use is only valid in assistant messages")
		}
		if strings.TrimSpace(block.ID) == "" || strings.TrimSpace(block.Name) == "" {
			return nil, fmt.Errorf("tool_use requires id and name")
		}
		var args map[string]any
		if len(block.Input) > 0 && string(block.Input) != "null" {
			if err := json.Unmarshal(block.Input, &args); err != nil {
				return nil, fmt.Errorf("tool_use %q input: %w", block.Name, err)
			}
		}
		meta := parseToolID(block.ID)
		calls[block.ID] = toolCall{name: block.Name, callID: meta.CallID, order: len(calls)}
		return []*genai.Part{{
			FunctionCall:     &genai.FunctionCall{ID: meta.CallID, Name: block.Name, Args: args},
			ThoughtSignature: meta.Signature,
		}}, nil
	case "tool_result":
		if role != "user" {
			return nil, fmt.Errorf("tool_result is only valid in user messages")
		}
		call, ok := calls[block.ToolUseID]
		if !ok {
			return nil, fmt.Errorf("tool_result references unknown tool_use_id %q", block.ToolUseID)
		}
		text, media, err := toolResultContent(block.Content)
		if err != nil {
			return nil, fmt.Errorf("tool_result %q: %w", block.ToolUseID, err)
		}
		key := "output"
		if block.IsError {
			key = "error"
		}
		return []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
			ID:       call.callID,
			Name:     call.name,
			Response: map[string]any{key: text},
			Parts:    media,
		}}}, nil
	default:
		return nil, fmt.Errorf("content block type %q is not supported", block.Type)
	}
}

func mediaPart(block anthropic.ContentBlock) (*genai.Part, error) {
	if block.Source == nil {
		return nil, fmt.Errorf("%s block is missing source", block.Type)
	}
	switch block.Source.Type {
	case "base64":
		if block.Source.MediaType == "" {
			return nil, fmt.Errorf("%s block is missing media_type", block.Type)
		}
		data, err := base64.StdEncoding.DecodeString(block.Source.Data)
		if err != nil {
			return nil, fmt.Errorf("%s block data is not valid base64: %w", block.Type, err)
		}
		return &genai.Part{InlineData: &genai.Blob{MIMEType: block.Source.MediaType, Data: data}}, nil
	case "text":
		if block.Type != "document" {
			return nil, fmt.Errorf("%s block source type text is not supported", block.Type)
		}
		if block.Source.Data == "" {
			return nil, fmt.Errorf("document block text is empty")
		}
		return &genai.Part{Text: block.Source.Data}, nil
	default:
		// URL sources would make NeoSwitch fetch arbitrary URLs on the caller's behalf; Gemini's fileData only accepts its own file URIs.
		if block.Source.Type == "url" {
			return nil, &UnsupportedError{Message: "NeoSwitch cannot send " + block.Type + "s given as web links to this model. Download the file and attach it instead."}
		}
		return nil, fmt.Errorf("%s source type %q is not supported", block.Type, block.Source.Type)
	}
}

// unsupportedServerTool explains that a tool Anthropic runs on its own servers, such as web search, does not exist on the model NeoSwitch routes to.
func unsupportedServerTool(typ string) error {
	if strings.HasPrefix(typ, "web_search") {
		return &UnsupportedError{Message: "Web search (WebSearch) does not work with NeoSwitch on a Gemini API key. Sign in with /neoswitch:login to search with Google through Antigravity, ask without web search, or run /neoswitch:off to continue this session on Claude."}
	}
	if strings.HasPrefix(typ, "web_fetch") {
		return &UnsupportedError{Message: "Fetching web pages with Claude's built-in web fetch does not work with NeoSwitch. Ask without it, or run /neoswitch:off to continue this session on Claude."}
	}
	return &UnsupportedError{Message: fmt.Sprintf("Claude's built-in %q tool does not work with NeoSwitch. Ask without it, or run /neoswitch:off to continue this session on Claude.", typ)}
}

// toolResultContent splits Anthropic tool_result content into the text Gemini receives in functionResponse.response and the images it receives as multimodal functionResponse parts.
func toolResultContent(raw json.RawMessage) (string, []*genai.FunctionResponsePart, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return "", nil, nil
	}
	blocks, err := decodeBlocks(raw)
	if err != nil {
		return "", nil, err
	}
	var texts []string
	var media []*genai.FunctionResponsePart
	for i, block := range blocks {
		switch block.Type {
		case "text":
			texts = append(texts, block.Text)
		case "image", "document":
			part, err := mediaPart(block)
			if err != nil {
				return "", nil, fmt.Errorf("content[%d]: %w", i, err)
			}
			if part.InlineData == nil {
				texts = append(texts, part.Text)
				continue
			}
			media = append(media, &genai.FunctionResponsePart{InlineData: &genai.FunctionResponseBlob{
				MIMEType: part.InlineData.MIMEType,
				Data:     part.InlineData.Data,
			}})
		default:
			return "", nil, fmt.Errorf("content[%d]: block type %q is not supported", i, block.Type)
		}
	}
	return strings.Join(texts, "\n"), media, nil
}

func decodeBlocks(raw json.RawMessage) ([]anthropic.ContentBlock, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("content is empty")
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, err
		}
		return []anthropic.ContentBlock{{Type: "text", Text: text}}, nil
	}
	var blocks []anthropic.ContentBlock
	if err := json.Unmarshal(trimmed, &blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

func geminiRole(role string) string {
	if role == "assistant" {
		return genai.RoleModel
	}
	return genai.RoleUser
}

func toFloat32(v *float64) *float32 {
	if v == nil {
		return nil
	}
	f := float32(*v)
	return &f
}
