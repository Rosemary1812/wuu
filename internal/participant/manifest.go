package participant

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Named-agent manifest: the declarative overlay that turns the generic base
// agent into a specific named agent. It lives in the participant's home
// directory so identity, memory, and persona config are co-located and
// human-inspectable:
//
//	<workspace>/agent.md       prompt overlay appended to the persona prompt
//	<workspace>/manifest.json  {"skills": [...], "permission_tier": "..."}
//	<workspace>/memory/        identity notebook (already existing)
//
// The runtime resolves the manifest at run start; the base agent itself
// carries no per-agent special cases.

const (
	// PermissionTierWorkspace is the default: the run works inside the
	// inherited workspace file scope.
	PermissionTierWorkspace = "workspace"
	// PermissionTierUnrestricted clears the file-scope whitelist for the run.
	PermissionTierUnrestricted = "unrestricted"
)

const (
	ManifestFileName  = "manifest.json"
	PromptOverlayName = "agent.md"
)

// Manifest is the on-disk named-agent configuration. Empty fields mean
// "inherit the base defaults" everywhere.
type Manifest struct {
	// Skills names the agent's designated skills (guidance for load_skill;
	// the full standard tool surface stays mounted regardless).
	Skills []string `json:"skills,omitempty"`
	// PermissionTier is PermissionTierWorkspace (default/empty) or
	// PermissionTierUnrestricted.
	PermissionTier string `json:"permission_tier,omitempty"`
}

// NormalizedPermissionTier returns the effective tier, defaulting to
// PermissionTierWorkspace for empty or unknown values.
func (m Manifest) NormalizedPermissionTier() string {
	if strings.EqualFold(strings.TrimSpace(m.PermissionTier), PermissionTierUnrestricted) {
		return PermissionTierUnrestricted
	}
	return PermissionTierWorkspace
}

// ManifestPaths locates one participant's manifest files under its home dir.
func ManifestPaths(workspace string) (manifestFile, overlayFile string) {
	workspace = strings.TrimSpace(workspace)
	return filepath.Join(workspace, ManifestFileName), filepath.Join(workspace, PromptOverlayName)
}

// LoadManifest reads manifest.json from the participant home. A missing file
// is not an error: the zero Manifest (all defaults) is returned.
func LoadManifest(workspace string) (Manifest, error) {
	manifestFile, _ := ManifestPaths(workspace)
	raw, err := os.ReadFile(manifestFile)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, nil
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("read participant manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse participant manifest: %w", err)
	}
	return m, nil
}

// SaveManifest writes manifest.json into the participant home, creating the
// directory if needed.
func SaveManifest(workspace string, m Manifest) error {
	if strings.TrimSpace(workspace) == "" {
		return errors.New("workspace is required")
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return fmt.Errorf("create participant home: %w", err)
	}
	m.PermissionTier = m.NormalizedPermissionTier()
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode participant manifest: %w", err)
	}
	manifestFile, _ := ManifestPaths(workspace)
	if err := os.WriteFile(manifestFile, append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("write participant manifest: %w", err)
	}
	return nil
}

// LoadPromptOverlay reads the agent.md prompt overlay. Missing file means an
// empty overlay.
func LoadPromptOverlay(workspace string) (string, error) {
	_, overlayFile := ManifestPaths(workspace)
	raw, err := os.ReadFile(overlayFile)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read participant prompt overlay: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// SavePromptOverlay writes agent.md, creating the participant home if needed.
func SavePromptOverlay(workspace, overlay string) error {
	if strings.TrimSpace(workspace) == "" {
		return errors.New("workspace is required")
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return fmt.Errorf("create participant home: %w", err)
	}
	_, overlayFile := ManifestPaths(workspace)
	if err := os.WriteFile(overlayFile, []byte(strings.TrimSpace(overlay)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write participant prompt overlay: %w", err)
	}
	return nil
}
