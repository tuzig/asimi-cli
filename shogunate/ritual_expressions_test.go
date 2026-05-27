package shogunate

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
)

func TestStepDefRegistry(t *testing.T) {
	reg := NewStepDefRegistry()

	// Test matching built-in patterns
	tests := []struct {
		text    string
		wantKey string
		wantNil bool
	}{
		{"the edict details", "edict", false},
		{"the court status", "court_status", false},
		{"the manifests", "manifests", false},
		{"the verdicts", "verdicts", false},
		{"the precedents", "precedents", false},
		{"something unknown", "", true},
	}

	for _, tt := range tests {
		def, err := reg.Match(tt.text)
		if err != nil {
			t.Fatalf("Match(%q) error: %v", tt.text, err)
		}
		if tt.wantNil {
			if def != nil {
				t.Errorf("Match(%q) expected nil, got key=%q", tt.text, def.OutputKey)
			}
		} else {
			if def == nil {
				t.Errorf("Match(%q) expected key=%q, got nil", tt.text, tt.wantKey)
			} else if def.OutputKey != tt.wantKey {
				t.Errorf("Match(%q) expected key=%q, got %q", tt.text, tt.wantKey, def.OutputKey)
			}
		}
	}
}

func TestResolveStepDef(t *testing.T) {
	db := setupRitualTestDB(t)
	registry := NewRitualRegistry()
	runner := NewRitualRunner(registry, nil, nil, db, nil, nil, repo.RepoInfo{})

	// Test bash command resolution
	entry, err := runner.resolveStepDef("!just test")
	if err != nil {
		t.Fatalf("resolveStepDef('!just test') error: %v", err)
	}
	if entry.Kind != StepDefBash {
		t.Errorf("expected StepDefBash, got %d", entry.Kind)
	}
	if entry.Command != "just test" {
		t.Errorf("expected command 'just test', got %q", entry.Command)
	}

	// Test builtin resolution
	entry, err = runner.resolveStepDef("the edict details")
	if err != nil {
		t.Fatalf("resolveStepDef('the edict details') error: %v", err)
	}
	if entry.Kind != StepDefBuiltin {
		t.Errorf("expected StepDefBuiltin, got %d", entry.Kind)
	}
	if entry.Command != "get_edict" {
		t.Errorf("expected handler 'get_edict', got %q", entry.Command)
	}
	if entry.Key != "edict" {
		t.Errorf("expected key 'edict', got %q", entry.Key)
	}

	// Test unknown pattern
	_, err = runner.resolveStepDef("something that does not match")
	if err == nil {
		t.Error("expected error for unknown pattern")
	}
}

func TestRunGivenStep_Bash(t *testing.T) {
	db := setupRitualTestDB(t)
	registry := NewRitualRegistry()
	mockRunner := &mockCmdRunner{output: "diff output\n", exitCode: "0"}
	runner := NewRitualRunner(registry, nil, nil, db, mockRunner, nil, repo.RepoInfo{})

	exec := &RitualExecution{
		ID:         "test-exec",
		RitualName: "test",
		EdictID:    100,
	}

	entry := StepDefEntry{
		Kind:    StepDefBash,
		Key:     "git",
		Command: "git diff HEAD",
	}

	result, err := runner.runGivenStep(context.Background(), exec, entry)
	if err != nil {
		t.Fatalf("runGivenStep error: %v", err)
	}
	if result != "diff output\n" {
		t.Errorf("expected 'diff output\\n', got %q", result)
	}
}

