package appserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/agent"
	"github.com/blueberrycongee/wuu/internal/memdir"
	"github.com/blueberrycongee/wuu/internal/provideroptions"
	"github.com/blueberrycongee/wuu/internal/providers"
	"github.com/blueberrycongee/wuu/internal/tools"
)

// The two memory-panel agents (memory-redesign contract §8.3). Both reuse
// the base agent runtime (RunToolLoop + the ordinary file tools) with a
// tightened prompt and a FileScopeRoots whitelist pinned to the memory
// notebooks — the file boundary is enforced by the tool Env, not by prompt
// discipline.

const (
	memoryOverviewTimeout  = 90 * time.Second
	memoryOverviewMaxSteps = 4
)

// memoryOverviewCacheEntry memoizes one generated overview essay. It stays
// valid while the notebook's MEMORY.md mtime is unchanged (§8.1: 短文按
// （笔记本, 索引 mtime）缓存，未变更时二次打开秒出).
type memoryOverviewCacheEntry struct {
	sourceMtime time.Time
	result      MemoryOverviewResult
}

func memoryOverviewCacheKey(scope, participantID string) string {
	return scope + "\x00" + participantID
}

// handleMemoryOverview serves the panel's essay view: one restricted
// read-only agent pass over the real notebook, memoized per (scope,
// participant, index mtime).
func (s *Server) handleMemoryOverview(req Request) error {
	var params MemoryOverviewParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	dir, err := s.memoryNotebook(params.Scope, params.ParticipantID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	result, err := s.memoryOverview(params.Scope, strings.TrimSpace(params.ParticipantID), dir)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, result, nil)
}

func (s *Server) memoryOverview(scope, participantID, dir string) (MemoryOverviewResult, error) {
	sourceMtime := memoryIndexMtime(dir)
	key := memoryOverviewCacheKey(scope, participantID)

	s.memoryOverviewMu.Lock()
	if entry, ok := s.memoryOverviewCache[key]; ok && entry.sourceMtime.Equal(sourceMtime) {
		s.memoryOverviewMu.Unlock()
		cached := entry.result
		cached.Cached = true
		return cached, nil
	}
	s.memoryOverviewMu.Unlock()

	if err := memdir.EnsureDir(dir); err != nil {
		return MemoryOverviewResult{}, err
	}
	essay, err := s.runMemoryOverviewAgent(scope, dir)
	if err != nil {
		return MemoryOverviewResult{}, err
	}
	result := MemoryOverviewResult{
		EssayMD:     essay,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		SourceMtime: formatMemoryMtime(sourceMtime),
		Cached:      false,
	}
	s.memoryOverviewMu.Lock()
	if s.memoryOverviewCache == nil {
		s.memoryOverviewCache = make(map[string]memoryOverviewCacheEntry)
	}
	s.memoryOverviewCache[key] = memoryOverviewCacheEntry{sourceMtime: sourceMtime, result: result}
	s.memoryOverviewMu.Unlock()
	return result, nil
}

// invalidateMemoryOverview drops the cached essay for one notebook, so the
// next panel open regenerates even when the index mtime did not move (e.g.
// a chat run that only touched topic files).
func (s *Server) invalidateMemoryOverview(scope, participantID string) {
	s.memoryOverviewMu.Lock()
	delete(s.memoryOverviewCache, memoryOverviewCacheKey(scope, participantID))
	s.memoryOverviewMu.Unlock()
}

