package main

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"text/template"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

//go:embed prompts/init.tmpl
var initializePrompt string

//go:embed prompts/compact.txt
var compactPrompt string

//go:embed dotagents/sandbox/bashrc
var sandboxBashrc string

// InitTemplateData holds data for the initialization prompt template
type InitTemplateData struct {
	ProjectName  string
	ProjectSlug  string
	MissingFiles []string
	ClearMode    bool
	AgentsFile   string // The agents file name (AGENTS.md or CLAUDE.md)
}

// Command represents a slash command
type Command struct {
	Name        string
	Description string
	Handler     func(*TUIModel, []string) tea.Cmd
}

// compactConversationMsg is sent when the compact command is executed
type compactConversationMsg struct{}

// CommandRegistry holds all available commands
type CommandRegistry struct {
	Commands map[string]Command
	order    []string
}

func normalizeCommandName(name string) string {
	if name == "" {
		return ""
	}
	// Strip both : and / prefixes to store commands without prefix
	name = strings.TrimPrefix(name, ":")
	return name
}

// NewCommandRegistry creates a new command registry
func NewCommandRegistry() CommandRegistry {
	registry := CommandRegistry{
		Commands: make(map[string]Command),
	}

	// Register built-in commands (stored without prefix)
	registry.RegisterCommand("help", "Show help (usage: :help [topic])", handleHelpCommand)
	registry.RegisterCommand("new", "Start a new session", handleNewSessionCommand)
	registry.RegisterCommand("quit", "Quit the application", handleQuitCommand)
	registry.RegisterCommand("models", "Select AI model", handleModelsCommand)
	registry.RegisterCommand("context", "Show context usage details", handleContextCommand)
	registry.RegisterCommand("resume", "Resume a previous session", handleResumeCommand)
	registry.RegisterCommand("export", "Export conversation to file and open in $EDITOR (usage: :export [full|conversation])", handleExportCommand)
	registry.RegisterCommand("init", "Init project to work with asimi (usage: /init [clear])", handleInitCommand)
	registry.RegisterCommand("compact", "Compact conversation history to reduce context usage", handleCompactCommand)
	registry.RegisterCommand("1", "Jump to the beginning of the chat history", handleScrollTopCommand)
	registry.RegisterCommand("update", "Check for and install updates", handleUpdateCommand)

	return registry
}

// RegisterCommand registers a new command
func (cr *CommandRegistry) RegisterCommand(name, description string, handler func(*TUIModel, []string) tea.Cmd) {
	normalized := normalizeCommandName(name)
	if normalized == "" {
		return
	}
	if _, exists := cr.Commands[normalized]; !exists {
		cr.order = append(cr.order, normalized)
	}
	cr.Commands[normalized] = Command{
		Name:        normalized,
		Description: description,
		Handler:     handler,
	}
}

// GetCommand gets a command by name
func (cr CommandRegistry) GetCommand(name string) (Command, bool) {
	normalized := normalizeCommandName(name)
	cmd, exists := cr.Commands[normalized]
	return cmd, exists
}

// FindCommand finds commands by prefix (like vim).
// Returns:
// - exactMatch: the matched command if exactly one match is found
// - matches: all commands that start with the prefix
// - found: true if exactly one match was found
func (cr CommandRegistry) FindCommand(prefix string) (exactMatch Command, matches []string, found bool) {
	normalized := normalizeCommandName(prefix)
	if normalized == "" {
		return Command{}, nil, false
	}

	// First try exact match
	if cmd, exists := cr.Commands[normalized]; exists {
		return cmd, []string{normalized}, true
	}

	// Try prefix matching
	var matchedCommands []string

	for _, cmdName := range cr.order {
		if strings.HasPrefix(cmdName, normalized) {
			matchedCommands = append(matchedCommands, cmdName)
		}
	}

	if len(matchedCommands) == 1 {
		cmd := cr.Commands[matchedCommands[0]]
		return cmd, matchedCommands, true
	}

	return Command{}, matchedCommands, false
}

