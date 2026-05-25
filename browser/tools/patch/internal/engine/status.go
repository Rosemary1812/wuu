package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/git"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/patch"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/repo"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/resolve"
	"github.com/browseros-ai/BrowserOS/packages/browseros/tools/patch/internal/workspace"
)

type WorkspaceStatus struct {
	Workspace      workspace.Entry `json:"workspace"`
	RepoHead       string          `json:"repo_head"`
	BaseCommit     string          `json:"base_commit"`
	LastApplyRev   string          `json:"last_apply_rev,omitempty"`
	LastSyncRev    string          `json:"last_sync_rev,omitempty"`
	LastExtractRev string          `json:"last_extract_rev,omitempty"`
	ActiveResolve  bool            `json:"active_resolve"`
	NeedsApply     []string        `json:"needs_apply"`
	NeedsUpdate    []string        `json:"needs_update"`
	Orphaned       []string        `json:"orphaned"`
	UpToDate       []string        `json:"up_to_date"`
	SyncState      string          `json:"sync_state"`
}

type InspectWorkspaceOptions struct {
	Workspace workspace.Entry
	Repo      *repo.Info
	Progress  Progress
}

// InspectWorkspace compares a workspace against the patch repo and classifies drift.
func InspectWorkspace(ctx context.Context, opts InspectWorkspaceOptions) (*WorkspaceStatus, error) {
	reportProgress(opts.Progress, "Inspecting workspace drift")
	head, err := git.HeadRev(ctx, opts.Repo.Root)
	if err != nil {
		return nil, err
	}
	state, err := workspace.LoadState(opts.Workspace.Path)
	if err != nil {
		return nil, err
	}
	reportProgress(opts.Progress, "Loading repo patch set")
	repoSet, err := patch.LoadRepoPatchSet(opts.Repo.PatchesDir, nil)
	if err != nil {
		return nil, err
	}
	seriesSet, err := loadSeriesPatchSet(opts.Repo.Root)
	if err != nil {
		return nil, err
	}
	for rel, seriesPatch := range seriesSet {
		if _, exists := repoSet[rel]; !exists {
			repoSet[rel] = seriesPatch
		}
	}
	reportProgress(opts.Progress, "Building workspace patch set")
	localSet, err := patch.BuildWorkingTreePatchSet(ctx, opts.Workspace.Path, opts.Repo.BaseCommit, nil)
	if err != nil {
		return nil, err
	}
	status := &WorkspaceStatus{
		Workspace:      opts.Workspace,
		RepoHead:       head,
		BaseCommit:     opts.Repo.BaseCommit,
		LastApplyRev:   state.LastApplyRev,
		LastSyncRev:    state.LastSyncRev,
		LastExtractRev: state.LastExtractRev,
		ActiveResolve:  resolve.Exists(opts.Workspace.Path),
	}
	for _, delta := range patch.Compare(repoSet, localSet) {
		switch delta.Kind {
		case patch.NeedsApply:
			status.NeedsApply = append(status.NeedsApply, delta.Path)
		case patch.NeedsUpdate:
			status.NeedsUpdate = append(status.NeedsUpdate, delta.Path)
		case patch.Orphaned:
			if delta.Local != nil && patch.IsGitlinkPatch(*delta.Local) {
				continue
			}
			status.Orphaned = append(status.Orphaned, delta.Path)
		case patch.UpToDate:
			status.UpToDate = append(status.UpToDate, delta.Path)
		}
	}
	status.SyncState = inferSyncState(status)
	return status, nil
}

func inferSyncState(status *WorkspaceStatus) string {
	switch {
	case status.ActiveResolve:
		return "conflicted"
	case len(status.NeedsApply) > 0:
		if status.LastSyncRev == "" && status.LastApplyRev == "" && status.LastExtractRev == "" {
			return "never-synced"
		}
		return "drifted"
	case len(status.NeedsUpdate) > 0 || len(status.Orphaned) > 0:
		return "local-changes"
	case status.LastSyncRev == "" && status.LastApplyRev == "" && status.LastExtractRev == "":
		return "never-synced"
	default:
		return "synced"
	}
}

func loadSeriesPatchSet(repoRoot string) (patch.PatchSet, error) {
	set := patch.PatchSet{}
	for _, seriesFile := range seriesFiles(repoRoot) {
		entries, err := readSeriesEntries(seriesFile)
		if err != nil {
			return nil, err
		}
		for _, rel := range entries {
			body, err := os.ReadFile(filepath.Join(repoRoot, "series_patches", filepath.FromSlash(rel)))
			if err != nil {
				return nil, err
			}
			parsed, err := patch.ParseDiffOutput(string(body))
			if err != nil {
				return nil, err
			}
			for path, filePatch := range parsed {
				set[path] = filePatch
			}
		}
	}
	return set, nil
}

func seriesFiles(repoRoot string) []string {
	seriesDir := filepath.Join(repoRoot, "series_patches")
	files := []string{filepath.Join(seriesDir, "series")}
	switch runtime.GOOS {
	case "darwin":
		files = append(files, filepath.Join(seriesDir, "series.macos"))
	case "linux":
		files = append(files, filepath.Join(seriesDir, "series.linux"))
	case "windows":
		files = append(files, filepath.Join(seriesDir, "series.windows"))
	}
	existing := files[:0]
	for _, file := range files {
		if _, err := os.Stat(file); err == nil {
			existing = append(existing, file)
		}
	}
	return existing
}

func readSeriesEntries(seriesFile string) ([]string, error) {
	body, err := os.ReadFile(seriesFile)
	if err != nil {
		return nil, err
	}
	var entries []string
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, " #"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line != "" {
			entries = append(entries, line)
		}
	}
	return entries, nil
}
