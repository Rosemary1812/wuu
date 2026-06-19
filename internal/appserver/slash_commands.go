package appserver

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type slashCommandTemplate struct {
	Name        string
	Aliases     []string
	Execution   slashCommandExecution
	Prompt      string
	PromptNoArg string
}

type slashCommandExecution string

const slashCommandExecutionInlinePrompt slashCommandExecution = "inline_prompt"

var lightweightSlashCommandTemplates = []slashCommandTemplate{
	{
		Name:        "review",
		Aliases:     []string{"audit"},
		Execution:   slashCommandExecutionInlinePrompt,
		Prompt:      "Review the current code changes (staged, unstaged, and untracked files) with this focus:\n\n{{args}}\n\nProvide prioritized, actionable findings.",
		PromptNoArg: "Review the current code changes (staged, unstaged, and untracked files) and provide prioritized findings.",
	},
	{
		Name:        "debug",
		Aliases:     []string{"investigate", "diagnose"},
		Execution:   slashCommandExecutionInlinePrompt,
		Prompt:      "Investigate this problem before making changes:\n\n{{args}}\n\nFind the root cause first. Use the repository and runtime evidence as needed, then propose or implement the smallest complete fix.",
		PromptNoArg: "Investigate the current problem before making changes. Find the root cause first. Use the repository and runtime evidence as needed, then propose or implement the smallest complete fix.",
	},
	{
		Name:        "fix",
		Aliases:     []string{"repair"},
		Execution:   slashCommandExecutionInlinePrompt,
		Prompt:      "Fix this issue:\n\n{{args}}\n\nInspect the relevant code first, keep the change scoped, and verify the behavior.",
		PromptNoArg: "Fix the current issue. Inspect the relevant code first, keep the change scoped, and verify the behavior.",
	},
	{
		Name:        "test",
		Aliases:     []string{"tests"},
		Execution:   slashCommandExecutionInlinePrompt,
		Prompt:      "Add or update tests for this behavior:\n\n{{args}}\n\nFocus on real behavior coverage, then run the relevant verification.",
		PromptNoArg: "Add or update tests for the current behavior. Focus on real behavior coverage, then run the relevant verification.",
	},
	{
		Name:        "explain",
		Aliases:     []string{"why"},
		Execution:   slashCommandExecutionInlinePrompt,
		Prompt:      "Explain this clearly:\n\n{{args}}\n\nUse the relevant code, errors, or runtime evidence instead of guessing.",
		PromptNoArg: "Explain the current code or behavior clearly. Use the relevant code, errors, or runtime evidence instead of guessing.",
	},
	{
		Name:        "commit",
		Aliases:     []string{"save"},
		Execution:   slashCommandExecutionInlinePrompt,
		Prompt:      "Prepare an atomic git commit for the current changes.\n\nUser notes:\n{{args}}\n\nReview the diff, run relevant checks when practical, and create a clear English commit message.",
		PromptNoArg: "Prepare an atomic git commit for the current changes. Review the diff, run relevant checks when practical, and create a clear English commit message.",
	},
	{
		Name:        "pr",
		Aliases:     []string{"pull-request", "pullrequest"},
		Execution:   slashCommandExecutionInlinePrompt,
		Prompt:      "Prepare a pull request for the current branch.\n\nUser notes:\n{{args}}\n\nReview the diff, summarize the user-facing changes and verification, and create the PR when the branch is ready.",
		PromptNoArg: "Prepare a pull request for the current branch. Review the diff, summarize the user-facing changes and verification, and create the PR when the branch is ready.",
	},
}

func renderLightweightSlashCommandPrompt(prompt string) (string, string, bool) {
	display := strings.TrimSpace(prompt)
	if !strings.HasPrefix(display, "/") || strings.HasPrefix(display, "//") {
		return prompt, "", false
	}
	command, args, ok := splitSlashCommand(display)
	if !ok {
		return prompt, "", false
	}
	template, ok := findLightweightSlashCommandTemplate(command)
	if !ok {
		return prompt, "", false
	}
	if template.Execution != slashCommandExecutionInlinePrompt {
		return prompt, "", false
	}
	rendered := renderSlashTemplate(template, args)
	if strings.TrimSpace(rendered) == "" {
		return prompt, "", false
	}
	return rendered, display, true
}

func splitSlashCommand(value string) (string, string, bool) {
	body := strings.TrimPrefix(value, "/")
	if body == "" {
		return "", "", false
	}
	if first, _ := utf8.DecodeRuneInString(body); unicode.IsSpace(first) {
		return "", "", false
	}
	nameEnd := len(body)
	for index, r := range body {
		if unicode.IsSpace(r) {
			nameEnd = index
			break
		}
	}
	name := strings.ToLower(strings.TrimSpace(body[:nameEnd]))
	if name == "" {
		return "", "", false
	}
	args := ""
	if nameEnd < len(body) {
		args = strings.TrimSpace(body[nameEnd:])
	}
	return name, args, true
}

func findLightweightSlashCommandTemplate(name string) (slashCommandTemplate, bool) {
	for _, template := range lightweightSlashCommandTemplates {
		if template.Name == name {
			return template, true
		}
		for _, alias := range template.Aliases {
			if alias == name {
				return template, true
			}
		}
	}
	return slashCommandTemplate{}, false
}

func renderSlashTemplate(template slashCommandTemplate, args string) string {
	args = strings.TrimSpace(args)
	if args == "" && strings.TrimSpace(template.PromptNoArg) != "" {
		return strings.TrimSpace(template.PromptNoArg)
	}
	return strings.TrimSpace(strings.ReplaceAll(template.Prompt, "{{args}}", args))
}
