package modelvariant

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	openCodeGPT5FamilyRE       = regexp.MustCompile(`(?:^|/)gpt-5(?:[.-]|$)`)
	openCodeGPT5VersionRE      = regexp.MustCompile(`(?:^|/)gpt-5[.-](\d+)(?:[.-]|$)`)
	openCodeGPT5ProRE          = regexp.MustCompile(`(?:^|/)gpt-5[.-]?pro(?:[.-]|$)`)
	openCodeGPT5VersionedProRE = regexp.MustCompile(`(?:^|/)gpt-5[.-]\d+[.-]pro(?:[.-]|$)`)
	openCodeAnthropicOpusRE    = regexp.MustCompile(`(?i)opus-(\d+)[.-](\d+)(?:[.@-]|$)|claude-(\d+)[.-](\d+)-opus(?:[.@-]|$)`)
	openCodeSAPReasoningRE     = regexp.MustCompile(`\bo[1-9]`)
)

func openCodeExcludedReasoningModel(id string) bool {
	excluded := []string{
		"deepseek-chat",
		"deepseek-reasoner",
		"deepseek-r1",
		"deepseek-v3",
		"minimax",
		"glm",
		"kimi",
		"k2p",
		"qwen",
		"big-pickle",
	}
	for _, value := range excluded {
		if strings.Contains(id, value) {
			return true
		}
	}
	return false
}

func openCodeWidelySupportedEfforts() []string {
	return []string{"low", "medium", "high"}
}

func openCodeEfforts() []string {
	return []string{"none", "minimal", "low", "medium", "high", "xhigh"}
}

func openCodeReasoningEfforts(apiID, releaseDate string) []string {
	id := strings.ToLower(apiID)
	if strings.Contains(id, "deep-research") {
		return []string{"medium"}
	}
	if efforts, ok := openCodeGPT5ChatReasoningEfforts(id); ok {
		return efforts
	}
	if openCodeGPT5ProRE.MatchString(id) {
		return []string{"high"}
	}
	if efforts, ok := openCodeGPT5CodexReasoningEfforts(id); ok {
		return efforts
	}
	if efforts, ok := openCodeVersionedGPT5ReasoningEfforts(id); ok {
		return efforts
	}
	efforts := append([]string{}, openCodeWidelySupportedEfforts()...)
	if openCodeGPT5FamilyRE.MatchString(id) {
		efforts = append([]string{"minimal"}, efforts...)
	}
	if releaseDate >= openCodeNoneEffortRelease {
		efforts = append([]string{"none"}, efforts...)
	}
	if releaseDate >= openCodeXHighEffortRelease {
		efforts = append(efforts, "xhigh")
	}
	return efforts
}

func openCodeCompatibleReasoningEfforts(id string) []string {
	apiID := strings.ToLower(id)
	if efforts, ok := openCodeGPT5ChatReasoningEfforts(apiID); ok {
		return efforts
	}
	if openCodeGPT5ProRE.MatchString(apiID) {
		return []string{"high"}
	}
	if efforts, ok := openCodeGPT5CodexReasoningEfforts(apiID); ok {
		return efforts
	}
	if efforts, ok := openCodeVersionedGPT5ReasoningEfforts(apiID); ok {
		return efforts
	}
	return openCodeEfforts()
}

func openCodeGPT5Version(apiID string) int {
	match := openCodeGPT5VersionRE.FindStringSubmatch(apiID)
	if len(match) != 2 {
		return 0
	}
	version, err := strconv.Atoi(match[1])
	if err != nil {
		return 99
	}
	return version
}

func openCodeVersionedGPT5ReasoningEfforts(apiID string) ([]string, bool) {
	if openCodeGPT5VersionedProRE.MatchString(apiID) {
		return []string{"medium", "high", "xhigh"}, true
	}
	version := openCodeGPT5Version(apiID)
	if version == 0 {
		return nil, false
	}
	if version == 1 {
		return []string{"none", "low", "medium", "high"}, true
	}
	return []string{"none", "low", "medium", "high", "xhigh"}, true
}

func openCodeGPT5CodexReasoningEfforts(apiID string) ([]string, bool) {
	if !openCodeGPT5FamilyRE.MatchString(apiID) || !strings.Contains(apiID, "codex") {
		return nil, false
	}
	version := openCodeGPT5Version(apiID)
	if version >= 3 {
		return []string{"none", "low", "medium", "high", "xhigh"}, true
	}
	if strings.Contains(apiID, "codex-max") || version >= 2 {
		return []string{"low", "medium", "high", "xhigh"}, true
	}
	return openCodeWidelySupportedEfforts(), true
}

func openCodeGPT5ChatReasoningEfforts(apiID string) ([]string, bool) {
	if !openCodeGPT5FamilyRE.MatchString(apiID) || !strings.Contains(apiID, "-chat") {
		return nil, false
	}
	if openCodeGPT5Version(apiID) == 0 {
		return nil, true
	}
	return []string{"medium"}, true
}

func openCodeAnthropicAdaptiveEfforts(apiID string) []string {
	if openCodeAnthropicOpus47OrLater(apiID) {
		return []string{"low", "medium", "high", "xhigh", "max"}
	}
	if strings.Contains(apiID, "opus-4-6") || strings.Contains(apiID, "opus-4.6") ||
		strings.Contains(apiID, "4-6-opus") || strings.Contains(apiID, "4.6-opus") ||
		strings.Contains(apiID, "sonnet-4-6") || strings.Contains(apiID, "sonnet-4.6") ||
		strings.Contains(apiID, "4-6-sonnet") || strings.Contains(apiID, "4.6-sonnet") {
		return []string{"low", "medium", "high", "max"}
	}
	return nil
}

func openCodeAnthropicOpus47OrLater(apiID string) bool {
	match := openCodeAnthropicOpusRE.FindStringSubmatch(apiID)
	if len(match) == 0 {
		return false
	}
	major, minor := 0, 0
	if match[1] != "" {
		major = openCodeVersionNumber(match[1])
		minor = openCodeVersionNumber(match[2])
	} else {
		major = openCodeVersionNumber(match[3])
		minor = openCodeVersionNumber(match[4])
	}
	return major > 4 || (major == 4 && minor >= 7)
}

func openCodeVersionNumber(value string) int {
	version, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return version
}