func TestRunGivenStep_BashFailure(t *testing.T) {
	db := setupRitualTestDB(t)
	registry := NewRitualRegistry()
	failRunner := &mockCmdRunner{output: "FAIL\n", exitCode: "1"}
	runner := NewRitualRunner(registry, nil, nil, db, failRunner, nil, repo.RepoInfo{})

	exec := &RitualExecution{
		ID:         "test-exec",
		RitualName: "test",
		EdictID:    100,
	}

	entry := StepDefEntry{
		Kind:    StepDefBash,
		Key:     "test",
		Command: "exit 1",
	}

	_, err := runner.runGivenStep(context.Background(), exec, entry)
	if err == nil {
		t.Fatal("expected error for failing bash given step")
	}
	if !strings.Contains(err.Error(), "exit 1") {
		t.Errorf("expected exit code in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "FAIL") {
		t.Errorf("expected output in error, got: %v", err)
	}
}

func TestRunThenStep_Bash(t *testing.T) {
	db := setupRitualTestDB(t)
	registry := NewRitualRegistry()

	// Success case
	mockRunner := &mockCmdRunner{output: "ok\n", exitCode: "0"}
	runner := NewRitualRunner(registry, nil, nil, db, mockRunner, nil, repo.RepoInfo{})

	exec := &RitualExecution{
		ID:         "test-exec",
		RitualName: "test",
		EdictID:    100,
	}

	entry := StepDefEntry{
		Kind:    StepDefBash,
		Key:     "just",
		Command: "just test",
	}

	err := runner.runThenStep(context.Background(), exec, entry)
	if err != nil {
		t.Fatalf("runThenStep success case: %v", err)
	}

	// Failure case
	failRunner := &mockCmdRunner{output: "FAIL\n", exitCode: "1"}
	runner = NewRitualRunner(registry, nil, nil, db, failRunner, nil, repo.RepoInfo{})

	err = runner.runThenStep(context.Background(), exec, entry)
	if err == nil {
		t.Error("expected error for failing then step")
	}
	if !strings.Contains(err.Error(), "exit 1") {
		t.Errorf("expected exit code in error, got: %v", err)
	}
}

func TestRunThenStep_Multiple(t *testing.T) {
	db := setupRitualTestDB(t)

	// Create ritual with multiple then steps, second one fails
	ritual := &RitualDef{
		Name: "multi-then",
		Steps: []RitualStep{
			{
				Name:     "build",
				Minister: "forge",
				Task:     "build something",
				Then:     []string{"!echo check1", "!exit 1"},
			},
		},
	}

	registry := NewRitualRegistry()
	registry.Register(ritual)

	shogunate := newRitualTestShogunate(t, "build\n", nil)
	mockRunner := &mockCallCountRunner{
		results: []runners.Output{
			{Output: "ok\n", ExitCode: "0"},   // first then
			{Output: "FAIL\n", ExitCode: "1"}, // second then
		},
	}
	runner := NewRitualRunner(registry, shogunate.GetMinister, shogunate.PublishEvent, db, mockRunner, nil, repo.RepoInfo{})

	ctx := context.Background()
	exec, err := runner.Start(ctx, "multi-then", testEK(8), nil, nil)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}

	err = runner.Run(ctx, exec)
	if err == nil {
		t.Error("expected error from failing then step")
	}
	if !strings.Contains(err.Error(), "then") {
		t.Errorf("expected 'then' in error message, got: %v", err)
	}
}

