package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/storage"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestFixturesSetup verifies the test fixtures work correctly
func TestFixturesSetup(t *testing.T) {
	// Skip if demo1 doesn't exist
	if _, err := os.Stat("../../tests/demo1"); os.IsNotExist(err) {
		t.Skip("tests/demo1 not found, skipping fixture tests")
	}

	t.Run("SetupTestProject creates isolated copy", func(t *testing.T) {
		project := SetupTestProject(t, "../../tests/demo1")

		// Verify temp directory was created
		if project.Dir == "" {
			t.Fatal("Project directory is empty")
		}

		// Verify it's a different path from original
		if project.Dir == project.Original {
			t.Error("Project dir should not equal original")
		}

		// Verify directory exists
		if _, err := os.Stat(project.Dir); os.IsNotExist(err) {
			t.Errorf("Project directory does not exist: %s", project.Dir)
		}

		// Verify git was initialized
		gitDir := filepath.Join(project.Dir, ".git")
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			t.Error("Git was not initialized in project directory")
		}

		// Verify no uncommitted changes (clean state)
		if project.HasUncommittedChanges() {
			t.Error("Project should start with no uncommitted changes")
		}
	})

	t.Run("SetupTestProject excludes node_modules", func(t *testing.T) {
		project := SetupTestProject(t, "../../tests/demo1")

		// node_modules should not be copied
		nodeModules := filepath.Join(project.Dir, "node_modules")
		if _, err := os.Stat(nodeModules); err == nil {
			t.Error("node_modules should not be copied to test project")
		}
	})

	t.Run("CreateUncommittedChange creates dirty state", func(t *testing.T) {
		project := SetupTestProject(t, "../../tests/demo1")

		// Initially clean
		if project.HasUncommittedChanges() {
			t.Error("Project should start clean")
		}

		// Create uncommitted change
		if err := project.CreateUncommittedChange(); err != nil {
			t.Fatalf("Failed to create uncommitted change: %v", err)
		}

		// Now should be dirty
		if !project.HasUncommittedChanges() {
			t.Error("Project should have uncommitted changes after CreateUncommittedChange")
		}
	})

	t.Run("FileExists and WriteFile work correctly", func(t *testing.T) {
		project := SetupTestProject(t, "../../tests/demo1")

		// File that doesn't exist
		if project.FileExists("nonexistent_file.txt") {
			t.Error("FileExists should return false for nonexistent file")
		}

		// Create a new file
		if err := project.WriteFile("test_file.txt", "test content"); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		// Now it should exist
		if !project.FileExists("test_file.txt") {
			t.Error("FileExists should return true after WriteFile")
		}

		// Read it back
		content, err := project.ReadFile("test_file.txt")
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if content != "test content" {
			t.Errorf("File content mismatch: got %q, want %q", content, "test content")
		}
	})

	t.Run("GetInfraFileStatus returns correct status", func(t *testing.T) {
		project := SetupTestProject(t, "../../tests/demo1")

		// Initially no infra files should exist
		status := project.GetInfraFileStatus()

		for file, exists := range status {
			if exists {
				t.Errorf("Infrastructure file %s should not exist initially", file)
			}
		}

		// Create some infra files
		project.WriteFile("AGENTS.md", "# Test")
		project.WriteFile(".agents/asimi.conf", "# config")

		// Check again
		status = project.GetInfraFileStatus()
		if !status["AGENTS.md"] {
			t.Error("AGENTS.md should exist after creation")
		}
		if !status[".agents/asimi.conf"] {
			t.Error(".agents/asimi.conf should exist after creation")
		}
	})

	t.Run("Chdir changes and restores directory", func(t *testing.T) {
		project := SetupTestProject(t, "../../tests/demo1")

		originalDir, _ := os.Getwd()

		// Change to project dir
		restore := project.Chdir()

		// Verify we're in the project dir
		currentDir, _ := os.Getwd()
		if currentDir != project.Dir {
			t.Errorf("Should be in project dir: got %s, want %s", currentDir, project.Dir)
		}

		// Restore
		restore()

		// Verify we're back
		currentDir, _ = os.Getwd()
		if currentDir != originalDir {
			t.Errorf("Should be back in original dir: got %s, want %s", currentDir, originalDir)
		}
	})
}

