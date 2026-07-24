package appserver

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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
	"github.com/blueberrycongee/wuu/internal/securefs"
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
	memoryOverviewTTL      = 12 * time.Hour
	memoryChatTimeout      = 120 * time.Second
	memoryChatMaxSteps     = 8
)

// memoryOverviewCacheEntry memoizes one generated overview essay. The result
// remains the automatic view for 12 hours even if the notebook changes; users
// can explicitly bypass the cooldown with force_refresh.
type memoryOverviewCacheEntry struct {
	generatedAt time.Time
	result      MemoryOverviewResult
}

func memoryOverviewCacheKey(scope, participantID string) string {
	return scope + "\x00" + participantID
}

// handleMemoryOverview serves the panel's essay view: one restricted
// read-only agent pass over the real notebook, memoized for 12 hours unless
// the user explicitly requests a fresh summary.
func (s *Server) handleMemoryOverview(req Request) error {
	var params MemoryOverviewParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	dir, err := s.memoryNotebook(params.Scope, params.ParticipantID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	result, err := s.memoryOverview(params.Scope, strings.TrimSpace(params.ParticipantID), dir, params.ForceRefresh)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	return s.writeResponse(req.ID, result, nil)
}

func (s *Server) memoryOverview(scope, participantID, dir string, forceRefresh bool) (MemoryOverviewResult, error) {
	key := memoryOverviewCacheKey(scope, participantID)
	if !forceRefresh {
		if cached, ok := s.freshMemoryOverview(key, scope, participantID, time.Now()); ok {
			return cached, nil
		}
	}

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
		SourceMtime: formatMemoryMtime(memoryIndexMtime(dir)),
		Cached:      false,
	}
	generatedAt, _ := time.Parse(time.RFC3339, result.GeneratedAt)
	s.memoryOverviewMu.Lock()
	if s.memoryOverviewCache == nil {
		s.memoryOverviewCache = make(map[string]memoryOverviewCacheEntry)
	}
	s.memoryOverviewCache[key] = memoryOverviewCacheEntry{generatedAt: generatedAt, result: result}
	s.memoryOverviewMu.Unlock()
	if err := s.writeMemoryOverviewCache(scope, participantID, result); err != nil {
		providers.DebugLogf("memory overview cache: %v", err)
	}
	return result, nil
}

func (s *Server) freshMemoryOverview(key, scope, participantID string, now time.Time) (MemoryOverviewResult, bool) {
	s.memoryOverviewMu.Lock()
	entry, ok := s.memoryOverviewCache[key]
	s.memoryOverviewMu.Unlock()
	if !ok {
		var err error
		entry, err = s.readMemoryOverviewCache(scope, participantID)
		if err != nil {
			providers.DebugLogf("memory overview cache: %v", err)
			return MemoryOverviewResult{}, false
		}
		if entry.generatedAt.IsZero() {
			return MemoryOverviewResult{}, false
		}
		s.memoryOverviewMu.Lock()
		if s.memoryOverviewCache == nil {
			s.memoryOverviewCache = make(map[string]memoryOverviewCacheEntry)
		}
		s.memoryOverviewCache[key] = entry
		s.memoryOverviewMu.Unlock()
	}
	if entry.generatedAt.IsZero() || !now.Before(entry.generatedAt.Add(memoryOverviewTTL)) {
		return MemoryOverviewResult{}, false
	}
	result := entry.result
	result.Cached = true
	return result, true
}

func (s *Server) memoryOverviewCachePath(scope, participantID string) string {
	sum := sha256.Sum256([]byte(memoryOverviewCacheKey(scope, participantID)))
	return filepath.Join(strings.TrimSpace(s.rt.WuuHome), "cache", "memory-overview", fmt.Sprintf("%x.json", sum[:]))
}

func (s *Server) readMemoryOverviewCache(scope, participantID string) (memoryOverviewCacheEntry, error) {
	path := s.memoryOverviewCachePath(scope, participantID)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return memoryOverviewCacheEntry{}, nil
		}
		return memoryOverviewCacheEntry{}, fmt.Errorf("read %s: %w", path, err)
	}
	var result MemoryOverviewResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return memoryOverviewCacheEntry{}, fmt.Errorf("decode %s: %w", path, err)
	}
	generatedAt, err := time.Parse(time.RFC3339, result.GeneratedAt)
	if err != nil || strings.TrimSpace(result.EssayMD) == "" {
		return memoryOverviewCacheEntry{}, nil
	}
	return memoryOverviewCacheEntry{generatedAt: generatedAt, result: result}, nil
}

