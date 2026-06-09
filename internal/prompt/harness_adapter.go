package prompt

import (
	"fmt"
	"strings"

	"github.com/blueberrycongee/wuu/internal/modelprofile"
)

type HarnessFamily = modelprofile.Family

const (
	HarnessFamilyPortable = modelprofile.FamilyPortable
	HarnessFamilyClaude   = modelprofile.FamilyClaude
	HarnessFamilyCodex    = modelprofile.FamilyCodex
	HarnessFamilyGPT      = modelprofile.FamilyGPT
	HarnessFamilyGemini   = modelprofile.FamilyGemini
	HarnessFamilyKimi     = modelprofile.FamilyKimi
)

func HarnessFamilyForModel(providerName, model string) HarnessFamily {
	return modelprofile.Resolve(providerName, model).Family
}

func (b *Builder) AddHarnessAdapter(providerName, model string) {
	text := HarnessAdapterText(providerName, model)
	if strings.TrimSpace(text) == "" {
		return
	}
	b.AddSection("harness_adapter", text, true)
}

func HarnessAdapterText(providerName, model string) string {
	providerName = strings.TrimSpace(providerName)
	model = strings.TrimSpace(model)
	if providerName == "" && model == "" {
		return ""
	}
	profile := modelprofile.Resolve(providerName, model)
	header := "# Harness Adapter\n\n" +
		fmt.Sprintf("Provider/model: %s/%s. Profile family: %s. Default write mode: %s. Treat model behavior, prompts, tools, and subagents as one harness. Follow the model-family guidance below when choosing workflow, subagent, and tool style.\n\n", providerName, model, profile.Family, profile.Workflow.DefaultWriteMode)
	switch profile.Family {
	case modelprofile.FamilyClaude:
		return header + strings.Join([]string{
			"- Start workflow-shaped work with start_workflow driver=auto after loading the saved definition; force run_workflow only when the script driver is explicitly required.",
			"- For executable workflow definitions, let the script driver manage phase/spawn/await/synthesize state under the same Workflow Run and Workflow Team model.",
			"- Use subagents for parallel research, verification, and isolated implementation when the worker brief is explicit and self-contained.",
			"- Keep workflow and worker state in Workflow Run state, Agent Reports, and artifacts instead of carrying long-running state only in chat.",
		}, "\n")
	case modelprofile.FamilyCodex, modelprofile.FamilyGPT:
		return header + strings.Join([]string{
			"- Prefer compact, tool-contract-driven control flow: inspect, plan only when useful, execute, verify, and report.",
			"- Prefer apply_patch when it is exposed for this model profile; otherwise use exact edit_file replacements and write_file only for new or explicitly rewritten files.",
			"- Use start_workflow driver=auto when durable state, scheduling, repeatability, or multi-agent coordination matters; force lower-level workflow drivers only for recovery or an explicit driver requirement.",
			"- Use specialized modes such as review or goal tracking as separate task modes when available instead of folding every task into workflow state.",
		}, "\n")
	case modelprofile.FamilyGemini, modelprofile.FamilyKimi, modelprofile.FamilyDeepSeek, modelprofile.FamilyQwen:
		return header + strings.Join([]string{
			"- Treat tool schemas and tool descriptions as the source of truth; avoid relying on model-family-specific aliases or hidden conventions.",
			"- Prefer portable primitives: start_workflow driver=auto for workflow-shaped work, spawn_agent for delegated workers, and lower-level workflow drivers only when explicitly required.",
			"- Keep prompts explicit about required inputs, expected outputs, and tool result handling.",
		}, "\n")
	case modelprofile.FamilyLocal:
		return header + strings.Join([]string{
			"- Use a conservative local-model harness path: small context, few tools, explicit file evidence, and short autonomous loops.",
			"- Prefer read/search plus small guarded edits; ask for review before broad writes, shell execution, or risky workflow branching.",
			"- Keep task briefs explicit and self-contained when delegation or workflow execution depends on another agent.",
		}, "\n")
	default:
		return header + strings.Join([]string{
			"- Use the portable harness path: follow tool descriptions exactly and avoid model-family-specific assumptions.",
			"- Prefer generic primitives: start_workflow driver=auto for workflow-shaped work, spawn_agent for delegated workers, and lower-level workflow drivers only when explicitly required.",
			"- Keep task briefs explicit and self-contained when delegation or workflow execution depends on another agent.",
		}, "\n")
	}
}
