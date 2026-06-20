package prompt

import (
	"fmt"
	"strings"
)

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
	return "# Harness Adapter\n\n" +
		fmt.Sprintf("Provider/model: %s/%s. Treat wuu as the same product regardless of provider, model family, or BYOK backend.\n\n", providerName, model) +
		strings.Join([]string{
			"- Keep the same task behavior across providers; workflows, subagents, and profiles are task-handling options inside wuu.",
			"- Do not choose direct work, subagents, or workflows based on provider/model family or brand.",
			"- Choose execution shape from the user's task: direct local loop for simple work, subagents for independent parallel work, and workflows only when durable state, scheduling, repeatability, multiple phases/workers, or a matching saved workflow matters.",
			"- Follow the tools exposed in this session as the source of truth for editing and execution. If apply_patch is available, use it for manual edits; otherwise use edit_file/write_file as described by their tool schemas.",
			"- Treat provider/model differences as compatibility details only: tool schema, streaming behavior, context window, reasoning options (e.g. reasoning_effort, thinking budget, extended thinking toggle), prompt cache, and available tool set.",
		}, "\n")
}
