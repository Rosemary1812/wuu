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
	FilePaths         []string
	Attachments       Attachments
	Workdir           string
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
	ResumeID          string
	ResumeLast        bool
	Stdout            io.Writer
	Stderr            io.Writer
	Controller        Controller
}

type Controller interface {
	Initialize(context.Context) (appserver.InitializeResult, error)
	StartThread(context.Context) (appserver.Thread, error)
	ResumeThread(context.Context, string) (appserver.Thread, error)
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
