package appserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/session"
)

const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNgAAIAAAUAAen63NgAAAAASUVORK5CYII="

func TestParticipantAvatarImageUpload(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	out := &lockedBuffer{}
	srv := New(rt, out)

	dataURL := "data:image/png;base64," + tinyPNGBase64
	raw, _ := base64.StdEncoding.DecodeString(tinyPNGBase64)
	savePayload := map[string]any{
		"id":     "save-with-image",
		"method": MethodParticipantSave,
		"params": ParticipantSaveParams{
			Name:        "Avery",
			Role:        "reviewer",
			AvatarImage: dataURL,
			Tagline:     "Visual reviewer",
		},
	}
	saveRaw, err := json.Marshal(savePayload)
	if err != nil {
		t.Fatalf("marshal save: %v", err)
	}
	if err := srv.handleLine(context.Background(), saveRaw); err != nil {
		t.Fatalf("participant/save: %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "save-with-image")
	if resp["error"] != nil {
		t.Fatalf("expected success, got error: %v", resp["error"])
	}
	save := remarshal[ParticipantSaveResult](t, resp["result"])
	if save.Participant.ID == "" {
		t.Fatalf("expected saved participant id, got %+v", save.Participant)
	}
	if save.Participant.AvatarImage != dataURL {
		t.Fatalf("profile should echo avatar image data URL, got %q", save.Participant.AvatarImage)
	}

	avatarPath := participantAvatarImagePath(save.Participant.Workspace)
	written, err := os.ReadFile(avatarPath)
	if err != nil {
		t.Fatalf("avatar file should exist at %s: %v", avatarPath, err)
	}
	if string(written) != string(raw) {
		t.Fatalf("avatar bytes mismatch: want %q, got %q", raw, written)
	}

	mimePath := participantAvatarImageMimePath(save.Participant.Workspace)
	mime, err := os.ReadFile(mimePath)
	if err != nil {
		t.Fatalf("avatar mime sidecar should exist: %v", err)
	}
	if strings.TrimSpace(string(mime)) != "image/png" {
		t.Fatalf("avatar mime sidecar = %q, want image/png", mime)
	}

	listRaw := []byte(fmt.Sprintf(`{"id":"list-after-upload","method":%q}`, MethodParticipantList))
	if err := srv.handleLine(context.Background(), listRaw); err != nil {
		t.Fatalf("participant/list: %v", err)
	}
	list := remarshal[ParticipantListResult](t, responseByID(t, parseOutput(t, out.String()), "list-after-upload")["result"])
	var found *ParticipantProfile
	for i := range list.Participants {
		if list.Participants[i].ID == save.Participant.ID {
			found = &list.Participants[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("saved participant not in list")
	}
	if found.AvatarImage != dataURL {
		t.Fatalf("list profile should include avatar image data URL, got %q", found.AvatarImage)
	}
}

func TestParticipantAvatarImageClear(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	out := &lockedBuffer{}
	srv := New(rt, out)

	dataURL := "data:image/jpeg;base64," + tinyPNGBase64
	// initial save with image
	save1Raw, _ := json.Marshal(map[string]any{
		"id":     "save-1",
		"method": MethodParticipantSave,
		"params": ParticipantSaveParams{
			Name:        "Bea",
			Role:        "qa",
			AvatarImage: dataURL,
		},
	})
	if err := srv.handleLine(context.Background(), save1Raw); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	save1 := remarshal[ParticipantSaveResult](t, responseByID(t, parseOutput(t, out.String()), "save-1")["result"])
	if save1.Participant.AvatarImage == "" {
		t.Fatalf("expected initial avatar image")
	}

	// clear by saving again with clear flag
	save2Raw, _ := json.Marshal(map[string]any{
		"id":     "save-2",
		"method": MethodParticipantSave,
		"params": ParticipantSaveParams{
			ID:               save1.Participant.ID,
			Name:             "Bea",
			Role:             "qa",
			ClearAvatarImage: true,
		},
	})
	if err := srv.handleLine(context.Background(), save2Raw); err != nil {
		t.Fatalf("save 2 (clear): %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "save-2")
	if resp["error"] != nil {
		t.Fatalf("expected clear success, got error: %v", resp["error"])
	}
	save2 := remarshal[ParticipantSaveResult](t, resp["result"])
	if save2.Participant.AvatarImage != "" {
		t.Fatalf("avatar image should be cleared, got %q", save2.Participant.AvatarImage)
	}

	avatarPath := participantAvatarImagePath(save1.Participant.Workspace)
	if _, err := os.Stat(avatarPath); !os.IsNotExist(err) {
		t.Fatalf("avatar file should be removed, stat err = %v", err)
	}
	mimePath := participantAvatarImageMimePath(save1.Participant.Workspace)
	if _, err := os.Stat(mimePath); !os.IsNotExist(err) {
		t.Fatalf("avatar mime sidecar should be removed, stat err = %v", err)
	}
}

func TestParticipantAvatarImageRejectsOversized(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	out := &lockedBuffer{}
	srv := New(rt, out)

	// 600KB of zeros (> 512KB cap)
	huge := make([]byte, 600*1024)
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(huge)

	raw, _ := json.Marshal(map[string]any{
		"id":     "save-big",
		"method": MethodParticipantSave,
		"params": ParticipantSaveParams{
			Name:        "Cai",
			Role:        "worker",
			AvatarImage: dataURL,
		},
	})
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("participant/save: %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "save-big")
	if resp["error"] == nil {
		t.Fatalf("expected error for oversized avatar, got success: %+v", resp["result"])
	}
	msg, _ := resp["error"].(map[string]any)["message"].(string)
	if !strings.Contains(strings.ToLower(msg), "too large") && !strings.Contains(strings.ToLower(msg), "exceed") {
		t.Fatalf("error message should mention size, got %q", msg)
	}
}

func TestParticipantAvatarImageRejectsInvalidDataURL(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	out := &lockedBuffer{}
	srv := New(rt, out)

	// missing comma after header
	raw, _ := json.Marshal(map[string]any{
		"id":     "save-bad",
		"method": MethodParticipantSave,
		"params": ParticipantSaveParams{
			Name:        "Dan",
			Role:        "worker",
			AvatarImage: "data:image/png;base64" + tinyPNGBase64,
		},
	})
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("participant/save: %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "save-bad")
	if resp["error"] == nil {
		t.Fatalf("expected error for invalid data URL, got success: %+v", resp["result"])
	}
}

func TestParticipantAvatarImageRejectsUnsupportedMime(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	out := &lockedBuffer{}
	srv := New(rt, out)

	dataURL := "data:image/gif;base64," + tinyPNGBase64
	raw, _ := json.Marshal(map[string]any{
		"id":     "save-gif",
		"method": MethodParticipantSave,
		"params": ParticipantSaveParams{
			Name:        "Eli",
			Role:        "worker",
			AvatarImage: dataURL,
		},
	})
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("participant/save: %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "save-gif")
	if resp["error"] == nil {
		t.Fatalf("expected error for unsupported mime, got success: %+v", resp["result"])
	}
}

func TestParticipantRetireNamed(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	out := &lockedBuffer{}
	srv := New(rt, out)

	// create named
	saveRaw, _ := json.Marshal(map[string]any{
		"id":     "save",
		"method": MethodParticipantSave,
		"params": ParticipantSaveParams{
			Name: "Faye",
			Role: "reviewer",
		},
	})
	if err := srv.handleLine(context.Background(), saveRaw); err != nil {
		t.Fatalf("save: %v", err)
	}
	save := remarshal[ParticipantSaveResult](t, responseByID(t, parseOutput(t, out.String()), "save")["result"])

	// retire
	retireRaw, _ := json.Marshal(map[string]any{
		"id":     "retire",
		"method": MethodParticipantRetire,
		"params": ParticipantRetireParams{ParticipantID: save.Participant.ID},
	})
	if err := srv.handleLine(context.Background(), retireRaw); err != nil {
		t.Fatalf("retire: %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "retire")
	if resp["error"] != nil {
		t.Fatalf("expected retire success, got error: %v", resp["error"])
	}
	retired := remarshal[ParticipantRetireResult](t, resp["result"])
	if retired.Participant.ID != save.Participant.ID {
		t.Fatalf("retired participant mismatch: %+v", retired)
	}
	if retired.Participant.AvatarImage != "" {
		t.Fatalf("retire response should not include avatar image bytes, got %q", retired.Participant.AvatarImage)
	}

	// list should not include retired
	listRaw := []byte(fmt.Sprintf(`{"id":"list-after-retire","method":%q}`, MethodParticipantList))
	if err := srv.handleLine(context.Background(), listRaw); err != nil {
		t.Fatalf("list: %v", err)
	}
	list := remarshal[ParticipantListResult](t, responseByID(t, parseOutput(t, out.String()), "list-after-retire")["result"])
	for _, p := range list.Participants {
		if p.ID == save.Participant.ID {
			t.Fatalf("retired participant should not appear in list: %+v", p)
		}
	}
}

func TestParticipantRetireNotFound(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	out := &lockedBuffer{}
	srv := New(rt, out)

	raw, _ := json.Marshal(map[string]any{
		"id":     "retire-missing",
		"method": MethodParticipantRetire,
		"params": ParticipantRetireParams{ParticipantID: "prt-doesnotexist0000"},
	})
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("retire: %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "retire-missing")
	if resp["error"] == nil {
		t.Fatalf("expected not-found error, got success: %+v", resp["result"])
	}
}

func TestParticipantRetireRejectsNonNamed(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	out := &lockedBuffer{}
	srv := New(rt, out)

	// Insert an ephemeral participant directly into the store.
	eph := participant.Participant{
		ID:   participant.NewID(),
		Kind: participant.KindEphemeral,
		Name: "Worker·task",
		Role: "reviewer",
	}
	if err := session.UpsertParticipant(rt.SessionDir, eph); err != nil {
		t.Fatalf("upsert ephemeral: %v", err)
	}

	raw, _ := json.Marshal(map[string]any{
		"id":     "retire-eph",
		"method": MethodParticipantRetire,
		"params": ParticipantRetireParams{ParticipantID: eph.ID},
	})
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("retire: %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "retire-eph")
	if resp["error"] == nil {
		t.Fatalf("expected error for non-named, got success: %+v", resp["result"])
	}
	msg, _ := resp["error"].(map[string]any)["message"].(string)
	if !strings.Contains(strings.ToLower(msg), "named") {
		t.Fatalf("error message should mention named kind, got %q", msg)
	}
}

// TestParticipantAvatarImageRejectsOversizedPreDecode asserts that the
// oversized-avatar path is rejected with the same "too large" error message
// regardless of whether the payload is over the limit by a small margin
// (post-decode check) or by a large margin (pre-decode short-circuit). The
// pre-decode short-circuit avoids a multi-MB base64 allocation, so both
// shapes must funnel through the same user-visible error.
func TestParticipantAvatarImageRejectsOversizedPreDecode(t *testing.T) {
	rt := newTestRuntime(t, &fakeClient{})
	rt.WuuHome = filepath.Join(rt.RootDir, ".wuu")
	out := &lockedBuffer{}
	srv := New(rt, out)

	// ~2MB of zeros; DecodedLen far exceeds the 512KB cap so the pre-decode
	// short-circuit must fire.
	huge := make([]byte, 2*1024*1024)
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(huge)

	raw, _ := json.Marshal(map[string]any{
		"id":     "save-pre",
		"method": MethodParticipantSave,
		"params": ParticipantSaveParams{
			Name:        "Pre",
			Role:        "worker",
			AvatarImage: dataURL,
		},
	})
	if err := srv.handleLine(context.Background(), raw); err != nil {
		t.Fatalf("participant/save: %v", err)
	}
	resp := responseByID(t, parseOutput(t, out.String()), "save-pre")
	if resp["error"] == nil {
		t.Fatalf("expected pre-decode cap error, got success: %+v", resp["result"])
	}
	msg, _ := resp["error"].(map[string]any)["message"].(string)
	if !strings.Contains(strings.ToLower(msg), "too large") {
		t.Fatalf("pre-decode cap error message should mention size, got %q", msg)
	}
}
