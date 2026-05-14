package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/internal/utils"
	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/storage"
	tea "github.com/charmbracelet/bubbletea"
)

// Verify that shogunate.Session implements ExportableSession
var _ ExportableSession = (*shogunate.Session)(nil)

//go:embed prompts/init.tmpl
var initializePrompt string

//go:embed prompts/compact.txt
var compactPrompt string

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

func showSystemMsg(msg string) tea.Msg {
	return showContextMsg{content: systemPrefix + msg}
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
	registry.RegisterCommand("logout", "Logout from current provider and clear credentials", handleLogoutCommand)
	registry.RegisterCommand("tabnew", "Open a new tab (usage: :tabnew [hunting|<minister>|ritual <run_id>])", handleTabNewCommand)
	registry.RegisterCommand("tabclose", "Close the current tab", handleTabCloseCommand)
	registry.RegisterCommand("seal", "Grant Ruler's seal to an edict (usage: :seal [edict_id] | :seal to select)", handleSealCommand)

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

	// Clear the chat instead of creating a new component to avoid re-initializing the markdown renderer
	model.tabs.Content().Chat.Clear()

	// Reset the appropriate minister session based on active tab
	if model.shogunate != nil {
		tab := model.tabs.ActiveTab()
		if model.shogunate.HasMinister(string(tab.Type)) {
			model.shogunate.ResetMinisterSession(string(tab.Type))
		} else {
			slog.Debug("Failed to get session", "miniter", tab.Type)
		}
	}

	// Use the generic startConversationMsg to reset the session
	// The tryUpgradeToSandbox flag tells the handler to attempt upgrading
	// from host to sandbox runner asynchronously
	return func() tea.Msg {
		return startConversationMsg{
			clearHistory:        true,
			tryUpgradeToSandbox: true,
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
		state, ok := model.currentSessionState()
		if !ok {
			return showSystemMsg("No active session. Use :models to configure a model and start chatting.")
		}
		ci := state.ContextInfo
		info := ContextInfo{
			Model:              ci.Model,
			TotalTokens:        ci.TotalTokens,
			UsedTokens:         ci.UsedTokens,
			SystemPromptTokens: ci.SystemPromptTokens,
			SystemToolsTokens:  ci.SystemToolsTokens,
			MemoryFilesTokens:  ci.MemoryFilesTokens,
			MessagesTokens:     ci.MessagesTokens,
			FreeTokens:         ci.FreeTokens,
			AutocompactBuffer:  ci.AutocompactBuffer,
		}
		return showContextMsg{content: renderContextInfo(info)}
	}
}

func handleResumeCommand(model *TUIModel, args []string) tea.Cmd {
	// Immediately show the resume view with loading state
	showResumeCmd := model.tabs.Content().ShowResume([]shogunate.Session{})
	model.tabs.Content().resume.SetLoading(true)

	// Load sessions in the background
	loadCmd := func() tea.Msg {
		if model == nil || model.config == nil {
			return sessionResumeErrorMsg{err: fmt.Errorf("resume unavailable: missing configuration")}
		}

		if !model.config.Session.Enabled {
			return showSystemMsg("Session resume is disabled in configuration.")
		}

		repoInfo := repo.GetRepoInfo()
		currentBranch := repoInfo.BranchSlugOrDefault()
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

		currentTabType := string(model.tabs.ActiveTab().Type)

		sessions, err := model.sessionStore.ListSessions(listLimit, currentTabType)
		if err != nil {
			return sessionResumeErrorMsg{err: fmt.Errorf("failed to list sessions: %w", err)}
		}

		return sessionsLoadedMsg{sessions: sessions}
	}

	// Return both commands - show view immediately, then load data
	return tea.Batch(showResumeCmd, loadCmd)
}

func handleExportCommand(model *TUIModel, args []string) tea.Cmd {
	var session ExportableSession

	if model.shogunate != nil {
		if exp, err := model.shogunate.GetSessionExport(model.currentTabTarget()); err == nil && exp != nil {
			session = exp
			slog.Debug("using Shogunate session for export", "edict_id", model.currentEdictKey.ID)
		}
	}

	if session == nil {
		return func() tea.Msg {
			return showSystemMsg("No active session to export. Start a conversation first.")
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
	filepath, err := exportSession(session, exportType)
	if err != nil {
		return func() tea.Msg {
			return showSystemMsg(fmt.Sprintf("Export failed: %v", err))
		}
	}

	// Open the file in the editor using ExecProcess
	cmd := openInEditor(filepath)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return showSystemMsg(fmt.Sprintf("Editor exited with error: %v", err))
		}
		model.commandLine.AddToast(fmt.Sprintf("Conversation exported successfully (%s).", exportType), "success", 3000)
		return nil
	})
}