// TestMockLLM verifies the mock LLM works correctly
func TestMockLLM(t *testing.T) {
	t.Run("Returns default response", func(t *testing.T) {
		mock := NewMockLLM().WithDefaultResponse("default answer")

		responseChan := mock.SendPrompt(nil, "any prompt")
		response := <-responseChan

		if response != "default answer" {
			t.Errorf("Expected default answer, got %q", response)
		}
	})

	t.Run("Returns pattern-matched response", func(t *testing.T) {
		mock := NewMockLLM().
			WithResponse("create files", "Files created!").
			WithDefaultResponse("default")

		// Pattern match
		responseChan := mock.SendPrompt(nil, "Please create files for the project")
		response := <-responseChan
		if response != "Files created!" {
			t.Errorf("Expected pattern match, got %q", response)
		}

		// No pattern match
		responseChan = mock.SendPrompt(nil, "Something else")
		response = <-responseChan
		if response != "default" {
			t.Errorf("Expected default, got %q", response)
		}
	})

	t.Run("Returns sequential responses", func(t *testing.T) {
		mock := NewMockLLM().WithSequentialResponses("first", "second", "third")

		for i, expected := range []string{"first", "second", "third"} {
			responseChan := mock.SendPrompt(nil, "prompt")
			response := <-responseChan
			if response != expected {
				t.Errorf("Response %d: expected %q, got %q", i, expected, response)
			}
		}
	})

	t.Run("Records prompts", func(t *testing.T) {
		mock := NewMockLLM()

		// Need to wait for the goroutine to complete
		<-mock.SendPrompt(nil, "first prompt")
		<-mock.SendPrompt(nil, "second prompt")

		if err := mock.AssertCallCount(2); err != nil {
			t.Error(err)
		}

		if err := mock.AssertPromptContains("first"); err != nil {
			t.Error(err)
		}

		history := mock.GetPromptHistory()
		if len(history) != 2 {
			t.Errorf("Expected 2 prompts in history, got %d", len(history))
		}
	})

	t.Run("LastPrompt returns most recent", func(t *testing.T) {
		mock := NewMockLLM()

		// Need to wait for each goroutine to complete
		<-mock.SendPrompt(nil, "first")
		<-mock.SendPrompt(nil, "second")
		<-mock.SendPrompt(nil, "last one")

		if mock.LastPrompt() != "last one" {
			t.Errorf("Expected 'last one', got %q", mock.LastPrompt())
		}
	})
}

