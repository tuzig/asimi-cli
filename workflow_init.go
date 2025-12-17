package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/afittestide/asimi/storage"
	tea "github.com/charmbracelet/bubbletea"
)

// initWorkflowProgressMsg is sent to update the UI with workflow progress
type initWorkflowProgressMsg struct {
	stepIndex int
	stepState StepState
	message   string
}

// initWorkflowCompleteMsg is sent when the init workflow completes
type initWorkflowCompleteMsg struct {
	success bool
	message string
}

// initWorkflowErrorMsg is sent when the init workflow encounters an error
type initWorkflowErrorMsg struct {
	err error
}

// NewInitWorkflow creates a workflow for initializing a project
func NewInitWorkflow(db *storage.DB, repoInfo RepoInfo, clearMode bool, agentsFile string) *Workflow {
	w := NewWorkflow("init", db, repoInfo, WithMaxRetries(5))

	// Store init parameters in workflow data
	w.Set("clearMode", fmt.Sprintf("%t", clearMode))
	w.Set("agentsFile", agentsFile)

	// Get the project slug from RepoInfo
	slug := repoInfo.Slug
	projectName := slug
	if idx := strings.LastIndex(slug, "/"); idx >= 0 {
		projectName = slug[idx+1:]
	}
	w.Set("projectName", projectName)
	w.Set("projectSlug", slug)

	w.AddGate("pre-checks", func(w *Workflow) bool {
		w.ReportProgress("Checking for uncommitted changes...")
		return !hasUncommittedChanges()
	}, "Please commit or stash your changes and run again").
		AddRun("setup-directories", func(w *Workflow) error {
			w.ReportProgress("Creating .agents/sandbox directory...")
			if err := os.MkdirAll(".agents/sandbox", 0o755); err != nil {
				return fmt.Errorf("error creating .agents directory: %v", err)
			}
			return nil
		}).
		AddRun("write-embeds", func(w *Workflow) error {
			clearMode := w.Get("clearMode") == "true"
			agentsFile := w.Get("agentsFile")

			w.ReportProgress("Writing embedded configuration files...")

			// In clear mode, remove all infrastructure files first
			if clearMode {
				for _, file := range []string{agentsFile, "Justfile", ".agents/asimi.conf", ".agents/sandbox/bashrc", ".agents/sandbox/Dockerfile"} {
					os.Remove(file)
				}
			}

			// Write asimi.conf
			if _, err := os.Stat(".agents/asimi.conf"); os.IsNotExist(err) || clearMode {
				if err := os.WriteFile(".agents/asimi.conf", []byte(defaultConfContent), 0o644); err != nil {
					return fmt.Errorf("error writing .agents/asimi.conf: %v", err)
				}
			}

			// Write bashrc
			if _, err := os.Stat(".agents/sandbox/bashrc"); os.IsNotExist(err) || clearMode {
				if err := os.WriteFile(".agents/sandbox/bashrc", []byte(sandboxBashrc), 0o644); err != nil {
					return fmt.Errorf("error writing .agents/sandbox/bashrc: %v", err)
				}
			}

			return nil
		}).
		// AI analysis needs full Add - has Prepare, Prompt, and custom Verify
		Add(Step{
			Name: "ai-analysis",
			Prepare: func(w *Workflow) (map[string]interface{}, error) {
				agentsFile := w.Get("agentsFile")
				clearMode := w.Get("clearMode") == "true"
				missingFiles := checkMissingInfraFiles(agentsFile)

				if len(missingFiles) == 0 && !clearMode {
					w.Set("skipAIAnalysis", true)
					w.ReportProgress("All infrastructure files exist, skipping AI analysis")
					return map[string]interface{}{"skipAIAnalysis": true}, nil
				}

				w.ReportProgress("Analyzing project and generating infrastructure files...")
				return map[string]interface{}{
					"MissingFiles": missingFiles,
					"ClearMode":    clearMode,
					"AgentsFile":   agentsFile,
					"ProjectName":  w.Get("projectName"),
					"ProjectSlug":  w.Get("projectSlug"),
				}, nil
			},
			Prompt: initializePrompt,
			Verify: func(w *Workflow, response string) StepResult {
				if skip, ok := w.GetValue("skipAIAnalysis").(bool); ok && skip {
					return Next(checkPrefix + " Skipped - all files exist")
				}
				agentsFile := w.Get("agentsFile")
				if _, err := os.Stat(agentsFile); os.IsNotExist(err) {
					return Retry(fmt.Sprintf("❌ %s not created", agentsFile))
				}
				if _, err := os.Stat("Justfile"); os.IsNotExist(err) {
					return Retry("❌ Justfile not created")
				}
				// Reload config - AI may have modified .agents/asimi.conf
				if cfg, err := LoadConfig(); err != nil {
					slog.Warn("Failed to reload config after ai-analysis", "error", err)
				} else {
					initShellRunner(cfg)
				}
				return Next(checkPrefix + " AI analysis completed")
			},
		}).
		// Host tests - has Prompt and custom Verify with hostRun
		Add(Step{
			Name:   "host-tests",
			Prompt: "Run `just test` on the host to verify the project builds and tests pass. If there are errors, fix them.",
			Verify: func(w *Workflow, response string) StepResult {
				w.ReportProgress("$ just test # on host")
				ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
				defer cancel()

				result, err := hostRun(ctx, RunShellCommandInput{Command: "just test", Description: "Running tests on host"})
				if err != nil || result.ExitCode != "0" {
					return Retry(fmt.Sprintf("❌ just test failed (exit code: %s): %s", result.ExitCode, truncateOutput(result.Output, 500)))
				}
				return Next(checkPrefix + " Host tests passed")
			},
		}).
		// Build sandbox - has Prompt, custom Verify, and side effect
		Add(Step{
			Name:   "build-sandbox",
			Prompt: "Run `just build-sandbox` to build the container image. If there are errors, fix the Dockerfile or Justfile.",
			Verify: func(w *Workflow, response string) StepResult {
				w.ReportProgress("$ just build-sandbox # on host")
				ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
				defer cancel()

				result, err := hostRun(ctx, RunShellCommandInput{Command: "just build-sandbox", Description: "Building sandbox container"})
				if err != nil || result.ExitCode != "0" {
					return Retry(fmt.Sprintf("❌ just build-sandbox failed (exit code: %s): %s", result.ExitCode, truncateOutput(result.Output, 500)))
				}

				// Reload config to pick up any changes from the build (e.g., image_name in Justfile)
				if cfg, err := LoadConfig(); err != nil {
					slog.Warn("Failed to reload config after build-sandbox", "error", err)
				} else {
					slog.Debug("Reinitializing shell runner after build-sandbox", "image_name", cfg.RunShellCommand.ImageName)
					initShellRunner(cfg)
				}
				return Next(checkPrefix + " Sandbox built successfully")
			},
		}).
		AddCheck("smoke-test", func(w *Workflow) StepResult {
			w.ReportProgress("Running smoke test in container...")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			runner := getShellRunner()
			if runner == nil {
				return Retry("❌ Container runner not available")
			}

			result, err := runner.Run(ctx, RunShellCommandInput{Command: "uname", Description: "Running smoke test in container"})
			if err != nil || result.ExitCode != "0" || !strings.Contains(result.Output, "Linux") {
				return Retry(fmt.Sprintf("❌ Smoke test failed (exit code: %s): %s", result.ExitCode, result.Output))
			}
			return Next(checkPrefix + " Smoke test passed")
		}).
		// Container tests - has Prompt and custom Verify with container runner
		Add(Step{
			Name:   "container-tests",
			Prompt: "Run `just test` in the container to verify the project works in the sandbox environment. If there are errors, fix them.",
			Verify: func(w *Workflow, response string) StepResult {
				w.ReportProgress("$ just test # in container")
				ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
				defer cancel()

				runner := getShellRunner()
				if runner == nil {
					return Retry("❌ Container runner not available")
				}

				result, err := runner.Run(ctx, RunShellCommandInput{Command: "just test", Description: "Running tests in container"})
				if err != nil || result.ExitCode != "0" {
					return Retry(fmt.Sprintf("❌ Container tests failed (exit code: %s): %s", result.ExitCode, truncateOutput(result.Output, 500)))
				}
				return Next(checkPrefix + " Container tests passed")
			},
		}).
		AddRun("git-stage", func(w *Workflow) error {
			agentsFile := w.Get("agentsFile")
			w.ReportProgress("Staging files for commit...")

			filesToStage := []string{agentsFile, "Justfile", ".agents/"}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			for _, file := range filesToStage {
				result, err := hostRun(ctx, RunShellCommandInput{
					Command:     fmt.Sprintf("git add %s", file),
					Description: fmt.Sprintf("Staging %s", file),
				})
				if err != nil || result.ExitCode != "0" {
					slog.Warn("Failed to stage file", "file", file, "error", err, "exitCode", result.ExitCode)
				}
			}

			w.Set("stagedFiles", strings.Join(filesToStage, ", "))
			return nil
		})

	return w
}

