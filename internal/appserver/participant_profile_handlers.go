package appserver

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/session"
)

const (
	participantMemoryFileName = "MEMORY.md"
	participantAvatarFileName = "avatar"
	participantAvatarMimeName = "avatar.mime"
	// participantAvatarMaxBytes caps the decoded avatar image payload.
	// 512KB is generous for a square avatar while keeping the
	// workspace from becoming a vector for oversized uploads.
	participantAvatarMaxBytes = 512 * 1024
	// participantAvatarReadMaxBytes is a defensive cap used when
	// re-reading the avatar during profile serialization. It is set
	// above the upload cap so a file that somehow grew past the limit
	// is skipped silently rather than blocking the profile read.
	participantAvatarReadMaxBytes = 1024 * 1024
	// participantSummaryAvatarMaxBytes caps the avatar bytes embedded
	// into wire participant summaries (chat bubbles and group member chips
	// — resolveParticipantSummary). Unlike the profile read,
	// a summary is duplicated into every thread item it attributes, so a
	// full-size upload (512KB → ~683KB as base64) would multiply into
	// hundreds of MB on a long group history resume. 64KB raw (~85KB as
	// base64) comfortably covers a properly sized square avatar (a 256px
	// png/webp is typically 10-50KB) while keeping even a pathological
	// resume payload bounded. Larger uploads still render wherever the
	// full profile is loaded (profile panel and participant/list roster)
	// — only
	// the inline summaries fall back to the initial-letter avatar.
	participantSummaryAvatarMaxBytes = 64 * 1024
)

// supportedAvatarMimes lists the image mime types the participant
// profile avatar upload accepts. webp is intentionally included for
// modern UIs; gif and svg are rejected because gif doesn't compress
// well for avatars and svg can carry script payloads.
var supportedAvatarMimes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
}

func (s *Server) handleParticipantList(req Request) error {
	participants, err := session.ListParticipants(s.rt.SessionDir, participant.KindNamed)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	profiles := make([]ParticipantProfile, 0, len(participants))
	for _, p := range participants {
		profile, err := s.participantProfile(p)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		profiles = append(profiles, profile)
	}
	return s.writeResponse(req.ID, ParticipantListResult{Participants: profiles}, nil)
}

func (s *Server) notifyParticipantUpdated(profile ParticipantProfile) error {
	return s.writeNotification(NotificationParticipantUpdated, ParticipantUpdatedNotification{
		ParticipantID: profile.ID,
		Participant:   &profile,
	})
}

func (s *Server) handleParticipantSave(req Request) error {
	var params ParticipantSaveParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return s.writeResponse(req.ID, nil, errors.New("participant name is required"))
	}
	now := time.Now().UTC()
	p := participant.Participant{
		ID:        strings.TrimSpace(params.ID),
		Kind:      participant.KindNamed,
		Name:      name,
		Role:      strings.TrimSpace(params.Role),
		Tagline:   strings.TrimSpace(params.Tagline),
		Model:     strings.TrimSpace(params.Model),
		UpdatedAt: now,
	}
	isNew := p.ID == ""
	if isNew {
		p.ID = participant.NewID()
		p.CreatedAt = now
	} else if existing, err := session.GetParticipant(s.rt.SessionDir, p.ID); err == nil {
		p.CreatedAt = existing.CreatedAt
		p.Workspace = existing.Workspace
		// Legacy emoji avatars stay in the store untouched; the save
		// path no longer accepts or rewrites them.
		p.Avatar = existing.Avatar
	} else if errors.Is(err, session.ErrParticipantNotFound) {
		// Caller-supplied ID with no existing row: still a creation.
		isNew = true
	} else {
		return s.writeResponse(req.ID, nil, err)
	}
	if isNew {
		if err := s.ensureNamedParticipantCapacity(); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	if p.Workspace == "" {
		workspace, err := s.participantWorkspace(p.ID)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		p.Workspace = workspace
	}
	if err := os.MkdirAll(p.Workspace, 0o755); err != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("create participant workspace: %w", err))
	}
	if params.ClearAvatarImage {
		if err := removeParticipantAvatarImage(p.Workspace); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	} else if raw := strings.TrimSpace(params.AvatarImage); raw != "" {
		mime, decoded, err := decodeAvatarDataURL(raw)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		if err := writeParticipantAvatarImage(p.Workspace, mime, decoded); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}
	// The profile panel no longer edits memory (it moved to the memdir
	// notebook + memory panel); only legacy callers such as team template
	// import still send the field. An absent/empty field must not clobber
	// the legacy MEMORY.md with an empty file.
	if strings.TrimSpace(params.Memory) != "" {
		if err := os.WriteFile(participantMemoryPath(p.Workspace), []byte(params.Memory), 0o644); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("write participant memory: %w", err))
		}
	}
	if err := session.UpsertParticipant(s.rt.SessionDir, p); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	s.invalidateParticipantSummary(p.ID)
	profile, err := s.participantProfile(p)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	result := ParticipantSaveResult{Participant: profile}
	if isNew {
		result.ArchivedPredecessorID = s.archivedPredecessorID(p.Name, p.ID)
	}
	if err := s.writeResponse(req.ID, result, nil); err != nil {
		return err
	}
	return s.notifyParticipantUpdated(profile)
}

