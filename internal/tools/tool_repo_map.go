package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/providers"
)

const (
	defaultRepoMapToolMaxListedFiles = 120
	maxRepoMapToolMaxListedFiles     = 500
	maxRepoMapToolMaxFiles           = 10000
)

type RepoMapTool struct{ env *Env }

func NewRepoMapTool(env *Env) *RepoMapTool { return &RepoMapTool{env: env} }

func (t *RepoMapTool) Name() string            { return "repo_map" }
func (t *RepoMapTool) IsReadOnly() bool        { return true }
func (t *RepoMapTool) IsConcurrencySafe() bool { return true }

func (t *RepoMapTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "repo_map",
		Description: "Returns a compact structured map of the workspace for repository orientation.\n\n" +
			"Usage:\n" +
			"- Use repo_map at the start of broad exploration or when you need test mappings and representative files\n" +
			"- Results are candidates and summaries; use read_file, grep, ast_search, or semantic_search before editing\n" +
			"- Skips common generated/build directories and sensitive internal state paths\n" +
			"- Includes language counts, likely test files, likely source-to-test mappings, representative files, and workspace_revision",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"max_listed_files": map[string]any{
					"type":        "integer",
					"description": "Maximum representative files to return. Default 120, max 500.",
				},
				"max_files": map[string]any{
					"type":        "integer",
					"description": "Maximum files to scan before marking the map truncated. Default 4000, max 10000.",
				},
			},
		},
	}
}

func (t *RepoMapTool) Execute(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		MaxListedFiles int `json:"max_listed_files"`
		MaxFiles       int `json:"max_files"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	if args.MaxListedFiles < 0 || args.MaxFiles < 0 {
		return "", errors.New("repo_map max_listed_files and max_files must be non-negative")
	}
	maxListed := args.MaxListedFiles
	if maxListed == 0 {
		maxListed = defaultRepoMapToolMaxListedFiles
	}
	if maxListed > maxRepoMapToolMaxListedFiles {
		return "", fmt.Errorf("repo_map max_listed_files must be <= %d", maxRepoMapToolMaxListedFiles)
	}
	maxFiles := args.MaxFiles
	if maxFiles > maxRepoMapToolMaxFiles {
		return "", fmt.Errorf("repo_map max_files must be <= %d", maxRepoMapToolMaxFiles)
	}

	summary, ok, err := wuucontext.BuildRepoMap(t.env.RootDir, wuucontext.RepoMapOptions{
		MaxFiles:       maxFiles,
		MaxListedFiles: maxListed,
	})
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errors.New("repo_map found no files to summarize")
	}
	result := map[string]any{
		"workspace_revision": workspaceRevision(ctx, t.env.RootDir),
		"summary":            summary,
		"next_suggestions": []string{
			"use read_file, grep, ast_search, or semantic_search to confirm candidate files before editing",
		},
	}
	if summary.Truncated || summary.OmittedFiles > 0 {
		result["next_suggestions"] = []string{
			"narrow follow-up exploration with semantic_search, grep, ast_search, or read_file on representative files",
		}
	}
	if strings.TrimSpace(t.env.RootDir) == "" {
		result["warnings"] = []string{"workspace root is empty"}
	}
	return mustJSON(result)
}
