package tools

import "github.com/blueberrycongee/wuu/internal/modelprofile"

type EditToolMode string

const (
	EditToolModeText  EditToolMode = "text"
	EditToolModePatch EditToolMode = "patch"
)

// EditToolModeForModel keeps the legacy one-argument helper for tests and
// callers that only know the API model. New runtime code should pass provider
// name too so the model profile can use both signals.
func EditToolModeForModel(model string) EditToolMode {
	return EditToolModeForProviderModel("", model)
}

func EditToolModeForProviderModel(providerName, model string) EditToolMode {
	profile := modelprofile.Resolve(providerName, model)
	if profile.Workflow.DefaultWriteMode == modelprofile.WriteModePatch {
		return EditToolModePatch
	}
	return EditToolModeText
}

func (t *Toolkit) ConfigureEditToolsForModel(model string) {
	t.SetEditToolMode(EditToolModeForModel(model))
}

func (t *Toolkit) ConfigureEditToolsForProviderModel(providerName, model string) {
	t.SetEditToolMode(EditToolModeForProviderModel(providerName, model))
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