// GetAllCommands returns all registered commands
func (cr CommandRegistry) GetAllCommands() []Command {
	var commands []Command
	for _, name := range cr.order {
		if cmd, ok := cr.Commands[name]; ok {
			commands = append(commands, cmd)
		}
	}
	return commands
}

// Command handlers

type showHelpMsg struct {
	topic string
}
type showContextMsg struct{ content string }

// agentAskLLMMsg is a message sent by the agent to trigger a new LLM conversation
type agentAskLLMMsg struct {
	prompt string
}

func handleHelpCommand(model *TUIModel, args []string) tea.Cmd {
	// Determine the help topic from args
	topic := "index" // Default topic
	if len(args) > 0 {
		topic = args[0]
	}

	return func() tea.Msg {
		return showHelpMsg{topic: topic}
	}
}

func handleNewSessionCommand(model *TUIModel, args []string) tea.Cmd {
	model.saveSession()

	model.sessionActive = true

	markdownEnabled := false
	if model != nil && model.config != nil {
		markdownEnabled = model.config.UI.MarkdownEnabled
	}
	chat := model.content.Chat
	model.content.Chat = NewChatComponent(chat.Width, chat.Height, markdownEnabled)

	// Use the generic startConversationMsg to reset the session
	return func() tea.Msg {
		return startConversationMsg{
			clearHistory: true,
		}
	}
}

func handleQuitCommand(model *TUIModel, args []string) tea.Cmd {
	// Shutdown handles saving the session and waiting for completion
	model.shutdown()
	// Quit the application
	return tea.Quit
}

func handleContextCommand(model *TUIModel, args []string) tea.Cmd {
	return func() tea.Msg {
		if model.session == nil {
			return showContextMsg{content: "No active session. Use :models to configure a model and start chatting."}
		}
		info := model.session.GetContextInfo()
		return showContextMsg{content: renderContextInfo(info)}
	}
}

func handleResumeCommand(model *TUIModel, args []string) tea.Cmd {
	// Immediately show the resume view with loading state
	showResumeCmd := model.content.ShowResume([]Session{})
	model.content.resume.SetLoading(true)

	// Load sessions in the background
	loadCmd := func() tea.Msg {
		if model == nil || model.config == nil {
			return sessionResumeErrorMsg{err: fmt.Errorf("resume unavailable: missing configuration")}
		}

		if !model.config.Session.Enabled {
			return showContextMsg{content: "Session resume is disabled in configuration."}
		}

		repoInfo := GetRepoInfo()

		currentBranch := branchSlugOrDefault(repoInfo.Branch)
		if model.sessionStore == nil ||
			model.sessionStore.ProjectRoot != repoInfo.ProjectRoot ||
			model.sessionStore.Branch != currentBranch {

			maxSessions := 50
			if model.config.Session.MaxSessions > 0 {
				maxSessions = model.config.Session.MaxSessions
			}

			maxAgeDays := 30
			if model.config.Session.MaxAgeDays > 0 {
				maxAgeDays = model.config.Session.MaxAgeDays
			}

			store, err := NewSessionStore(model.db, repoInfo, maxSessions, maxAgeDays)
			if err != nil {
				return sessionResumeErrorMsg{err: fmt.Errorf("failed to initialize session store: %w", err)}
			}

			if model.sessionStore != nil {
				model.sessionStore.Close()
			}
			model.sessionStore = store
		}

		if model.sessionStore == nil {
			return sessionResumeErrorMsg{err: fmt.Errorf("session store not initialized")}
		}

		listLimit := 0
		if model.config.Session.ListLimit >= 0 {
			listLimit = model.config.Session.ListLimit
		}

		sessions, err := model.sessionStore.ListSessions(listLimit)
		if err != nil {
			return sessionResumeErrorMsg{err: fmt.Errorf("failed to list sessions: %w", err)}
		}

		return sessionsLoadedMsg{sessions: sessions}
	}

	// Return both commands - show view immediately, then load data
	return tea.Batch(showResumeCmd, loadCmd)
}

