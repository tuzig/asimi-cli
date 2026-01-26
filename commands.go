package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/internal/utils"
	"github.com/afittestide/asimi/shogunate"
	tea "github.com/charmbracelet/bubbletea"
)

//go:embed prompts/init.tmpl
var initializePrompt string

//go:embed prompts/compact.txt
var compactPrompt string

//go:embed dotagents/sandbox/bashrc
var sandboxBashrc string

//go:embed dotagents/Justfile
var defaultJustfile string

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
	model.shogunate.SaveSession(model.activeEdictID)

	model.sessionActive = true

	// Clear the chat instead of creating a new component to avoid re-initializing the markdown renderer
	model.content.Chat.Clear()

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
	model.shogunate.SaveSession(model.activeEdictID)
	return tea.Quit
}

func handleContextCommand(model *TUIModel, args []string) tea.Cmd {
	return func() tea.Msg {
		if model.shogunate.Session(model.activeEdictID) == nil {
			return showSystemMsg("No active session. Use :models to configure a model and start chatting.")
		}
		info := model.shogunate.GetContextInfo(model.activeEdictID)
		return showContextMsg{content: renderContextInfo(info)}
	}
}

func handleResumeCommand(model *TUIModel, args []string) tea.Cmd {
	// Immediately show the resume view with loading state
	showResumeCmd := model.content.ShowResume([]shogunate.Session{})
	model.content.resume.SetLoading(true)

	// Load sessions in the background
	loadCmd := func() tea.Msg {
		if model == nil || model.config == nil {
			return sessionResumeErrorMsg{err: fmt.Errorf("resume unavailable: missing configuration")}
		}

		if !model.config.Session.Enabled {
			return showSystemMsg("Session resume is disabled in configuration.")
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
	session := model.shogunate.Session(model.activeEdictID)
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
	hostRunner := GetHostRunner()
	if hostRunner == nil {
		report("❌ Host runner not available")
		*results = append(*results, "Host runner not initialized")
		return false
	}
	result, err := hostRunner.Run(ctx, runners.Input{
		Command:        "just build-sandbox",
		Description:    "Building infrastructure files",
		BypassApproval: true,
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
		Command:        "uname",
		Description:    "Running smoke test in container",
		BypassApproval: true,
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
	hostRunner := GetHostRunner()
	if hostRunner == nil {
		report("❌ Host runner not available")
		*results = append(*results, "Host runner not initialized")
		return false
	}
	result, err := hostRunner.Run(ctx, runners.Input{
		Command:        "just test",
		Description:    "Running tests on host",
		BypassApproval: true,
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
		Command:        "just test",
		Description:    "Running tests in container",
		BypassApproval: true,
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
	session := model.shogunate.Session(model.activeEdictID)
	if session == nil {
		return func() tea.Msg {
			return showSystemMsg("No active session to compact. Start a conversation first.")
		}
	}

	return func() tea.Msg {
		// Check if there's enough conversation to compact
		if len(session.Messages) <= 2 {
			return showSystemMsg("Not enough conversation history to compact. Continue chatting first.")
		}

		// Show compacting message
		if program != nil {
			msg := utils.NewMsgBlockBuilder(systemPrefix)
			msg.WriteLn("Compacting conversation history...")
			msg.WriteLn("This may take a moment as we summarize the conversation.")
			program.Send(showContextMsg{content: msg.String()})
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
			program.Send(showSystemMsg("Checking for updates..."))
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
			msg := utils.NewMsgBlockBuilder(systemPrefix)
			msg.WriteLn("Downloading and installing update...")
			msg.WriteLn("This may take a moment.")
			program.Send(showContextMsg{content: msg.String()})
		}

		// Perform update
		err := SelfUpdate(version)
		if err != nil {
			return updateCompleteMsg{success: false, err: err}
		}

		return updateCompleteMsg{success: true}
	}
}
