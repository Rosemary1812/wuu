package exec

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRunAttachmentsLoadsFilesAndImages(t *testing.T) {
	root := t.TempDir()
	imageBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	pdfBytes := []byte("%PDF-1.7\n%test\n")
	if err := os.WriteFile(filepath.Join(root, "shot.png"), imageBytes, 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "report.pdf"), pdfBytes, 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}

	attachments, err := resolveRunAttachments(Options{
		Workdir:    root,
		ImagePaths: []string{"shot.png"},
		FilePaths:  []string{"report.pdf"},
	})
	if err != nil {
		t.Fatalf("resolveRunAttachments: %v", err)
	}
	if len(attachments.Images) != 1 || attachments.Images[0].MediaType != "image/png" {
		t.Fatalf("unexpected image attachment: %+v", attachments.Images)
	}
	if got := attachments.Images[0].Data; got != base64.StdEncoding.EncodeToString(imageBytes) {
		t.Fatalf("image data = %q", got)
	}
	if len(attachments.Files) != 1 || attachments.Files[0].MediaType != "application/pdf" || attachments.Files[0].Filename != "report.pdf" {
		t.Fatalf("unexpected file attachment: %+v", attachments.Files)
	}
	if got := attachments.Files[0].Data; got != base64.StdEncoding.EncodeToString(pdfBytes) {
		t.Fatalf("file data = %q", got)
	}
}

func TestResolveRunAttachmentsRejectsNonImageForImageFlag(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write text: %v", err)
	}

	_, err := resolveRunAttachments(Options{Workdir: root, ImagePaths: []string{"notes.txt"}})
	if err == nil || !strings.Contains(err.Error(), "expected image/*") {
		t.Fatalf("expected image media type error, got %v", err)
	}
}