// archivedPredecessorID implements the same-name rebuild guard: when a NEW
// named participant takes the name of a retired one, the predecessor's ID is
// surfaced so callers know archived state exists under
// participants/.archived/<id>/. Detection is advisory — the new participant
// always starts fresh, and a lookup failure never blocks the save.
func (s *Server) archivedPredecessorID(name, newID string) string {
	predecessor, ok, err := session.FindRetiredParticipantByName(s.rt.SessionDir, participant.KindNamed, name)
	if err != nil {
		providers.DebugLogf("find retired predecessor for %q: %v", name, err)
		return ""
	}
	if !ok || predecessor.ID == strings.TrimSpace(newID) {
		return ""
	}
	return predecessor.ID
}

func (s *Server) handleParticipantFeedback(req Request) error {
	var params ParticipantFeedbackParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	id := strings.TrimSpace(params.ParticipantID)
	text := strings.TrimSpace(params.Text)
	if id == "" {
		return s.writeResponse(req.ID, nil, errors.New("participant_id is required"))
	}
	if text == "" {
		return s.writeResponse(req.ID, nil, errors.New("feedback text is required"))
	}
	p, err := session.GetParticipant(s.rt.SessionDir, id)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if err := os.MkdirAll(p.Workspace, 0o755); err != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("create participant workspace: %w", err))
	}
	var b strings.Builder
	b.WriteString("\n\n## Feedback ")
	b.WriteString(time.Now().UTC().Format("2006-01-02"))
	b.WriteString("\n")
	if taskID := strings.TrimSpace(params.TaskID); taskID != "" {
		b.WriteString("- Task: ")
		b.WriteString(taskID)
		b.WriteString("\n")
	}
	if messageID := strings.TrimSpace(params.MessageID); messageID != "" {
		b.WriteString("- Message: ")
		b.WriteString(messageID)
		b.WriteString("\n")
	}
	b.WriteString("- Note: ")
	b.WriteString(text)
	b.WriteString("\n")
	if err := appendParticipantMemory(p.Workspace, b.String()); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	p.UpdatedAt = time.Now().UTC()
	if err := session.UpsertParticipant(s.rt.SessionDir, p); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	profile, err := s.participantProfile(p)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if err := s.writeResponse(req.ID, ParticipantFeedbackResult{Participant: profile}, nil); err != nil {
		return err
	}
	return s.notifyParticipantUpdated(profile)
}

func (s *Server) handleParticipantReset(req Request) error {
	var params ParticipantResetParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	id := strings.TrimSpace(params.ParticipantID)
	if id == "" {
		return s.writeResponse(req.ID, nil, errors.New("participant_id is required"))
	}
	scope := strings.ToLower(strings.TrimSpace(params.Scope))
	// The "restart" scope was retired: it was an empty case that reported
	// success without doing anything, and a named agent is one continuous
	// brain with no per-DM runtime to restart (consistency repair plan §1
	// #6). Reject it explicitly until the frontend button is removed.
	if scope == "restart" {
		return s.writeResponse(req.ID, nil, errors.New("restart scope is no longer supported"))
	}
	p, err := session.GetParticipant(s.rt.SessionDir, id)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	switch scope {
	case "session":
		if err := appendParticipantMemory(p.Workspace, "\n\n## Session reset\n- Runtime state reset.\n"); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	case "full":
		if err := os.MkdirAll(p.Workspace, 0o755); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("create participant workspace: %w", err))
		}
		if err := os.WriteFile(participantMemoryPath(p.Workspace), nil, 0o644); err != nil {
			return s.writeResponse(req.ID, nil, fmt.Errorf("reset participant memory: %w", err))
		}
	default:
		return s.writeResponse(req.ID, nil, fmt.Errorf("unsupported reset scope %q", scope))
	}
	p.UpdatedAt = time.Now().UTC()
	if err := session.UpsertParticipant(s.rt.SessionDir, p); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	s.invalidateParticipantSummary(p.ID)
	profile, err := s.participantProfile(p)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if err := s.writeResponse(req.ID, ParticipantResetResult{Participant: profile}, nil); err != nil {
		return err
	}
	return s.notifyParticipantUpdated(profile)
}

