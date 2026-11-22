package provider

import (
	"os"
	"strings"

	"github.com/yldm-ai/neo-switch/internal/translate"
)

// Background model.
//
// At the end of every turn Claude Code asks for a prompt suggestion, and it names sessions with short side requests. Answering them with the main model spends that model's quota on work nobody reads closely; agy reports quotas per model ("You have exhausted your quota on this model"). So these requests go to a cheaper model of the same plan, and everything that builds on the conversation keeps the main model: the main thread, compactions, subagents, workflow agents, memory extraction, and Claude Code's WebSearch.
//
// Claude Code's hint headers (translate.Origin) tell the requests apart:
//   - A prompt suggestion is class auxiliary, kind followup-generation.
//   - A session title and Claude Code's other tool-free side queries are kind summary-extraction without tools.
//   - Memory extraction and the away summary are summary-extraction too, but forked with the main thread's tools. The two cannot be told apart, so both keep the main model.

// DefaultBackgroundModel answers background requests unless NEOSWITCH_BACKGROUND_MODEL names another model.
const DefaultBackgroundModel = "gemini-3.8-flash"

// backgroundModel returns the model for background requests, or "" when they use the main model:
//   - NEOSWITCH_BACKGROUND_MODEL=main keeps them on the main model.
//   - A NEOSWITCH_BACKGROUND_MODEL naming a model selects it.
//   - Without the variable, DefaultBackgroundModel answers them, unless GEMINI_MODEL chose a main model other than the default: that choice is the user's, and might already be the cheaper one.
func backgroundModel(main string) string {
	background, set := os.LookupEnv("NEOSWITCH_BACKGROUND_MODEL")
	background = strings.TrimSpace(background)
	switch {
	case strings.EqualFold(background, "main"):
		return ""
	case set && background != "":
	case main != DefaultModel:
		return ""
	default:
		background = DefaultBackgroundModel
	}
	if background == main {
		return ""
	}
	return background
}

// modelFor returns the model that answers the request.
func (a *AntigravityACP) modelFor(req translate.Request) string {
	if a.background == "" || a.backgroundMissing.Load() || !backgroundRequest(req) {
		return a.model
	}
	return a.background
}

// backgroundRequest reports whether a request is one of Claude Code's background requests the background model answers.
func backgroundRequest(req translate.Request) bool {
	o := req.Origin
	if o.Class != "auxiliary" || o.Agent != "" || req.WebSearch != nil {
		return false
	}
	switch o.Kind {
	case "followup-generation":
		return true
	case "summary-extraction":
		return req.Config == nil || len(req.Config.Tools) == 0
	}
	return false
}
