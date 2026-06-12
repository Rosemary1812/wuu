package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/statepath"
)

const profileMetadataFile = "profile.json"

type ProfileResolutionOptions struct {
	WuuHome       string
	Definition    Definition
	CreateMissing bool
}

type ProfileEnsureOptions struct {
	WuuHome      string
	Name         string
	Source       string
	WorkflowName string
	Role         string
	Description  string
}

type ProfileSummary struct {
	Name           string    `json:"name"`
	Source         string    `json:"source,omitempty"`
	WorkflowName   string    `json:"workflow_name,omitempty"`
	Role           string    `json:"role,omitempty"`
	Description    string    `json:"description,omitempty"`
	ProfileDir     string    `json:"profile_dir"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
	LastResolvedAt time.Time `json:"last_resolved_at,omitempty"`
}

type ProfileResolution struct {
	Name       string `json:"name"`
	Required   bool   `json:"required,omitempty"`
	Exists     bool   `json:"exists"`
	Created    bool   `json:"created,omitempty"`
	Action     string `json:"action"`
	Reason     string `json:"reason,omitempty"`
	ProfileDir string `json:"profile_dir,omitempty"`
}

type profileMetadata struct {
	Name           string    `json:"name"`
	Source         string    `json:"source"`
	WorkflowName   string    `json:"workflow_name,omitempty"`
	Role           string    `json:"role,omitempty"`
	Description    string    `json:"description,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	LastResolvedAt time.Time `json:"last_resolved_at"`
}

func AutoCreateProfiles(policy string) bool {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "auto", "always", "true", "yes":
		return true
	default:
		return false
	}
}

func ResolveProfiles(opts ProfileResolutionOptions) ([]ProfileResolution, error) {
	if strings.TrimSpace(opts.WuuHome) == "" {
		return nil, fmt.Errorf("wuu home is required")
	}
	out := make([]ProfileResolution, 0, len(opts.Definition.Profiles))
	for _, ref := range opts.Definition.Profiles {
		name := strings.TrimSpace(ref.Name)
		if name == "" {
			continue
		}
		dir, err := statepath.ProfileDir(opts.WuuHome, name)
		if err != nil {
			return nil, err
		}
		resolution := ProfileResolution{
			Name:       name,
			Required:   ref.Required,
			ProfileDir: dir,
		}
		exists, err := profileDirExists(dir)
		if err != nil {
			return nil, err
		}
		if exists {
			if _, _, err := EnsureProfile(ProfileEnsureOptions{
				WuuHome:      opts.WuuHome,
				Name:         name,
				Source:       "workflow",
				WorkflowName: opts.Definition.Name,
			}); err != nil {
				return nil, err
			}
			resolution.Exists = true
			resolution.Action = "use_existing"
			out = append(out, resolution)
			continue
		}
		if ref.Required && opts.CreateMissing {
			if _, _, err := EnsureProfile(ProfileEnsureOptions{
				WuuHome:      opts.WuuHome,
				Name:         name,
				Source:       "workflow",
				WorkflowName: opts.Definition.Name,
			}); err != nil {
				return nil, err
			}
			resolution.Exists = true
			resolution.Created = true
			resolution.Action = "created_profile"
			resolution.Reason = "required profile was missing and workflow policy allows automatic creation"
			out = append(out, resolution)
			continue
		}
		if ref.Required {
			resolution.Action = "pause_missing_required"
			resolution.Reason = "required Agent Profile is missing"
		} else {
			resolution.Action = "spawn_ephemeral"
			resolution.Reason = "optional Agent Profile is missing; use a temporary worker without profile memory unless the user approves a saved profile"
		}
		out = append(out, resolution)
	}
	return out, nil
}

func MissingRequiredProfiles(resolutions []ProfileResolution) []ProfileResolution {
	out := make([]ProfileResolution, 0)
	for _, resolution := range resolutions {
		if resolution.Required && !resolution.Exists {
			out = append(out, resolution)
		}
	}
	return out
}

