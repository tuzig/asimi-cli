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

// startInitWorkflowMsg triggers the init workflow to start
type startInitWorkflowMsg struct {
	ClearMode  bool
	AgentsFile string
}

// newInitWorkflow creates a workflow for initializing a project
func newInitWorkflow(model *TUIModel, clearMode bool, agentsFile string) *Workflow {
	repoInfo := GetRepoInfo()
	host, org, project := parseProjectSlug(repoInfo.ProjectRoot)
	branch := branchSlugOrDefault(repoInfo.Branch)
	repoCtx := RepoContext{
		Host:    host,
		Org:     org,
		Project: project,
		Branch:  branch,
	}

	SetIDGenerator(generateSessionID)

	w := New("init", model.db, repoCtx, WithMaxRetries(5))

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
			if err := os.MkdirAll(".agents/sandbox", 0o755); err != nil {
				return fmt.Errorf("error creating .agents directory: %v", err)
			}
			w.ReportProgress("Created .agents/sandbox directory")
			return nil
		}).
		AddRun("write-embeds", func(w *Workflow) error {
			clearMode := w.Get("clearMode") == "true"
			agentsFile := w.Get("agentsFile")

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
				w.ReportProgress("Added default .agents/asimi.conf with lots of comments")
			}

			// Write bashrc
			if _, err := os.Stat(".agents/sandbox/bashrc"); os.IsNotExist(err) || clearMode {
				if err := os.WriteFile(".agents/sandbox/bashrc", []byte(sandboxBashrc), 0o644); err != nil {
					return fmt.Errorf("error writing .agents/sandbox/bashrc: %v", err)
				}
				w.ReportProgress("Added default .agents/sandbox/bashrc")
			}

			return nil
		}).
		AddRun("remove-image", func(w *Workflow) error {
			clearMode := w.Get("clearMode") == "true"
			if !clearMode {
				return nil
			}

			imageName := fmt.Sprintf("localhost/asimi-sandbox-%s:latest", w.Get("projectSlug"))

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			result, err := hostRun(ctx, RunShellCommandInput{
				Command:        fmt.Sprintf("podman rmi %s 2>/dev/null || true", imageName),
				Description:    "Removing container image",
				BypassApproval: true,
			})
			if err != nil {
				slog.Warn("Failed to remove image", "image", imageName, "error", err)
			} else if result.ExitCode == "0" && !strings.Contains(result.Output, "Error") {
				slog.Debug("Image removed", "image", imageName)
			}
			w.ReportProgress("Removed podman image " + imageName)
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

				w.ReportProgress("Analyzing project and generating infrastructure files")
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
					return w.Next(checkPrefix + " Skipped - all files exist")
				}
				agentsFile := w.Get("agentsFile")

				// Check which files are still missing
				var missingFiles []string
				if _, err := os.Stat(agentsFile); os.IsNotExist(err) {
					missingFiles = append(missingFiles, agentsFile)
				}
				if _, err := os.Stat("Justfile"); os.IsNotExist(err) {
					missingFiles = append(missingFiles, "Justfile")
				}
				if _, err := os.Stat(".agents/sandbox/Dockerfile"); os.IsNotExist(err) {
					missingFiles = append(missingFiles, ".agents/sandbox/Dockerfile")
				}

				if len(missingFiles) > 0 {
					return w.Retry(fmt.Sprintf("❌ Files not created: %s. Please create them.", strings.Join(missingFiles, ", ")))
				}

				// Reload config - AI may have modified .agents/asimi.conf
				if cfg, err := LoadConfig(); err != nil {
					slog.Warn("Failed to reload config after ai-analysis", "error", err)
				} else {
					initShellRunner(cfg, model.scheduler)
				}
				return w.Next(checkPrefix + " AI analysis completed")
			},
		}).
		// Host tests - run tests and fix if they fail
		AddCheck("host-tests", func(w *Workflow) StepResult {
			w.ReportProgress("$ just test # on host")
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()

			result, err := hostRun(ctx, RunShellCommandInput{
				Command:        "just test",
				Description:    "Running tests on host",
				BypassApproval: true,
			})
			if err != nil || result.ExitCode != "0" {
				w.Set("hostTestError", truncateOutput(result.Output, 2000))
				return w.GoTo("fix-host-tests", fmt.Sprintf("❌ just test failed (exit code: %s)", result.ExitCode))
			}
			// Skip fix step and proceed to build-sandbox
			return w.GoTo("build-sandbox", checkPrefix+" Host tests passed")
		}).
		Add(Step{
			Name: "fix-host-tests",
			Prepare: func(w *Workflow) (map[string]interface{}, error) {
				return map[string]interface{}{
					"TestError": w.Get("hostTestError"),
				}, nil
			},
			Prompt: `The host tests failed with the following error:

{{.TestError}}

Please analyze the error and fix the issue. This could be:
- Missing dependencies in the Justfile
- Code errors that need to be fixed
- Configuration issues

Read the relevant files, understand the error, and make the necessary corrections.`,
			Verify: func(w *Workflow, response string) StepResult {
				if response == "" {
					return w.Retry("No response from AI, retrying")
				}
				return w.GoTo("host-tests", "Code updated, re-running tests")
			},
		}).
		// Build sandbox - has Prompt, custom Verify, and side effect
		AddCheck("build-sandbox", func(w *Workflow) StepResult {
			w.ReportProgress("$ just build-sandbox # on host")
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
			defer cancel()

			result, err := hostRun(ctx, RunShellCommandInput{
				Command:        "just build-sandbox",
				Description:    "Building sandbox container",
				BypassApproval: true})
			if err != nil || result.ExitCode != "0" {
				// Store the error for the fix-dockerfile step
				w.Set("buildError", truncateOutput(result.Output, 2000))
				return w.GoTo("fix-dockerfile", fmt.Sprintf("❌ just build-sandbox failed (exit code: %s)", result.ExitCode))
			}

			// Reload config to pick up any changes from the build (e.g., image_name in Justfile)
			if cfg, err := LoadConfig(); err != nil {
				slog.Warn("Failed to reload config after build-sandbox", "error", err)
			} else {
				slog.Debug("Reinitializing shell runner after build-sandbox", "image_name", cfg.RunShellCommand.ImageName)
				initShellRunner(cfg, model.scheduler)
			}
			// Skip fix-dockerfile and go directly to smoke-test
			return w.GoTo("smoke-test", checkPrefix+" Sandbox built successfully")
		}).
		Add(Step{
			Name: "fix-dockerfile",
			Prepare: func(w *Workflow) (map[string]interface{}, error) {
				return map[string]interface{}{
					"BuildError": w.Get("buildError"),
				}, nil
			},
			Prompt: `The sandbox container build failed with the following error:

{{.BuildError}}

Please fix the Dockerfile at .agents/sandbox/Dockerfile to resolve this build error.
Read the current Dockerfile, understand the error, and make the necessary corrections.
Common issues include missing packages, incorrect base images, or syntax errors.`,
			Verify: func(w *Workflow, response string) StepResult {
				if response == "" {
					return w.Retry("No response from AI, retrying")
				}
				// Go back to build-sandbox to try the build again
				return w.GoTo("build-sandbox", "Dockerfile updated, retrying build")
			},
		}).
		AddCheck("smoke-test", func(w *Workflow) StepResult {
			w.ReportProgress("Running smoke test in container...")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			runner := getShellRunner()
			if runner == nil {
				return w.Retry("❌ Container runner not available")
			}

			result, err := runner.Run(ctx, RunShellCommandInput{
				Command:        "uname",
				Description:    "Running smoke test in container",
				BypassApproval: true,
			})
			if err != nil || result.ExitCode != "0" || !strings.Contains(result.Output, "Linux") {
				// Smoke test failure likely means container/Dockerfile issue
				w.Set("buildError", fmt.Sprintf("Container smoke test failed - uname returned: %s (exit code: %s)", result.Output, result.ExitCode))
				return w.GoTo("fix-dockerfile", fmt.Sprintf("❌ Smoke test failed (exit code: %s)", result.ExitCode))
			}
			return w.Next(checkPrefix + " Smoke test passed")
		}).
		// Container tests - run tests in container and fix if they fail
		AddCheck("container-tests", func(w *Workflow) StepResult {
			w.ReportProgress("$ just test # in container")
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()

			runner := getShellRunner()
			if runner == nil {
				return w.Retry("❌ Container runner not available")
			}

			result, err := runner.Run(ctx, RunShellCommandInput{
				Command:        "just test",
				Description:    "Running tests in container",
				BypassApproval: true,
			})
			if err != nil || result.ExitCode != "0" {
				w.Set("containerTestError", truncateOutput(result.Output, 2000))
				return w.GoTo("fix-container-tests", fmt.Sprintf("❌ Container tests failed (exit code: %s)", result.ExitCode))
			}
			// Skip fix step and proceed to git-stage
			return w.GoTo("git-stage", checkPrefix+" Container tests passed")
		}).
		Add(Step{
			Name: "fix-container-tests",
			Prepare: func(w *Workflow) (map[string]interface{}, error) {
				return map[string]interface{}{
					"TestError": w.Get("containerTestError"),
				}, nil
			},
			Prompt: `The container tests failed with the following error:

{{.TestError}}

The tests pass on the host but fail in the container. This is usually caused by:
- Missing packages in the Dockerfile
- Different environment variables or paths
- Missing tools or dependencies in the container

Please fix the issue. You may need to update:
- .agents/sandbox/Dockerfile (to add missing packages)
- .agents/sandbox/bashrc (to set environment variables)
- Justfile (if paths need adjustment)`,
			Verify: func(w *Workflow, response string) StepResult {
				if response == "" {
					return w.Retry("No response from AI, retrying")
				}
				// Check if Dockerfile was modified - if so, rebuild
				// For now, just re-run tests (AI might have fixed bashrc or Justfile)
				return w.GoTo("container-tests", "Container environment updated, re-running tests")
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
		// Create the workflow
		w := newInitWorkflow(model, clearMode, agentsFile)

		model.content.Chat.Indent = 1

		// Set up progress callback for step state changes and ad-hoc messages
		w.SetOnProgress(func(stepIndex int, stepState StepState, message string) {
			if program != nil {
				if message != "" {
					// Ad-hoc progress message (indentation applied by chat component)
					program.Send(showContextMsg{content: message})
				} else {
					// Step state change
					program.Send(initWorkflowProgressMsg{
						stepIndex: stepIndex,
						stepState: stepState,
						message:   stepState.Message,
					})
				}
			}
		})

		// Set up the sendPrompt function to use the session
		w.SetSendPrompt(func(ctx context.Context, prompt string) <-chan string {
			responseChan := make(chan string, 1)

			go func() {
				defer close(responseChan)

				if model.session == nil {
					slog.Warn("No session available for workflow prompt")
					responseChan <- ""
					return
				}

				// Use AskWithStreaming to show model output in the UI while blocking
				response, err := model.session.AskWithStreaming(ctx, prompt)
				if err != nil {
					slog.Error("Workflow prompt failed", "error", err)
					responseChan <- ""
					return
				}

				responseChan <- response
			}()

			return responseChan
		})

		// Allow host fallback during init
		runner := getShellRunner()
		if runner != nil {
			runner.AllowFallback(true)
			defer runner.AllowFallback(false)
		}

		// Run the workflow
		ctx := context.Background()
		err := w.Run(ctx)

		// Reset indentation
		model.content.Chat.Indent = 0
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

// handleInitCommandWithWorkflow is the workflow-based implementation of :init
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
			initialMsg = systemPrefix + "Starting fresh initialization (clear mode)..."
		} else {
			msg := NewChatMsgBuilder(systemPrefix)
			msg.WriteLn("Missing infrastructure files detected:")
			for _, file := range missingFiles {
				msg.WriteLnf("✗ %s", file)
			}
			msg.WriteLn()
			msg.WriteLn("Starting initialization ..")
			initialMsg = msg.String()
		}

		// Send initial message and start workflow
		if program != nil {
			program.Send(showContextMsg{content: initialMsg})
		}

		// Return a command to start the workflow
		return startInitWorkflowMsg{
			ClearMode:  clearMode,
			AgentsFile: agentsFile,
		}
	}
}

// BuildInitPrompt builds the initialization prompt from the template
func BuildInitPrompt(projectName, projectSlug, agentsFile, initPromptTemplate string, missingFiles []string, clearMode bool) (string, error) {
	templateData := InitTemplateData{
		ProjectName:  projectName,
		ProjectSlug:  projectSlug,
		MissingFiles: missingFiles,
		ClearMode:    clearMode,
		AgentsFile:   agentsFile,
	}

	tmpl, err := template.New("init").Parse(initPromptTemplate)
	if err != nil {
		return "", fmt.Errorf("error parsing initialization template: %v", err)
	}

	var initPrompt bytes.Buffer
	if err := tmpl.Execute(&initPrompt, templateData); err != nil {
		return "", fmt.Errorf("error executing initialization template: %v", err)
	}

	return initPrompt.String(), nil
}
