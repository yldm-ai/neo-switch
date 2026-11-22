package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync/atomic"

	"google.golang.org/genai"

	"github.com/yldm-ai/neo-switch/internal/translate"
)

const DefaultModel = "gemini-4-neon"

// Gemini is the upstream backend. Credentials are resolved by genai from the standard environment: GEMINI_API_KEY / GOOGLE_API_KEY for the Gemini Developer API, or GOOGLE_GENAI_USE_VERTEXAI=true with GOOGLE_CLOUD_PROJECT, GOOGLE_CLOUD_LOCATION and Application Default Credentials for Vertex AI.
type Gemini struct {
	client *genai.Client
	Model  string
	// thinkingLevelRejected records that Model answered a thinking level with HTTP 400, so later requests on this backend stop sending one.
	thinkingLevelRejected atomic.Bool
}

// NewGeminiFromEnv builds the backend from the process environment. GEMINI_BASE_URL overrides the API host; earlier NeoSwitch versions expected the version path in it, so a trailing /v1beta or /v1 is stripped because genai appends the version itself.
func NewGeminiFromEnv(ctx context.Context) (*Gemini, error) {
	config := &genai.ClientConfig{
		// No client-level timeout: responses are streamed and long agent turns legitimately run for minutes. Each request is bounded by its context instead.
		HTTPClient: &http.Client{},
	}
	// genai >= 1.79 defaults the Developer API to v1, where some preview models are not served. NeoSwitch has always called v1beta, so keep it unless the user picks a version with GOOGLE_GENAI_API_VERSION. Vertex AI keeps genai's own default.
	if os.Getenv("GOOGLE_GENAI_API_VERSION") == "" && !usesVertex() {
		config.HTTPOptions.APIVersion = "v1beta"
	}
	if base := strings.TrimSpace(os.Getenv("GEMINI_BASE_URL")); base != "" {
		base = strings.TrimRight(base, "/")
		base = strings.TrimSuffix(strings.TrimSuffix(base, "/v1beta"), "/v1")
		config.HTTPOptions.BaseURL = base + "/"
	}
	if !usesVertex() && strings.TrimSpace(os.Getenv("GEMINI_API_KEY")) == "" && strings.TrimSpace(os.Getenv("GOOGLE_API_KEY")) == "" {
		return nil, fmt.Errorf("no Gemini credentials: set GEMINI_API_KEY, or GOOGLE_GENAI_USE_VERTEXAI=true with GOOGLE_CLOUD_PROJECT and GOOGLE_CLOUD_LOCATION")
	}
	client, err := genai.NewClient(ctx, config)
	if err != nil {
		// genai appends a %#v dump of the ClientConfig to some errors, which can include the API key; keep only the reason.
		reason, _, _ := strings.Cut(err.Error(), " ClientConfig:")
		return nil, fmt.Errorf("configure Gemini client: %s", strings.TrimSpace(reason))
	}
	model := strings.TrimSpace(os.Getenv("GEMINI_MODEL"))
	if model == "" {
		model = DefaultModel
	}
	return &Gemini{client: client, Model: model}, nil
}

// usesVertex mirrors genai's backend selection from the environment (GOOGLE_GENAI_USE_ENTERPRISE takes precedence over GOOGLE_GENAI_USE_VERTEXAI).
func usesVertex() bool {
	truthy := func(v string) bool { v = strings.ToLower(v); return v == "1" || v == "true" }
	if v, ok := os.LookupEnv("GOOGLE_GENAI_USE_ENTERPRISE"); ok {
		return truthy(v)
	}
	return truthy(os.Getenv("GOOGLE_GENAI_USE_VERTEXAI"))
}

// BackendName describes the configured backend for diagnostics without exposing credentials.
func (g *Gemini) BackendName() string {
	if g.client.ClientConfig().Backend == genai.BackendVertexAI {
		return "Vertex AI"
	}
	return "Gemini Developer API"
}

// Generate sends the request with its thinking level. If the model rejects the level, the request is sent once more without it.
func (g *Gemini) Generate(ctx context.Context, req translate.Request) (*genai.GenerateContentResponse, error) {
	req = identified(req)
	config, leveled := g.generateConfig(req)
	resp, err := g.client.Models.GenerateContent(ctx, g.Model, req.Contents, config)
	if leveled && g.rejectedThinkingLevel(err) {
		return g.client.Models.GenerateContent(ctx, g.Model, req.Contents, req.Config)
	}
	return resp, err
}

