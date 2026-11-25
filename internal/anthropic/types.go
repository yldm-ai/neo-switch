package anthropic

import "encoding/json"

// MessagesRequest is the subset of the Anthropic Messages request that NeoSwitch translates. Fields Claude Code sends but Gemini has no equivalent for (metadata, thinking, cache_control) are accepted and ignored at the JSON level; content blocks are validated strictly by the translator.
type MessagesRequest struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	Messages    []Message       `json:"messages"`
	System      json.RawMessage `json:"system,omitempty"`
	Tools       []Tool          `json:"tools,omitempty"`
	ToolChoice  *ToolChoice     `json:"tool_choice,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	TopK        *int            `json:"top_k,omitempty"`
	Stop        []string        `json:"stop_sequences,omitempty"`
	// OutputConfig carries the effort Claude Code sends with every request (its /effort and --effort setting, "medium" by default).
	OutputConfig *OutputConfig `json:"output_config,omitempty"`
	// Safeguards carries Claude Code's server-side Auto Mode review requests (type "dangerous_tool_use").
	Safeguards []SafeguardRequest `json:"safeguards,omitempty"`
}

type SafeguardRequest struct {
	Type              string          `json:"type"`
	ClassifierContext json.RawMessage `json:"classifier_context,omitempty"`
}

type SafeguardResult struct {
	Type   string          `json:"type"`
	Status SafeguardStatus `json:"status"`
}

type SafeguardStatus struct {
	Type     string                      `json:"type"`
	ToolUses map[string]SafeguardVerdict `json:"tool_uses,omitempty"`
}

type SafeguardVerdict struct {
	Type        string `json:"type"`
	Outcome     string `json:"outcome,omitempty"`
	Explanation string `json:"explanation,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type OutputConfig struct {
	// Effort is low, medium, high, xhigh or max.
	Effort string `json:"effort,omitempty"`
}

type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type Tool struct {
	// Type is empty for client tools. Server tools (web_search, code_execution, ...) carry a versioned type and cannot be executed by Gemini.
	Type        string          `json:"type,omitempty"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
	// AllowedDomains, BlockedDomains and MaxUses configure the web search server tool.
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	BlockedDomains []string `json:"blocked_domains,omitempty"`
	MaxUses        int      `json:"max_uses,omitempty"`
}

type ToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Source    *Source         `json:"source,omitempty"`
}

// Source is the payload of image and document blocks.
type Source struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type Response struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Model        string         `json:"model"`
	Content      []ContentBlock `json:"content"`
	StopReason   string         `json:"stop_reason"`
	StopSequence     *string           `json:"stop_sequence"`
	Usage            Usage             `json:"usage"`
	SafeguardResults []SafeguardResult `json:"safeguard_results,omitempty"`
}

type Usage struct {
	InputTokens          int              `json:"input_tokens"`
	OutputTokens         int              `json:"output_tokens"`
	CacheReadInputTokens int              `json:"cache_read_input_tokens"`
	ServerToolUse        *ServerToolUsage `json:"server_tool_use,omitempty"`
}

// ServerToolUsage counts the server tool calls of a response; Claude Code counts web searches from it.
type ServerToolUsage struct {
	WebSearchRequests int `json:"web_search_requests"`
}

type CountTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}

type ErrorResponse struct {
	Type  string `json:"type"`
	Error Error  `json:"error"`
}

type Error struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
