package exec

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/blueberrycongee/wuu/internal/appserver"
)

// actionKind classifies how a scripted step reaches the app server.
type actionKind int

const (
	// actionRPC steps map onto an existing client RPC and are driven with a
	// single Controller.Call. These are the human/orchestrator-side methods the
	// desktop GUI already uses (group create, add member, open reply, post into a
	// reply, escalate to task, participant save/list/retire).
	actionRPC actionKind = iota
	// actionNamedTurn steps run a turn AS a named participant (participant/start)
	// so the resident tool surface is mounted — the only path that lets a named
	// agent speak (post_message) or manage the roster (manage_participant fork).
	// The action name is only a label; which of those tools the turn actually
	// invokes is decided by the named agent's provider, not by exec. Reuses the
	// W1 participant machinery.
	actionNamedTurn
)

// actionMethodEntry binds a scripted action name to the app-server method it
// drives. inject seeds fixed params the method requires (e.g. group:true for a
// group thread) so the payload never has to repeat structural constants.
type actionMethodEntry struct {
	kind   actionKind
	method string
	inject map[string]any
}

// actionMethodTable is the fixed allowlist of scripted actions. Every entry maps
// to an ALREADY-implemented app-server method — exec adds no new group-chat
// logic, it only drives the existing handlers. Keeping this a closed table (not
// an open method passthrough) keeps the exec surface scoped and cache-stable.
var actionMethodTable = map[string]actionMethodEntry{
	"create_group":        {kind: actionRPC, method: appserver.MethodThreadStart, inject: map[string]any{"group": true}},
	"add_group_member":    {kind: actionRPC, method: appserver.MethodThreadMembersAdd},
	"remove_group_member": {kind: actionRPC, method: appserver.MethodThreadMembersRemove},
	"save_participant":    {kind: actionRPC, method: appserver.MethodParticipantSave},
	"list_participants":   {kind: actionRPC, method: appserver.MethodParticipantList},
	"retire_participant":  {kind: actionRPC, method: appserver.MethodParticipantRetire},
	"open_reply":          {kind: actionRPC, method: appserver.MethodThreadOpenSub},
	"list_replies":        {kind: actionRPC, method: appserver.MethodThreadListSub},
	"resolve_reply":       {kind: actionRPC, method: appserver.MethodThreadResolveSub},
	"escalate_task":       {kind: actionRPC, method: appserver.MethodThreadEscalateSub},
	"post_subthread":      {kind: actionRPC, method: appserver.MethodMessagePostSubthread},
	// Named-turn actions: run a turn AS a named participant. post_message is
	// the speak-in-a-group case; participant_turn is the general name for any
	// other turn AS that participant, including forking a copy via
	// manage_participant. Both route to the same participant/start path —
	// the distinction is purely for readable scripts, since the invoked tool
	// is chosen by the agent, not the action.
	"post_message":     {kind: actionNamedTurn},
	"participant_turn": {kind: actionNamedTurn},
}

// runActionSequence drives a scripted list of group/reply/task steps against the
// app server. RPC steps are issued directly (Controller.Call); the post_message
// step runs a named-participant turn (participant/start) so the agent can speak.
// Variables bound by earlier steps' save_as are substituted into later steps'
// params, letting a script thread generated ids (group -> reply -> task) without
// knowing them ahead of time. Each step emits an action_started/action_completed
// JSONL pair; an assertion or RPC failure aborts the sequence.
func runActionSequence(ctx context.Context, controller Controller, opts Options, state *runState) error {
	vars := make(map[string]string)
	for i, action := range opts.Actions {
		name := strings.TrimSpace(action.Action)
		entry, ok := actionMethodTable[name]
		if !ok {
			err := fmt.Errorf("action %d: unknown action %q", i, action.Action)
			emitActionFailed(opts, i, name, err.Error())
			return WithExitCode(ExitInvalidInput, err)
		}
		emitActionStarted(opts, i, name)

		var (
			result map[string]any
			err    error
		)
		switch entry.kind {
		case actionNamedTurn:
			result, err = runNamedTurnAction(ctx, controller, opts, action, vars)
		default:
			result, err = runRPCAction(ctx, controller, entry, action, vars)
		}
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return classifyProtocolOrContextError(ctx, err)
			}
			emitActionFailed(opts, i, name, err.Error())
			return WithExitCode(ExitTurnFailed, fmt.Errorf("action %d (%s): %w", i, name, err))
		}
		if err := applyActionSaveAs(action, result, vars); err != nil {
			emitActionFailed(opts, i, name, err.Error())
			return WithExitCode(ExitTurnFailed, fmt.Errorf("action %d (%s): %w", i, name, err))
		}
		if err := checkActionExpectations(action, result); err != nil {
			emitActionFailed(opts, i, name, err.Error())
			return WithExitCode(ExitTurnFailed, fmt.Errorf("action %d (%s): %w", i, name, err))
		}
		emitActionCompleted(opts, i, name, result)
	}

	state.status = "completed"
	emitResult(opts, *state, "completed", "")
	return nil
}