func handleInitCommand(model *TUIModel, args []string) tea.Cmd {
	// Clear mode runs even without an active session so users can recover from
	// a broken init without first having a working LLM connection.
	if len(args) > 0 && args[0] == "clear" {
		errors := clearAsimiFiles(model)
		if len(errors) > 0 {
			return func() tea.Msg {
				return showContextMsg{content: fmt.Sprintf("Failed to clear files: %s", strings.Join(errors, ", "))}
			}
		}
	}

	if model.shogunate == nil {
		return func() tea.Msg {
			return showContextMsg{content: "No model connection available. Please ensure a session is active."}
		}
	}

	// Check if project name is already set in config
	if model.config.Shogunate.Project == "" {
		// Project name is missing - prompt user for it
		return model.commandLine.EnterInputMode("Enter project name (e.g., owner/repo):")
	}

	// Project name exists, proceed with creating the edict
	return createInitEdict(model)
}

// createInitEdict creates the edict for project-init ritual
func createInitEdict(model *TUIModel) tea.Cmd {
	// Use CreateEdictSilent: we already know the ritual (project-init) and will
	// dispatch it directly below. Publishing EventEdictCreated would make the
	// chancellor LLM also try to enact a ritual for this edict, starting it twice.
	edict, err := model.shogunate.CreateEdictSilent("", "Initialize project with Asimi agent configuration")
	if err != nil {
		return func() tea.Msg {
			return showContextMsg{content: fmt.Sprintf("Failed to create edict: %v", err)}
		}
	}
	payload := storage.JSON{
		"ritual_name": "project-init",
		"edict_id":    edict.ID,
	}
	model.raiseShogunateEvent(storage.EventRitualEnacted, payload)
	return nil
}

// clearAsimiFiles removes infrastructure files to allow a fresh init
func clearAsimiFiles(model *TUIModel) []string {
	files := []string{
		".agents",
		"AGENTS.md",
		"Justfile",
	}

	var errors []string
	for _, file := range files {
		if err := os.RemoveAll(file); err != nil && !os.IsNotExist(err) {
			errors = append(errors, fmt.Sprintf("%s: %v", file, err))
		}
	}
	return errors
}

// handleProjectNameInput handles the user's project name input
func handleProjectNameInput(model *TUIModel, projectName string) tea.Cmd {
	if projectName == "" {
		// User cancelled, abort the init
		return func() tea.Msg {
			return showContextMsg{content: "Project initialization cancelled."}
		}
	}

	// Seed .agents/asimi.conf from the embedded default template before writing
	// the project name. Without this, SetProjectConfig would create a stub file
	// containing only the [shogunate] section, and the ritual's template-seeding
	// step would later skip the file (since it exists), leaving the user with a
	// near-empty config missing every default section.
	if err := shogunate.EnsureProjectConfig(); err != nil {
		return func() tea.Msg {
			return showContextMsg{content: fmt.Sprintf("Failed to seed project config: %v", err)}
		}
	}

	// Save project name to .agents/asimi.conf
	if err := config.SetProjectConfig("shogunate", "project", projectName); err != nil {
		return func() tea.Msg {
			return showContextMsg{content: fmt.Sprintf("Failed to save project name: %v", err)}
		}
	}

	// Reload project config to pick up the new project name
	if err := model.config.ReloadProjectConf(); err != nil {
		slog.Warn("Failed to reload config after setting project name", "error", err)
	}

	// Proceed with creating the init edict
	return createInitEdict(model)
}

// startConversationMsg is sent to start a new conversation with optional guardrails
type startConversationMsg struct {
	prompt              string
	clearHistory        bool
	initialMessages     []string                // Messages to display after clearing history (before streaming starts)
	onStreamComplete    func(*TUIModel) tea.Cmd // Optional guardrail function to run after stream completes
	RunOnHost           bool                    // When true, use host shell runner instead of podman
	tryUpgradeToSandbox bool                    // When true, attempt to upgrade from host to sandbox runner
}

// sandboxUpgradeMsg is sent after attempting to upgrade from host to sandbox runner
type sandboxUpgradeMsg struct {
	upgraded bool
}

// verifyInit runs validation checks after init completes
// It accepts a containerRunner parameter to run tests in the container
func verifyInit(model *TUIModel, containerRunner runners.Runner) tea.Cmd {
	return verifyInitWithRetry(model, containerRunner, 0)
}