func handleExportCommand(model *TUIModel, args []string) tea.Cmd {
	if model.session == nil {
		return func() tea.Msg {
			return showContextMsg{content: "No active session to export. Start a conversation first."}
		}
	}

	// Determine export type from args, default to conversation
	exportType := ExportTypeConversation
	if len(args) > 0 {
		switch args[0] {
		case "full":
			exportType = ExportTypeFull
		case "conversation":
			exportType = ExportTypeConversation
		default:
			model.commandLine.AddToast(fmt.Sprintf("Unknown export type '%s'. Use 'full' or 'conversation'", args[0]), "error", 3000)
			return nil
		}
	}

	// Export the session to a file
	filepath, err := exportSession(model.session, exportType)
	if err != nil {
		return func() tea.Msg {
			return showContextMsg{content: fmt.Sprintf("Export failed: %v", err)}
		}
	}

	// Open the file in the editor using ExecProcess
	cmd := openInEditor(filepath)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return showContextMsg{content: fmt.Sprintf("Editor exited with error: %v", err)}
		}
		model.commandLine.AddToast(fmt.Sprintf("Conversation exported successfully (%s).", exportType), "success", 3000)
		return nil
	})
}

func handleInitCommand(model *TUIModel, args []string) tea.Cmd {
	if model.session == nil {
		return func() tea.Msg {
			return showContextMsg{content: "No model connection. Use :models to configure a model and start chatting."}
		}
	}

	return func() tea.Msg {
		// Check for uncommitted changes before proceeding
		if hasUncommittedChanges() {
			return showContextMsg{content: "FAILED: Please commit or stash your changes and run again"}
		}

		// Collect messages to display after clearing history
		var initialMessages []string

		// Check if user wants to clear and regenerate everything
		clearMode := len(args) > 0 && args[0] == "clear"

		// Ensure .agents directory exists
		if err := os.MkdirAll(".agents/sandbox", 0o755); err != nil {
			return showContextMsg{content: fmt.Sprintf("Error creating .agents directory: %v", err)}
		}

		// In clear mode, remove all infrastructure files first
		if clearMode {
			filesToRemove := []string{
				"AGENTS.md",
				"Justfile",
				".agents/asimi.conf",
				".agents/sandbox/bashrc",
				".agents/sandbox/Dockerfile",
			}
			for _, file := range filesToRemove {
				os.Remove(file) // Ignore errors - file might not exist
			}
			initialMessages = append(initialMessages, "Cleared existing infrastructure files. Starting fresh initialization...\n")
		}

		// Always write embedded files (asimi.conf and bashrc)
		// These are simple files we can provide directly
		projectConfigPath := ".agents/asimi.conf"
		if _, err := os.Stat(projectConfigPath); os.IsNotExist(err) || clearMode {
			if err := os.WriteFile(projectConfigPath, []byte(defaultConfContent), 0o644); err != nil {
				return showContextMsg{content: fmt.Sprintf("Error writing project config file %s: %v", projectConfigPath, err)}
			}
			if !clearMode {
				initialMessages = append(initialMessages, fmt.Sprintf("Initialized %s from embedded default\n", projectConfigPath))
			}
		}

		bashrcPath := ".agents/sandbox/bashrc"
		if _, err := os.Stat(bashrcPath); os.IsNotExist(err) || clearMode {
			if err := os.WriteFile(bashrcPath, []byte(sandboxBashrc), 0o644); err != nil {
				return showContextMsg{content: fmt.Sprintf("Error writing bashrc file %s: %v", bashrcPath, err)}
			}
			if !clearMode {
				initialMessages = append(initialMessages, fmt.Sprintf("Initialized %s from embedded default\n", bashrcPath))
			}
		}

		// Determine the agents file name - use CLAUDE.md if it exists, otherwise AGENTS.md
		agentsFile := "AGENTS.md"
		if _, err := os.Stat("CLAUDE.md"); err == nil {
			agentsFile = "CLAUDE.md"
			// Update the config file to set agents_file
			if err := SetProjectConfig("session", "agents_file", agentsFile); err != nil {
				initialMessages = append(initialMessages, fmt.Sprintf("Warning: Could not update config with agents_file: %v\n", err))
			} else {
				initialMessages = append(initialMessages, fmt.Sprintf("Detected %s, configured as agents file\n", agentsFile))
			}
		}

		// Check for missing infrastructure files (agents file, Justfile, Dockerfile)
		missingFiles := checkMissingInfraFiles(agentsFile)

		if len(missingFiles) == 0 && !clearMode {
			return showContextMsg{content: strings.Join([]string{treeMidPrefix + "All Asimi's files already exist:",
				fmt.Sprintf(treeMidPrefix+"✓ %s", agentsFile),
				treeMidPrefix + "✓ Justfile",
				treeMidPrefix + "✓ .agents/sandbox/Dockerfile",
				treeMidPrefix + "✓ .agents/sandbox/bashrc",
				treeMidPrefix + "✓ .agents/asimi.conf",
				treeMidPrefix,
				treeFinalPrefix + "Use `:init clear` to remove and regenerate them."}, "\n")}
		}

		// Show missing files message (if not in clear mode)
		if len(missingFiles) > 0 && !clearMode {
			var message strings.Builder
			message.WriteString("Missing infrastructure files detected:\n")
			for _, file := range missingFiles {
				message.WriteString(fmt.Sprintf("✗ %s\n", file))
			}
			message.WriteString("\nStarting initialization process. Embrace yourself for much approvals as there's no sandbox yet.\n")
			initialMessages = append(initialMessages, message.String())
		}

		// Get the project slug from RepoInfo
		slug := GetRepoInfo().Slug

		// Extract just the project name from the slug (last part after /)
		// For "owner/repo" or "host/owner/repo", we want just "repo"
		projectName := slug
		if idx := strings.LastIndex(slug, "/"); idx >= 0 {
			projectName = slug[idx+1:]
		}

		// Prepare template data
		templateData := InitTemplateData{
			ProjectName:  projectName,
			ProjectSlug:  slug,
			MissingFiles: missingFiles,
			ClearMode:    clearMode,
			AgentsFile:   agentsFile,
		}

		// Parse and execute the template
		tmpl, err := template.New("init").Parse(initializePrompt)
		if err != nil {
			return showContextMsg{content: fmt.Sprintf("Error parsing initialization template: %v", err)}
		}

		var initPrompt bytes.Buffer
		if err := tmpl.Execute(&initPrompt, templateData); err != nil {
			return showContextMsg{content: fmt.Sprintf("Error executing initialization template: %v", err)}
		}

		// Capture the original shell runner before switching to host mode
		// This will be the container runner that we'll use for running tests
		shellRunnerMu.RLock()
		originalRunner := currentShellRunner
		shellRunnerMu.RUnlock()

		// Send the initialization prompt to the session with guardrails
		// Use host shell runner for init to avoid container issues
		return startConversationMsg{
			prompt:          initPrompt.String(),
			clearHistory:    true,
			initialMessages: initialMessages,
			onStreamComplete: func(model *TUIModel) tea.Cmd {
				return verifyInit(model, originalRunner)
			},
			RunOnHost: true,
		}
	}
}

