package workflow

import "sort"

type TeamArbitration struct {
	Status              string               `json:"status"`
	OpenAgentRuns       []string             `json:"open_agent_runs,omitempty"`
	MissingReports      []string             `json:"missing_reports,omitempty"`
	FailedAgentRuns     []string             `json:"failed_agent_runs,omitempty"`
	ChangedFileOverlaps []ChangedFileOverlap `json:"changed_file_overlaps,omitempty"`
	NextActions         []string             `json:"next_actions,omitempty"`
}

type ChangedFileOverlap struct {
	File        string   `json:"file"`
	AgentRunIDs []string `json:"agent_run_ids"`
}

func AnalyzeTeamArbitration(agents []AgentRun) TeamArbitration {
	out := TeamArbitration{Status: "clear"}
	changedByFile := make(map[string][]string)
	for _, agent := range agents {
		id := agent.ID
		if id == "" {
			id = agent.AgentID
		}
		if id == "" {
			id = agent.TaskName
		}
		if id == "" {
			continue
		}
		if !IsTerminalAgentRunState(agent.Status) {
			out.OpenAgentRuns = append(out.OpenAgentRuns, id)
		}
		if agent.ReportMissing || (agent.Status == AgentRunStateCompleted && agent.ReportPath == "") {
			out.MissingReports = append(out.MissingReports, id)
		}
		if agent.Status == AgentRunStateFailed {
			out.FailedAgentRuns = append(out.FailedAgentRuns, id)
		}
		for _, file := range agent.ChangedFiles {
			if file == "" {
				continue
			}
			changedByFile[file] = append(changedByFile[file], id)
		}
	}
	for file, ids := range changedByFile {
		if len(ids) > 1 {
			sort.Strings(ids)
			out.ChangedFileOverlaps = append(out.ChangedFileOverlaps, ChangedFileOverlap{
				File:        file,
				AgentRunIDs: ids,
			})
		}
	}
	sort.Strings(out.OpenAgentRuns)
	sort.Strings(out.MissingReports)
	sort.Strings(out.FailedAgentRuns)
	sort.Slice(out.ChangedFileOverlaps, func(i, j int) bool {
		return out.ChangedFileOverlaps[i].File < out.ChangedFileOverlaps[j].File
	})
	if len(out.OpenAgentRuns) > 0 ||
		len(out.MissingReports) > 0 ||
		len(out.FailedAgentRuns) > 0 ||
		len(out.ChangedFileOverlaps) > 0 {
		out.Status = "attention_required"
	}
	out.NextActions = teamArbitrationNextActions(out)
	return out
}

func teamArbitrationNextActions(arbitration TeamArbitration) []string {
	if arbitration.Status == "clear" {
		return []string{"No team arbitration issues detected."}
	}
	out := make([]string, 0, 4)
	if len(arbitration.OpenAgentRuns) > 0 {
		out = append(out, "Wait for, follow up with, or close open agent runs before synthesis.")
	}
	if len(arbitration.MissingReports) > 0 {
		out = append(out, "Require missing agent_report handoffs before treating completed worker text as durable evidence.")
	}
	if len(arbitration.FailedAgentRuns) > 0 {
		out = append(out, "Review failed agent runs and decide whether to retry, rollback, or ask the user.")
	}
	if len(arbitration.ChangedFileOverlaps) > 0 {
		out = append(out, "Inspect overlapping changed files and arbitrate the intended owner before applying or synthesizing changes.")
	}
	return out
}