// TestMockRunner verifies the mock runner works correctly
func TestMockRunner(t *testing.T) {
	t.Run("Returns default result", func(t *testing.T) {
		mock := NewMockHostRunner().WithDefaultResult(TestSuccessResult("default output"))

		result, err := mock.Run(nil, RunShellCommandInput{Command: "any command"})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if result.Output != "default output" {
			t.Errorf("Expected 'default output', got %q", result.Output)
		}
		if result.ExitCode != "0" {
			t.Errorf("Expected exit code 0, got %q", result.ExitCode)
		}
	})

	t.Run("Returns pattern-matched result", func(t *testing.T) {
		mock := NewMockHostRunner().
			WithCommandResult("just test", TestSuccessResult("Tests passed")).
			WithCommandResult("just build", TestFailResult("Build failed"))

		result, _ := mock.Run(nil, RunShellCommandInput{Command: "just test"})
		if result.Output != "Tests passed" {
			t.Errorf("Expected 'Tests passed', got %q", result.Output)
		}

		result, _ = mock.Run(nil, RunShellCommandInput{Command: "just build"})
		if result.ExitCode != "1" {
			t.Errorf("Expected exit code 1, got %q", result.ExitCode)
		}
	})

	t.Run("Records commands", func(t *testing.T) {
		mock := NewMockHostRunner()

		mock.Run(nil, RunShellCommandInput{Command: "first command"})
		mock.Run(nil, RunShellCommandInput{Command: "second command"})

		if err := mock.AssertCallCount(2); err != nil {
			t.Error(err)
		}

		if err := mock.AssertCommandExecuted("first"); err != nil {
			t.Error(err)
		}

		if err := mock.AssertCommandNotExecuted("nonexistent"); err != nil {
			t.Error(err)
		}
	})

	t.Run("PodmanRunner includes uname response", func(t *testing.T) {
		mock := NewMockPodmanRunner()

		result, _ := mock.Run(nil, RunShellCommandInput{Command: "uname"})
		if result.Output != "Linux" {
			t.Errorf("Expected 'Linux', got %q", result.Output)
		}

		if mock.RunnerType() != "podman" {
			t.Errorf("Expected runner type 'podman', got %q", mock.RunnerType())
		}
	})

	t.Run("Sequential results", func(t *testing.T) {
		mock := NewMockHostRunner().WithSequentialResults(
			TestSuccessResult("first"),
			TestFailResult("second"),
			TestExitResult("third", "42"),
		)

		result, _ := mock.Run(nil, RunShellCommandInput{Command: "cmd"})
		if result.Output != "first" || result.ExitCode != "0" {
			t.Errorf("First result wrong: %+v", result)
		}

		result, _ = mock.Run(nil, RunShellCommandInput{Command: "cmd"})
		if result.Output != "second" || result.ExitCode != "1" {
			t.Errorf("Second result wrong: %+v", result)
		}

		result, _ = mock.Run(nil, RunShellCommandInput{Command: "cmd"})
		if result.Output != "third" || result.ExitCode != "42" {
			t.Errorf("Third result wrong: %+v", result)
		}
	})

	t.Run("FailAfterN", func(t *testing.T) {
		mock := NewMockHostRunner().WithFailAfterN(2, TestFailResult("failed after 2"))

		// First two should succeed (default)
		result, _ := mock.Run(nil, RunShellCommandInput{Command: "cmd"})
		if result.ExitCode != "0" {
			t.Errorf("First command should succeed")
		}

		result, _ = mock.Run(nil, RunShellCommandInput{Command: "cmd"})
		if result.ExitCode != "0" {
			t.Errorf("Second command should succeed")
		}

		// Third should fail
		result, _ = mock.Run(nil, RunShellCommandInput{Command: "cmd"})
		if result.ExitCode != "1" || result.Output != "failed after 2" {
			t.Errorf("Third command should fail: %+v", result)
		}
	})
}

// setupE2ETestDB creates a test database with all required migrations
func setupE2ETestDB(t *testing.T) *gorm.DB {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "e2e_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := tmpDir + "/test.db"
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// Run migrations for all shogunate tables
	err = db.AutoMigrate(
		&storage.Edict{},
		&storage.ZhengmingRequest{},
		&storage.TianEvent{},
		&storage.TianEventDLQ{},
		&storage.Ling{},
		&storage.ForgeManifest{},
		&storage.JudgeVerdict{},
		&storage.CensorPrecedent{},
		&storage.MarshalIncident{},
		&storage.RulerCouncil{},
		&storage.RitualGuardCheckpoint{},
	)
	if err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	return db
}

