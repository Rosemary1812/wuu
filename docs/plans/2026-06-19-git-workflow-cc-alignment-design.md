# Git Workflow Alignment with Claude Code

> Date: 2026-06-19
> Status: decision memo
> Purpose: explain the shared-index incident, document Claude Code's current git/worktree design, and define the target direction for Wuu.

---

## 1. Background

Wuu currently exposes a restricted structured `git` tool and explicitly blocks git usage through `run_shell` and `start_process`.

Relevant current behavior:

- `internal/tools/tool_shell.go` rejects any shell command that invokes `git`.
- `internal/tools/tool_process.go` applies the same rejection to managed processes.
- `internal/tools/tool_git.go` exposes a structured `git` tool for status, diff, explicit-path add, unstage, commit, and push.
- `internal/tools/git.go` rewrites `git add` to explicit `git add -- <paths>`, but `git commit -m` commits the whole shared index.
- `internal/config/config.go` tells the model to use the structured git tool, not shell, for version-control work.
- `internal/skills/bundled/commit.md` teaches commit creation through the structured git tool.

The incident we investigated was not a missing git tool. The git tool was present and `git status` / `git add` worked. The failure came from two Wuu sessions sharing one repository checkout and one Git index:

1. Session A staged its intended files.
2. Session B staged another file in the same repo.
3. Session B ran `git commit -m`.
4. Git committed everything currently staged in that shared index, including Session A's files.
5. Session A later saw no staged changes and misdiagnosed the situation as a git/tool issue.

This is a normal Git property: the index belongs to the worktree, not to an agent session. If two sessions share one worktree, they share the same staging area.

The Wuu-specific problem is that the structured tool split the common human workflow into separate model/tool turns:

```text
git status -> git diff -> git add <paths> -> later git commit -m ...
```

That makes the window between staging and committing large enough for another session to interleave.

---

## 2. What Claude Code Does

Claude Code does not appear to solve this by adding an `expected_paths` field, staged-file ownership, or a long-lived add-to-commit mutex.

It combines four product layers.

### 2.1 Bash-first git workflow

Claude Code treats git as normal shell work. Its commit command grants Bash command patterns such as:

- `Bash(git add:*)`
- `Bash(git status:*)`
- `Bash(git commit:*)`

The built-in commit prompt gathers context with shell git commands, then tells the model to stage and commit in one message. It also recommends HEREDOC syntax for commit messages.

Local reference:

- `thirdparty/claude-code-sourcemap/src/commands/commit.ts`
- `thirdparty/claude-code-sourcemap/src/commands/commit-push-pr.ts`
- `thirdparty/claude-code-sourcemap/src/tools/BashTool/prompt.ts`

Official API docs also present git operations as a Bash workflow, including chained commands such as `git status && git add . && git commit -m "message"`:

- https://platform.claude.com/docs/en/agents-and-tools/tool-use/bash-tool

### 2.2 Prompt-level git method

Claude Code's prompt gives the model git safety rules:

- Only commit when the user asks.
- Inspect status, diff, and recent log first.
- Prefer specific files over `git add .` / `git add -A`.
- Do not commit likely secrets.
- Do not update git config.
- Do not skip hooks unless explicitly requested.
- Do not use destructive commands unless explicitly requested.
- Do not amend unless explicitly requested.
- Do not use interactive git flags.

This is methodology, not a hard isolation boundary. It reduces bad attempts but cannot make a shared Git index session-local.

### 2.3 Permission and safety layer for Bash

Claude Code's permissions operate on Bash commands, including git commands. Current official docs describe:

- Bash commands normally require approval.
- Permission rules can match command patterns such as `Bash(git *)`.
- Deny, ask, and allow rules are evaluated by the harness, not by model obedience.
- Compound commands are split so each subcommand must be permitted.

Official docs:

- https://code.claude.com/docs/en/permissions
- https://code.claude.com/docs/en/settings

Local reference:

- `thirdparty/claude-code-sourcemap/src/tools/BashTool/bashPermissions.ts`
- `thirdparty/claude-code-sourcemap/src/tools/BashTool/bashSecurity.ts`

The local safety code has special handling for `git commit -m`. Simple quoted messages can be allowed; suspicious metacharacters, command substitution, redirection, or obfuscated flags fall back to stronger validation or ask.

### 2.4 Worktree isolation for parallelism

Claude Code's current official docs are stronger than our earlier local-code-only read.

Current official docs say:

