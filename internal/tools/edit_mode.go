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
	return EditToolModeForProfile(modelprofile.Resolve(providerName, model))
}

func EditToolModeForProfile(profile modelprofile.Profile) EditToolMode {
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

// ConfigureSurfaceForProviderModel compiles the surface for the given
// provider/model and installs it as the toolkit's active profile. The
// forMainAgent flag selects whether the compiled surface includes the
// helpme recovery tool — main-agent kits pass true so helpme is part
// of the model's tool list, worker kits pass false so the compiled
// surface omits it cleanly. Runtime defense-in-depth (DisallowedTools
// in internal/agentcontrol/worker_types.go and the path check in
// HelpMeTool.Execute) is unchanged.
func (t *Toolkit) ConfigureSurfaceForProviderModel(providerName, model string, forMainAgent bool) {
	t.SetActiveProfile(modelprofile.Resolve(providerName, model), forMainAgent)
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