// startConversationMsg is sent to start a new conversation with optional guardrails
type startConversationMsg struct {
	prompt           string
	clearHistory     bool
	initialMessages  []string                // Messages to display after clearing history (before streaming starts)
	onStreamComplete func(*TUIModel) tea.Cmd // Optional guardrail function to run after stream completes
	RunOnHost        bool                    // When true, use host shell runner instead of podman
}

// verifyInit runs validation checks after init completes
// It accepts a containerRunner parameter to run tests in the container
func verifyInit(model *TUIModel, containerRunner shellRunner) tea.Cmd {
	return verifyInitWithRetry(model, containerRunner, 0)
}

// verifyInitWithRetry is the internal implementation with retry tracking
func verifyInitWithRetry(model *TUIModel, containerRunner shellRunner, retryCount int) tea.Cmd {
	const maxRetries = 5 // Maximum number of retry attempts

	return func() tea.Msg {
		slog.Debug("verifyInitWithRetry called", "retryCount", retryCount, "containerRunner", containerRunner)

		// Reload configuration on retry attempts to pick up any changes made by the LLM
		// (e.g., modifications to .agents/asimi.conf, Dockerfile, etc.)
		slog.Debug("Reloading configuration for retry attempt", "retryCount", retryCount)
		err := model.config.ReloadProjectConf()
		if err != nil {
			slog.Warn("Failed to reload config during verifyInit retry", "error", err)
		} else {
			slog.Debug("Configuration reloaded successfully")
		}

		var results []string

		report := func(message string) {
			results = append(results, message)
			if program != nil {
				program.Send(showContextMsg{content: treeMidPrefix + message})
			}
		}

		// Send initial message
		if program != nil {
			msg := "\n" + systemPrefix + "Testing infrastructure"
			if retryCount > 0 {
				msg += fmt.Sprintf(" (attempt %d/%d)", retryCount+1, maxRetries+1)
			}
			program.Send(showContextMsg{content: msg})
		}

		slog.Debug("Starting verification checks", "retryCount", retryCount)

		// Determine agents file from config
		agentsFile := "AGENTS.md"
		if model.config != nil && model.config.Session.AgentsFile != "" {
			agentsFile = model.config.Session.AgentsFile
		}

		// Check required files exist - collect all failures before returning
		slog.Debug("Checking required files")
		agentsMdExists := checkFileExists(agentsFile, agentsFile+" created", report)
		justfileExists := checkFileExists("Justfile", "Justfile created", report)

		if !agentsMdExists || !justfileExists {
			slog.Debug("Required files missing, handling failure")
			return handleVerificationFailure(model, containerRunner, retryCount, maxRetries, results)
		}

		// Run build-sandbox
		slog.Debug("Running build-sandbox")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if !runBuildSandbox(ctx, report, &results) {
			slog.Debug("build-sandbox failed, handling failure")
			return handleVerificationFailure(model, containerRunner, retryCount, maxRetries, results)
		}

		// After build-sandbox succeeds, reinitialize the shell runner to get a fresh container
		// This is necessary because the previous container was closed and the image was rebuilt
		slog.Debug("Reinitializing shell runner after build-sandbox")
		initShellRunner(model.config)
		containerRunner = getShellRunner()
		slog.Debug("Shell runner reinitialized", "containerRunner", containerRunner)

		// Run smoke test in container
		slog.Debug("Running smoke test in container", "containerRunner", containerRunner)
		if !runSmokeTest(ctx, containerRunner, report) {
			slog.Debug("Smoke test failed, handling failure")
			return handleVerificationFailure(model, containerRunner, retryCount, maxRetries, results)
		}

		// Run tests on host
		slog.Debug("Running tests on host")
		if !runHostTests(ctx, report, &results) {
			slog.Debug("Host tests failed, handling failure")
			return handleVerificationFailure(model, containerRunner, retryCount, maxRetries, results)
		}

		// Run tests in container
		slog.Debug("Running tests in container")
		if !runContainerTests(ctx, containerRunner, report, &results) {
			slog.Debug("Container tests failed, handling failure")
			return handleVerificationFailure(model, containerRunner, retryCount, maxRetries, results)
		}

		// All tests passed - stage the files
		slog.Debug("All verification tests passed! Staging files...")

		// Stage all added/changed files in .agents/ and root infrastructure files
		filesToStage := []string{
			agentsFile,
			"Justfile",
			".agents/",
		}

		ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel2()

		for _, file := range filesToStage {
			result, err := hostRun(ctx2, RunInShellInput{
				Command:     fmt.Sprintf("git add %s", file),
				Description: fmt.Sprintf("Staging %s", file),
			})

			if err != nil || result.ExitCode != "0" {
				slog.Warn("Failed to stage file", "file", file, "error", err, "exitCode", result.ExitCode)
				report(fmt.Sprintf("⚠️  Failed to stage %s", file))
			} else {
				slog.Debug("Staged file successfully", "file", file)
			}
		}

		if program != nil {
			m := []string{
				treeMidPrefix + checkPrefix + "Verified!",
				treeMidPrefix + strings.Join(filesToStage, ", ") + " staged",
				treeFinalPrefix + "Start fresh with `:new` and review project's recipes with `:!just -l`"}

			program.Send(showContextMsg{content: strings.Join(m, "\n")})
		}
		return nil
	}
}

