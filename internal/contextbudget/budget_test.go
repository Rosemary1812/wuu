package contextbudget

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/providers"
)

func TestEstimateImageTokensUsesVisualPatchBudget(t *testing.T) {
	image := providers.InputImage{
		MediaType: "image/png",
		Data:      strings.Repeat("a", 1_000_000),
		Width:     2048,
		Height:    2048,
	}

	if got, want := EstimateImageTokens(image), 4096; got != want {
		t.Fatalf("image tokens should use patch budget, got %d, want %d", got, want)
	}
}

func TestEstimateImageTokensCapsAtImageBudget(t *testing.T) {
	image := providers.InputImage{
		MediaType: "image/png",
		Data:      strings.Repeat("a", 1_000_000),
		Width:     4096,
		Height:    4096,
	}

	if got, want := EstimateImageTokens(image), 10_000; got != want {
		t.Fatalf("image tokens should cap at normalized image budget, got %d, want %d", got, want)
	}
}

func TestEstimateImageTokensDecodesLegacyDimensions(t *testing.T) {
	data := encodeBudgetTestPNG(t, 33, 65)
	image := providers.InputImage{
		MediaType: "image/png",
		Data:      base64.StdEncoding.EncodeToString(data),
	}

	if got, want := EstimateImageTokens(image), 6; got != want {
		t.Fatalf("legacy image tokens should decode dimensions, got %d, want %d", got, want)
	}
}

func TestEstimateMessagesTokensDoesNotCountImageTransportBytes(t *testing.T) {
	messages := []providers.ChatMessage{{
		Role: "user",
		Images: []providers.InputImage{{
			MediaType: "image/png",
			Data:      strings.Repeat("a", 1_000_000),
			Width:     2048,
			Height:    2048,
		}},
	}}

	if got := EstimateMessagesTokens(messages); got > 5000 {
		t.Fatalf("message estimate should ignore image transport byte size, got %d", got)
	}
}

func encodeBudgetTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x80, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}
