package appserver

// Named-agent birth rule: agents are born from recurring work, never from
// anticipation. New kinds of work default to the generalist (Andy); once the
// same kind of task has completed enough times under the default target, the
// board can suggest spinning off a dedicated named agent, and the spawn call
// hands the relevant history over as the new agent's initial memory.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/kanban"
	"github.com/blueberrycongee/wuu/internal/memdir"
	"github.com/blueberrycongee/wuu/internal/participant"
	"github.com/blueberrycongee/wuu/internal/session"
)

// kanbanSpawnMinCompletions is the recurrence threshold: a normalized task
// topic must reach this many completed executions, all under the default
// target, before a spin-off is suggested.
const kanbanSpawnMinCompletions = 3

type kanbanSpawnSuggestion struct {
	Topic           string   `json:"topic"`
	SuggestedName   string   `json:"suggested_name"`
	TaskCount       int      `json:"task_count"`
	SampleTaskIDs   []string `json:"sample_task_ids"`
	DefaultTargetID string   `json:"default_target_id"`
}

var kanbanTopicNormalizer = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// normalizeKanbanTopic folds a task title into a display form: case-folded,
// punctuation collapsed to single spaces.
func normalizeKanbanTopic(title string) string {
	return strings.TrimSpace(strings.ToLower(kanbanTopicNormalizer.ReplaceAllString(title, " ")))
}

// kanbanTopicKey is the grouping key: the normalized topic with every space
// stripped, so punctuation/space variants of the same work ("发布官网" vs
// "发布 官网!") group together.
func kanbanTopicKey(title string) string {
	return strings.ReplaceAll(normalizeKanbanTopic(title), " ", "")
}

// groupKanbanSpawnCandidates finds recurring topics whose completed leaf
// tasks were all executed by the default target. Pure for tests.
func groupKanbanSpawnCandidates(tasks []kanban.Task, runsByTask map[string][]kanban.Run, defaultTargetID string, minCount int) []kanbanSpawnSuggestion {
	childrenByParent := map[string]int{}
	for _, t := range tasks {
		if t.ParentID != "" {
			childrenByParent[t.ParentID]++
		}
	}
	completedByTopic := map[string][]kanban.Task{}
	for _, t := range tasks {
		if childrenByParent[t.ID] > 0 {
			continue
		}
		if t.Status != kanban.TaskStatusReview && t.Status != kanban.TaskStatusDone {
			continue
		}
		topic := kanbanTopicKey(t.Title)
		if topic == "" {
			continue
		}
		completedByTopic[topic] = append(completedByTopic[topic], t)
	}
	var out []kanbanSpawnSuggestion
	for _, group := range completedByTopic {
		if len(group) < minCount {
			continue
		}
		allDefault := true
		var sampleIDs []string
		for _, t := range group {
			runs := runsByTask[t.ID]
			if len(runs) == 0 {
				allDefault = false
				break
			}
			for _, r := range runs {
				if r.TargetID != defaultTargetID {
					allDefault = false
					break
				}
			}
			if !allDefault {
				break
			}
			if len(sampleIDs) < 5 {
				sampleIDs = append(sampleIDs, t.ID)
			}
		}
		if !allDefault {
			continue
		}
		out = append(out, kanbanSpawnSuggestion{
			Topic:           normalizeKanbanTopic(group[0].Title),
			SuggestedName:   group[0].Title,
			TaskCount:       len(group),
			SampleTaskIDs:   sampleIDs,
			DefaultTargetID: defaultTargetID,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TaskCount != out[j].TaskCount {
			return out[i].TaskCount > out[j].TaskCount
		}
		return out[i].Topic < out[j].Topic
	})
	return out
}

type KanbanSpawnSuggestionsParams struct {
	SessionID string `json:"session_id"`
}

func (s *Server) handleKanbanSpawnSuggestions(req Request) error {
	var params KanbanSpawnSuggestionsParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if strings.TrimSpace(params.SessionID) == "" {
		return s.writeResponse(req.ID, nil, errors.New("session_id is required"))
	}
	defaultTargetID, err := s.resolveDefaultDispatchTarget()
	if err != nil {
		return s.writeResponse(req.ID, []kanbanSpawnSuggestion{}, nil)
	}
	tasks, err := session.ListAllKanbanTasks(s.rt.SessionDir, params.SessionID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	runsByTask := map[string][]kanban.Run{}
	for _, t := range tasks {
		if t.Status != kanban.TaskStatusReview && t.Status != kanban.TaskStatusDone {
			continue
		}
		runs, err := session.ListKanbanRuns(s.rt.SessionDir, t.ID)
		if err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
		runsByTask[t.ID] = runs
	}
	suggestions := groupKanbanSpawnCandidates(tasks, runsByTask, defaultTargetID, kanbanSpawnMinCompletions)
	if suggestions == nil {
		suggestions = []kanbanSpawnSuggestion{}
	}
	return s.writeResponse(req.ID, suggestions, nil)
}

// ---- spin-off creation ----

type KanbanSpawnAgentParams struct {
	SessionID string   `json:"session_id"`
	Name      string   `json:"name"`
	Role      string   `json:"role,omitempty"`
	Tagline   string   `json:"tagline,omitempty"`
	Model     string   `json:"model,omitempty"`
	Topic     string   `json:"topic"`
	TaskIDs   []string `json:"task_ids,omitempty"`
}

// handleKanbanSpawnAgent creates the spun-off named agent and hands the
// referenced task history over as its initial memory notebook content: the
// agent is born with a job description and a work record, never blank.
func (s *Server) handleKanbanSpawnAgent(req Request) error {
	var params KanbanSpawnAgentParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return s.writeResponse(req.ID, nil, errors.New("name is required"))
	}
	if strings.EqualFold(name, defaultSeedParticipantName) {
		return s.writeResponse(req.ID, nil, fmt.Errorf("name %q is reserved for the default agent", name))
	}

	now := time.Now().UTC()
	p := participant.Participant{
		ID:        participant.NewID(),
		Kind:      participant.KindNamed,
		Name:      name,
		Role:      strings.TrimSpace(params.Role),
		Tagline:   strings.TrimSpace(params.Tagline),
		Model:     strings.TrimSpace(params.Model),
		CreatedAt: now,
		UpdatedAt: now,
	}
	workspace, err := s.participantWorkspace(p.ID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	p.Workspace = workspace
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("create participant workspace: %w", err))
	}
	if err := session.UpsertParticipant(s.rt.SessionDir, p); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	if err := s.seedKanbanSpawnMemory(p, params.Topic, params.TaskIDs); err != nil {
		return s.writeResponse(req.ID, nil, fmt.Errorf("seed spawn memory: %w", err))
	}
	summary := p.Summary()
	summary.AvatarImage = participantSummaryAvatarDataURL(p.Workspace)
	return s.writeResponse(req.ID, summary, nil)
}