// runRPCAction issues one app-server RPC for an actionRPC step and returns the
// decoded result object for save_as/expect evaluation.
func runRPCAction(ctx context.Context, controller Controller, entry actionMethodEntry, action GroupAction, vars map[string]string) (map[string]any, error) {
	params := substituteVars(action.Params, vars)
	if len(entry.inject) > 0 {
		if params == nil {
			params = make(map[string]any, len(entry.inject))
		}
		for key, value := range entry.inject {
			params[key] = value
		}
	}
	var result map[string]any
	if err := controller.Call(ctx, entry.method, params, &result); err != nil {
		return nil, err
	}
	if result == nil {
		result = map[string]any{}
	}
	return result, nil
}

// runNamedTurnAction runs a post_message step as a named-participant turn. The
// named agent's group-chat tool surface (post_message) is mounted by
// participant/start; the agent decides to speak. The step result exposes the
// spawned agent id and the last posted/returned message so save_as/expect can
// key on them.
func runNamedTurnAction(ctx context.Context, controller Controller, opts Options, action GroupAction, vars map[string]string) (map[string]any, error) {
	params := substituteVars(action.Params, vars)
	threadID := stringParam(params, "thread_id")
	if threadID == "" {
		return nil, errors.New("named turn requires params.thread_id (the group/DM thread the named agent runs in)")
	}
	participantID := strings.TrimSpace(action.As)
	if participantID == "" {
		participantID = stringParam(params, "participant_id")
	}
	prompt := strings.TrimSpace(action.Prompt)
	if prompt == "" {
		prompt = stringParam(params, "prompt")
	}
	// Resolve $var references embedded anywhere in the prompt (not only a whole
	// "$name" value) so a script can hand the named agent a runtime id it must
	// act on — for example "post into $cth" or "target $group". Params are
	// already whole-value substituted above; this covers ids woven into prose.
	prompt = substituteEmbedded(prompt, vars)
	if prompt == "" {
		return nil, errors.New("named turn requires a prompt (what the named agent should do)")
	}
	baselineItems := map[string]struct{}{}
	if thread, err := controller.ResumeThread(ctx, threadID); err == nil {
		baselineItems = threadItemIDs(thread)
	}

	start := appserver.ParticipantStartParams{
		ThreadID:      threadID,
		ParticipantID: participantID,
		Prompt:        prompt,
		TaskName:      stringParam(params, "task_name"),
		Description:   stringParam(params, "description"),
		SubagentType:  stringParam(params, "subagent_type"),
		AgentProfile:  stringParam(params, "agent_profile"),
		Isolation:     stringParam(params, "isolation"),
	}
	agent, err := startParticipantTurnWaitingForQuiesce(ctx, controller, start)
	if err != nil {
		return nil, err
	}
	agentID := strings.TrimSpace(agent.ID)
	if agentID == "" {
		return nil, errors.New("participant/start returned no agent id")
	}
	emitParticipantTurnStarted(opts, threadID, participantID, agent)

	// Isolate the wait in a per-step sub-state so a named turn's final message
	// does not clobber the sequence aggregate; the thread id anchors isCurrentTurn
	// so the named agent's own child turns are ignored (only its agent/updated
	// terminal status ends the wait).
	sub := runState{
		threadID:      threadID,
		turnID:        agentID,
		participantID: participantID,
		status:        "running",
	}
	if err := waitForParticipantRun(ctx, controller, opts, &sub, agentID); err != nil {
		return nil, err
	}
	result := map[string]any{
		"agent_id":       agentID,
		"participant_id": participantID,
		"thread_id":      threadID,
		"status":         sub.status,
		"text":           sub.finalMessage,
	}
	if item, ok := latestParticipantMessageForAction(ctx, controller, threadID, participantID, baselineItems, sub); ok {
		result["anchor_item_id"] = item.ID
		result["message_seq"] = item.Seq
	}
	if sub.status == "failed" {
		msg := strings.TrimSpace(sub.finalMessage)
		if msg == "" {
			msg = "named turn failed"
		}
		return result, errors.New(msg)
	}
	return result, nil
}

