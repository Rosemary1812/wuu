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
	AllowTools        []string
	DenyTools         []string
	ApprovalHandler   string
	ApprovalSocket    string
	// Approvals pre-grants specific blocked calls by approval key (or
	// request id / arguments hash) from a previous run's denial.
	Approvals []string
	// ApprovalsMode picks how the run treats approval questions:
	//   "auto" (default): resolve approval_policy to never - a
	//     delegated run flows; destructive actions stay denied and the
	//     tool hard protections are always on.
	//   "strict": keep on_request; requests nothing answers are denied
	//     with a grant recipe (--approve closes the loop on rerun).
	//   "prompt": keep on_request and ask the human on the controlling
	//     terminal.
	// It never blocks by default because exec's primary callers are
	// other agents whose shells may hold a pty: an unanswered prompt
	// would hang their run.
	ApprovalsMode     string
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
}

type Controller interface {
	Initialize(context.Context) (appserver.InitializeResult, error)
	StartThread(context.Context, bool) (appserver.Thread, error)
	ResumeThread(context.Context, string) (appserver.Thread, error)
	ForkThread(context.Context, string) (appserver.Thread, error)
	StartTurn(context.Context, string, TurnInput) (appserver.Turn, error)
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
