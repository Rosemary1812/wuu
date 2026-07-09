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
			"- Keep the same task behavior across providers; runtime goals, task rail, and subagents are wuu coordination primitives.",
			"- Do not choose direct local work, runtime goals, task-rail delegation, or subagents based on provider/model family or brand.",
			"- Choose execution shape from the user's task: direct local loop for simple work. Use a runtime goal when the user-visible objective needs continuation across turns, and task rail or subagents for delegated or independent parallel work.",
			"- Follow the tools exposed in this session as the source of truth for editing and execution. If apply_patch is available, use it for manual edits; otherwise use edit_file/write_file as described by their tool schemas.",
			"- Treat provider/model differences as compatibility details only: tool schema, streaming behavior, context window, reasoning options (e.g. reasoning_effort, thinking budget, extended thinking toggle), prompt cache, and available tool set.",
		}, "\n")
}