// verifyInitWithRetry is the internal implementation with retry tracking
func verifyInitWithRetry(model *TUIModel, containerRunner runners.Runner, retryCount int) tea.Cmd {
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
		// TODO: should the repoInfo be part of status?
		containerRunner = runners.InitShellRunner(&model.config.Sandbox, *model.status.repoInfo)
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
			result, err := runners.HostRun(ctx2, runners.Input{
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
			msg := NewChatMsgBuilder(systemPrefix)
			msg.WriteString(checkPrefix).WriteLn(" Verified!")
			msg.WriteLn(strings.Join(filesToStage, ", ") + " staged")
			msg.WriteLn("Start fresh with `:new` and review project's recipes with `:!just -l`")

			program.Send(showContextMsg{content: msg.String()})
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
	result, err := runners.HostRun(ctx, runners.Input{
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
func runSmokeTest(ctx context.Context, containerRunner runners.Runner, report func(string)) bool {
	slog.Debug("runSmokeTest called", "containerRunner", containerRunner)
	if containerRunner == nil {
		slog.Error("containerRunner is nil in runSmokeTest")
		report("❌ Container runner is not available")
		return false
	}

	slog.Debug("Calling containerRunner.Run for smoke test")
	result, err := containerRunner.Run(ctx, runners.Input{
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
	result, err := runners.HostRun(ctx, runners.Input{
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
func runContainerTests(ctx context.Context, containerRunner runners.Runner, report func(string), results *[]string) bool {
	report("$ just test # in container")
	result, err := containerRunner.Run(ctx, runners.Input{
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
func handleVerificationFailure(model *TUIModel, containerRunner runners.Runner, retryCount, maxRetries int, results []string) tea.Msg {
	slog.Debug("In verifyInit - handleVerificationFailure", "hasErrors", true, "messages", results, "retryCount", retryCount)

	// Check if we've exceeded the maximum retry count
	if retryCount >= maxRetries {
		slog.Debug("Max retries exceeded, giving up", "retryCount", retryCount, "maxRetries", maxRetries)
		msg := NewChatMsgBuilder(systemPrefix)
		msg.WriteLnf("❌ Initialization failed after %d attempts.", maxRetries+1)
		msg.WriteLn("The following issues could not be resolved:")
		for _, result := range results {
			msg.WriteLn(result)
		}
		msg.WriteLn("For help check out the humans in Asimi's github discussions")
		return showContextMsg{content: msg.String()}
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
	// TODO: refactor this as this is not really start of conversation but a hack
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
	state, ok := model.currentSessionState()
	messageCount := 0
	if ok {
		messageCount = state.MessageCount
	}

	if messageCount == 0 {
		return func() tea.Msg {
			return showSystemMsg("No active session to compact. Start a conversation first.")
		}
	}

	return func() tea.Msg {
		// Check if there's enough conversation to compact
		if messageCount <= 2 {
			return showSystemMsg("Not enough conversation history to compact. Continue chatting first.")
		}

		// Send the compact request (message shown in tui.go handler)
		return compactConversationMsg{}
	}
}

func handleScrollTopCommand(model *TUIModel, args []string) tea.Cmd {
	if model == nil || model.tabs.Content().GetActiveView() != ViewChat {
		return nil
	}
	model.tabs.Content().Chat.ScrollToTop()
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
			program.Send(showSystemMsg("Checking for updates..."))
		}

		// Check for updates
		latest, hasUpdate, err := utils.CheckForUpdates()
		if err != nil {
			return updateCheckMsg{hasUpdate: false, err: err}
		}

		if !hasUpdate {
			return updateCheckMsg{hasUpdate: false, latest: utils.AsimiVersion}
		}

		// Update available - show confirmation
		return updateCheckMsg{hasUpdate: true, latest: latest.Version.String()}
	}
}

func handleUpdateConfirm(model *TUIModel) tea.Cmd {
	return func() tea.Msg {
		// Show updating message
		if program != nil {
			msg := NewChatMsgBuilder(systemPrefix)
			msg.WriteLn("Downloading and installing update...")
			msg.WriteLn("This may take a moment.")
			program.Send(showContextMsg{content: msg.String()})
		}

		// Perform update
		err := SelfUpdate(utils.AsimiVersion)
		if err != nil {
			return updateCompleteMsg{success: false, err: err}
		}

		return updateCompleteMsg{success: true}
	}
}

func handleTabNewCommand(model *TUIModel, args []string) tea.Cmd {
	if len(args) == 0 {
		// Default: open a Sage tab
		model.tabs.Add("Sage", "sage", "sage")
		model.commandLine.AddToast("Opened Sage tab", "success", time.Second*2)
		return nil
	}

	target := args[0]
	switch target {
	case "sage":
		model.tabs.Add("Sage", "sage", "sage")
		model.commandLine.AddToast("Opened Sage tab", "success", time.Second*2)
	case "court":
		model.tabs.Add("Court", TabCourt, "court")
		model.commandLine.AddToast("Opened Court dashboard", "success", time.Second*2)
		return nil
	case "ritual":
		if len(args) < 2 {
			model.commandLine.AddToast("Usage: :tabnew ritual <run_id>", "error", time.Second*3)
			return nil
		}
		runID := args[1]
		model.tabs.Add("Ritual:"+runID, "ritual", runID)
		model.commandLine.AddToast(fmt.Sprintf("Opened Ritual tab: %s", runID), "success", time.Second*2)
	default:
		// Treat as minister name
		if model.shogunate != nil && model.shogunate.HasMinister(target) {
			label := strings.ToUpper(target[:1]) + target[1:]
			model.tabs.Add(label, TabType(target), target)
			model.commandLine.AddToast(fmt.Sprintf("Opened %s tab", label), "success", time.Second*2)
		} else {
			model.commandLine.AddToast(fmt.Sprintf("Unknown minister: %s", target), "error", time.Second*3)
		}
	}
	return nil
}

func handleTabCloseCommand(model *TUIModel, args []string) tea.Cmd {
	err := model.tabs.Close()
	if err != nil {
		model.commandLine.AddToast(err.Error(), "error", time.Second*3)
		return nil
	}
	model.commandLine.AddToast("Tab closed", "success", time.Second*2)
	return nil
}

// handleSealCommand grants the Ruler's seal to an edict
func handleSealCommand(model *TUIModel, args []string) tea.Cmd {
	if model.shogunate == nil {
		return func() tea.Msg {
			return showSystemMsg("Shogunate not active - cannot grant seal")
		}
	}

	// If no args, show selection of pending edicts
	if len(args) == 0 {
		return func() tea.Msg {
			edicts, err := model.shogunate.ListActiveEdicts()
			if err != nil {
				return showSystemMsg(fmt.Sprintf("Failed to list pending edicts: %v", err))
			}
			return sealedEdictsLoadedMsg{edicts: edicts}
		}
	}

	// Parse arguments: :seal [edict_id] [notes]
	var edictID uint
	var notes string

	parsed, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return func() tea.Msg {
			return showSystemMsg(fmt.Sprintf("Invalid edict ID '%s': must be a number", args[0]))
		}
	}
	edictID = uint(parsed)
	if len(args) > 1 {
		notes = strings.Join(args[1:], " ")
	}

	// Build full EdictKey with username and project context
	key := model.shogunate.EdictKey(edictID)

	// Validate edict exists BEFORE seal lookup
	if _, err := model.shogunate.GetEdict(edictID); err != nil {
		return func() tea.Msg {
			return showSystemMsg(fmt.Sprintf("Edict not found %v", key))
		}
	}

	// Get current seals for the edict
	seals, err := model.shogunate.GetEdictSeals(key)
	if err != nil {
		return func() tea.Msg {
			return showSystemMsg(fmt.Sprintf("Failed to get seals for %d: %v", edictID, err))
		}
	}

	// Display seal chain status
	sealChainMsg := renderSealChain(seals, 60)

	// Check if Judge and Sage seals exist
	hasJudge := false
	hasSage := false
	hasRuler := false

	for _, seal := range seals {
		switch seal.MinisterID {
		case "judge":
			hasJudge = true
		case "sage":
			hasSage = true
		case "ruler":
			hasRuler = true
		}
	}

	// If Ruler seal already exists, inform user
	if hasRuler {
		return func() tea.Msg {
			return showSystemMsg(fmt.Sprintf("Ruler's seal already granted to %d\n%s", edictID, sealChainMsg))
		}
	}

	// Check prerequisites
	var missingSeals []string
	if !hasJudge {
		missingSeals = append(missingSeals, "Judge")
	}
	if !hasSage {
		missingSeals = append(missingSeals, "Sage")
	}

	if len(missingSeals) > 0 {
		// Enter YesNo mode to allow Ruler to override
		question := fmt.Sprintf("Edict is missing a seal by [%s], seal?", strings.Join(missingSeals, ", "))
		// Store pending seal state
		model.pendingSealOverride = &pendingSealOverride{
			edictID: edictID,
			notes:   notes,
		}
		return model.commandLine.EnterYesNoMode(question)
	}

	// All prerequisites met - grant Ruler's seal directly
	return grantRulerSealCmd(model, edictID, notes)
}

// grantRulerSealCmd creates a command that grants the Ruler's seal to an edict.
func grantRulerSealCmd(model *TUIModel, edictID uint, notes string) tea.Cmd {
	return func() tea.Msg {
		if err := model.shogunate.GrantRulerSeal(edictID, notes); err != nil {
			return showSystemMsg(fmt.Sprintf("Failed to grant Ruler's seal: %v", err))
		}
		return nil
	}
}
