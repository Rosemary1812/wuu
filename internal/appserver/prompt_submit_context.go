package appserver

import (
	"context"
	"fmt"
	"strings"

	wuucontext "github.com/blueberrycongee/wuu/internal/context"
	"github.com/blueberrycongee/wuu/internal/hooks"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/runtime"
	skillpkg "github.com/blueberrycongee/wuu/internal/skills"
)

type slashTaskCommand struct {
	title   string
	content string
}

var slashTaskCommands = map[string]slashTaskCommand{
	"review": {
		title:   "/review",
		content: "The user invoked /review. Review the relevant code or current changes. Lead with real bugs, security issues, logic errors, or missing verification, ordered by severity. Do not make code changes unless the user explicitly asks.",
	},
	"debug": {
		title:   "/debug",
		content: "The user invoked /debug. Investigate the bug, failure, or confusing behavior. Reproduce it or locate concrete evidence first, identify the root cause, then fix it only when the fix is clear and verify the affected path.",
	},
	"fix": {
		title:   "/fix",
		content: "The user invoked /fix. Read the relevant code, make the smallest coherent product fix, keep unrelated changes out, and run focused verification.",
	},
	"test": {
		title:   "/test",
		content: "The user invoked /test. Add or update focused tests for the requested behavior, then run the relevant verification and report any remaining test gaps.",
	},
	"explain": {
		title:   "/explain",
		content: "The user invoked /explain. Explain the relevant behavior, code, or error with concrete references. Do not edit files unless the user also asks for a change.",
	},
	"commit": {
		title:   "/commit",
		content: "The user invoked /commit. Review local changes, verify them when practical, and create one atomic commit with an English commit message if the changes are ready. Do not include unrelated work.",
	},
	"pr": {
		title:   "/pr",
		content: "The user invoked /pr. Prepare a pull request for the current branch when the repository and remote state are ready. Include the user-facing change and verification, and use GitHub tooling for GitHub operations.",
	},
}

func (s *Server) userPromptSubmitContext(ctx context.Context, th *threadState, threadRuntime *runtime.ThreadRuntime, prompt string) ([]providers.ChatMessage, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, nil
	}

	var blocks []wuucontext.Block
	if block, ok := s.slashCommandContextBlock(prompt, threadRuntime); ok {
		blocks = append(blocks, block)
	}
	if block, err := s.userPromptSubmitHookBlock(ctx, th, prompt); err != nil {
		return nil, err
	} else if strings.TrimSpace(block.Content) != "" {
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	return []providers.ChatMessage{{
		Role:    "user",
		Name:    wuucontext.SystemReminderMessageName,
		Content: wuucontext.FormatSystemReminderBlocks(blocks...),
	}}, nil
}

func (s *Server) userPromptSubmitHookBlock(ctx context.Context, th *threadState, prompt string) (wuucontext.Block, error) {
	if s == nil || s.rt == nil || s.rt.HookDispatcher == nil || !s.rt.HookDispatcher.HasHooks(hooks.UserPromptSubmit) {
		return wuucontext.Block{}, nil
	}
	sessionID := ""
	cwd := ""
	if th != nil {
		sessionID = th.ID
		cwd = strings.TrimSpace(th.CWD)
	}
	if cwd == "" {
		cwd = s.rt.RootDir
	}
	out, err := s.rt.HookDispatcher.Dispatch(ctx, hooks.UserPromptSubmit, &hooks.Input{
		SessionID: sessionID,
		CWD:       cwd,
		Prompt:    prompt,
	})
	if err != nil {
		if hooks.IsBlocked(err) {
			return wuucontext.Block{}, fmt.Errorf("prompt blocked by hook: %w", err)
		}
		return wuucontext.Block{}, fmt.Errorf("run UserPromptSubmit hook: %w", err)
	}
	if out == nil || strings.TrimSpace(out.Context) == "" {
		return wuucontext.Block{}, nil
	}
	return wuucontext.Block{
		Kind:    wuucontext.BlockAdditionalContext,
		Title:   "UserPromptSubmit",
		Source:  "hook",
		Content: strings.TrimSpace(out.Context),
	}, nil
}

func (s *Server) slashCommandContextBlock(prompt string, threadRuntime *runtime.ThreadRuntime) (wuucontext.Block, bool) {
	name, args, ok := parseLeadingSlashCommand(prompt)
	if !ok {
		return wuucontext.Block{}, false
	}
	if command, ok := slashTaskCommands[name]; ok {
		content := command.content
		if args != "" {
			content += "\n\nArguments after the command:\n" + args
		}
		return wuucontext.Block{
			Kind:    wuucontext.BlockAdditionalContext,
			Title:   command.title,
			Source:  "slash-command",
			Content: content,
		}, true
	}
	if skill, ok := findUserInvocableSkill(name, runtimeSkills(s, threadRuntime)); ok {
		content := "The user invoked the skill slash command `/" + skill.Name + "`. Load the matching skill with `load_skill` before acting, then treat the text after the command as the skill arguments."
		if args != "" {
			content += "\n\nSkill arguments:\n" + args
		}
		return wuucontext.Block{
			Kind:    wuucontext.BlockAdditionalContext,
			Title:   "/" + skill.Name,
			Source:  "skill-slash-command",
			Content: content,
		}, true
	}
	return wuucontext.Block{}, false
}

func parseLeadingSlashCommand(prompt string) (name, args string, ok bool) {
	trimmed := strings.TrimSpace(prompt)
	if !strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "//") {
		return "", "", false
	}
	withoutSlash := strings.TrimPrefix(trimmed, "/")
	if withoutSlash == "" {
		return "", "", false
	}
	fields := strings.Fields(withoutSlash)
	if len(fields) == 0 {
		return "", "", false
	}
	name = strings.ToLower(strings.TrimSpace(fields[0]))
	if name == "" || strings.Contains(name, "/") {
		return "", "", false
	}
	args = strings.TrimSpace(strings.TrimPrefix(withoutSlash, fields[0]))
	return name, args, true
}

func runtimeSkills(s *Server, threadRuntime *runtime.ThreadRuntime) []skillpkg.Skill {
	if threadRuntime != nil && threadRuntime.Toolkit != nil {
		return threadRuntime.Toolkit.Skills()
	}
	if s != nil && s.rt != nil {
		return s.rt.Skills
	}
	return nil
}

func findUserInvocableSkill(name string, skills []skillpkg.Skill) (skillpkg.Skill, bool) {
	skill, ok := skillpkg.Find(skills, name)
	if !ok || !skill.UserInvocable || skill.DisableModelInvoke {
		return skillpkg.Skill{}, false
	}
	return skill, true
}
