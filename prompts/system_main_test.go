package prompts

import (
	"strings"
	"testing"
)

func TestSystemMainTeachesInceptionTriggers(t *testing.T) {
	prompt := SystemMain()
	for _, want := range []string{
		"Use it proactively during the work",
		"keeps its working context clean",
		"not a last resort",
		"<system>CHECKPOINT N</system>",
		"available in the current tool list",
		"large file",
		"web/search result",
		"dead end",
		"coding or debugging detour",
		"Do not wait until only the final answer remains",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("SystemMain missing %q:\n%s", want, prompt)
		}
	}
}

func TestSystemGuidesNaturalUserCenteredReplies(t *testing.T) {
	prompt := System()
	for _, want := range []string{
		"from the user's mental model, not internal jargon",
		"what the user likely already knows and what they need next",
		"Skip ritual openings",
		"Sure!",
		"concrete next action is enough",
		"Prefer natural, approachable prose",
		"If a user assumption is wrong or risky, say so plainly",
		"Prefer short paragraphs for ordinary answers",
		"Avoid frequent line breaks, stacked headers, tables, or bullet lists",
		"when a sentence or two would read more naturally",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("System missing %q:\n%s", want, prompt)
		}
	}
}
