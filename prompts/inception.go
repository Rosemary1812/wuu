package prompts

// InceptionTimingBrief is the single-source, condensed statement of WHEN to
// reach for the inception context-rewrite tool. system_main.md carries the
// fuller triggers list for the main agent; this compact form is shared by the
// surfaces that never receive system_main.md — the inception tool description
// (all agents) and the resident named-agent persona prompt — so the timing
// guidance cannot drift between them. It is phrased to slot in after a colon or
// dash, with "it" referring to inception.
func InceptionTimingBrief() string {
	return "reach for it proactively as each exploration branch, large read, or debugging detour collapses into a small durable result; fold that noisy suffix into a short continuation summary instead of carrying the raw transcript forward, and do not wait until context is near full"
}