// TestChancellorInvokesForgeToCreateFile verifies the end-to-end flow:
// Chancellor uses invoke_minister tool -> Forge receives task -> Forge creates a file
func TestChancellorInvokesForgeToCreateFile(t *testing.T) {
	// Skip if demo1 doesn't exist
	if _, err := os.Stat("../../tests/demo1"); os.IsNotExist(err) {
		t.Skip("tests/demo1 not found, skipping e2e test")
	}

	t.Run("E2E: Chancellor invokes Forge with mock LLM to create file", func(t *testing.T) {
		// This is the TRUE end-to-end test:
		// 1. Chancellor invokes Forge via invoke_minister
		// 2. Forge creates a session with a mock LLM
		// 3. Mock LLM returns a write_file tool call
		// 4. Forge's session executes the tool
		// 5. File is created on disk

		// Setup: Create isolated test project
		project := SetupTestProject(t, "../../tests/demo1")

		// Change to project directory for the test
		restore := project.Chdir()
		defer restore()

		// Verify hello_world.go doesn't exist initially
		if project.FileExists("hello_world.go") {
			t.Fatal("hello_world.go should not exist initially")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		db := setupE2ETestDB(t)

		// Create a mock LLM that will return a write_file tool call
		helloWorldContent := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
		mockLLM := NewMockLLMModel().
			WithWriteFileToolCall("hello_world.go", helloWorldContent).
			WithTextResponse("I have created the hello_world.go file.")

		// Create Chancellor (without LLM - it just routes tasks)
		base := shogunate.NewMinisterBase(db, nil, nil, repo.RepoInfo{}, nil)
		chancellor := shogunate.NewChancellor(base)

		// Create Forge with the mock LLM
		forge := shogunate.NewForge(base)

		// Configure Forge with the mock LLM via SetMinisterConfig
		forgeConfig := &shogunate.SessionConfig{}
		forge.SetMinisterConfig(mockLLM, forgeConfig, repo.RepoInfo{})

		// Create a minimal Shogunate and wire Chancellor to it
		s := shogunate.NewShogunate(db, nil, nil)
		s.Chancellor = chancellor
		s.Forge = forge
		chancellor.SetShogunate(s)

		// Create an edict for the test
		edictID := "test-e2e-file-creation"
		if err := chancellor.CreateEdict(edictID, "Create hello_world.go via Forge"); err != nil {
			t.Fatalf("Failed to create edict: %v", err)
		}

		// Start the Forge's Run loop
		go forge.Run(ctx)

		// Get the invoke_minister tool from Chancellor
		chancellorTools := chancellor.Tools(nil)
		var invokeMinisterTool shogunate.Tool
		for _, tool := range chancellorTools {
			if tool.Name() == "invoke_minister" {
				invokeMinisterTool = tool
				break
			}
		}
		if invokeMinisterTool == nil {
			t.Fatal("invoke_minister tool not found in Chancellor tools")
		}

		// Invoke Forge with the task (synchronous blocking - waits for Forge to complete)
		taskInput := fmt.Sprintf(`{"minister_id": "forge", "edict_id": "%s", "task": "Create a hello_world.go file with a simple main function that prints Hello, World!"}`, edictID)
		result, err := invokeMinisterTool.Call(ctx, taskInput)
		if err != nil {
			t.Fatalf("Failed to invoke minister: %v", err)
		}
		t.Logf("invoke_minister result: %s", result)

		// Parse the result to verify completion
		var response struct {
			MinisterID string `json:"minister_id"`
			EdictID    string `json:"edict_id"`
			Status     string `json:"status"`
			Sealed     bool   `json:"sealed"`
			Output     string `json:"output"`
		}
		if err := json.Unmarshal([]byte(result), &response); err != nil {
			t.Fatalf("Failed to parse result: %v", err)
		}

		if response.Status != "completed" {
			t.Errorf("Expected status 'completed', got %s", response.Status)
		}
		if response.EdictID != edictID {
			t.Errorf("Expected EdictID %s, got %s", edictID, response.EdictID)
		}
		if response.MinisterID != "forge" {
			t.Errorf("Expected MinisterID 'forge', got %s", response.MinisterID)
		}
		t.Logf("Forge completed task. Output: %s", response.Output)

		// THE KEY ASSERTION: Verify the file was actually created
		if !project.FileExists("hello_world.go") {
			t.Fatal("hello_world.go should exist after Forge processed the task through mock LLM")
		}

		// Verify the content is correct
		content, err := project.ReadFile("hello_world.go")
		if err != nil {
			t.Fatalf("Failed to read hello_world.go: %v", err)
		}
		if content != helloWorldContent {
			t.Errorf("File content mismatch:\ngot: %q\nwant: %q", content, helloWorldContent)
		}

		t.Log("SUCCESS: True end-to-end test passed - Chancellor -> Forge -> MockLLM -> write_file -> file created")
	})
}

// TestEdictSessionReuse verifies that subsequent prompts reuse the same edict and session
// This is a critical test for conversation continuity - every prompt should NOT start a new edict
func TestEdictSessionReuse(t *testing.T) {
	t.Run("Chancellor reuses session for same edict ID", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		db := setupE2ETestDB(t)

		// Create a mock LLM that tracks calls
		mockLLM := NewMockLLMModel().
			WithTextResponse("First response").
			WithTextResponse("Second response").
			WithTextResponse("Third response")

		// Create Chancellor with mock LLM
		base := shogunate.NewMinisterBase(db, mockLLM, nil, repo.RepoInfo{}, nil)
		chancellor := shogunate.NewChancellor(base)

		// Start Chancellor's Run loop
		go chancellor.Run(ctx)

		// --- First prompt: should create new edict ---
		replyChan1 := make(chan shogunate.PromptReply, 100)
		env1 := &shogunate.EdictEnvelope{
			Prompt:    "First prompt",
			EdictID:   "", // Empty = create new edict
			ReplyChan: replyChan1,
		}
		chancellor.Edicts <- env1

		// Collect edict ID from first response
		var firstEdictID string
		for reply := range replyChan1 {
			if reply.Type == shogunate.ReplyStreamStart {
				firstEdictID = reply.EdictID
				t.Logf("First prompt created edict: %s", firstEdictID)
			}
		}

		if firstEdictID == "" {
			t.Fatal("First prompt should have created an edict with an ID")
		}

		// Verify edict was created in database
		var edictCount int64
		db.Model(&storage.Edict{}).Count(&edictCount)
		if edictCount != 1 {
			t.Errorf("Expected 1 edict in database, got %d", edictCount)
		}

		// --- Second prompt: should reuse same edict ---
		replyChan2 := make(chan shogunate.PromptReply, 100)
		env2 := &shogunate.EdictEnvelope{
			Prompt:    "Second prompt",
			EdictID:   firstEdictID, // Pass the edict ID to continue conversation
			ReplyChan: replyChan2,
		}
		chancellor.Edicts <- env2

		// Collect edict ID from second response
		var secondEdictID string
		for reply := range replyChan2 {
			if reply.Type == shogunate.ReplyStreamStart {
				secondEdictID = reply.EdictID
				t.Logf("Second prompt used edict: %s", secondEdictID)
			}
		}

		// Verify same edict ID was used
		if secondEdictID != firstEdictID {
			t.Errorf("Second prompt should reuse same edict ID: got %s, want %s", secondEdictID, firstEdictID)
		}

		// Verify no new edict was created
		db.Model(&storage.Edict{}).Count(&edictCount)
		if edictCount != 1 {
			t.Errorf("Should still have 1 edict, got %d (new edict was incorrectly created)", edictCount)
		}

		// --- Third prompt: should still reuse same edict ---
		replyChan3 := make(chan shogunate.PromptReply, 100)
		env3 := &shogunate.EdictEnvelope{
			Prompt:    "Third prompt",
			EdictID:   firstEdictID,
			ReplyChan: replyChan3,
		}
		chancellor.Edicts <- env3

		var thirdEdictID string
		for reply := range replyChan3 {
			if reply.Type == shogunate.ReplyStreamStart {
				thirdEdictID = reply.EdictID
			}
		}

		if thirdEdictID != firstEdictID {
			t.Errorf("Third prompt should reuse same edict ID: got %s, want %s", thirdEdictID, firstEdictID)
		}

		// Verify the mock LLM received all 3 prompts (same session = accumulated messages)
		recordedMsgs := mockLLM.GetRecordedMessages()
		t.Logf("Mock LLM received %d GenerateContent calls", len(recordedMsgs))

		// With session reuse, messages accumulate. Each call should have more messages.
		// First call: system + user (2 messages minimum)
		// Second call: system + user1 + assistant1 + user2 (4 messages minimum)
		// Third call: system + user1 + assistant1 + user2 + assistant2 + user3 (6 messages minimum)
		if len(recordedMsgs) != 3 {
			t.Errorf("Expected 3 LLM calls, got %d", len(recordedMsgs))
		} else {
			// First call should have minimal messages
			firstCallMsgCount := len(recordedMsgs[0])
			// Second call should have MORE messages (session reuse)
			secondCallMsgCount := len(recordedMsgs[1])
			// Third call should have even MORE messages
			thirdCallMsgCount := len(recordedMsgs[2])

			t.Logf("Message counts: first=%d, second=%d, third=%d",
				firstCallMsgCount, secondCallMsgCount, thirdCallMsgCount)

			if secondCallMsgCount <= firstCallMsgCount {
				t.Errorf("Session not reused: second call should have more messages than first (got %d <= %d)",
					secondCallMsgCount, firstCallMsgCount)
			}
			if thirdCallMsgCount <= secondCallMsgCount {
				t.Errorf("Session not reused: third call should have more messages than second (got %d <= %d)",
					thirdCallMsgCount, secondCallMsgCount)
			}
		}

		t.Log("SUCCESS: Edict and session reuse verified")
	})

	t.Run("Empty edict ID creates new edict each time", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		db := setupE2ETestDB(t)

		mockLLM := NewMockLLMModel().
			WithTextResponse("Response 1").
			WithTextResponse("Response 2")

		base := shogunate.NewMinisterBase(db, mockLLM, nil, repo.RepoInfo{}, nil)
		chancellor := shogunate.NewChancellor(base)

		go chancellor.Run(ctx)

		// First prompt with empty edict ID
		replyChan1 := make(chan shogunate.PromptReply, 100)
		chancellor.Edicts <- &shogunate.EdictEnvelope{
			Prompt:    "First prompt",
			EdictID:   "",
			ReplyChan: replyChan1,
		}

		var firstEdictID string
		for reply := range replyChan1 {
			if reply.Type == shogunate.ReplyStreamStart {
				firstEdictID = reply.EdictID
			}
		}

		// Second prompt also with empty edict ID - should create NEW edict
		replyChan2 := make(chan shogunate.PromptReply, 100)
		chancellor.Edicts <- &shogunate.EdictEnvelope{
			Prompt:    "Second prompt",
			EdictID:   "", // Empty = new edict
			ReplyChan: replyChan2,
		}

		var secondEdictID string
		for reply := range replyChan2 {
			if reply.Type == shogunate.ReplyStreamStart {
				secondEdictID = reply.EdictID
			}
		}

		// Verify different edict IDs
		if secondEdictID == firstEdictID {
			t.Errorf("Empty edict ID should create new edict, but got same ID: %s", firstEdictID)
		}

		// Verify 2 edicts exist
		var edictCount int64
		db.Model(&storage.Edict{}).Count(&edictCount)
		if edictCount != 2 {
			t.Errorf("Expected 2 edicts, got %d", edictCount)
		}

		t.Logf("SUCCESS: Empty edict ID correctly creates new edicts (%s, %s)", firstEdictID, secondEdictID)
	})
}