// Stream sends the request with its thinking level. If the model rejects the level before the first chunk, the request is sent once more without it; an error after a chunk was yielded is passed through, because the caller has already forwarded output.
func (g *Gemini) Stream(ctx context.Context, req translate.Request) iter.Seq2[*genai.GenerateContentResponse, error] {
	req = identified(req)
	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		config, leveled := g.generateConfig(req)
		retry := false
		for resp, err := range g.client.Models.GenerateContentStream(ctx, g.Model, req.Contents, config) {
			if leveled && g.rejectedThinkingLevel(err) {
				retry = true
				break
			}
			leveled = false
			if !yield(resp, err) {
				return
			}
		}
		if !retry {
			return
		}
		for resp, err := range g.client.Models.GenerateContentStream(ctx, g.Model, req.Contents, req.Config) {
			if !yield(resp, err) {
				return
			}
		}
	}
}

// ModelIdentity tells the model answering through NeoSwitch that the model Claude Code's system prompt names is not necessarily it: the prompt names the Claude model the session asks for, and a model told so answers questions about itself as that model. It names no model, so it stays true for requests another model answers, such as the background model's, and it never changes, so the cached prefix of the instructions stays the same.
const ModelIdentity = "Where these instructions name the model you run on, that is the name Claude Code shows for the session, which NeoSwitch answers with another model; about yourself, say what you know of yourself."

// identified opens the request's system instructions with ModelIdentity, on a copy: the shared request is never modified. It goes first, before Claude Code's text and the system messages appended to it during a conversation, so the instructions keep one stable prefix.
func identified(req translate.Request) translate.Request {
	config := genai.GenerateContentConfig{}
	if req.Config != nil {
		config = *req.Config
	}
	system := genai.Content{}
	if config.SystemInstruction != nil {
		system = *config.SystemInstruction
	}
	system.Parts = append([]*genai.Part{genai.NewPartFromText(ModelIdentity)}, system.Parts...)
	config.SystemInstruction = &system
	req.Config = &config
	return req
}

// generateConfig returns the config to send and whether it carries the request's thinking level. The level is set on a copy so the shared request config is never modified. It is left out when the request names none or when this model has already rejected one. The per-level support of each model was not verified against the live API, which is why a rejection drops the level instead of failing the request.
func (g *Gemini) generateConfig(req translate.Request) (*genai.GenerateContentConfig, bool) {
	if req.ThinkingLevel == "" || g.thinkingLevelRejected.Load() {
		return req.Config, false
	}
	config := *req.Config
	var thinking genai.ThinkingConfig
	if req.Config.ThinkingConfig != nil {
		thinking = *req.Config.ThinkingConfig
	}
	thinking.ThinkingLevel = req.ThinkingLevel
	config.ThinkingConfig = &thinking
	return &config, true
}

// rejectedThinkingLevel reports whether err is Gemini refusing the thinking configuration (HTTP 400 naming thinking), and if so remembers that the model does not accept a thinking level.
func (g *Gemini) rejectedThinkingLevel(err error) bool {
	var apiErr genai.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != http.StatusBadRequest || !strings.Contains(strings.ToLower(apiErr.Message), "thinking") {
		return false
	}
	g.thinkingLevelRejected.Store(true)
	return true
}

