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

	goalrunner "github.com/blueberrycongee/wuu/internal/goal"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

const defaultGoalID = "demo"

func runGoal(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("goal subcommand is required (demo or status)")
	}
	switch args[0] {
	case "demo":
		return runGoalDemo(args[1:])
	case "status":
		return runGoalStatus(args[1:])
	default:
		return fmt.Errorf("unknown goal subcommand %q", args[0])
	}
}

func runGoalDemo(args []string) error {
	fs := flag.NewFlagSet("goal demo", flag.ContinueOnError)
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
	store, goalID, workspaceStateDir, err := resolveGoalStore(rootDir, *id)
	if err != nil {
		return err
	}
	commands := make([]goalrunner.CommandCheck, 0, len(verifyCommands))
	for i, command := range verifyCommands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		commands = append(commands, goalrunner.CommandCheck{
			Name:           fmt.Sprintf("command-%d", i+1),
			Command:        command,
			WorkDir:        rootDir,
			TimeoutSeconds: 120,
			Required:       true,
		})
	}

	spec := goalrunner.Spec{
		ID:            goalID,
		Goal:          strings.TrimSpace(*goal),
		Task:          strings.TrimSpace(*task),
		AssignedAgent: "lead",
		Trigger: goalrunner.Trigger{
			Type:   "manual",
			Source: "cli",
		},
		Permissions: goalrunner.Permissions{
			ReadOnly:                  true,
			EditAllowedPaths:          []string{filepath.Join(workspaceStateDir, "goals", goalID, "**")},
			ShellAllowedCommands:      append([]string(nil), verifyCommands...),
			NetworkAllowed:            false,
			BrowserAllowed:            false,
			GitAllowedOperations:      []string{"status", "diff"},
			DestructiveActionApproval: true,
			SecretAccessPolicy:        "deny",
			ExternalConnectorPolicy:   "deny",
		},
		VerificationPolicy: goalrunner.VerificationPolicy{
			Commands:      commands,
			RequireReview: true,
		},
		RetryPolicy: goalrunner.RetryPolicy{
			MaxRetries: 1,
		},
		EscalationPolicy: goalrunner.EscalationPolicy{
			EscalateOnFailure:      true,
			EscalateOnRetryExhaust: true,
			HumanReviewRequired:    false,
		},
	}
	runner := goalrunner.Runner{
		Store:    store,
		Verifier: goalrunner.CommandVerifier{WorkDir: rootDir},
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
	printGoalStateSummary(state, runner.Store.Dir())
	return nil
}

func runGoalStatus(args []string) error {
	fs := flag.NewFlagSet("goal status", flag.ContinueOnError)
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
	store, _, _, err := resolveGoalStore(rootDir, *id)
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
	printGoalStateSummary(state, store.Dir())
	return nil
}

func printGoalStateSummary(state goalrunner.State, goalDir string) {
	fmt.Printf("goal_id: %s\n", state.ID)
	fmt.Printf("status: %s\n", state.Status)
	if state.CurrentStep != "" {
		fmt.Printf("current_step: %s\n", state.CurrentStep)
	}
	fmt.Printf("state: %s\n", filepath.Join(goalDir, "state.json"))
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

func resolveGoalStore(rootDir, requestedID string) (*goalrunner.Store, string, string, error) {
	goalID := strings.TrimSpace(requestedID)
	if goalID == "" {
		goalID = defaultGoalID
	}
	if err := validateGoalID(goalID); err != nil {
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
	return goalrunner.NewStore(statepath.GoalDir(workspaceStateDir, goalID)), goalID, workspaceStateDir, nil
}

func validateGoalID(goalID string) error {
	if goalID == "" {
		return errors.New("goal id is required")
	}
	if goalID == "." || goalID == ".." || filepath.Base(goalID) != goalID || strings.ContainsAny(goalID, `/\`) {
		return fmt.Errorf("goal id must be an id, not a path: %q", goalID)
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
