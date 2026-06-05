package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
			resolution.Exists = true
			resolution.Action = "use_existing"
			out = append(out, resolution)
			continue
		}
		if ref.Required && opts.CreateMissing {
			if err := createProfileMetadata(dir, name, opts.Definition.Name); err != nil {
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
			resolution.Reason = "optional Agent Profile is missing; use a memoryless worker unless the user approves a durable profile"
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

func createProfileMetadata(dir, name, workflowName string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	now := time.Now().UTC()
	metadata := profileMetadata{
		Name:           strings.TrimSpace(name),
		Source:         "workflow",
		WorkflowName:   strings.TrimSpace(workflowName),
		CreatedAt:      now,
		LastResolvedAt: now,
	}
	path := filepath.Join(dir, profileMetadataFile)
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