// CountTokens counts the prompt as Gemini would see it. Vertex AI accepts the system instruction and tools directly. The Gemini Developer API countTokens call through genai accepts contents only, so there the system instruction and the tool declarations are prepended as text contents: the result is an estimate that can differ from the billed count by the framing tokens Gemini adds around them.
func (g *Gemini) CountTokens(ctx context.Context, req translate.Request) (int, error) {
	req = identified(req)
	contents := req.Contents
	var config *genai.CountTokensConfig
	if g.client.ClientConfig().Backend == genai.BackendVertexAI {
		config = &genai.CountTokensConfig{SystemInstruction: req.Config.SystemInstruction, Tools: req.Config.Tools}
	} else {
		var preamble []*genai.Content
		if req.Config.SystemInstruction != nil {
			preamble = append(preamble, &genai.Content{Role: genai.RoleUser, Parts: req.Config.SystemInstruction.Parts})
		}
		if len(req.Config.Tools) > 0 {
			declarations, err := json.Marshal(req.Config.Tools)
			if err != nil {
				return 0, fmt.Errorf("encode tools for token counting: %w", err)
			}
			preamble = append(preamble, genai.NewContentFromText(string(declarations), genai.RoleUser))
		}
		contents = append(preamble, contents...)
	}
	resp, err := g.client.Models.CountTokens(ctx, g.Model, contents, config)
	if err != nil {
		return 0, err
	}
	return int(resp.TotalTokens), nil
}

// Check verifies that the configured credentials can use the model. models.get separates a missing model from a missing permission, and a short generation catches region, quota and billing limits that only apply when generating.
func (g *Gemini) Check(ctx context.Context) error {
	if _, err := g.client.Models.Get(ctx, g.Model, nil); err != nil {
		return checkError(ctx, "look up model "+g.Model, err)
	}
	if _, err := g.client.Models.GenerateContent(ctx, g.Model, genai.Text("Reply with OK."), &genai.GenerateContentConfig{MaxOutputTokens: 256}); err != nil {
		return checkError(ctx, "generate with "+g.Model, err)
	}
	return nil
}

// ListModels returns the IDs of the models these credentials can generate content with, sorted, following every page of models.list. The Gemini Developer API reports each model's supported methods, so embedding-only and other non-generating models are left out. Vertex AI reports none, so there Gemini models are kept by name.
func (g *Gemini) ListModels(ctx context.Context) ([]string, error) {
	vertex := g.client.ClientConfig().Backend == genai.BackendVertexAI
	var ids []string
	page, err := g.client.Models.List(ctx, nil)
	for {
		if err != nil {
			return nil, checkError(ctx, "list models", err)
		}
		for _, model := range page.Items {
			id := model.Name[strings.LastIndex(model.Name, "/")+1:]
			if id != "" && generatesContent(model, id, vertex) {
				ids = append(ids, id)
			}
		}
		page, err = page.Next(ctx)
		if errors.Is(err, genai.ErrPageDone) {
			break
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
}

func generatesContent(model *genai.Model, id string, vertex bool) bool {
	if vertex && len(model.SupportedActions) == 0 {
		return strings.HasPrefix(id, "gemini-")
	}
	return slices.Contains(model.SupportedActions, "generateContent")
}

func checkError(ctx context.Context, step string, err error) error {
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		return fmt.Errorf("%s: %s (HTTP %d %s: %s)", step, diagnose(apiErr), apiErr.Code, apiErr.Status, apiErr.Message)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%s: Gemini did not answer in time", step)
	}
	return fmt.Errorf("%s: could not reach Gemini: %w", step, err)
}

// diagnose names the cause of a failed check in terms the user can act on.
func diagnose(e genai.APIError) string {
	message := strings.ToLower(e.Message)
	switch {
	case e.Code == http.StatusUnauthorized || e.Status == "UNAUTHENTICATED" || strings.Contains(message, "api key not valid") || hasReason(e, "API_KEY_INVALID"):
		return "authentication failed: the API key or credentials were rejected"
	case strings.Contains(message, "location is not supported") || strings.Contains(message, "not available in your country") || strings.Contains(message, "region"):
		return "region restricted: the model is not available where this request comes from"
	case e.Code == http.StatusForbidden || e.Status == "PERMISSION_DENIED":
		return "permission denied: this account or project has no access to the model"
	case e.Code == http.StatusNotFound || e.Status == "NOT_FOUND":
		return "model not found: check GEMINI_MODEL and whether this API offers the model"
	case e.Code == http.StatusTooManyRequests || e.Status == "RESOURCE_EXHAUSTED":
		return "quota or rate limit reached"
	case e.Code >= http.StatusInternalServerError:
		return "Gemini service unavailable"
	default:
		return "request rejected"
	}
}

func hasReason(e genai.APIError, reason string) bool {
	for _, detail := range e.Details {
		if detail["reason"] == reason {
			return true
		}
	}
	return false
}
