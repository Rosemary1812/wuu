package tools

import (
	"github.com/blueberrycongee/wuu/internal/capability"
	"github.com/blueberrycongee/wuu/internal/workflow"
)

// FilterWorkflowsForSurface applies the same model-surface boundary to
// reusable workflows that FilterSkillsForSurface applies to skills.
// Workflows can carry arbitrary project-authored instructions, so a
// no-shell profile must not see definitions that teach terminal paths.
func FilterWorkflowsForSurface(all []workflow.Definition, surface capability.Surface) []workflow.Definition {
	if surface.ProfileName == "" {
		out := make([]workflow.Definition, len(all))
		copy(out, all)
		return out
	}
	out := make([]workflow.Definition, 0, len(all))
	for _, def := range all {
		if workflowAllowedBySurface(def, surface) {
			out = append(out, def)
		}
	}
	return out
}

func workflowAllowedBySurface(def workflow.Definition, surface capability.Surface) bool {
	if surface.ProfileName == "" || surfaceHasVisibleCapability(surface, capability.CapabilityCommandBash) {
		return true
	}
	return !mentionsTerminalOnlyPath(
		def.Name,
		def.Description,
		def.WhenToUse,
		def.ArgumentHint,
		def.Content,
		def.Path,
		def.Dir,
		def.Source,
	)
}
