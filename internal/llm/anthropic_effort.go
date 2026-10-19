package llm

import (
	"regexp"
	"slices"
	"strings"
)

// anthropicModel is what Claude Code knows of a model: the efforts it takes
// (none: it takes no effort), the one sent when none is set, the output
// tokens it is given and whether it thinks adaptively.
type anthropicModel struct {
	levels    []string
	def       string
	maxTokens int
	adaptive  bool
}

var (
	anthropicLowToHigh = []string{"low", "medium", "high"}
	anthropicNoXHigh   = []string{"low", "medium", "high", "max"}
	anthropicAllLevels = []string{"low", "medium", "high", "xhigh", "max"}
)

// anthropicModels follows Claude Code 2.1.303's model catalog and checks.
var anthropicModels = map[string]anthropicModel{
	"claude-opus-4-0":   {maxTokens: 32000},
	"claude-opus-4-1":   {maxTokens: 32000},
	"claude-sonnet-4-0": {maxTokens: 32000},
	"claude-sonnet-4-5": {maxTokens: 32000},
	"claude-haiku-4-5":  {maxTokens: 32000},
	"claude-opus-4-5":   {anthropicLowToHigh, "high", 32000, false},
	"claude-opus-4-6":   {anthropicNoXHigh, "high", 64000, true},
	"claude-sonnet-4-6": {anthropicNoXHigh, "high", 32000, true},
	"claude-opus-4-7":   {anthropicAllLevels, "xhigh", 64000, true},
	"claude-opus-4-8":   {anthropicAllLevels, "high", 64000, true},
	"claude-opus-5":     {anthropicAllLevels, "high", 64000, true},
	"claude-sonnet-5":   {anthropicAllLevels, "high", 64000, true},
	"claude-fable-5":    {anthropicAllLevels, "high", 64000, true},
	"claude-fable-5-1":  {anthropicAllLevels, "high", 64000, true},
	"claude-mythos-5":   {anthropicAllLevels, "high", 64000, true},
	"claude-mythos-5-1": {anthropicAllLevels, "high", 64000, true},
	"claude-opus-5-5":   {anthropicAllLevels, "medium", 128000, true},
	"claude-sonnet-5-5": {anthropicAllLevels, "medium", 128000, true},
	"claude-haiku-5-5":  {anthropicAllLevels, "medium", 128000, true},
}

var (
	anthropicDateSuffix   = regexp.MustCompile(`-\d{8}$`)
	anthropicBedrockTail  = regexp.MustCompile(`-v\d+(:\d+)?$`)
	anthropicOldFirstName = map[string]string{"claude-opus-4": "claude-opus-4-0", "claude-sonnet-4": "claude-sonnet-4-0"}
)

// canonicalAnthropicModel is a model's name without a provider's prefix,
// its date, version or context suffix: claude-sonnet-4-20250514 is
// claude-sonnet-4-0.
func canonicalAnthropicModel(model string) string {
	name := strings.ToLower(strings.TrimSpace(model))
	if i := strings.Index(name, "claude-"); i > 0 {
		name = name[i:]
	}
	name = strings.TrimSuffix(name, "[1m]")
	name = strings.TrimSuffix(name, "-latest")
	if i := strings.Index(name, "@"); i >= 0 {
		name = name[:i]
	}
	name = anthropicBedrockTail.ReplaceAllString(name, "")
	name = anthropicDateSuffix.ReplaceAllString(name, "")
	if renamed, ok := anthropicOldFirstName[name]; ok {
		name = renamed
	}
	return name
}

// lookupAnthropicModel is what is known of a model; known is false for a
// model Claude Code's catalog did not have.
func lookupAnthropicModel(model string) (anthropicModel, bool) {
	name := canonicalAnthropicModel(model)
	switch {
	case strings.HasPrefix(name, "claude-3-7"):
		return anthropicModel{maxTokens: 32000}, true
	case strings.HasPrefix(name, "claude-3"):
		return anthropicModel{maxTokens: 8192}, true
	}
	known, ok := anthropicModels[name]
	return known, ok
}

// anthropicSentEffort is the effort sent for a model, as Claude Code sends
// it: none to a model that takes none; its default when none is set or the
// one set is no level; xhigh or max as high to a model without them. A
// model not known gets what is set, and nothing when nothing is.
func anthropicSentEffort(model, effort string) string {
	known, ok := lookupAnthropicModel(model)
	if !ok {
		return effort
	}
	if len(known.levels) == 0 {
		return ""
	}
	if !slices.Contains(anthropicAllLevels, effort) {
		return known.def
	}
	if !slices.Contains(known.levels, effort) {
		return "high"
	}
	return effort
}