// TestEdictIDFlowThroughStreamStartMsg verifies that the EdictID correctly flows
// through the StreamStartMsg from Chancellor to TUI and is properly captured.
// This tests the exact message type used in the TUI communication.
func TestEdictIDFlowThroughStreamStartMsg(t *testing.T) {
	t.Run("EdictID flows through PromptReply to StreamStartMsg", func(t *testing.T) {
		// This test verifies the data flow:
		// 1. Chancellor sends PromptReply{EdictID: "..."} via channel
		// 2. TUI goroutine receives it and creates StreamStartMsg{EdictID: "..."}
		// 3. StreamStartMsg correctly carries the EdictID

		// Simulate what happens in receiveChancellorReplies
		replyChan := make(chan shogunate.PromptReply, 10)

		// Chancellor sends a ReplyStreamStart with edict ID
		expectedEdictID := "test-edict-12345"
		replyChan <- shogunate.PromptReply{
			Type:    shogunate.ReplyStreamStart,
			EdictID: expectedEdictID,
		}
		close(replyChan)

		// Simulate what receiveChancellorReplies does
		reply := <-replyChan

		// Verify the EdictID is in the PromptReply
		if reply.EdictID != expectedEdictID {
			t.Errorf("PromptReply.EdictID mismatch: got %q, want %q", reply.EdictID, expectedEdictID)
		}

		// Create StreamStartMsg as the TUI does
		streamStartMsg := shogunate.StreamStartMsg{EdictID: reply.EdictID}

		// Verify StreamStartMsg carries the EdictID
		if streamStartMsg.EdictID != expectedEdictID {
			t.Errorf("StreamStartMsg.EdictID mismatch: got %q, want %q", streamStartMsg.EdictID, expectedEdictID)
		}

		t.Logf("SUCCESS: EdictID correctly flows from PromptReply to StreamStartMsg: %s", expectedEdictID)
	})

	t.Run("Full Chancellor-to-Reply flow preserves EdictID", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		db := setupE2ETestDB(t)

		mockLLM := NewMockLLMModel().WithTextResponse("Test response")

		base := shogunate.NewMinisterBase(db, mockLLM, nil, repo.RepoInfo{}, nil)
		chancellor := shogunate.NewChancellor(base)

		go chancellor.Run(ctx)

		// Send prompt with empty edict ID (should create new)
		replyChan := make(chan shogunate.PromptReply, 100)
		chancellor.Edicts <- &shogunate.EdictEnvelope{
			Prompt:    "Test prompt",
			EdictID:   "",
			ReplyChan: replyChan,
		}

		// Collect ALL replies and find the StreamStart
		var replies []shogunate.PromptReply
		for reply := range replyChan {
			replies = append(replies, reply)
		}

		// Find StreamStart reply
		var streamStartReply *shogunate.PromptReply
		for i, r := range replies {
			if r.Type == shogunate.ReplyStreamStart {
				streamStartReply = &replies[i]
				break
			}
		}

		if streamStartReply == nil {
			t.Fatal("No ReplyStreamStart received from Chancellor")
		}

		if streamStartReply.EdictID == "" {
			t.Error("ReplyStreamStart has empty EdictID - this is the bug!")
			t.Log("All replies received:")
			for i, r := range replies {
				t.Logf("  [%d] Type=%d, EdictID=%q, Content=%q", i, r.Type, r.EdictID, r.Content)
			}
		} else {
			t.Logf("SUCCESS: ReplyStreamStart has EdictID: %s", streamStartReply.EdictID)
		}
	})
}