func latestParticipantMessageForAction(ctx context.Context, controller Controller, threadID, participantID string, existing map[string]struct{}, state runState) (appserver.ThreadItem, bool) {
	if id := strings.TrimSpace(state.lastParticipantItem); id != "" {
		return appserver.ThreadItem{ID: id, Seq: state.lastParticipantSeq}, true
	}

	const settleWindow = 2 * time.Second
	const pollInterval = 50 * time.Millisecond
	deadline := time.Now().Add(settleWindow)
	for {
		if thread, err := controller.ResumeThread(ctx, threadID); err == nil {
			if item, ok := newestParticipantMessageOutside(thread, participantID, existing); ok {
				return item, true
			}
		}
		if time.Now().After(deadline) {
			return appserver.ThreadItem{}, false
		}
		select {
		case <-ctx.Done():
			return appserver.ThreadItem{}, false
		case <-time.After(pollInterval):
		}
	}
}

func threadItemIDs(thread appserver.Thread) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, turn := range thread.Turns {
		for _, item := range turn.Items {
			if strings.TrimSpace(item.ID) != "" {
				ids[item.ID] = struct{}{}
			}
		}
	}
	return ids
}

func newestParticipantMessageOutside(thread appserver.Thread, participantID string, existing map[string]struct{}) (appserver.ThreadItem, bool) {
	var newest appserver.ThreadItem
	for _, turn := range thread.Turns {
		for _, item := range turn.Items {
			if item.Type != appserver.ThreadItemParticipantMsg {
				continue
			}
			if _, seen := existing[item.ID]; seen {
				continue
			}
			ownedByParticipant := strings.TrimSpace(item.AgentID) == participantID
			if item.Participant != nil {
				ownedByParticipant = ownedByParticipant || strings.TrimSpace(item.Participant.ID) == participantID
			}
			if !ownedByParticipant {
				continue
			}
			newest = item
		}
	}
	return newest, newest.ID != ""
}

// startParticipantTurnWaitingForQuiesce starts a named-participant turn, briefly
// retrying while the identity is still draining a just-finished run. A named
// agent runs one task at a time: when a script runs two turns as the SAME agent
// back to back, exec observes the first run reach a terminal agent status while
// the server's busy lock is still being released on its drain goroutine, so an
// immediate second start is transiently rejected with "is busy replying". The
// state is self-clearing (well under a second in practice), and the server's own
// error tells the caller to wait — so a bounded poll makes sequential same-agent
// turns reliable in headless scripts instead of racing the release. A genuinely
// long-running agent still surfaces the busy error once the window elapses.
func startParticipantTurnWaitingForQuiesce(ctx context.Context, controller Controller, start appserver.ParticipantStartParams) (appserver.Agent, error) {
	const quiesceWait = 20 * time.Second
	const pollInterval = 50 * time.Millisecond
	deadline := time.Now().Add(quiesceWait)
	for {
		agent, err := controller.StartParticipantTurn(ctx, start)
		if err == nil || !isResidentBusyError(err) || time.Now().After(deadline) {
			return agent, err
		}
		select {
		case <-ctx.Done():
			return appserver.Agent{}, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// isResidentBusyError reports whether err is the transient app-server rejection
// raised when a named identity is still executing or draining a prior run.
func isResidentBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "is busy replying") || strings.Contains(msg, "is busy running another task")
}

// substituteVars deep-copies params, replacing string values of the exact form
// "$name" with the bound variable value. Arrays and nested objects are walked so
// participant subsets (participants: ["$ada"]) and nested params resolve too.
func substituteVars(params map[string]any, vars map[string]string) map[string]any {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]any, len(params))
	for key, value := range params {
		out[key] = substituteValue(value, vars)
	}
	return out
}

func substituteValue(value any, vars map[string]string) any {
	switch typed := value.(type) {
	case string:
		return substituteString(typed, vars)
	case []any:
		out := make([]any, len(typed))
		for i, elem := range typed {
			out[i] = substituteValue(elem, vars)
		}
		return out
	case map[string]any:
		return substituteVars(typed, vars)
	default:
		return value
	}
}