// truncateOutput truncates output to a maximum length
func truncateOutput(output string, maxLen int) string {
	if len(output) <= maxLen {
		return output
	}
	return output[:maxLen] + "..."
}

// runInitWorkflowAsync runs the init workflow asynchronously and sends messages to the TUI
func runInitWorkflowAsync(model *TUIModel, clearMode bool, agentsFile string) tea.Cmd {
	return func() tea.Msg {
		repoInfo := GetRepoInfo()

		// Create the workflow
		w := NewInitWorkflow(model.db, repoInfo, clearMode, agentsFile)

		// Set up progress callback for step state changes and ad-hoc messages
		w.onProgress = func(stepIndex int, stepState StepState, message string) {
			if program != nil {
				if message != "" {
					// Ad-hoc progress message
					program.Send(showContextMsg{content: treeMidPrefix + message})
				} else {
					// Step state change
					program.Send(initWorkflowProgressMsg{
						stepIndex: stepIndex,
						stepState: stepState,
						message:   stepState.Message,
					})
				}
			}
		}

		// Set up the sendPrompt function to use the model's session
		w.sendPrompt = func(ctx context.Context, prompt string) <-chan string {
			responseChan := make(chan string, 1)

			go func() {
				defer close(responseChan)

				if model.session == nil {
					slog.Warn("No session available for workflow prompt")
					responseChan <- ""
					return
				}

				// Use the session's Ask method for synchronous response
				response, err := model.session.Ask(ctx, prompt)
				if err != nil {
					slog.Error("Workflow prompt failed", "error", err)
					responseChan <- ""
					return
				}

				responseChan <- response
			}()

			return responseChan
		}

		// Allow host fallback during init
		currentShellRunner.AllowFallback(true)
		defer currentShellRunner.AllowFallback(false)

		// Run the workflow
		ctx := context.Background()
		err := w.Run(ctx)

		if err != nil {
			return initWorkflowErrorMsg{err: err}
		}

		// Build success message
		stagedFiles := w.Get("stagedFiles")
		msg := NewChatMsgBuilder(systemPrefix)
		msg.WriteString(checkPrefix).WriteLn(" Initialization complete!")
		msg.WriteLn(stagedFiles + " staged")
		msg.WriteLn("Start fresh with `:new` and review project's recipes with `:!just -l`")

		return initWorkflowCompleteMsg{
			success: true,
			message: msg.String(),
		}
	}
}

