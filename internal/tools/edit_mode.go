package tools

import "strings"

type EditToolMode string

const (
	EditToolModeText  EditToolMode = "text"
	EditToolModePatch EditToolMode = "patch"
)

// EditToolModeForModel follows OpenCode's current split: GPT-family models
// except GPT-4 and OSS variants get apply_patch; other models keep edit/write.
func EditToolModeForModel(model string) EditToolMode {
	id := strings.ToLower(strings.TrimSpace(model))
	if idx := strings.LastIndex(id, "/"); idx >= 0 {
		id = id[idx+1:]
	}
	if strings.Contains(id, "gpt-") &&
		!strings.Contains(id, "gpt-4") &&
		!strings.Contains(id, "oss") {
		return EditToolModePatch
	}
	return EditToolModeText
}

func (t *Toolkit) ConfigureEditToolsForModel(model string) {
	t.SetEditToolMode(EditToolModeForModel(model))
}

func (t *Toolkit) SetEditToolMode(mode EditToolMode) {
	switch mode {
	case EditToolModePatch:
		t.EnableTools("apply_patch")
		t.DisableTools("edit_file", "write_file")
	default:
		t.EnableTools("edit_file", "write_file")
		t.DisableTools("apply_patch")
	}
}
