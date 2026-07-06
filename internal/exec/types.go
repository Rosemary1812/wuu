package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/blueberrycongee/wuu/internal/appserver"
)

const (
	ExitOK                 = 0
	ExitTurnFailed         = 1
	ExitInvalidInput       = 2
	ExitPermissionDenied   = 3
	ExitTimeout            = 4
	ExitInterrupted        = 5
	ExitProtocol           = 6
	ExitProviderModelError = 7
	ExitToolFailed         = 8
)

type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return fmt.Sprintf("exit code %d", e.Code)
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func WithExitCode(code int, err error) error {
	if err == nil {
		return nil
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return err
	}
	return &ExitError{Code: code, Err: err}
}

func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) && exitErr.Code != 0 {
		return exitErr.Code
	}
	return ExitTurnFailed
}

type Options struct {
	Prompt            string
	ImagePaths        []string
	ImageOriginal     bool
	FilePaths         []string
	Attachments       Attachments
	Workdir           string
	ConfigPath        string
	AgentProfile      string
	IgnoreUserConfig  bool
	StrictConfig      bool
	Env               []string
	MaxTurns          int
	Provider          string
	Model             string
	Effort            string
	Variant           string
	PermissionMode    string
	NoTools           bool
	JSON              bool
	Ephemeral         bool
	Timeout           time.Duration
	OutputLastMessage string
	OutputSchemaPath  string
	ResumeID          string
	ResumeLast        bool
	ForkID            string
	Stdout            io.Writer
	Stderr            io.Writer
	Controller        Controller
	// Participant, when requested, switches exec from a plain SurfaceMain
	// turn to running the turn as a named conversation participant inside a
	// group/DM thread. This is the only path that mounts the group-chat tool
	// surface (post_message / manage_participant / create_group / workflow)
	// for a headless exec run, letting CI/agents drive named group chat that
	// is otherwise only reachable from the desktop GUI.
	Participant ParticipantRun
	// Actions, when non-empty, switches exec into scripted group-chat mode: an
	// ordered list of group/reply/task steps driven against the app server's
	// existing RPCs (and, for named speaking turns, participant/start). This is
	// the headless JSON-driven entry that lets CI/agents build a group, open a
	// reply, escalate it to a task, etc., and assert each step — the multi-step
	// counterpart to a single ParticipantRun. Mutually exclusive with a plain
	// SurfaceMain turn; when set it takes precedence.
	Actions []GroupAction
}

// GroupAction is one step in a scripted group-chat sequence. Each step maps onto
// an existing app-server RPC (or, for the post_message step, a participant/start
// named turn) via a fixed action->method allowlist — exec never becomes an
// arbitrary RPC proxy. The step schema is static; all dynamic content lives in
// Params (the --input-json payload), keeping prompt-cache prefixes stable.
type GroupAction struct {
	// Action names the step, e.g. create_group, add_group_member, open_reply,
	// post_subthread, escalate_task, post_message. See actionMethodTable.
	Action string `json:"action"`
	// As names the acting named participant for a post_message step (id or name).
	As string `json:"as,omitempty"`
	// Prompt is the instruction for a post_message named turn (ignored by RPC
	// steps, which carry everything in Params).
	Prompt string `json:"prompt,omitempty"`
	// Params is the RPC parameter object. String values of the exact form "$name"
	// (and array elements) are substituted from variables bound by earlier steps'
	// SaveAs before the call.
	Params map[string]any `json:"params,omitempty"`
	// SaveAs binds result fields into the variable table: key = variable name,
	// value = dotted JSON path into the step result (e.g. "thread.id",
	// "subthread.id"). Later steps reference the variable as "$name".
	SaveAs map[string]string `json:"save_as,omitempty"`
	// Expect asserts result fields after the call: key = dotted JSON path into the
	// step result, value = expected scalar. A mismatch fails the whole sequence.
	Expect map[string]any `json:"expect,omitempty"`
}

// ParticipantRun describes a headless "run a turn as a named participant"
// request. It maps 1:1 onto the app server's participant/start RPC, which
// spawns a speech-capable named-agent run bound to ParticipantID inside
// ThreadID. When ThreadID is empty exec starts a fresh thread first; when
// ParticipantID is empty the run is an anonymous speech-capable turn.
type ParticipantRun struct {
	ParticipantID string
	ThreadID      string
	TaskName      string
	Description   string
	SubagentType  string
	AgentProfile  string
	Isolation     string
}

// Requested reports whether the caller asked for a named/group participant
// turn rather than an ordinary SurfaceMain turn.
func (p ParticipantRun) Requested() bool {
	return p.ParticipantID != "" || p.ThreadID != ""
}

type Controller interface {
	Initialize(context.Context) (appserver.InitializeResult, error)
	StartThread(context.Context, bool) (appserver.Thread, error)
	ResumeThread(context.Context, string) (appserver.Thread, error)
	ForkThread(context.Context, string) (appserver.Thread, error)
	StartTurn(context.Context, string, TurnInput) (appserver.Turn, error)
	// StartParticipantTurn drives the app server's participant/start RPC to
	// spawn a speech-capable named-agent run. The returned Agent carries the
	// spawned run id; completion is asynchronous and observed on the
	// Notifications() channel (agent/updated reaching a terminal status),
	// NOT via StartTurn's turn/completed contract.
	StartParticipantTurn(context.Context, appserver.ParticipantStartParams) (appserver.Agent, error)
	// Call issues an arbitrary app-server RPC over the same transport. The
	// scripted-action driver uses it to reach the already-implemented
	// group/reply/task methods (thread/start, thread/members/add, thread/openSub,
	// message/postSubthread, thread/escalateSub, participant/*) without adding a
	// typed wrapper per method. params/result follow the ProtocolClient contract.
	Call(ctx context.Context, method string, params, result any) error
	Interrupt(context.Context, string) error
	Shutdown(context.Context) error
	Notifications() <-chan Notification
}

type Attachments struct {
	Images []appserver.TurnStartImage
	Files  []appserver.TurnStartFile
}

func (a Attachments) Empty() bool {
	return len(a.Images) == 0 && len(a.Files) == 0
}

type TurnInput struct {
	Prompt string
	Images []appserver.TurnStartImage
	Files  []appserver.TurnStartFile
}