// memoryIndexMtime returns the notebook index's modification time; the zero
// time when MEMORY.md does not exist yet.
func memoryIndexMtime(dir string) time.Time {
	info, err := os.Stat(filepath.Join(dir, memdir.EntrypointName))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func formatMemoryMtime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// runMemoryOverviewAgent performs the single restricted agent run that
// turns the notebook into the structured essay: read-only tools, file scope
// pinned to the notebook directory, small step budget, 90s deadline.
func (s *Server) runMemoryOverviewAgent(scope, dir string) (string, error) {
	client, model, cfg, err := s.memoryPanelModel()
	if err != nil {
		return "", err
	}
	index, err := memdir.ReadIndex(dir)
	if err != nil {
		return "", err
	}
	messages := []providers.ChatMessage{
		{Role: "system", Content: memoryOverviewSystemPrompt(scope, dir)},
		{Role: "user", Content: memoryOverviewUserPrompt(index.Content)},
	}
	executor := newMemoryPanelExecutor(dir, []string{dir}, false)

	ctx, cancel := context.WithTimeout(context.Background(), memoryOverviewTimeout)
	defer cancel()
	cfg.Tools = executor
	cfg.Model = model
	cfg.MaxSteps = memoryOverviewMaxSteps
	result, err := agent.RunToolLoop(ctx, messages, cfg, memoryPanelStep{client: client})
	if err != nil {
		return "", fmt.Errorf("memory overview agent: %w", err)
	}
	essay := strings.TrimSpace(result.Content)
	if essay == "" {
		return "", errors.New("memory overview agent returned an empty essay")
	}
	return essay, nil
}

// memoryPanelModel resolves the client/model both panel agents run on: the
// session's live stream runner (per M2 scope: client/model 取 s.rt.StreamRunner).
func (s *Server) memoryPanelModel() (providers.Client, string, agent.LoopConfig, error) {
	runner := s.rt.StreamRunner
	if runner == nil || runner.Client == nil {
		return nil, "", agent.LoopConfig{}, errors.New("model runtime is not available")
	}
	model := strings.TrimSpace(runner.APIModel)
	if model == "" {
		model = strings.TrimSpace(runner.Model)
	}
	if model == "" {
		return nil, "", agent.LoopConfig{}, errors.New("no model is configured")
	}
	cfg := agent.LoopConfig{
		Temperature:     runner.Temperature,
		Effort:          runner.Effort,
		ProviderOptions: provideroptions.Clone(runner.ProviderOptions),
	}
	return runner.Client, model, cfg, nil
}

const memoryOverviewUserEssaySections = "## 身份背景\n## 协作偏好\n## 沟通风格\n## 当前关注"

const memoryOverviewParticipantEssaySections = "## 与用户的相处之道\n## 协作教训\n## 技艺笔记\n## 承诺与定案"

func memoryOverviewSystemPrompt(scope, dir string) string {
	notebook := fmt.Sprintf("这本笔记本是主 agent 关于用户的长期记忆，位于 `%s`。", dir)
	sections := memoryOverviewUserEssaySections
	voice := "全文使用中文，语气自然、面向用户本人。"
	if scope == MemoryScopeParticipant {
		notebook = fmt.Sprintf("这本笔记本是一位常驻同事 agent 的身份记忆，位于 `%s`。", dir)
		sections = memoryOverviewParticipantEssaySections
		voice = "全文使用中文，语气自然，向用户介绍这位同事积累了怎样的经验与共识。"
	}
	return strings.Join([]string{
		"你是设置页记忆面板的概览 agent。你的唯一任务：阅读一本记忆笔记本，为用户生成一篇结构化的中文小短文，展示这本笔记本目前记住了什么。",
		notebook + " `" + memdir.EntrypointName + "` 是索引（一行一条记忆），各条记忆的正文在独立的主题文件里。",
		"",
		"工作方式：",
		"- 你只有 read_file、list_files、glob 三个只读工具，且只能访问这本笔记本目录。绝不要尝试修改任何文件。",
		"- 索引内容已附在用户消息里；只在需要补充细节时打开少量主题文件。步数有限，不要逐个读完所有文件。",
		"",
		"输出要求（严格遵守）：",
		"- 直接输出 Markdown 短文本身，不要任何前言、解释或代码围栏。",
		"- 固定使用以下小节标题，每节一到三句话；某节没有内容就写\"暂无记录\"：",
		sections,
		"- " + voice,
		"- 如果笔记本是空的（没有索引或没有任何条目），不要使用上述小节，改为输出一段友好的中文空态短文：说明这本笔记本还没有积累记忆，多与 agent 协作、或在下方输入框直接告诉它需要记住什么，它就会开始记录。",
	}, "\n")
}

func memoryOverviewUserPrompt(indexContent string) string {
	index := strings.TrimSpace(indexContent)
	if index == "" {
		index = "（索引为空——这本笔记本还没有任何记忆条目。）"
	}
	return "以下是这本笔记本的 " + memdir.EntrypointName + " 索引内容：\n\n" + index + "\n\n请按模板生成概览短文。"
}

// memoryPanelStep is the one-round-trip Step for the panel agents: a plain
// blocking Chat call (same shape as runtime's profileMemoryReviewStep).
type memoryPanelStep struct {
	client providers.Client
}

func (s memoryPanelStep) Execute(ctx context.Context, req providers.ChatRequest) (agent.StepResult, error) {
	resp, err := s.client.Chat(ctx, req)
	if err != nil {
		return agent.StepResult{}, err
	}
	return agent.StepResult{
		Content:          resp.Content,
		ReasoningContent: resp.ReasoningContent,
		ReasoningBlocks:  append([]providers.ReasoningBlock(nil), resp.ReasoningBlocks...),
		ToolCalls:        append([]providers.ToolCall(nil), resp.ToolCalls...),
		FinishReason:     resp.FinishReason,
		Truncated:        resp.Truncated,
		StopReason:       resp.StopReason,
		Usage:            resp.Usage,
	}, nil
}

// memoryPanelExecutor is the restricted tool executor for both panel
// agents. rootDir anchors relative paths inside the primary notebook;
// scopeRoots is the hard FileScopeRoots whitelist (reads are rejected
// outside it the same as writes). writable adds write_file/edit_file for
// the manager agent; the overview agent stays read-only.
type memoryPanelExecutor struct {
	registry *tools.Registry
	defs     []providers.ToolDefinition
}

func newMemoryPanelExecutor(rootDir string, scopeRoots []string, writable bool) *memoryPanelExecutor {
	env := &tools.Env{
		RootDir:        rootDir,
		FileScopeRoots: append([]string(nil), scopeRoots...),
	}
	toolset := []tools.Tool{
		tools.NewReadFileTool(env),
		tools.NewListFilesTool(env),
		tools.NewGlobTool(env),
	}
	if writable {
		toolset = append(toolset,
			tools.NewWriteFileTool(env),
			tools.NewEditFileTool(env),
		)
	}
	registry := tools.NewRegistry(toolset...)
	return &memoryPanelExecutor{registry: registry, defs: registry.Definitions()}
}

func (e *memoryPanelExecutor) Definitions() []providers.ToolDefinition {
	if e == nil {
		return nil
	}
	return e.defs
}

func (e *memoryPanelExecutor) Execute(ctx context.Context, call providers.ToolCall) (string, error) {
	if e == nil || e.registry == nil {
		return "", fmt.Errorf("memory panel: tool %q is not available", call.Name)
	}
	tool := e.registry.Lookup(call.Name)
	if tool == nil {
		return "", fmt.Errorf("memory panel: tool %q is not available", call.Name)
	}
	return tool.Execute(ctx, call.Arguments)
}

func (e *memoryPanelExecutor) ToolMetadata(call providers.ToolCall) (agent.ToolMetadata, bool) {
	if e == nil || e.registry == nil {
		return agent.ToolMetadata{}, false
	}
	tool := e.registry.Lookup(call.Name)
	if tool == nil {
		return agent.ToolMetadata{}, false
	}
	info := tools.ToolClassification{
		ReadOnly:        tool.IsReadOnly(),
		ConcurrencySafe: tool.IsConcurrencySafe(),
	}
	if classifier, ok := tool.(tools.InputClassifyingTool); ok {
		info = classifier.Classify(call.Arguments)
	}
	return agent.ToolMetadata{
		ReadOnly:        info.ReadOnly,
		ConcurrencySafe: info.ConcurrencySafe,
		Destructive:     info.Destructive,
		Risk:            string(info.Risk),
		Reason:          info.Reason,
	}, true
}