// TestTUIModelActiveEdictIDPersistence simulates the Bubble Tea model update flow
// to verify that activeEdictID persists correctly across Update calls.
// This tests the VALUE receiver pattern used by TUIModel.
func TestTUIModelActiveEdictIDPersistence(t *testing.T) {
	t.Run("activeEdictID persists through value receiver chain", func(t *testing.T) {
		// Simulate the Bubble Tea pattern:
		// 1. Model has a value receiver Update method
		// 2. Update modifies the copy and returns it
		// 3. Bubble Tea uses the returned model for the next Update

		type MockTUIModel struct {
			activeEdictID string
		}

		// Value receiver (like TUIModel.Update)
		handleStreamStart := func(m MockTUIModel, edictID string) MockTUIModel {
			m.activeEdictID = edictID
			return m
		}

		// Value receiver that reads activeEdictID (like handleCustomMessages -> sendPromptToChancellor)
		getEdictID := func(m MockTUIModel) string {
			return m.activeEdictID
		}

		// Initial model with empty activeEdictID
		model := MockTUIModel{activeEdictID: ""}

		// Verify initial state
		if getEdictID(model) != "" {
			t.Error("Initial activeEdictID should be empty")
		}

		// Simulate StreamStartMsg handling - this returns a NEW model
		model = handleStreamStart(model, "test-edict-001")

		// Verify the model was updated (Bubble Tea uses the returned model)
		if getEdictID(model) != "test-edict-001" {
			t.Errorf("activeEdictID should be 'test-edict-001', got %q", getEdictID(model))
		}

		// Simulate second Update call - should still have the edict ID
		if getEdictID(model) != "test-edict-001" {
			t.Errorf("activeEdictID should persist, got %q", getEdictID(model))
		}

		t.Log("SUCCESS: Value receiver pattern correctly persists state when returned model is reused")
	})

	t.Run("activeEdictID NOT persisted if returned model is ignored", func(t *testing.T) {
		type MockTUIModel struct {
			activeEdictID string
		}

		handleStreamStart := func(m MockTUIModel, edictID string) MockTUIModel {
			m.activeEdictID = edictID
			return m
		}

		getEdictID := func(m MockTUIModel) string {
			return m.activeEdictID
		}

		model := MockTUIModel{activeEdictID: ""}

		// BUG PATTERN: Call handleStreamStart but IGNORE the returned model
		_ = handleStreamStart(model, "test-edict-001")

		// The original model is NOT updated because we didn't use the return value
		if getEdictID(model) != "" {
			t.Error("BUG: original model should NOT be updated when return value is ignored")
		}

		t.Log("Confirmed: ignoring return value from value-receiver method loses state changes")
	})
}

