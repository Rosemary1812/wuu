package appserver

import "github.com/blueberrycongee/wuu/internal/agenttemplate"

func (s *Server) handleAgentTemplateList(req Request) error {
	return s.writeResponse(req.ID, AgentTemplateListResult{
		Templates:   agentTemplateSummaries(s.rt.AgentTemplates),
		Diagnostics: agentTemplateDiagnostics(s.rt.AgentTemplateDiagnostics),
	}, nil)
}

func agentTemplateSummaries(items []agenttemplate.Template) []AgentTemplateSummary {
	out := make([]AgentTemplateSummary, 0, len(items))
	for _, item := range items {
		metadata := make(map[string]string, len(item.Metadata))
		for key, value := range item.Metadata {
			metadata[key] = value
		}
		out = append(out, AgentTemplateSummary{
			Name:           item.Name,
			Description:    item.Description,
			Instructions:   item.Instructions,
			Source:         item.Source,
			Path:           item.Path,
			Model:          item.Model,
			PermissionMode: item.PermissionMode,
			Effort:         item.Effort,
			Metadata:       metadata,
		})
	}
	return out
}

func agentTemplateDiagnostics(items []agenttemplate.Diagnostic) []AgentTemplateDiagnostic {
	out := make([]AgentTemplateDiagnostic, 0, len(items))
	for _, item := range items {
		out = append(out, AgentTemplateDiagnostic{Path: item.Path, Message: item.Message})
	}
	return out
}
