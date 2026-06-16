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
	"github.com/blueberrycongee/wuu/internal/statepath"
)

const defaultLoopID = "demo"

func runGoal(args []string) error {
	return runGoalCommand(args, "goal")
}

func runLoop(args []string) error {
	return runGoalCommand(args, "loop")
}

func runGoalCommand(args []string, commandName string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s subcommand is required (demo or status)", commandName)
	}
	switch args[0] {
	case "demo":
		return runGoalDemo(args[1:], commandName)
	case "status":
		return runGoalStatus(args[1:], commandName)
	default:
		return fmt.Errorf("unknown %s subcommand %q", commandName, args[0])
	}
}

func runGoalDemo(args []string, commandName string) error {
	fs := flag.NewFlagSet(commandName+" demo", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	workdir := fs.String("workdir", "", "workspace directory")
	id := fs.String("id", "", "goal id")
	goal := fs.String("goal", "Demonstrate durable goal workflow", "goal objective")
	task := fs.String("task", "", "optional task detail")
	jsonOutput := fs.Bool("json", false, "output final goal state as JSON")
	var verifyCommands stringListFlag
	fs.Var(&verifyCommands, "verify-command", "verification command to run; may be repeated")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rootDir, err := resolveWorkdir(*workdir)
	if err != nil {
		return err
	}
	store, loopID, workspaceStateDir, err := resolveLoopStore(rootDir, *id)
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
		ID:            loopID,
		Goal:          strings.TrimSpace(*goal),
		Task:          strings.TrimSpace(*task),
		AssignedAgent: "lead",
		Trigger: looprunner.Trigger{
			Type:   "manual",
			Source: "cli",
		},
		Permissions: looprunner.Permissions{
			ReadOnly:                  true,
			EditAllowedPaths:          []string{filepath.Join(workspaceStateDir, "loops", loopID, "**")},
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
		Store:    store,
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

func runGoalStatus(args []string, commandName string) error {
	fs := flag.NewFlagSet(commandName+" status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	workdir := fs.String("workdir", "", "workspace directory")
	id := fs.String("id", "", "goal id")
	jsonOutput := fs.Bool("json", false, "output goal state as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rootDir, err := resolveWorkdir(*workdir)
	if err != nil {
		return err
	}
	store, _, _, err := resolveLoopStore(rootDir, *id)
	if err != nil {
		return err
	}
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
	fmt.Printf("goal_id: %s\n", state.ID)
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

func resolveLoopStore(rootDir, requestedID string) (*looprunner.Store, string, string, error) {
	loopID := strings.TrimSpace(requestedID)
	if loopID == "" {
		loopID = defaultLoopID
	}
	if err := validateLoopID(loopID); err != nil {
		return nil, "", "", err
	}
	wuuHome, err := statepath.Home("")
	if err != nil {
		return nil, "", "", err
	}
	workspaceStateDir, err := statepath.WorkspaceDir(wuuHome, rootDir)
	if err != nil {
		return nil, "", "", err
	}
	return looprunner.NewStore(statepath.LoopDir(workspaceStateDir, loopID)), loopID, workspaceStateDir, nil
}

func validateLoopID(loopID string) error {
	if loopID == "" {
		return errors.New("goal id is required")
	}
	if loopID == "." || loopID == ".." || filepath.Base(loopID) != loopID || strings.ContainsAny(loopID, `/\`) {
		return fmt.Errorf("goal id must be an id, not a path: %q", loopID)
	}
	return nil
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