func TestAwaitRulerSeal_StageManifestFiles(t *testing.T) {
	db := setupRitualTestDB(t)

	// Migrate edict and manifest tables
	if err := db.AutoMigrate(&storage.Edict{}, &storage.ForgeManifest{}); err != nil {
		t.Fatalf("failed to migrate edict/manifest tables: %v", err)
	}

	registry := NewRitualRegistry()
	runner := NewRitualRunner(registry, nil, nil, db, nil, nil, repo.RepoInfo{})

	// Create test edict
	edict := storage.Edict{
		ID:        1,
		SessionID: "test-session",
		Intent:    "Test manifest staging",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	// Create test manifests for 3 files
	manifests := []storage.ForgeManifest{
		{ManifestID: "manifest-1", EdictID: edict.ID, FilePath: "shogunate/ritual.go", Status: storage.ManifestForged},
		{ManifestID: "manifest-2", EdictID: edict.ID, FilePath: "shogunate/builtin_rituals.yaml", Status: storage.ManifestForged},
		{ManifestID: "manifest-3", EdictID: edict.ID, FilePath: "shogunate/chancellor.go", Status: storage.ManifestForged},
	}
	for i := range manifests {
		if err := db.Create(&manifests[i]).Error; err != nil {
			t.Fatalf("failed to create manifest %d: %v", i, err)
		}
	}

	// Mock runner to capture git add command
	var stagedFiles string
	mockRunner := &mockCmdRunner{
		output:   "",
		exitCode: "0",
		onRun: func(cmd string) {
			// Extract files from git add command
			if strings.HasPrefix(cmd, "git add ") {
				stagedFiles = strings.TrimPrefix(cmd, "git add ")
			}
		},
	}
	runner = NewRitualRunner(registry, nil, nil, db, mockRunner, nil, repo.RepoInfo{})

	// Create execution
	exec := &RitualExecution{
		ID:         "test-await-ruler-seal",
		RitualName: "swift-strike",
		EdictID:    edict.ID,
	}

	// Call await_ruler_seal handler
	err := runner.runThen(context.Background(), exec, "await_ruler_seal")
	if err != nil {
		t.Fatalf("runThen(await_ruler_seal) error: %v", err)
	}

	// Verify only manifest files were staged
	expectedFiles := "shogunate/ritual.go shogunate/builtin_rituals.yaml shogunate/chancellor.go"
	if stagedFiles != expectedFiles {
		t.Errorf("expected staged files %q, got %q", expectedFiles, stagedFiles)
	}
}

func TestAwaitRulerSeal_NoManifests(t *testing.T) {
	db := setupRitualTestDB(t)

	// Migrate edict and manifest tables
	if err := db.AutoMigrate(&storage.Edict{}, &storage.ForgeManifest{}); err != nil {
		t.Fatalf("failed to migrate edict/manifest tables: %v", err)
	}

	registry := NewRitualRegistry()

	// Create test edict with no manifests
	edict := storage.Edict{
		ID:        1,
		SessionID: "test-session",
		Intent:    "Test no manifests",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	// Mock runner - should not be called
	mockRunner := &mockCmdRunner{
		output:   "",
		exitCode: "0",
		onRun: func(cmd string) {
			t.Errorf("git add should not be called when no manifests exist, but got: %s", cmd)
		},
	}
	runner := NewRitualRunner(registry, nil, nil, db, mockRunner, nil, repo.RepoInfo{})

	exec := &RitualExecution{
		ID:         "test-await-ruler-seal-empty",
		RitualName: "swift-strike",
		EdictID:    edict.ID,
	}

	// Call await_ruler_seal handler - should not error
	err := runner.runThen(context.Background(), exec, "await_ruler_seal")
	if err != nil {
		t.Fatalf("runThen(await_ruler_seal) error with no manifests: %v", err)
	}
}

func TestCheckVerdictsPassed_AllApproved(t *testing.T) {
	db := setupRitualTestDB(t)

	// Ensure JudgeVerdict table exists for the verdict check query
	if err := db.AutoMigrate(&storage.JudgeVerdict{}); err != nil {
		t.Fatalf("Failed to migrate JudgeVerdict: %v", err)
	}

	// Create manifests with approved status
	manifest := storage.ForgeManifest{
		ManifestID: GenerateID("manifest", "1", "test", "file.go"),
		EdictID:    1,
		Username:   "testuser",
		Project:    "testproject",
		FilePath:   "file.go",
		Status:     storage.ManifestLive,
	}
	if err := db.Create(&manifest).Error; err != nil {
		t.Fatalf("Failed to create manifest: %v", err)
	}

	registry := NewRitualRegistry()
	runner := NewRitualRunner(registry, nil, nil, db, nil, slog.Default(), repo.RepoInfo{})

	exec := &RitualExecution{
		EdictID:  1,
		Username: "testuser",
		Project:  "testproject",
	}

	err := runner.runThen(context.Background(), exec, "check_verdicts_passed")
	if err != nil {
		t.Errorf("Expected no error when all manifests approved, got: %v", err)
	}
}

// TestCheckVerdictsPassed_SomeRejected verifies the handler fails when any manifest is rejected
func TestCheckVerdictsPassed_SomeRejected(t *testing.T) {
	db := setupRitualTestDB(t)

	// Ensure JudgeVerdict table exists for the verdict check query
	if err := db.AutoMigrate(&storage.JudgeVerdict{}); err != nil {
		t.Fatalf("Failed to migrate JudgeVerdict: %v", err)
	}

	// Create one approved and one rejected manifest
	forged := storage.ForgeManifest{
		ManifestID: GenerateID("manifest", "1", "test", "good.go"),
		EdictID:    1,
		Username:   "testuser",
		Project:    "testproject",
		FilePath:   "good.go",
		Status:     storage.ManifestLive,
	}
	rejected := storage.ForgeManifest{
		ManifestID: GenerateID("manifest", "1", "test", "bad.go"),
		EdictID:    1,
		Username:   "testuser",
		Project:    "testproject",
		FilePath:   "bad.go",
		Status:     storage.ManifestRejected,
	}
	if err := db.Create(&forged).Error; err != nil {
		t.Fatalf("Failed to create forged manifest: %v", err)
	}
	if err := db.Create(&rejected).Error; err != nil {
		t.Fatalf("Failed to create rejected manifest: %v", err)
	}

	registry := NewRitualRegistry()
	runner := NewRitualRunner(registry, nil, nil, db, nil, slog.Default(), repo.RepoInfo{})

	exec := &RitualExecution{
		EdictID:  1,
		Username: "testuser",
		Project:  "testproject",
	}

	err := runner.runThen(context.Background(), exec, "check_verdicts_passed")
	if err == nil {
		t.Error("Expected error when some manifests are rejected, got nil")
	}
	if !strings.Contains(err.Error(), "verdict check failed") {
		t.Errorf("Expected error to contain 'verdict check failed', got: %v", err)
	}
	if !strings.Contains(err.Error(), "bad.go") {
		t.Errorf("Expected error to mention rejected file 'bad.go', got: %v", err)
	}
}

// TestCheckVerdictsPassed_AllRejected verifies the handler fails when all manifests are rejected
func TestCheckVerdictsPassed_AllRejected(t *testing.T) {
	db := setupRitualTestDB(t)

	// Ensure JudgeVerdict table exists for the verdict check query
	if err := db.AutoMigrate(&storage.JudgeVerdict{}); err != nil {
		t.Fatalf("Failed to migrate JudgeVerdict: %v", err)
	}

	// Create multiple rejected manifests
	manifests := []storage.ForgeManifest{
		{
			ManifestID: GenerateID("manifest", "1", "test", "file1.go"),
			EdictID:    1,
			Username:   "testuser",
			Project:    "testproject",
			FilePath:   "file1.go",
			Status:     storage.ManifestRejected,
		},
		{
			ManifestID: GenerateID("manifest", "1", "test", "file2.go"),
			EdictID:    1,
			Username:   "testuser",
			Project:    "testproject",
			FilePath:   "file2.go",
			Status:     storage.ManifestRejected,
		},
	}
	for _, m := range manifests {
		if err := db.Create(&m).Error; err != nil {
			t.Fatalf("Failed to create manifest: %v", err)
		}
	}

	registry := NewRitualRegistry()
	runner := NewRitualRunner(registry, nil, nil, db, nil, slog.Default(), repo.RepoInfo{})

	exec := &RitualExecution{
		EdictID:  1,
		Username: "testuser",
		Project:  "testproject",
	}

	err := runner.runThen(context.Background(), exec, "check_verdicts_passed")
	if err == nil {
		t.Error("Expected error when all manifests are rejected, got nil")
	}
	if !strings.Contains(err.Error(), "2 manifest(s) rejected") {
		t.Errorf("Expected error to mention '2 manifest(s) rejected', got: %v", err)
	}
}

// TestCheckVerdictsPassed_NoManifests verifies the handler passes when no manifests exist
func TestCheckVerdictsPassed_NoManifests(t *testing.T) {
	db := setupRitualTestDB(t)

	// Ensure JudgeVerdict table exists for the verdict check query
	if err := db.AutoMigrate(&storage.JudgeVerdict{}); err != nil {
		t.Fatalf("Failed to migrate JudgeVerdict: %v", err)
	}

	registry := NewRitualRegistry()
	runner := NewRitualRunner(registry, nil, nil, db, nil, slog.Default(), repo.RepoInfo{})

	exec := &RitualExecution{
		EdictID:  1,
		Username: "testuser",
		Project:  "testproject",
	}

	err := runner.runThen(context.Background(), exec, "check_verdicts_passed")
	if err != nil {
		t.Errorf("Expected no error when no manifests exist, got: %v", err)
	}
}

// TestCheckVerdictsPassed_FailedVerdict verifies that check_verdicts_passed returns an error
// when a JudgeVerdict has outcome='failed', even if the ForgeManifest status is not ManifestRejected.
// These are separate concepts: manifest status vs verdict outcome.
func TestCheckVerdictsPassed_FailedVerdict(t *testing.T) {
	db := setupRitualTestDB(t)

	// Ensure JudgeVerdict table exists
	if err := db.AutoMigrate(&storage.JudgeVerdict{}); err != nil {
		t.Fatalf("Failed to migrate JudgeVerdict: %v", err)
	}

	// Create a ForgeManifest with quenched status (passing manifest)
	manifest := storage.ForgeManifest{
		ManifestID: GenerateID("manifest", "1", "test", "file.go"),
		EdictID:    1,
		Username:   "testuser",
		Project:    "testproject",
		FilePath:   "file.go",
		Status:     storage.ManifestQuenched, // Passing manifest status
	}
	if err := db.Create(&manifest).Error; err != nil {
		t.Fatalf("Failed to create manifest: %v", err)
	}

	// Create a JudgeVerdict with failed outcome for the same manifest
	verdict := storage.JudgeVerdict{
		VerdictID:  GenerateID("verdict", "1", "test", "file.go"),
		ManifestID: manifest.ManifestID,
		TestSuite:  "unit",
		Outcome:    storage.VerdictFailed, // Failed verdict outcome
	}
	if err := db.Create(&verdict).Error; err != nil {
		t.Fatalf("Failed to create verdict: %v", err)
	}

	registry := NewRitualRegistry()
	runner := NewRitualRunner(registry, nil, nil, db, nil, slog.Default(), repo.RepoInfo{})

	exec := &RitualExecution{
		EdictID:  1,
		Username: "testuser",
		Project:  "testproject",
	}

	// Call check_verdicts_passed - should return error for failed verdict
	err := runner.runThen(context.Background(), exec, "check_verdicts_passed")
	if err == nil {
		t.Error("Expected error when verdict outcome is failed, got nil")
	}
	if !strings.Contains(err.Error(), "verdict check failed") {
		t.Errorf("Expected error to contain 'verdict check failed', got: %v", err)
	}
}

func TestRitualRunner_RepoInfoStoredAndUsed(t *testing.T) {
	// Verify that the repoInfo passed to NewRitualRunner is stored in r.repoInfo
	// and used by expressions, not re-fetched via GetRepoInfo().
	t.Run("repoInfo is stored from constructor argument", func(t *testing.T) {
		info := repo.RepoInfo{
			ProjectRoot:  "/explicit/daemon/root",
			Branch:       "custom-branch",
			IsWorktree:   true,
			WorktreePath: "worktrees/custom",
			Slug:         "owner/repo",
		}
		registry := NewRitualRegistry()
		runner := NewRitualRunner(registry, nil, nil, nil, nil, nil, info)

		// The runner's repoInfo field should match what was passed in
		assert.Equal(t, info, runner.repoInfo, "RitualRunner should store the repoInfo passed to constructor")
	})

	t.Run("repoInfo with empty ProjectRoot", func(t *testing.T) {
		info := repo.RepoInfo{} // all zero values
		registry := NewRitualRegistry()
		runner := NewRitualRunner(registry, nil, nil, nil, nil, nil, info)

		assert.Equal(t, "", runner.repoInfo.ProjectRoot, "empty ProjectRoot should be stored as-is")
		assert.Equal(t, "", runner.repoInfo.Branch, "empty Branch should be stored as-is")
	})

	t.Run("repoInfo differs from os.Getwd", func(t *testing.T) {
		// In daemon mode, repoInfo.ProjectRoot may differ from the process cwd.
		// The RitualRunner should use the provided repoInfo, not call GetRepoInfo().
		daemonRoot := "/daemon/project/root"
		info := repo.RepoInfo{
			ProjectRoot: daemonRoot,
			Branch:      "main",
		}
		registry := NewRitualRegistry()
		runner := NewRitualRunner(registry, nil, nil, nil, nil, nil, info)

		// Verify the runner stores the daemon root, not os.Getwd()
		assert.Equal(t, daemonRoot, runner.repoInfo.ProjectRoot)
	})
}