// seedKanbanSpawnMemory writes the initial identity notebook: one handoff
// topic file with the distilled history of the referenced tasks, plus the
// MEMORY.md index line pointing at it.
func (s *Server) seedKanbanSpawnMemory(p participant.Participant, topic string, taskIDs []string) error {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		topic = strings.TrimSpace(p.Role)
	}
	if topic == "" {
		topic = p.Name
	}
	notebookDir := memdir.ParticipantMemdir(s.rt.WuuHome, p.ID)
	if notebookDir == "" {
		return errors.New("participant notebook dir is unavailable")
	}
	if err := memdir.EnsureDir(notebookDir); err != nil {
		return err
	}

	var body strings.Builder
	fmt.Fprintf(&body, "---\nname: handoff-%s\ndescription: work history handed over when %s was spun off\ntype: reference\n---\n\n", slugifyKanbanTopic(topic), p.Name)
	fmt.Fprintf(&body, "# 我从通用 agent 处接手的领域：%s\n\n", topic)
	body.WriteString("这些任务在我出生前由通用 agent 完成，现在归我负责。以下是工作记录，\n")
	body.WriteString("包括每个任务的简报和最近一次执行的总结：\n\n")
	for _, taskID := range taskIDs {
		task, err := session.GetKanbanTask(s.rt.SessionDir, strings.TrimSpace(taskID))
		if err != nil {
			continue
		}
		fmt.Fprintf(&body, "## %s\n\n", strings.TrimSpace(task.Title))
		if brief := strings.TrimSpace(task.Brief); brief != "" {
			fmt.Fprintf(&body, "简报：%s\n\n", brief)
		}
		if task.LatestRunID != "" {
			if run, err := session.GetKanbanRun(s.rt.SessionDir, task.LatestRunID); err == nil && strings.TrimSpace(run.Summary) != "" {
				fmt.Fprintf(&body, "最近一次执行总结：%s\n\n", strings.TrimSpace(run.Summary))
			}
		}
	}
	topicFile := slugifyKanbanTopic(topic) + ".md"
	if err := os.WriteFile(filepath.Join(notebookDir, topicFile), []byte(body.String()), 0o644); err != nil {
		return err
	}
	indexLine := fmt.Sprintf("- [接手的领域：%s](%s) — 出生即带的工作记录与历史简报\n", topic, topicFile)
	indexPath := filepath.Join(notebookDir, memdir.EntrypointName)
	existing, err := os.ReadFile(indexPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(existing)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += indexLine
	return os.WriteFile(indexPath, []byte(content), 0o644)
}

// slugifyKanbanTopic turns a topic into a filename-safe slug.
func slugifyKanbanTopic(topic string) string {
	slug := kanbanTopicNormalizer.ReplaceAllString(strings.ToLower(strings.TrimSpace(topic)), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "handoff"
	}
	if len([]rune(slug)) > 48 {
		slug = string([]rune(slug)[:48])
	}
	return slug
}
