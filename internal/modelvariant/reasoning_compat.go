package modelvariant

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	compatGPT5FamilyRE       = regexp.MustCompile(`(?:^|/)gpt-5(?:[.-]|$)`)
	compatGPT5VersionRE      = regexp.MustCompile(`(?:^|/)gpt-5[.-](\d+)(?:[.-]|$)`)
	compatGPT5ProRE          = regexp.MustCompile(`(?:^|/)gpt-5[.-]?pro(?:[.-]|$)`)
	compatGPT5VersionedProRE = regexp.MustCompile(`(?:^|/)gpt-5[.-]\d+[.-]pro(?:[.-]|$)`)
	compatAnthropicOpusRE    = regexp.MustCompile(`(?i)opus-(\d+)[.-](\d+)(?:[.@-]|$)|claude-(\d+)[.-](\d+)-opus(?:[.@-]|$)`)
	compatSAPReasoningRE     = regexp.MustCompile(`\bo[1-9]`)
)

func compatExcludedReasoningModel(id string) bool {
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

func compatWidelySupportedEfforts() []string {
	return []string{"low", "medium", "high"}
}

func compatEfforts() []string {
	return []string{"none", "minimal", "low", "medium", "high", "xhigh"}
}

func compatReasoningEfforts(apiID, releaseDate string) []string {
	id := strings.ToLower(apiID)
	if strings.Contains(id, "deep-research") {
		return []string{"medium"}
	}
	if efforts, ok := compatGPT5ChatReasoningEfforts(id); ok {
		return efforts
	}
	if compatGPT5ProRE.MatchString(id) {
		return []string{"high"}
	}
	if efforts, ok := compatGPT5CodexReasoningEfforts(id); ok {
		return efforts
	}
	if efforts, ok := compatVersionedGPT5ReasoningEfforts(id); ok {
		return efforts
	}
	efforts := append([]string{}, compatWidelySupportedEfforts()...)
	if compatGPT5FamilyRE.MatchString(id) {
		efforts = append([]string{"minimal"}, efforts...)
	}
	if releaseDate >= compatNoneEffortRelease {
		efforts = append([]string{"none"}, efforts...)
	}
	if releaseDate >= compatXHighEffortRelease {
		efforts = append(efforts, "xhigh")
	}
	return efforts
}

func compatCompatibleReasoningEfforts(id string) []string {
	apiID := strings.ToLower(id)
	if efforts, ok := compatGPT5ChatReasoningEfforts(apiID); ok {
		return efforts
	}
	if compatGPT5ProRE.MatchString(apiID) {
		return []string{"high"}
	}
	if efforts, ok := compatGPT5CodexReasoningEfforts(apiID); ok {
		return efforts
	}
	if efforts, ok := compatVersionedGPT5ReasoningEfforts(apiID); ok {
		return efforts
	}
	return compatEfforts()
}

func compatGPT5Version(apiID string) int {
	match := compatGPT5VersionRE.FindStringSubmatch(apiID)
	if len(match) != 2 {
		return 0
	}
	version, err := strconv.Atoi(match[1])
	if err != nil {
		return 99
	}
	return version
}

func compatVersionedGPT5ReasoningEfforts(apiID string) ([]string, bool) {
	if compatGPT5VersionedProRE.MatchString(apiID) {
		return []string{"medium", "high", "xhigh"}, true
	}
	version := compatGPT5Version(apiID)
	if version == 0 {
		return nil, false
	}
	if version == 1 {
		return []string{"none", "low", "medium", "high"}, true
	}
	return []string{"none", "low", "medium", "high", "xhigh"}, true
}

func compatGPT5CodexReasoningEfforts(apiID string) ([]string, bool) {
	if !compatGPT5FamilyRE.MatchString(apiID) || !strings.Contains(apiID, "codex") {
		return nil, false
	}
	version := compatGPT5Version(apiID)
	if version >= 3 {
		return []string{"none", "low", "medium", "high", "xhigh"}, true
	}
	if strings.Contains(apiID, "codex-max") || version >= 2 {
		return []string{"low", "medium", "high", "xhigh"}, true
	}
	return compatWidelySupportedEfforts(), true
}

func compatGPT5ChatReasoningEfforts(apiID string) ([]string, bool) {
	if !compatGPT5FamilyRE.MatchString(apiID) || !strings.Contains(apiID, "-chat") {
		return nil, false
	}
	if compatGPT5Version(apiID) == 0 {
		return nil, true
	}
	return []string{"medium"}, true
}

func compatAnthropicAdaptiveEfforts(apiID string) []string {
	if compatAnthropicOpus47OrLater(apiID) {
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

func compatAnthropicOpus47OrLater(apiID string) bool {
	match := compatAnthropicOpusRE.FindStringSubmatch(apiID)
	if len(match) == 0 {
		return false
	}
	major, minor := 0, 0
	if match[1] != "" {
		major = compatVersionNumber(match[1])
		minor = compatVersionNumber(match[2])
	} else {
		major = compatVersionNumber(match[3])
		minor = compatVersionNumber(match[4])
	}
	return major > 4 || (major == 4 && minor >= 7)
}

func compatVersionNumber(value string) int {
	version, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return version
}