// substituteEmbedded replaces every "$name" token embedded in a free-text
// string with its bound variable value, leaving unknown tokens verbatim. Unlike
// substituteString (which only resolves a value that is exactly "$name"), this
// walks the whole string so a named-turn prompt can weave a runtime id into
// prose. A "$" not followed by an identifier character is emitted unchanged.
func substituteEmbedded(value string, vars map[string]string) string {
	if !strings.Contains(value, "$") || len(vars) == 0 {
		return value
	}
	var b strings.Builder
	for i := 0; i < len(value); {
		if value[i] != '$' {
			b.WriteByte(value[i])
			i++
			continue
		}
		j := i + 1
		for j < len(value) && isVarNameChar(value[j]) {
			j++
		}
		name := value[i+1 : j]
		if bound, ok := vars[name]; ok && name != "" {
			b.WriteString(bound)
		} else {
			b.WriteString(value[i:j])
		}
		i = j
	}
	return b.String()
}

func isVarNameChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// substituteString resolves a "$name" reference to its bound value. A "$name"
// that names no bound variable is left as-is so a script author sees a clear
// downstream failure rather than a silently emptied field.
func substituteString(value string, vars map[string]string) string {
	if !strings.HasPrefix(value, "$") || len(value) < 2 {
		return value
	}
	name := value[1:]
	if bound, ok := vars[name]; ok {
		return bound
	}
	return value
}

// applyActionSaveAs binds result fields into the variable table for later steps.
func applyActionSaveAs(action GroupAction, result map[string]any, vars map[string]string) error {
	for name, path := range action.SaveAs {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		found, ok := lookupPath(result, path)
		if !ok {
			return fmt.Errorf("save_as %q: result has no path %q", name, path)
		}
		vars[name] = scalarString(found)
	}
	return nil
}

// checkActionExpectations asserts each expected result field. A missing path or a
// value mismatch fails the step (and the whole sequence).
func checkActionExpectations(action GroupAction, result map[string]any) error {
	for path, want := range action.Expect {
		got, ok := lookupPath(result, path)
		if !ok {
			return fmt.Errorf("expect %q: result has no such path", path)
		}
		if !valuesEqual(got, want) {
			return fmt.Errorf("expect %q = %v, got %v", path, want, got)
		}
	}
	return nil
}

// lookupPath walks a dotted path (e.g. "subthread.lead_participant_id") into a
// decoded result object.
func lookupPath(result map[string]any, path string) (any, bool) {
	segments := strings.Split(strings.TrimSpace(path), ".")
	var current any = result
	for _, segment := range segments {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := obj[segment]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

// valuesEqual compares an extracted result value against an expected value from
// the input JSON. Both come from json.Unmarshal, so numbers are float64 on both
// sides; a JSON round-trip normalizes the shapes before a structural compare.
func valuesEqual(got, want any) bool {
	if reflect.DeepEqual(got, want) {
		return true
	}
	// Numbers authored as ints in the payload vs float64 from decoding, or scalar
	// string forms, still compare equal after normalization.
	return scalarString(got) == scalarString(want)
}

func scalarString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return fmt.Sprintf("%v", typed)
	case bool:
		return fmt.Sprintf("%t", typed)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func stringParam(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	value, _ := params[key].(string)
	return strings.TrimSpace(value)
}

func emitActionStarted(opts Options, index int, action string) {
	if opts.JSON {
		emitJSON(opts, map[string]any{"type": "action_started", "index": index, "action": action})
		return
	}
	fmt.Fprintf(opts.Stderr, "action_started: [%d] %s\n", index, action)
}

func emitActionCompleted(opts Options, index int, action string, result map[string]any) {
	if opts.JSON {
		emitJSON(opts, map[string]any{"type": "action_completed", "index": index, "action": action, "result": result})
		return
	}
	fmt.Fprintf(opts.Stderr, "action_completed: [%d] %s\n", index, action)
}

func emitActionFailed(opts Options, index int, action, errText string) {
	if opts.JSON {
		emitJSON(opts, map[string]any{"type": "action_failed", "index": index, "action": action, "error": errText})
		return
	}
	fmt.Fprintf(opts.Stderr, "action_failed: [%d] %s: %s\n", index, action, errText)
}
