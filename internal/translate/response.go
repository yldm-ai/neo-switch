package translate

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/anthropic"
)

// UpstreamError is a Gemini response that completed at the HTTP level but cannot be presented to Claude Code as a successful turn.
type UpstreamError struct {
	Reason string
}

func (e *UpstreamError) Error() string { return e.Reason }

// UnavailableError means the upstream cannot answer any request until the user acts, for example by signing in again. Message is a complete sentence that names the fix; the server passes it to Claude Code unchanged, with a status Claude Code does not retry.
type UnavailableError struct {
	Message string
}

func (e *UnavailableError) Error() string { return e.Message }

// RetryableError means the upstream failed in a way the next attempt is likely to fix, for example a crashed agy that the next request starts again. The server answers with a status Claude Code retries with backoff; Message is shown only if every retry fails.
type RetryableError struct {
	Message string
}

func (e *RetryableError) Error() string { return e.Message }

// RateLimitError means the upstream is limiting how fast requests may come, for a short while. The server answers 429, which Claude Code retries with backoff; Message is shown only if every retry fails.
type RateLimitError struct {
	Message string
	// Retried means the upstream already retried the request itself before giving up, so the server adds x-should-retry: false and Claude Code shows Message at once instead of stacking its own retries on top.
	Retried bool
}

func (e *RateLimitError) Error() string { return e.Message }

// UnsupportedError is a request feature NeoSwitch cannot carry to the model. Message is a complete sentence for the user; the server reports it without the request field path the error is wrapped in.
type UnsupportedError struct {
	Message string
}

func (e *UnsupportedError) Error() string { return e.Message }

// FromGemini translates a complete (non-streaming) Gemini response, skipping thought parts.
func FromGemini(resp *genai.GenerateContentResponse, model string) (anthropic.Response, error) {
	return FromGeminiWithOptions(resp, model, false)
}

// FromGeminiWithOptions translates a complete (non-streaming) Gemini response, optionally emitting Anthropic thinking blocks when includeThoughts is true.
func FromGeminiWithOptions(resp *genai.GenerateContentResponse, model string, includeThoughts bool) (anthropic.Response, error) {
	out := anthropic.Response{
		ID:      NewMessageID(),
		Type:    "message",
		Role:    "assistant",
		Model:   model,
		Content: []anthropic.ContentBlock{},
		Usage:   Usage(resp.UsageMetadata),
	}
	if len(resp.Candidates) == 0 {
		if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
			out.StopReason = "refusal"
			return out, nil
		}
		return anthropic.Response{}, &UpstreamError{Reason: "upstream returned no candidates"}
	}
	candidate := resp.Candidates[0]
	if candidate.Content != nil {
		blocks, err := BlocksWithOptions(candidate.Content.Parts, includeThoughts)
		if err != nil {
			return anthropic.Response{}, err
		}
		out.Content = append(out.Content, blocks...)
	}
	stop, err := StopReason(candidate.FinishReason, HasToolUse(out.Content))
	if err != nil {
		return anthropic.Response{}, err
	}
	out.StopReason = stop
	return out, nil
}

// Blocks converts Gemini parts to Anthropic content blocks, skipping thought parts.
func Blocks(parts []*genai.Part) ([]anthropic.ContentBlock, error) {
	return BlocksWithOptions(parts, false)
}

// BlocksWithOptions converts Gemini parts to Anthropic content blocks. It is used for whole responses and for each streamed chunk; consecutive text and thinking blocks from successive chunks are merged by the stream writer.
func BlocksWithOptions(parts []*genai.Part, includeThoughts bool) ([]anthropic.ContentBlock, error) {
	var blocks []anthropic.ContentBlock
	for i, part := range parts {
		switch {
		case part == nil:
		case part.Thought:
			if includeThoughts && part.Text != "" {
				sig := ""
				blocks = append(blocks, anthropic.ContentBlock{
					Type:      "thinking",
					Thinking:  part.Text,
					Signature: &sig,
				})
			}
		case part.FunctionCall != nil:
			input, err := json.Marshal(part.FunctionCall.Args)
			if err != nil {
				return nil, fmt.Errorf("marshal function call %d: %w", i, err)
			}
			if part.FunctionCall.Args == nil {
				input = []byte("{}")
			}
			blocks = append(blocks, anthropic.ContentBlock{
				Type:  "tool_use",
				ID:    newToolID(part.ThoughtSignature, part.FunctionCall.ID),
				Name:  part.FunctionCall.Name,
				Input: input,
			})
		case part.Text != "":
			blocks = append(blocks, anthropic.ContentBlock{Type: "text", Text: part.Text})
		case part.InlineData != nil, part.FileData != nil, part.ExecutableCode != nil, part.CodeExecutionResult != nil:
			return nil, &UpstreamError{Reason: fmt.Sprintf("upstream returned part %d with a type Claude Code cannot receive", i)}
		}
	}
	return blocks, nil
}

func HasToolUse(blocks []anthropic.ContentBlock) bool {
	for _, b := range blocks {
		if b.Type == "tool_use" {
			return true
		}
	}
	return false
}

// StopReason maps a Gemini finish reason to an Anthropic stop_reason. Finish reasons that mean the turn failed (a malformed or rejected tool call, a missing thought signature) become errors: reporting them as end_turn would make Claude Code silently treat a broken turn as finished.
func StopReason(reason genai.FinishReason, hasToolUse bool) (string, error) {
	switch reason {
	case "", genai.FinishReasonUnspecified, genai.FinishReasonStop:
		if hasToolUse {
			return "tool_use", nil
		}
		return "end_turn", nil
	case genai.FinishReasonMaxTokens, genai.FinishReasonContinuation:
		return "max_tokens", nil
	case genai.FinishReasonSafety, genai.FinishReasonRecitation, genai.FinishReasonBlocklist,
		genai.FinishReasonProhibitedContent, genai.FinishReasonSPII, genai.FinishReasonLanguage,
		genai.FinishReasonImageSafety, genai.FinishReasonImageProhibitedContent, genai.FinishReasonImageRecitation,
		genai.FinishReasonModelArmor:
		return "refusal", nil
	default:
		return "", &UpstreamError{Reason: "Gemini stopped with finish reason " + strings.ToUpper(string(reason))}
	}
}

// Usage maps Gemini token accounting onto Anthropic's: cached prompt tokens are reported separately, as Anthropic does, and thinking tokens count as output because they are billed as output.
func Usage(meta *genai.GenerateContentResponseUsageMetadata) anthropic.Usage {
	if meta == nil {
		return anthropic.Usage{}
	}
	return anthropic.Usage{
		InputTokens:          int(meta.PromptTokenCount - meta.CachedContentTokenCount),
		OutputTokens:         int(meta.CandidatesTokenCount + meta.ThoughtsTokenCount),
		CacheReadInputTokens: int(meta.CachedContentTokenCount),
	}
}

func NewMessageID() string {
	return "msg_ns_" + rand.Text()
}