- CLI: `claude --worktree <name>` starts Claude in `<repo>/.claude/worktrees/<name>`.
- The desktop app creates a worktree for every new session automatically.
- Subagents can use `isolation: worktree`.
- Background sessions have `worktree.bgIsolation`; `"worktree"` is documented as the default for background sessions.
- Worktrees created for subagents/background sessions are cleaned up when safe.

Official docs:

- https://code.claude.com/docs/en/worktrees
- https://code.claude.com/docs/en/cli-reference
- https://code.claude.com/docs/en/settings

Local reference:

- `thirdparty/claude-code-sourcemap/src/setup.ts`
- `thirdparty/claude-code-sourcemap/src/utils/worktree.ts`
- `thirdparty/claude-code-sourcemap/src/tools/AgentTool/AgentTool.tsx`
- `thirdparty/claude-code-sourcemap/src/skills/bundled/batch.ts`

Important nuance: the local sourcemap and current official docs are not exactly the same product snapshot. For this decision, official docs are the stronger evidence for current Claude Code behavior.

---

## 3. Why CC's Design Is Usable

Claude Code is usable because it avoids making the model fight a custom git abstraction for the common path.

The practical safety comes from:

1. Shorter git transactions.
   Shell lets the model do `git add <paths> && git commit -m ...` in one tool call or one assistant message, reducing the shared-index interleaving window.

2. Normal tool affordance.
   Models and developers already understand shell git. The model does not need to learn an unusual `subcommand` JSON contract for common work.

3. Harness-level permission checks.
   Risky shell commands are not controlled only by prompt wording. They are classified and approved/blocked by the runtime.

4. Worktree isolation where overlap is expected.
   Parallel sessions, subagents, background tasks, and batch workers are steered toward separate working directories.

This does not mathematically eliminate all git races. Two ordinary shell sessions in the same checkout can still interfere if they deliberately or accidentally share staged state. CC reduces how often users hit the problem and gives stronger isolation when the product knows parallel work is happening.

---

## 4. Wuu Decision

Wuu should move the main commit path toward Claude Code's Bash-first design, but we should not blindly copy every current CC desktop behavior without an explicit product decision.

Recommended target:

1. Allow safe git through `run_shell`.
2. Keep `start_process` rejecting git, because long-lived git processes are not the common commit path.
3. Move commit methodology from the structured `git` tool into prompt/skill guidance for shell git.
4. Add a CC-style shell git classifier:
   - Read-only: `git status`, `git diff`, `git log`, `git show`, `git branch --show-current`, `git rev-parse`, etc.
   - Medium local writes: `git add <explicit paths>`, `git restore --staged <explicit paths>`, `git commit -m ...`.
   - Remote writes: `git push`, ask/approval by default.
   - High-risk or blocked: `reset --hard`, `clean -f`, `checkout .`, broad destructive `restore`, force push, config mutation, interactive flags, hook skipping unless explicitly requested.
5. Preserve the structured `git` tool as an auxiliary capability:
   - structured status for UI/tool results,
   - sensitive path checks,
   - read-only git commands,
   - optional fallback for providers that handle structured tools better than shell.
6. Do not add `expected_paths` or staged ownership as the first move.
   These would make Wuu more bespoke than CC and would add friction to normal git workflows. They remain possible later if telemetry shows shell-first plus worktree isolation is insufficient.

### Worktree decision point

There is one unresolved product choice.

The active Wuu goal says ordinary sessions should not be forced into worktrees, and Wuu's project guidance currently says to work directly on `main` unless the user asks for isolation or a concrete safety reason requires it.

Current Claude Code official docs say its desktop app creates a worktree for every new session automatically.

Those two positions conflict. We should treat this as a product decision, not an implementation detail.

Recommended staged approach:

1. Phase 1: fix the git workflow mismatch first.
   Allow shell git with safety classification and update prompts. This directly addresses the incident without changing Wuu's session model.

2. Phase 2: align parallel/background isolation.
   Make background agents, explicit parallel work, batch workflows, and broad worker edits default to worktree isolation. Wuu already has pieces of this in `internal/agentcontrol` and `.wuu/worktrees`.

3. Phase 3: decide desktop new-session behavior.
   Offer a setting or product experiment for "new desktop session uses worktree". If the product decision is strict CC desktop parity, make this the default. If Wuu's product priority is direct main-branch iteration, keep it opt-in and surface a warning when multiple active sessions share one repo.

This keeps the decision reversible while acknowledging that "full current CC parity" includes stronger worktree defaults than our earlier assumption.