func (s *Server) writeMemoryOverviewCache(scope, participantID string, result MemoryOverviewResult) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode cache: %w", err)
	}
	path := s.memoryOverviewCachePath(scope, participantID)
	if err := securefs.WriteFileAtomic(path, append(raw, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
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
	ctx = providers.WithInferenceJournal(ctx, s.rt.InferenceJournalForOwner("memory-overview"))
	cfg.Tools = executor
	cfg.Model = model
	cfg.MaxSteps = memoryOverviewMaxSteps
	cfg.InferenceOperationKind = providers.InferenceOperationMemory
	cfg.InferenceWorkloadProfile = providers.InferenceProfileBestEffort
	result, err := agent.RunToolLoop(ctx, messages, cfg, agent.NewStreamStep(client))
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

// handleMemoryChat serves the panel's management chat: the manager agent
// edits the REAL notebook files with the ordinary file tools inside a
// FileScopeRoots whitelist of the user notebook (plus, in participant
// scope, that agent's identity notebook). changed_files is computed by
// diffing (path → mtime,size) snapshots of the whitelisted notebooks taken
// before and after the run. The overview keeps its 12-hour cache after a
// write; the user can request an immediate fresh summary from the panel.
func (s *Server) handleMemoryChat(req Request) error {
	var params MemoryChatParams
	if err := decodeParams(req.Params, &params); err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	message := strings.TrimSpace(params.Message)
	if message == "" {
		return s.writeResponse(req.ID, nil, errors.New("message is required"))
	}
	dir, err := s.memoryNotebook(params.Scope, params.ParticipantID)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	userDir := memdir.UserMemdir(strings.TrimSpace(s.rt.WuuHome))
	roots := []string{userDir}
	if params.Scope == MemoryScopeParticipant {
		roots = append(roots, dir)
	}
	for _, root := range roots {
		if err := memdir.EnsureDir(root); err != nil {
			return s.writeResponse(req.ID, nil, err)
		}
	}

	before := snapshotMemoryRoots(roots)
	reply, err := s.runMemoryChatAgent(params.Scope, dir, userDir, roots, message)
	if err != nil {
		return s.writeResponse(req.ID, nil, err)
	}
	changed := diffMemorySnapshots(before, snapshotMemoryRoots(roots), s.rt.WuuHome)

	return s.writeResponse(req.ID, MemoryChatResult{ReplyMD: reply, ChangedFiles: changed}, nil)
}

// runMemoryChatAgent performs the manager agent run: base RunToolLoop with
// read+write file tools, relative paths anchored in the target notebook,
// and the whitelist limited to the notebook roots.
func (s *Server) runMemoryChatAgent(scope, targetDir, userDir string, roots []string, message string) (string, error) {
	client, model, cfg, err := s.memoryPanelModel()
	if err != nil {
		return "", err
	}
	messages := []providers.ChatMessage{
		{Role: "system", Content: memoryChatSystemPrompt(scope, targetDir, userDir)},
		{Role: "user", Content: message},
	}
	executor := newMemoryPanelExecutor(targetDir, roots, true)

	ctx, cancel := context.WithTimeout(context.Background(), memoryChatTimeout)
	defer cancel()
	ctx = providers.WithInferenceJournal(ctx, s.rt.InferenceJournalForOwner("memory-manager"))
	cfg.Tools = executor
	cfg.Model = model
	cfg.MaxSteps = memoryChatMaxSteps
	cfg.InferenceOperationKind = providers.InferenceOperationMemory
	cfg.InferenceWorkloadProfile = providers.InferenceProfileInteractive
	result, err := agent.RunToolLoop(ctx, messages, cfg, agent.NewStreamStep(client))
	if err != nil {
		return "", fmt.Errorf("memory manager agent: %w", err)
	}
	reply := strings.TrimSpace(result.Content)
	if reply == "" {
		reply = "已处理，但管理 agent 没有返回说明。"
	}
	return reply, nil
}

func memoryChatSystemPrompt(scope, targetDir, userDir string) string {
	target := fmt.Sprintf("当前管理的是用户记忆笔记本：`%s`。", userDir)
	if scope == MemoryScopeParticipant {
		target = fmt.Sprintf(
			"当前管理的是一位常驻同事 agent 的身份笔记本：`%s`。用户记忆笔记本 `%s` 也在你的可写范围内，但除非用户的指令明确涉及它，否则不要改动它。",
			targetDir, userDir)
	}
	return strings.Join([]string{
		"你是设置页记忆面板的管理 agent。用户通过面板对话框对一本记忆笔记本下达管理指令；你用文件工具（read_file、list_files、glob、write_file、edit_file）直接修改真实的笔记本文件，修改立即生效。相对路径以当前笔记本目录为根；你的文件访问被硬性限制在笔记本目录内。",
		target,
		"",
		"笔记本格式与守则：",
		memdir.ManagerTeaching(),
		"",
		"两步保存纪律（任何修改都必须保持索引与正文一致）：",
		"- 新增或修改一条记忆：先写/改主题文件，再同步更新 " + memdir.EntrypointName + " 中对应的索引行。",
		"- 删除一条记忆：清空对应主题文件的正文（工具不能删除文件，用 write_file 写入空内容即可），并删除 " + memdir.EntrypointName + " 中对应的索引行。",
		"- 绝不把记忆正文直接写进 " + memdir.EntrypointName + "；修改前先读取要改的文件。",
		"",
		"回复要求：完成后用中文一句话说明你做了什么（保存/修改/删除了哪条记忆）；如果没有需要修改的内容或指令不清楚，也用一句话说明原因。不要输出多余的解释或文件内容。",
	}, "\n")
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