func (s *Server) handleParticipantRetire(req Request) error {
	var params ParticipantRetireParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	id := strings.TrimSpace(params.ParticipantID)
	if id == "" {
		return s.writeResponse(req.ID, nil, errors.New("participant_id is required"))
	}
	p, err := session.GetParticipant(s.rt.SessionDir, id)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if p.Kind != participant.KindNamed {
		return s.writeResponse(req.ID, nil, fmt.Errorf("participant %q is not a named agent (kind=%s)", id, p.Kind))
	}
	// Retire the stored profile and archive its workspace without deleting
	// memory so the identity can be restored later.
	if err := s.retireNamedParticipant(p); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	// Reload so the profile reflects the new RetiredAt timestamp and the
	// archived workspace path set by the cleanup.
	updated, err := session.GetParticipant(s.rt.SessionDir, id)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	profile, err := s.participantProfile(updated)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	// Keep the retire response lightweight: avatar image bytes are not
	// useful to the caller and the participant is no longer in the
	// active roster. The DB row remains for audit/history.
	profile.AvatarImage = ""
	if err := s.writeResponse(req.ID, ParticipantRetireResult{Participant: profile}, nil); err != nil {
		return err
	}
	return s.notifyParticipantUpdated(profile)
}

func (s *Server) participantProfile(p participant.Participant) (ParticipantProfile, error) {
	memory := ""
	if path := participantMemoryPath(p.Workspace); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			memory = string(data)
		} else if !errors.Is(err, os.ErrNotExist) {
			return ParticipantProfile{}, fmt.Errorf("read participant memory: %w", err)
		}
	}
	avatarImage, err := readParticipantAvatarDataURL(p.Workspace)
	if err != nil {
		return ParticipantProfile{}, err
	}
	runs, err := session.ListParticipantRuns(s.rt.SessionDir, p.ID, 10)
	if err != nil {
		return ParticipantProfile{}, err
	}
	trackRecord := make([]ParticipantRunEntry, 0, len(runs))
	for _, run := range runs {
		trackRecord = append(trackRecord, ParticipantRunEntry{
			TaskID:    run.TaskID,
			Summary:   run.Summary,
			Outcome:   run.Outcome,
			CreatedAt: run.CreatedAt,
		})
	}
	return ParticipantProfile{
		ID:          p.ID,
		Kind:        string(p.Kind),
		Name:        p.Name,
		Role:        p.Role,
		AvatarImage: avatarImage,
		Tagline:     p.Tagline,
		Workspace:   p.Workspace,
		Model:       p.Model,
		Memory:      memory,
		TrackRecord: trackRecord,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}, nil
}

func (s *Server) participantWorkspace(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("participant id is required")
	}
	root := strings.TrimSpace(s.rt.WuuHome)
	if root == "" {
		stateDir, err := s.workspaceStateDir()
		if err != nil {
			return "", err
		}
		root = stateDir
	}
	return filepath.Join(root, "participants", id), nil
}

func participantMemoryPath(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return ""
	}
	return filepath.Join(workspace, participantMemoryFileName)
}

func appendParticipantMemory(workspace, text string) error {
	if strings.TrimSpace(workspace) == "" {
		return errors.New("participant workspace is required")
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return fmt.Errorf("create participant workspace: %w", err)
	}
	file, err := os.OpenFile(participantMemoryPath(workspace), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("append participant memory: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		return fmt.Errorf("append participant memory: %w", err)
	}
	return nil
}

// participantAvatarImagePath returns the absolute path of the avatar image
// file inside the given participant workspace.
func participantAvatarImagePath(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return ""
	}
	return filepath.Join(workspace, participantAvatarFileName)
}

// participantAvatarImageMimePath returns the absolute path of the avatar
// mime sidecar. The mime is stored separately so the avatar file is the
// raw image bytes the browser can serve directly when extended later.
func participantAvatarImageMimePath(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return ""
	}
	return filepath.Join(workspace, participantAvatarMimeName)
}