// checkFileExists checks if a file exists and reports the result
func checkFileExists(filename, successMsg string, report func(string)) bool {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		report(fmt.Sprintf("❌ %s was not created", filename))
		return false
	}
	report(checkPrefix + " " + successMsg)
	return true
}

// runBuildSandbox runs the build-sandbox command on the host
func runBuildSandbox(ctx context.Context, report func(string), results *[]string) bool {
	report("$ just build-sandbox # on host")
	result, err := hostRun(ctx, RunInShellInput{
		Command:     "just build-sandbox",
		Description: "Building infrastructure files",
	})

	if err != nil || result.ExitCode != "0" {
		report(fmt.Sprintf("❌ just build-sandbox failed (exit code: %s)", result.ExitCode))
		if result.Output != "" {
			*results = append(*results, fmt.Sprintf("   Output: %s", strings.TrimSpace(result.Output)))
		}
		return false
	}

	report(checkPrefix + " just build-sandbox completed successfully")
	return true
}

// runSmokeTest runs a basic smoke test in the container
func runSmokeTest(ctx context.Context, containerRunner shellRunner, report func(string)) bool {
	slog.Debug("runSmokeTest called", "containerRunner", containerRunner)
	if containerRunner == nil {
		slog.Error("containerRunner is nil in runSmokeTest")
		report("❌ Container runner is not available")
		return false
	}

	slog.Debug("Calling containerRunner.Run for smoke test")
	result, err := containerRunner.Run(ctx, RunInShellInput{
		Command:     "uname",
		Description: "Running smoke test in container",
	})

	slog.Debug("Smoke test result", "output", result.Output, "exitCode", result.ExitCode, "error", err)
	isLinux := strings.Contains(result.Output, "Linux")
	if err != nil || result.ExitCode != "0" || !isLinux {
		report(fmt.Sprintf("❌ Sandbox smoke test failed (exit code: %s)", result.ExitCode))
		return false
	}

	report(checkPrefix + " Sandbox smoke test passed")
	return true
}

