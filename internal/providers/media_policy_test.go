package providers

import (
	"strings"
	"testing"
)

func testImage() InputImage {
	return InputImage{MediaType: "image/png", Data: "aW1hZ2U=", Width: 2, Height: 2}
}

func testFile() InputFile {
	return InputFile{MediaType: "application/pdf", Data: "cGRm", Filename: "spec.pdf"}
}

func TestProjectMediaForPolicyStripsMediaWithMarker(t *testing.T) {
	t.Parallel()
	msgs := []ChatMessage{
		{Role: "user", Content: "look at this", Images: []InputImage{testImage(), testImage()}, Files: []InputFile{testFile()}},
		{Role: "user", Content: "", Images: []InputImage{testImage()}},
		{Role: "assistant", Content: "no media here"},
	}
	out := ProjectMediaForPolicy(msgs, MediaInputPolicy{ImageKnown: true, FileKnown: true})

	if len(out[0].Images) != 0 || len(out[0].Files) != 0 {
		t.Fatalf("media not stripped: %+v", out[0])
	}
	if !strings.Contains(out[0].Content, "look at this") {
		t.Fatalf("original text lost: %q", out[0].Content)
	}
	if !strings.Contains(out[0].Content, "[2 images omitted: unsupported]") {
		t.Fatalf("missing plural image marker: %q", out[0].Content)
	}
	if !strings.Contains(out[0].Content, "[1 file omitted: unsupported]") {
		t.Fatalf("missing singular file marker: %q", out[0].Content)
	}
	if out[1].Content != "[1 image omitted: unsupported]" {
		t.Fatalf("empty content should become marker only, got %q", out[1].Content)
	}
	if out[2].Content != "no media here" {
		t.Fatalf("media-free message changed: %q", out[2].Content)
	}
	// Input must stay untouched: stored history keeps media for other readers.
	if len(msgs[0].Images) != 2 || len(msgs[0].Files) != 1 {
		t.Fatal("input messages mutated")
	}
}

func TestProjectMediaForPolicyAdmittedKindsPassThrough(t *testing.T) {
	t.Parallel()
	msgs := []ChatMessage{
		{Role: "user", Content: "hi", Images: []InputImage{testImage()}, Files: []InputFile{testFile()}},
	}
	for name, policy := range map[string]MediaInputPolicy{
		"both admitted": {Image: true, File: true},
		"image only":    {Image: true, FileKnown: true},
		"file only":     {File: true, ImageKnown: true},
	} {
		out := ProjectMediaForPolicy(msgs, policy)
		wantImages, wantFiles := 0, 0
		if policy.Image {
			wantImages = 1
		}
		if policy.File {
			wantFiles = 1
		}
		if len(out[0].Images) != wantImages || len(out[0].Files) != wantFiles {
			t.Fatalf("%s: got %+v, want %d images %d files", name, out[0], wantImages, wantFiles)
		}
	}
}

func TestProjectMediaForPolicyUnknownKindsPassThrough(t *testing.T) {
	t.Parallel()
	msgs := []ChatMessage{{
		Role: "user", Content: "inspect this",
		Images: []InputImage{testImage()}, Files: []InputFile{testFile()},
	}}

	out := ProjectMediaForPolicy(msgs, MediaInputPolicy{})
	if len(out[0].Images) != 1 || len(out[0].Files) != 1 {
		t.Fatalf("unknown media capability must not discard explicit input: %+v", out[0])
	}
	if out[0].Content != "inspect this" {
		t.Fatalf("unknown media capability added an omission marker: %q", out[0].Content)
	}
}

func TestPrepareMessagesForProviderRequestWithPolicyStripsBeforeValidation(t *testing.T) {
	t.Parallel()
	msgs := []ChatMessage{
		{Role: "user", Content: "see attached", Images: []InputImage{testImage()}},
		{Role: "assistant", Content: "ok"},
	}
	prepared, err := PrepareMessagesForProviderRequestWithPolicy("p", "m", msgs, MediaInputPolicy{ImageKnown: true})
	if err != nil {
		t.Fatalf("prepare failed: %v", err)
	}
	if len(prepared[0].Images) != 0 {
		t.Fatal("images survived the request boundary")
	}
	if !strings.Contains(prepared[0].Content, "[1 image omitted: unsupported]") {
		t.Fatalf("marker missing: %q", prepared[0].Content)
	}
}
