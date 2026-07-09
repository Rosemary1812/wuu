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
	if profile.Execution.DefaultWriteMode == modelprofile.WriteModePatch {
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
// forMainAgent flag selects whether the compiled surface includes or hides
// main-agent-only recovery tools consistently. Runtime defense-in-depth is
// enforced by worker tool filtering and the tools' own main-agent path checks.
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