---

## 5. Implementation Shape

### 5.1 Shell classification

Current Wuu code treats any shell git command as high risk and then execution rejects it.

Change target:

- Replace the blanket `shellCommandInvokesGit` rejection in `run_shell` with `classifyShellGitCommand`.
- Keep environment dumps, sensitive path reads, package/network mutation checks, and destructive shell checks.
- Allow read-only git commands directly.
- Allow medium-risk local git writes through existing tool policy/approval modes.
- Deny or require explicit approval for dangerous git commands.

The existing `ToolClassification`, `ToolPolicy`, and auto-mode machinery can carry this. We do not need a separate permission system for git shell.

### 5.2 Prompt and skill updates

Update:

- `internal/config/config.go`
- `internal/skills/bundled/commit.md`
- relevant worker prompts in `internal/agentcontrol/worker_types.go`

New guidance:

- Use shell git for normal version-control work.
- Inspect `git status`, `git diff`, and recent commits before committing.
- Stage explicit paths.
- Prefer one short shell transaction for stage plus commit when appropriate.
- Do not use destructive git commands unless the user explicitly requested them.
- Do not skip hooks unless the user explicitly requested it.
- Do not push unless the user explicitly asked for remote write.

### 5.3 Structured git tool repositioning

The structured `git` tool should not be removed immediately.

Keep it for:

- structured status output,
- sensitive path protection,
- UI and telemetry consistency,
- backward compatibility with existing sessions/prompts,
- providers or modes where shell permissioning is not enabled.

But its description should no longer tell the model it is the only valid git path.

### 5.4 Worktree policy

Current Wuu has worktree support for agent control and goal review, but ordinary root sessions still share the same checkout.

Target:

- Background/subagent/batch work that can edit overlapping files should default to worktree.
- In-place agents should remain available for small, clearly scoped, user-visible edits.
- Desktop should eventually detect concurrent active sessions in one repo and either recommend worktree or create one depending on the product decision above.

---

## 6. Non-goals

- Do not implement staged-file ownership in phase 1.
- Do not hold a process-wide lock across `git add` and `git commit`.
- Do not force all Wuu sessions into worktrees until the desktop session model decision is made.
- Do not remove the structured `git` tool in the first migration.
- Do not allow arbitrary shell git just because CC uses Bash. The harness still needs command classification.

---

## 7. Verification Plan

Minimum tests for the implementation:

1. Shell metadata:
   - `git status` is read-only/low risk.
   - `git diff` and `git log` are read-only/low risk.
   - `git add src/file.ts` is medium-risk local write.
   - `git commit -m "message"` is medium-risk local write.
   - `git push` requires remote-write approval.
   - `git reset --hard`, `git clean -f`, `git checkout .`, `git config user.name x`, `git commit --no-verify`, and force push are blocked or require explicit high-risk approval.

2. Shell execution:
   - `run_shell` no longer rejects safe git commands just because they invoke git.
   - `start_process` still rejects git commands.
   - sensitive path and environment dumping protections still apply.

3. Prompt/tool definitions:
   - system prompt no longer says "Use the git tool, not run_shell".
   - commit skill teaches shell git.
   - structured git tool is described as optional/auxiliary.

4. Race reproduction:
   - Create two sessions or two toolkits against the same repo.
   - Demonstrate the old staged-index interleaving risk with split add/commit.
   - Demonstrate the new recommended path can stage and commit in one short shell call.
   - This does not prove the race impossible; it proves the product path no longer unnecessarily widens the window.

5. Worktree behavior:
   - Worktree-isolated agents do not share the parent index.
   - In-place agents still work for direct edits.
   - If desktop auto-worktree is implemented later, verify every new desktop session starts in its own worktree and cleanup/merge UX is clear.

---

## 8. Recommendation

Adopt CC's main idea, not Wuu-specific staged ownership.

The first useful product move is:

1. Stop hard-banning shell git in `run_shell`.
2. Add CC-style git command classification and safety gates.
3. Update prompts and commit skill to make shell git the normal path.
4. Keep structured git as support, not doctrine.
5. Strengthen worktree defaults for known parallel/background work.

Then make an explicit separate call on desktop new-session auto-worktree parity. If the product requirement is "fully match current Claude Code desktop", Wuu should implement that. If the requirement is "keep Wuu's direct-main fast iteration model", Wuu should keep ordinary sessions in-place and warn or offer worktree when multiple sessions share a repo.
