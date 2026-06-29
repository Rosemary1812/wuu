package contextbudget

import (
	"encoding/base64"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"unicode/utf8"

	"github.com/blueberrycongee/wuu/internal/imageproc"
	"github.com/blueberrycongee/wuu/internal/providers"
	_ "golang.org/x/image/webp"
)

const (
	attachmentFallbackTokenEstimate = 2000
	toolDefinitionOverhead          = 500
)

// EstimateTokens provides a rough token count estimate.
// English: ~4 chars per token. CJK: ~2 chars per token.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}

	var cjkCount, totalChars int
	for _, r := range text {
		totalChars++
		if isCJK(r) {
			cjkCount++
		}
	}

	nonCJK := totalChars - cjkCount
	return (nonCJK / 4) + (cjkCount / 2) + 1
}

// EstimateJSONTokens estimates tokens for JSON content. JSON is denser than
// prose because single-character structural tokens ({, }, :, ,, ") each
// consume one token.
func EstimateJSONTokens(text string) int {
	if text == "" {
		return 0
	}
	return utf8.RuneCountInString(text)/2 + 1
}

// EstimateMessagesTokens estimates total tokens for a message list.
func EstimateMessagesTokens(messages []providers.ChatMessage) int {
	total := 0
	hasTools := false
	for _, msg := range messages {
		total += EstimateTokens(msg.Content)
		total += EstimateTokens(msg.ReasoningContent)
		total += 4
		for _, tc := range msg.ToolCalls {
			hasTools = true
			total += EstimateTokens(tc.Name)
			total += EstimateJSONTokens(tc.Arguments)
			total += 8
		}
		for _, image := range msg.Images {
			total += EstimateImageTokens(image)
		}
		for _, file := range msg.Files {
			total += EstimateFileTokens(file)
		}
	}
	if hasTools {
		total += toolDefinitionOverhead
	}
	return total
}

// EstimateImageTokens estimates the model-visible budget for an image. It is
// deliberately based on visual patches, not on base64 transport bytes.
func EstimateImageTokens(image providers.InputImage) int {
	if strings.TrimSpace(image.Data) == "" {
		return 0
	}
	width, height := image.Width, image.Height
	if width == 0 || height == 0 {
		width, height = imageDimensionsFromBase64(image.Data)
	}
	if width == 0 || height == 0 {
		return attachmentFallbackTokenEstimate
	}
	patches := ceilDivUint32(width, imageproc.PatchSize) * ceilDivUint32(height, imageproc.PatchSize)
	if patches == 0 {
		return attachmentFallbackTokenEstimate
	}
	if patches > imageproc.DefaultMaxPatches {
		return imageproc.DefaultMaxPatches
	}
	return patches
}

// EstimateFileTokens estimates non-image attachments from their encoded size.
func EstimateFileTokens(file providers.InputFile) int {
	dataLen := len(strings.TrimSpace(file.Data))
	if dataLen == 0 {
		return 0
	}
	payloadEstimate := dataLen / 4
	if payloadEstimate > attachmentFallbackTokenEstimate {
		return payloadEstimate
	}
	return attachmentFallbackTokenEstimate
}

// ShouldCompact returns true if messages exceed the threshold.
func ShouldCompact(messages []providers.ChatMessage, maxContextTokens int) bool {
	if maxContextTokens <= 0 {
		return false
	}
	estimated := EstimateMessagesTokens(messages)
	threshold := int(float64(maxContextTokens) * 0.8)
	return estimated > threshold
}

func imageDimensionsFromBase64(data string) (uint32, uint32) {
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(strings.TrimSpace(data)))
	cfg, _, err := image.DecodeConfig(decoder)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0
	}
	return uint32(cfg.Width), uint32(cfg.Height)
}

func ceilDivUint32(n, d uint32) int {
	if d == 0 {
		return 0
	}
	return int((uint64(n) + uint64(d) - 1) / uint64(d))
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x20000 && r <= 0x2A6DF) ||
		(r >= 0x2A700 && r <= 0x2B73F) ||
		(r >= 0x2B740 && r <= 0x2B81F) ||
		(r >= 0x2B820 && r <= 0x2CEAF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0x2F800 && r <= 0x2FA1F) ||
		(r >= 0x3040 && r <= 0x309F) ||
		(r >= 0x30A0 && r <= 0x30FF) ||
		(r >= 0xAC00 && r <= 0xD7AF)
}