// handleInitCommandWithWorkflow is the new implementation using the workflow framework
func handleInitCommandWithWorkflow(model *TUIModel, args []string) tea.Cmd {
	if model.session == nil {
		return func() tea.Msg {
			return showSystemMsg("No model connection. Use :models to configure a model and start chatting.")
		}
	}

	return func() tea.Msg {
		// Check for uncommitted changes before proceeding
		if hasUncommittedChanges() {
			return showSystemMsg("init failed: Please commit or stash your changes and run again")
		}

		// Check if user wants to clear and regenerate everything
		clearMode := len(args) > 0 && args[0] == "clear"

		// Determine the agents file name - use CLAUDE.md if it exists, otherwise AGENTS.md
		agentsFile := "AGENTS.md"
		if _, err := os.Stat("CLAUDE.md"); err == nil {
			agentsFile = "CLAUDE.md"
			// Update the config file to set agents_file
			if err := SetProjectConfig("session", "agents_file", agentsFile); err != nil {
				slog.Warn("Could not update config with agents_file", "error", err)
			}
		}

		// Check for missing infrastructure files
		missingFiles := checkMissingInfraFiles(agentsFile)

		if len(missingFiles) == 0 && !clearMode {
			msg := NewChatMsgBuilder(systemPrefix)
			msg.WriteLn("All Asimi's files already exist:")
			msg.WriteLnf("✓ %s", agentsFile)
			msg.WriteLn("✓ Justfile")
			msg.WriteLn("✓ .agents/sandbox/Dockerfile")
			msg.WriteLn("✓ .agents/sandbox/bashrc")
			msg.WriteLn("✓ .agents/asimi.conf")
			msg.WriteLn()
			msg.WriteLn("Use `:init clear` to remove and regenerate them.")
			return showContextMsg{content: msg.String()}
		}

		// Show initial message
		var initialMsg string
		if clearMode {
			initialMsg = systemPrefix + "Starting fresh initialization (clear mode)...\n"
		} else {
			msg := NewChatMsgBuilder(systemPrefix)
			msg.WriteLn("Missing infrastructure files detected:")
			for _, file := range missingFiles {
				msg.WriteLnf("✗ %s", file)
			}
			msg.WriteLn()
			msg.WriteLn("Starting initialization workflow...")
			initialMsg = msg.String()
		}

		// Send initial message and start workflow
		if program != nil {
			program.Send(showContextMsg{content: initialMsg})
		}

		// Return a command to start the workflow
		return startInitWorkflowMsg{
			clearMode:  clearMode,
			agentsFile: agentsFile,
		}
	}
}

// startInitWorkflowMsg triggers the init workflow to start
type startInitWorkflowMsg struct {
	clearMode  bool
	agentsFile string
}

// buildInitPrompt builds the initialization prompt from the template
func buildInitPrompt(projectName, projectSlug, agentsFile string, missingFiles []string, clearMode bool) (string, error) {
	templateData := InitTemplateData{
		ProjectName:  projectName,
		ProjectSlug:  projectSlug,
		MissingFiles: missingFiles,
		ClearMode:    clearMode,
		AgentsFile:   agentsFile,
	}

	tmpl, err := template.New("init").Parse(initializePrompt)
	if err != nil {
		return "", fmt.Errorf("error parsing initialization template: %v", err)
	}

	var initPrompt bytes.Buffer
	if err := tmpl.Execute(&initPrompt, templateData); err != nil {
		return "", fmt.Errorf("error executing initialization template: %v", err)
	}

	return initPrompt.String(), nil
}