// runHostTests runs the test suite on the host
func runHostTests(ctx context.Context, report func(string), results *[]string) bool {
	report("$ just test # on host")
	result, err := hostRun(ctx, RunInShellInput{
		Command:     "just test",
		Description: "Running tests on host",
	})

	if err != nil || result.ExitCode != "0" {
		report(fmt.Sprintf("❌ just test on host failed (exit code: %s)", result.ExitCode))
		if result.Output != "" {
			*results = append(*results, fmt.Sprintf("   Output: %s", strings.TrimSpace(result.Output)))
		}
		return false
	}

	report(checkPrefix + " just test on host passed")
	return true
}

// runContainerTests runs the test suite in the container
func runContainerTests(ctx context.Context, containerRunner shellRunner, report func(string), results *[]string) bool {
	report("$ just test # in container")
	result, err := containerRunner.Run(ctx, RunInShellInput{
		Command:     "just test",
		Description: "Running tests in container",
	})

	if err != nil || result.ExitCode != "0" {
		report(fmt.Sprintf("❌ just test in container failed (exit code: %s)", result.ExitCode))
		if result.Output != "" {
			*results = append(*results, fmt.Sprintf("   Output: %s", strings.TrimSpace(result.Output)))
		}
		return false
	}

	report(checkPrefix + " just test in container passed")
	return true
}

