package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/internal/utils"
)

// RunShellCommand is a tool for running shell commands in a persistent shell.
// It uses the runners package for actual execution.
type RunShellCommand struct {
	shouldRunOnHost func(cmd string) (runOnHost, needsApproval bool)
}

// NewRunShellCommand creates a new RunShellCommand tool
func NewRunShellCommand(
	hostChecker func(string) (bool, bool),
) *RunShellCommand {
	return &RunShellCommand{
		shouldRunOnHost: hostChecker,
	}
}

func (t *RunShellCommand) Name() string {
	return "run_shell_command"
}

func (t *RunShellCommand) Description() string {
	return "Executes a shell command in a persistent shell session inside a container. Current working directory is maintained between commands. IMPORTANT: Each command runs in an isolated subshell for stability and predictability."
}

func (t *RunShellCommand) Call(ctx context.Context, input string) (string, error) {
	var params runners.Input
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	var output runners.Output
	var runErr error

	runnerInput := runners.Input{
		Command:        params.Command,
		Description:    params.Description,
		BypassApproval: params.BypassApproval,
	}

	// Check if command should run on host based on config patterns
	runOnHost, requiresApproval := false, true
	if t.shouldRunOnHost != nil {
		runOnHost, requiresApproval = t.shouldRunOnHost(params.Command)
	}

	r := runners.GetRunner()
	if runOnHost {
		// Set the approval flag based on config patterns
		runnerInput.BypassApproval = !requiresApproval

		// Create ephemeral host runner and run directly on host
		hostRunner := runners.NewHostRunner()
		runnerOutput, err := hostRunner.Run(ctx, runnerInput)
		output.Output = runnerOutput.Output
		output.ExitCode = runnerOutput.ExitCode
		runErr = err
	} else if r != nil {
		runnerOutput, err := r.Run(ctx, runnerInput)
		output.Output = runnerOutput.Output
		output.ExitCode = runnerOutput.ExitCode
		runErr = err

		// If we got a harness error, try to restart and retry once
		if runErr != nil {
			if restartErr := r.Restart(ctx); restartErr != nil {
				return "", fmt.Errorf("command failed and restart failed: %w (restart error: %v)", runErr, restartErr)
			}

			runnerOutput, err = r.Run(ctx, runnerInput)
			output.Output = runnerOutput.Output
			output.ExitCode = runnerOutput.ExitCode
			runErr = err
		}
	} else {
		return "", fmt.Errorf("no runner configured")
	}

	if runErr != nil {
		return "", runErr
	}

	outputBytes, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("failed to marshal output: %w", err)
	}

	return string(outputBytes), nil
}

func (t *RunShellCommand) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Shell command to run",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Why we run this command, will be displayed to the user",
			},
		},
		"required": []string{"command"},
	}
}

// Format formats a run_shell_command tool call for display
func (t *RunShellCommand) Format(input, result string, err error) string {
	var params runners.Input
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("")
	msg.WriteLn(params.Description)
	msg.Writef("$ %s", params.Command)

	if err != nil {
		msg.WriteLn()
		msg.Writef("ERROR: %v", err)
	} else if result != "" {
		var output map[string]interface{}
		if json.Unmarshal([]byte(result), &output) == nil {
			if ec, ok := output["exitCode"].(string); ok && ec != "0" {
				msg.WriteLn()
				msg.WriteString(ec)
			}
		} else {
			msg.WriteLn()
			msg.Writef("ERROR: %v", err)
		}
	}

	return msg.String() + "\n"
}
