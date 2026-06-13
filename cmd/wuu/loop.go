package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	looprunner "github.com/blueberrycongee/wuu/internal/loop"
)

func runLoop(args []string) error {
	if len(args) == 0 {
		return errors.New("loop subcommand is required (demo or status)")
	}
	switch args[0] {
	case "demo":
		return runLoopDemo(args[1:])
	case "status":
		return runLoopStatus(args[1:])
	default:
		return fmt.Errorf("unknown loop subcommand %q", args[0])
	}
}

func runLoopDemo(args []string) error {
	fs := flag.NewFlagSet("loop demo", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	workdir := fs.String("workdir", "", "workspace directory")
	id := fs.String("id", "", "loop id")
	goal := fs.String("goal", "Demonstrate durable loop workflow", "loop goal")
	task := fs.String("task", "", "optional task detail")
	jsonOutput := fs.Bool("json", false, "output final loop state as JSON")
	var verifyCommands stringListFlag
	fs.Var(&verifyCommands, "verify-command", "verification command to run; may be repeated")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rootDir, err := resolveWorkdir(*workdir)
	if err != nil {
		return err
	}
	commands := make([]looprunner.CommandCheck, 0, len(verifyCommands))
	for i, command := range verifyCommands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		commands = append(commands, looprunner.CommandCheck{
			Name:           fmt.Sprintf("command-%d", i+1),
			Command:        command,
			WorkDir:        rootDir,
			TimeoutSeconds: 120,
			Required:       true,
		})
	}

	spec := looprunner.Spec{
		ID:            strings.TrimSpace(*id),
		Goal:          strings.TrimSpace(*goal),
		Task:          strings.TrimSpace(*task),
		AssignedAgent: "lead",
		Trigger: looprunner.Trigger{
			Type:   "manual",
			Source: "cli",
		},
		Permissions: looprunner.Permissions{
			ReadOnly:                  true,
			EditAllowedPaths:          []string{".loop/**"},
			ShellAllowedCommands:      append([]string(nil), verifyCommands...),
			NetworkAllowed:            false,
			BrowserAllowed:            false,
			GitAllowedOperations:      []string{"status", "diff"},
			DestructiveActionApproval: true,
			SecretAccessPolicy:        "deny",
			ExternalConnectorPolicy:   "deny",
		},
		VerificationPolicy: looprunner.VerificationPolicy{
			Commands:      commands,
			RequireReview: true,
		},
		RetryPolicy: looprunner.RetryPolicy{
			MaxRetries: 1,
		},
		EscalationPolicy: looprunner.EscalationPolicy{
			EscalateOnFailure:      true,
			EscalateOnRetryExhaust: true,
			HumanReviewRequired:    false,
		},
	}
	runner := looprunner.Runner{
		Store:    looprunner.NewStore(looprunner.DefaultDir(rootDir)),
		Verifier: looprunner.CommandVerifier{WorkDir: rootDir},
	}
	state, err := runner.RunDemo(context.Background(), spec)
	if err != nil {
		return err
	}
	if *jsonOutput {
		data, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	printLoopStateSummary(state, runner.Store.Dir())
	return nil
}

func runLoopStatus(args []string) error {
	fs := flag.NewFlagSet("loop status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	workdir := fs.String("workdir", "", "workspace directory")
	jsonOutput := fs.Bool("json", false, "output loop state as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rootDir, err := resolveWorkdir(*workdir)
	if err != nil {
		return err
	}
	store := looprunner.NewStore(looprunner.DefaultDir(rootDir))
	state, err := store.LoadState()
	if err != nil {
		return err
	}
	if *jsonOutput {
		data, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	printLoopStateSummary(state, store.Dir())
	return nil
}

func printLoopStateSummary(state looprunner.State, loopDir string) {
	fmt.Printf("loop_id: %s\n", state.ID)
	fmt.Printf("status: %s\n", state.Status)
	if state.CurrentStep != "" {
		fmt.Printf("current_step: %s\n", state.CurrentStep)
	}
	fmt.Printf("state: %s\n", filepath.Join(loopDir, "state.json"))
	if state.FinalArtifact != "" {
		fmt.Printf("final_artifact: %s\n", state.FinalArtifact)
	}
	if state.CurrentBlocker != "" {
		fmt.Printf("blocker: %s\n", state.CurrentBlocker)
	}
	if len(state.NextSteps) > 0 {
		fmt.Printf("next_step: %s\n", state.NextSteps[0])
	}
}

type stringListFlag []string

func (f *stringListFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}