// handleVerificationFailure handles the case when verification fails
func handleVerificationFailure(model *TUIModel, containerRunner shellRunner, retryCount, maxRetries int, results []string) tea.Msg {
	slog.Debug("In verifyInit - handleVerificationFailure", "hasErrors", true, "messages", results, "retryCount", retryCount)

	// Check if we've exceeded the maximum retry count
	if retryCount >= maxRetries {
		slog.Debug("Max retries exceeded, giving up", "retryCount", retryCount, "maxRetries", maxRetries)
		var failureMsg strings.Builder
		failureMsg.WriteString(fmt.Sprintf("\n%s❌ Initialization failed after %d attempts.\n", systemPrefix, maxRetries+1))
		failureMsg.WriteString(treeMidPrefix + "The following issues could not be resolved:\n")
		for _, result := range results {
			failureMsg.WriteString(treeMidPrefix + result + "\n")
		}
		failureMsg.WriteString(treeFinalPrefix + "For help check out the humans in Asimi's github discussions")
		return showContextMsg{content: failureMsg.String()}
	}

	// Stop and remove the container so the next attempt will rebuild with fixes
	slog.Debug("Attempting to close container before retry", "containerRunner", containerRunner, "retryCount", retryCount)
	if containerRunner != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		slog.Debug("Calling containerRunner.Close()")
		if err := containerRunner.Close(ctx); err != nil {
			slog.Warn("Failed to close container during verifyInit", "error", err)
		} else {
			slog.Debug("Container closed successfully")
		}
	} else {
		slog.Debug("containerRunner is nil, skipping close")
	}

	// Build message for LLM to fix the issues
	var message strings.Builder
	message.WriteString("Issues found verifying initialization.\n" +
		"Please review the failures below and provide a fix.\n" +
		"If files need to be modified, use the appropriate tools.\n")
	for _, result := range results {
		message.WriteString(result + "\n")
	}

	// Return a startConversationMsg to send this message to the LLM session
	return startConversationMsg{
		prompt:       message.String(),
		clearHistory: false,
		RunOnHost:    true,
		onStreamComplete: func(model *TUIModel) tea.Cmd {
			return verifyInitWithRetry(model, containerRunner, retryCount+1)
		},
	}
}

// checkMissingInfraFiles checks which infrastructure files are missing
func checkMissingInfraFiles(agentsFile string) []string {
	var missing []string

	for _, file := range []string{
		agentsFile,
		"Justfile",
		".agents/asimi.conf",
		".agents/sandbox/Dockerfile",
		".agents/sandbox/bashrc",
	} {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			missing = append(missing, file)
		}
	}

	return missing
}

func handleCompactCommand(model *TUIModel, args []string) tea.Cmd {
	if model.session == nil {
		return func() tea.Msg {
			return showContextMsg{content: "No active session to compact. Start a conversation first."}
		}
	}

	return func() tea.Msg {
		// Check if there's enough conversation to compact
		if len(model.session.Messages) <= 2 {
			return showContextMsg{content: "Not enough conversation history to compact. Continue chatting first."}
		}

		// Show compacting message
		if program != nil {
			program.Send(showContextMsg{content: systemPrefix + "Compacting conversation history...\n" + treeFinalPrefix + "This may take a moment as we summarize the conversation."})
		}

		// Send the compact request
		return compactConversationMsg{}
	}
}

func handleScrollTopCommand(model *TUIModel, args []string) tea.Cmd {
	if model == nil || model.content.GetActiveView() != ViewChat {
		return nil
	}
	model.content.Chat.ScrollToTop()
	return nil
}

type updateCheckMsg struct {
	hasUpdate bool
	latest    string
	err       error
}

type updateCompleteMsg struct {
	success bool
	err     error
}

func handleUpdateCommand(model *TUIModel, args []string) tea.Cmd {
	return func() tea.Msg {
		// Show checking message
		if program != nil {
			program.Send(showContextMsg{content: systemPrefix + "Checking for updates..."})
		}

		// Check for updates
		latest, hasUpdate, err := CheckForUpdates(version)
		if err != nil {
			return updateCheckMsg{hasUpdate: false, err: err}
		}

		if !hasUpdate {
			return updateCheckMsg{hasUpdate: false, latest: version}
		}

		// Update available - show confirmation
		return updateCheckMsg{hasUpdate: true, latest: latest.Version.String()}
	}
}

func handleUpdateConfirm(model *TUIModel) tea.Cmd {
	return func() tea.Msg {
		// Show updating message
		if program != nil {
			program.Send(showContextMsg{content: systemPrefix + "Downloading and installing update...\n" + treeFinalPrefix + "This may take a moment."})
		}

		// Perform update
		err := SelfUpdate(version)
		if err != nil {
			return updateCompleteMsg{success: false, err: err}
		}

		return updateCompleteMsg{success: true}
	}
}