func ListProfiles(wuuHome string) ([]ProfileSummary, error) {
	if strings.TrimSpace(wuuHome) == "" {
		return nil, fmt.Errorf("wuu home is required")
	}
	root := filepath.Join(wuuHome, "profiles")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]ProfileSummary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		meta, err := readProfileMetadata(filepath.Join(dir, profileMetadataFile))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		summary := profileSummaryFromMetadata(meta, dir)
		if strings.TrimSpace(summary.Name) == "" {
			continue
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func LoadProfile(wuuHome, name string) (ProfileSummary, bool, error) {
	wuuHome = strings.TrimSpace(wuuHome)
	if wuuHome == "" {
		return ProfileSummary{}, false, fmt.Errorf("wuu home is required")
	}
	name = strings.TrimSpace(name)
	if name == "" || strings.EqualFold(name, "default") {
		return ProfileSummary{}, false, fmt.Errorf("profile name must be a non-default named agent")
	}
	dir, err := statepath.ProfileDir(wuuHome, name)
	if err != nil {
		return ProfileSummary{}, false, err
	}
	exists, err := profileDirExists(dir)
	if err != nil || !exists {
		return ProfileSummary{}, false, err
	}
	profile, _, err := EnsureProfile(ProfileEnsureOptions{WuuHome: wuuHome, Name: name, Source: "agent"})
	if err != nil {
		return ProfileSummary{}, false, err
	}
	return profile, true, nil
}

func EnsureProfile(opts ProfileEnsureOptions) (ProfileSummary, bool, error) {
	wuuHome := strings.TrimSpace(opts.WuuHome)
	if wuuHome == "" {
		return ProfileSummary{}, false, fmt.Errorf("wuu home is required")
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" || strings.EqualFold(name, "default") {
		return ProfileSummary{}, false, fmt.Errorf("profile name must be a non-default named agent")
	}
	dir, err := statepath.ProfileDir(wuuHome, name)
	if err != nil {
		return ProfileSummary{}, false, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ProfileSummary{}, false, err
	}

	path := filepath.Join(dir, profileMetadataFile)
	now := time.Now().UTC()
	meta, err := readProfileMetadata(path)
	created := false
	if os.IsNotExist(err) {
		created = true
		meta = profileMetadata{
			Name:      name,
			Source:    defaultProfileSource(opts.Source),
			CreatedAt: now,
		}
	} else if err != nil {
		return ProfileSummary{}, false, err
	}
	if strings.TrimSpace(meta.Name) == "" {
		meta.Name = name
	}
	if strings.TrimSpace(meta.Source) == "" {
		meta.Source = defaultProfileSource(opts.Source)
	}
	if workflowName := strings.TrimSpace(opts.WorkflowName); workflowName != "" {
		meta.WorkflowName = workflowName
	}
	if role := strings.TrimSpace(opts.Role); role != "" {
		meta.Role = role
	}
	if desc := strings.TrimSpace(opts.Description); desc != "" {
		meta.Description = desc
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	meta.LastResolvedAt = now
	if err := writeProfileMetadata(path, meta); err != nil {
		return ProfileSummary{}, false, err
	}
	return profileSummaryFromMetadata(meta, dir), created, nil
}

func profileDirExists(dir string) (bool, error) {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func readProfileMetadata(path string) (profileMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return profileMetadata{}, err
	}
	var meta profileMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return profileMetadata{}, fmt.Errorf("parse profile metadata %s: %w", path, err)
	}
	return meta, nil
}

func writeProfileMetadata(path string, metadata profileMetadata) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func profileSummaryFromMetadata(meta profileMetadata, dir string) ProfileSummary {
	return ProfileSummary{
		Name:           strings.TrimSpace(meta.Name),
		Source:         strings.TrimSpace(meta.Source),
		WorkflowName:   strings.TrimSpace(meta.WorkflowName),
		Role:           strings.TrimSpace(meta.Role),
		Description:    strings.TrimSpace(meta.Description),
		ProfileDir:     dir,
		CreatedAt:      meta.CreatedAt,
		LastResolvedAt: meta.LastResolvedAt,
	}
}

func defaultProfileSource(source string) string {
	if s := strings.TrimSpace(source); s != "" {
		return s
	}
	return "agent"
}
