// Package insight aggregates session-level token usage and model breakdowns
// for the desktop settings page and the LLM-driven session insights report.
//
// UsageReport, BuildUsageReport, and FormatUsageReport used to live here as
// a text-format summary of local sessions, but they had no callers once
// handleSettingsUsage took over the desktop usage snapshot. They were
// removed as dead code; the desktop now reads its usage data from the
// SettingsUsageResponse RPC instead of this text pipeline. See the git
// history of this file for the previous implementation.
package insight

