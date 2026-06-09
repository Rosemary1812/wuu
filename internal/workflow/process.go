package workflow

import "strings"

type ProcessOptions struct {
	Arguments   string
	WorkflowDir string
	SessionID   string
}

// ProcessBody applies portable workflow variable substitution. Unlike skills,
// workflows do not execute inline shell in the first version.
func ProcessBody(body string, opts ProcessOptions) string {
	replacer := strings.NewReplacer(
		"${ARGUMENTS}", opts.Arguments,
		"${WUU_WORKFLOW_DIR}", opts.WorkflowDir,
		"${WUU_SESSION_ID}", opts.SessionID,
		"${CLAUDE_WORKFLOW_DIR}", opts.WorkflowDir,
		"${CLAUDE_SESSION_ID}", opts.SessionID,
	)
	return replacer.Replace(body)
}