// decodeAvatarDataURL parses an image data URL of the form
// "data:<mime>;base64,<payload>" and returns the mime and decoded bytes.
// Empty input is rejected so callers can distinguish "no upload" from
// "empty upload".
func decodeAvatarDataURL(raw string) (string, []byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, errors.New("avatar_image is empty")
	}
	if !strings.HasPrefix(strings.ToLower(raw), "data:") {
		return "", nil, errors.New("avatar_image must be a data URL (data:image/...;base64,...)")
	}
	header, payload, ok := strings.Cut(raw, ",")
	if !ok {
		return "", nil, errors.New("invalid avatar_image data URL: missing comma separator")
	}
	if !strings.Contains(strings.ToLower(header), ";base64") {
		return "", nil, errors.New("avatar_image data URL must be base64 encoded")
	}
	mime := strings.TrimSpace(strings.ToLower(strings.TrimPrefix(strings.Split(header, ";")[0], "data:")))
	if mime == "" {
		return "", nil, errors.New("avatar_image data URL is missing a mime type")
	}
	if _, ok := supportedAvatarMimes[mime]; !ok {
		return "", nil, fmt.Errorf("unsupported avatar_image mime %q (allowed: png, jpeg, webp)", mime)
	}
	trimmed := strings.TrimSpace(payload)
	// Pre-decode cap: reject payloads whose decoded size would exceed the
	// limit without paying the cost (and allocation) of a full base64
	// decode. The post-decode check below is kept as the source of truth
	// because base64 padding can shift DecodedLen by a few bytes.
	if base64.StdEncoding.DecodedLen(len(trimmed)) > participantAvatarMaxBytes {
		return "", nil, fmt.Errorf("avatar_image too large: exceeds %d bytes", participantAvatarMaxBytes)
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return "", nil, fmt.Errorf("avatar_image base64 decode: %w", err)
	}
	if len(decoded) == 0 {
		return "", nil, errors.New("avatar_image decoded payload is empty")
	}
	if len(decoded) > participantAvatarMaxBytes {
		return "", nil, fmt.Errorf("avatar_image too large: %d bytes (max %d)", len(decoded), participantAvatarMaxBytes)
	}
	return mime, decoded, nil
}

// writeParticipantAvatarImage atomically replaces the avatar image (and its
// mime sidecar) inside the workspace. Existing files are overwritten so a
// re-upload replaces the previous avatar in a single step.
func writeParticipantAvatarImage(workspace, mime string, data []byte) error {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return errors.New("participant workspace is required for avatar upload")
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return fmt.Errorf("create participant workspace: %w", err)
	}
	imgPath := participantAvatarImagePath(workspace)
	mimePath := participantAvatarImageMimePath(workspace)
	if err := os.WriteFile(imgPath, data, 0o644); err != nil {
		return fmt.Errorf("write avatar image: %w", err)
	}
	if err := os.WriteFile(mimePath, []byte(strings.TrimSpace(strings.ToLower(mime))), 0o644); err != nil {
		// Best-effort cleanup: a half-written avatar is worse than no
		// avatar, so remove the bytes we just wrote.
		_ = os.Remove(imgPath)
		return fmt.Errorf("write avatar mime: %w", err)
	}
	return nil
}

// removeParticipantAvatarImage deletes both the avatar bytes and the mime
// sidecar. Missing files are not an error so the helper is safe to call as
// part of an idempotent reset.
func removeParticipantAvatarImage(workspace string) error {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	for _, path := range []string{participantAvatarImagePath(workspace), participantAvatarImageMimePath(workspace)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove participant avatar: %w", err)
		}
	}
	return nil
}

// readParticipantAvatarDataURL returns the avatar as a base64 data URL or
// an empty string when the workspace has no avatar. Files larger than
// participantAvatarReadMaxBytes are treated as corrupt and skipped, since
// they would exceed the upload limit and inflate profile payloads.
func readParticipantAvatarDataURL(workspace string) (string, error) {
	return readParticipantAvatarDataURLCapped(workspace, participantAvatarReadMaxBytes)
}

// participantSummaryAvatarDataURL is the lenient, wire-summary variant of
// readParticipantAvatarDataURL: avatars above the summary cap degrade to
// "" (initial-letter fallback in the UI) and read errors are swallowed —
// a participant summary must never fail to resolve because its avatar
// bytes are unreadable.
func participantSummaryAvatarDataURL(workspace string) string {
	avatar, err := readParticipantAvatarDataURLCapped(workspace, participantSummaryAvatarMaxBytes)
	if err != nil {
		providers.DebugLogf("read summary avatar from %q: %v", workspace, err)
		return ""
	}
	return avatar
}

// readParticipantAvatarDataURLCapped implements the data-URL read with a
// caller-chosen size ceiling; files above it read as "no avatar".
func readParticipantAvatarDataURLCapped(workspace string, maxBytes int64) (string, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "", nil
	}
	imgPath := participantAvatarImagePath(workspace)
	info, err := os.Stat(imgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("stat avatar image: %w", err)
	}
	if info.Size() > maxBytes {
		return "", nil
	}
	mime := "image/png"
	if raw, err := os.ReadFile(participantAvatarImageMimePath(workspace)); err == nil {
		if trimmed := strings.TrimSpace(strings.ToLower(string(raw))); trimmed != "" {
			mime = trimmed
		}
	}
	data, err := os.ReadFile(imgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read avatar image: %w", err)
	}
	return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data)), nil
}

func (s *Server) invalidateParticipantSummary(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	s.participantMu.Lock()
	delete(s.participantSummaryCache, id)
	s.participantMu.Unlock()
}
