// Package insight aggregates session-level token usage and model breakdowns
// for the desktop settings page (appserver handleSettingsUsage). The public
// surface is ScanSessions, CollectTokenUsageRows, and their row/meta types.
//
// Two earlier pipelines were removed as dead code and live only in git
// history:
//   - UsageReport/BuildUsageReport/FormatUsageReport, a text-format summary
//     superseded by the SettingsUsageResponse RPC;
//   - the LLM-driven insights report (Run, facet extraction via ExtractFacet,
//     GenerateInsights, GenerateHTML, and the usage-data cache), which never
//     had a production trigger.
package insight
