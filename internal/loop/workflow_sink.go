package loop

import (
	"strings"

	"github.com/blueberrycongee/wuu/internal/workflow"
)

type WorkflowArtifactSink struct {
	Store *Store
}

func NewWorkflowArtifactSink(store *Store) WorkflowArtifactSink {
	return WorkflowArtifactSink{Store: store}
}

func (s WorkflowArtifactSink) RecordWorkflowArtifact(in workflow.WorkflowArtifact) error {
	store, ok, err := resolveExternalLoopStore(s.Store, in.LoopDir, "workflow")
	if err != nil || !ok {
		return err
	}
	kind := strings.TrimSpace(string(in.Kind))
	if kind == "" {
		kind = "artifact"
	}
	_, err = store.RecordExternalArtifact(ExternalArtifact{
		Source:    "workflow",
		SourceID:  workflowArtifactSourceID(in),
		Kind:      kind,
		Path:      in.Path,
		CreatedAt: in.CreatedAt,
	})
	return err
}

func workflowArtifactSourceID(in workflow.WorkflowArtifact) string {
	if runID := strings.TrimSpace(in.RunID); runID != "" {
		return runID
	}
	if loopID := strings.TrimSpace(in.LoopID); loopID != "" {
		return loopID
	}
	return strings.TrimSpace(in.Path)
}
