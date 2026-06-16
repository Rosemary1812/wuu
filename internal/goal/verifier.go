package goal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultCommandTimeout = 5 * time.Minute
const maxVerificationOutputBytes = 24 * 1024

type VerificationResult struct {
	Passed     bool
	Summary    string
	Checks     []TestResult
	Artifact   string
	Failure    *Failure
	DurationMS int64
}

type Verifier interface {
	Verify(ctx context.Context, state State, policy VerificationPolicy) (VerificationResult, error)
}

type CommandVerifier struct {
	WorkDir string
	Now     func() time.Time
}

func (v CommandVerifier) Verify(ctx context.Context, state State, policy VerificationPolicy) (VerificationResult, error) {
	if len(policy.Commands) == 0 {
		return VerificationResult{
			Passed:  true,
			Summary: "No command checks configured.",
		}, nil
	}
	now := v.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	started := now()
	result := VerificationResult{Passed: true}
	for _, check := range policy.Commands {
		testResult := v.runCommand(ctx, check, now)
		result.Checks = append(result.Checks, testResult)
		required := check.Required
		if !required {
			required = true
		}
		if required && !testResult.Passed {
			result.Passed = false
			failure := Failure{
				Step:      StepVerification,
				Kind:      "verification_command_failed",
				Message:   fmt.Sprintf("%s failed with exit code %d", testResult.Name, testResult.ExitCode),
				Command:   testResult.Command,
				CreatedAt: testResult.CreatedAt,
			}
			if strings.TrimSpace(testResult.Error) != "" {
				failure.Message = testResult.Error
			}
			result.Failure = &failure
		}
	}
	result.DurationMS = now().Sub(started).Milliseconds()
	result.Summary = summarizeVerification(result)
	return result, nil
}

func (v CommandVerifier) runCommand(ctx context.Context, check CommandCheck, now func() time.Time) TestResult {
	name := strings.TrimSpace(check.Name)
	if name == "" {
		name = strings.TrimSpace(check.Command)
	}
	command := strings.TrimSpace(check.Command)
	result := TestResult{
		Name:      name,
		Command:   command,
		Passed:    false,
		ExitCode:  1,
		CreatedAt: now(),
	}
	if command == "" {
		result.Error = "verification command is required"
		return result
	}

	timeout := defaultCommandTimeout
	if check.TimeoutSeconds > 0 {
		timeout = time.Duration(check.TimeoutSeconds) * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-lc", command)
	cmd.Dir = strings.TrimSpace(check.WorkDir)
	if cmd.Dir == "" {
		cmd.Dir = strings.TrimSpace(v.WorkDir)
	}
	cmd.Env = append(os.Environ(),
		"EDITOR=true",
		"GIT_EDITOR=true",
		"GIT_PAGER=cat",
		"GIT_TERMINAL_PROMPT=0",
		"NO_COLOR=1",
		"PAGER=cat",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	started := now()
	err := cmd.Run()
	result.DurationMS = now().Sub(started).Milliseconds()
	output := stdout.String() + stderr.String()
	result.Output = truncateOutput(output, maxVerificationOutputBytes)
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result.ExitCode = 124
		result.Error = "verification command timed out"
		return result
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.Error = err.Error()
		}
		return result
	}
	result.Passed = true
	result.ExitCode = 0
	return result
}

func summarizeVerification(result VerificationResult) string {
	if len(result.Checks) == 0 {
		return "No verification checks ran."
	}
	passed := 0
	for _, check := range result.Checks {
		if check.Passed {
			passed++
		}
	}
	status := "passed"
	if !result.Passed {
		status = "failed"
	}
	return fmt.Sprintf("%s: %d/%d checks passed", status, passed, len(result.Checks))
}

func renderVerificationMarkdown(result VerificationResult) string {
	var b strings.Builder
	b.WriteString("# Verification\n\n")
	b.WriteString(result.Summary)
	b.WriteString("\n\n")
	for _, check := range result.Checks {
		status := "FAIL"
		if check.Passed {
			status = "PASS"
		}
		fmt.Fprintf(&b, "## %s\n\n", check.Name)
		fmt.Fprintf(&b, "- Status: %s\n", status)
		fmt.Fprintf(&b, "- Command: `%s`\n", check.Command)
		fmt.Fprintf(&b, "- Exit code: %d\n", check.ExitCode)
		fmt.Fprintf(&b, "- Duration: %dms\n\n", check.DurationMS)
		if strings.TrimSpace(check.Error) != "" {
			fmt.Fprintf(&b, "Error: %s\n\n", check.Error)
		}
		if strings.TrimSpace(check.Output) != "" {
			b.WriteString("```text\n")
			b.WriteString(check.Output)
			if !strings.HasSuffix(check.Output, "\n") {
				b.WriteByte('\n')
			}
			b.WriteString("```\n\n")
		}
	}
	return b.String()
}

func truncateOutput(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	if maxBytes <= len("\n[truncated]\n") {
		return value[:maxBytes]
	}
	return value[:maxBytes-len("\n[truncated]\n")] + "\n[truncated]\n"
}
