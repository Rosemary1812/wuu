package workflow

import "sort"

func BundledDefinitions() []Definition {
	return []Definition{{
		Name:          "compose",
		Description:   "Coordinate ambiguous or multi-agent coding work through skills, planning, agents, memory, and verification.",
		WhenToUse:     "Use when a task is broad, ambiguous, cross-cutting, workflow-shaped, or benefits from multiple skills or sub-agents.",
		Kind:          DefinitionKindMarkdown,
		Source:        "bundled",
		UserInvocable: true,
		Content: `## Intent

Use Compose when direct implementation would lose structure. Compose turns a broad request into a planned workflow that can combine skills, sub-agents, memory, and verification.

## Operating Rules

1. Check available skills and workflows first. Invoke any clearly matching skill before changing code.
2. If requirements are ambiguous, use a short plan and ask only for irreversible product, security, or architecture choices.
3. Use update_plan for visible workflow state, and keep exactly one step in progress.
4. Use spawn_agent only for independent investigation, implementation slices, or verification that benefits from separate context.
5. Use session_memory for durable project facts, session checkpoints, or notes that should survive context pruning.
6. Before completion, inspect the final diff or workflow state and run focused verification.

## Completion Contract

Finish only after the requested behavior is implemented, relevant verification has passed, and any durable memory candidates have been recorded or intentionally skipped.
`,
	}}
}

func MergeWithBundled(discovered []Definition) []Definition {
	bundled := BundledDefinitions()
	if len(bundled) == 0 {
		return discovered
	}
	seen := make(map[string]bool, len(discovered))
	for _, def := range discovered {
		seen[def.Name] = true
	}
	merged := make([]Definition, 0, len(discovered)+len(bundled))
	merged = append(merged, discovered...)
	for _, def := range bundled {
		if !seen[def.Name] {
			merged = append(merged, def)
		}
	}
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Name < merged[j].Name
	})
	return merged
}
