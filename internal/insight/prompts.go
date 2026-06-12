package insight

const facetExtractionPrompt = `Analyze this coding assistant session and extract structured facets.

Guidelines:

1. **goal_categories**: Count only what the user explicitly asked for.
   - Do not count the assistant's autonomous exploration
   - Count explicit user requests such as: "can you...", "please...", "I need...", "let's..."
   - Use categories: write_code, debug_investigate, refactor, explain, review, test, deploy, config, docs, warmup_minimal

2. **outcome**: Based on whether the user's goals were achieved.
   - fully_achieved, mostly_achieved, partially_achieved, not_achieved, unclear

3. **satisfaction**: Based on explicit user signals.
   - high: "great!", "perfect!", "awesome!"
   - medium: "thanks", "looks good", "ok"
   - low: "that's not right", "try again", frustrated signals
   - unknown: no clear signals

4. **helpfulness**: How helpful was the assistant overall?
   - unhelpful, slightly_helpful, moderately_helpful, very_helpful, essential

5. **session_type**: single_task, multi_task, iterative_refinement, exploration, quick_question

6. **friction**: Be specific about what went wrong.
   - misunderstood_request, wrong_approach, buggy_code, excessive_changes, slow_response, none

7. **primary_success**: What helped most?
   - none, fast_search, correct_edits, good_explanations, proactive_help, multi_file_changes, good_debugging

Respond with a valid JSON object and no markdown fences:
{
  "goal": "What the user fundamentally wanted to achieve",
  "goal_categories": {"category": count},
  "outcome": "fully_achieved|mostly_achieved|partially_achieved|not_achieved|unclear",
  "satisfaction": "high|medium|low|unknown",
  "helpfulness": "unhelpful|slightly_helpful|moderately_helpful|very_helpful|essential",
  "session_type": "single_task|multi_task|iterative_refinement|exploration|quick_question",
  "friction": {"friction_type": count},
  "friction_detail": "One sentence describing friction, or empty string",
  "primary_success": "what helped most",
  "summary": "One sentence: what user wanted and whether they got it"
}

Session transcript:
`

// insightSectionDefs defines the sections to generate in parallel.
type insightSectionDef struct {
	Name      string
	Title     string
	Prompt    string
	MaxTokens int
}

var insightSections = []insightSectionDef{
	{
		Name:  "usage_patterns",
		Title: "Usage Patterns",
		Prompt: `Based on these session statistics, describe the user's usage patterns in 2-3 short paragraphs.
Focus on: when they use the tool, how long sessions last, how many messages per session.
Write in second person ("you"). Be specific, reference the data.

Stats:
%s

Respond in markdown.`,
		MaxTokens: 1024,
	},
	{
		Name:  "common_goals",
		Title: "What You Work On",
		Prompt: `Based on these session summaries and goal categories, identify 4-5 main project areas the user works on.
For each area, write a short description of what kind of work they do there.

Data:
%s

Respond as a markdown list with **bold area names** and descriptions.`,
		MaxTokens: 1024,
	},
	{
		Name:  "tool_usage",
		Title: "How You Use Tools",
		Prompt: `Based on these tool usage statistics, describe how this user interacts with coding tools.
What tools do they use most? What does this say about their workflow?
Write in second person, 2-3 short paragraphs.

Data:
%s

Respond in markdown.`,
		MaxTokens: 1024,
	},
	{
		Name:  "what_works",
		Title: "Impressive Things You Did",
		Prompt: `From these session summaries and outcomes, identify 3 impressive or effective workflows the user has employed.
For each, explain what made it effective.

Data:
%s

Respond as a numbered markdown list with **bold titles** and 1-2 sentence descriptions.`,
		MaxTokens: 1024,
	},
	{
		Name:  "friction_analysis",
		Title: "Where Things Go Wrong",
		Prompt: `Based on these friction data and session details, identify the top 3 categories of friction the user experiences.
For each, give 1-2 concrete examples from their sessions and suggest what could help.

Data:
%s

Respond as a markdown list with **bold friction type** names, examples, and suggestions.`,
		MaxTokens: 1024,
	},
	{
		Name:  "suggestions",
		Title: "Tips & Suggestions",
		Prompt: `Based on this user's usage patterns, tool usage, and friction points, provide actionable suggestions in 3 categories:

1. **Quick Wins** — 2-3 specific things they could try right now
2. **Workflow Improvements** — 2-3 ways to improve their interaction patterns
3. **Advanced Techniques** — 1-2 more ambitious workflows they could explore

Data:
%s

Be specific and practical. Don't be generic. Write in second person.
Respond in markdown with the three categories as headers.`,
		MaxTokens: 1536,
	},
}

const atAGlancePrompt = `You're writing an "At a Glance" summary for a coding assistant usage insights report.

Use this 4-part structure:

1. **What's working** — What is the user's unique style and what impactful things have they done? Keep it high level, 2-3 sentences. Don't be fluffy.

2. **What's hindering you** — Split into (a) assistant's fault (misunderstandings, wrong approaches) and (b) user-side friction (not enough context, environment issues). Be honest but constructive. 2-3 sentences.

3. **Quick wins to try** — Specific features or workflow techniques they should try. Avoid generic advice. 2-3 sentences.

4. **Ambitious workflows** — As models improve, what workflows that seem hard now will become possible? 2-3 sentences.

Respond with a valid JSON object and no markdown fences:
{
  "whats_working": "...",
  "whats_hindering": "...",
  "quick_wins": "...",
  "ambitious_workflows": "..."
}

Context from analysis:
%s`