// TestProgressCapture verifies the progress capture works correctly
func TestProgressCapture(t *testing.T) {
	t.Run("Captures messages", func(t *testing.T) {
		capture := NewProgressCapture()

		capture.Capture(0, "step1", "running", "Starting step 1")
		capture.Capture(0, "step1", "completed", "Step 1 done")
		capture.Capture(1, "step2", "running", "Starting step 2")

		if capture.Count() != 3 {
			t.Errorf("Expected 3 messages, got %d", capture.Count())
		}

		if !capture.HasMessage("Starting step 1") {
			t.Error("Should have message 'Starting step 1'")
		}

		if !capture.HasStepWithStatus("step1", "completed") {
			t.Error("step1 should have completed status")
		}
	})

	t.Run("GetStepOrder returns execution order", func(t *testing.T) {
		capture := NewProgressCapture()

		capture.Capture(0, "first", "running", "")
		capture.Capture(1, "second", "running", "")
		capture.Capture(0, "first", "completed", "") // Duplicate step shouldn't re-add
		capture.Capture(2, "third", "running", "")

		order := capture.GetStepOrder()
		expected := []string{"first", "second", "third"}

		if len(order) != len(expected) {
			t.Errorf("Expected %d steps, got %d", len(expected), len(order))
		}

		for i, step := range expected {
			if i >= len(order) || order[i] != step {
				t.Errorf("Step %d: expected %q, got %q", i, step, order[i])
			}
		}
	})

	t.Run("GetMessagesForStep filters correctly", func(t *testing.T) {
		capture := NewProgressCapture()

		capture.Capture(0, "step1", "running", "msg1")
		capture.Capture(1, "step2", "running", "msg2")
		capture.Capture(0, "step1", "completed", "msg3")

		step1Messages := capture.GetMessagesForStep("step1")
		if len(step1Messages) != 2 {
			t.Errorf("Expected 2 messages for step1, got %d", len(step1Messages))
		}
	})
}
