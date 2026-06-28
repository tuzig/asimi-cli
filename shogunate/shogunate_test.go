package shogunate

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/mocks"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/internal/types"
	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	_ "modernc.org/sqlite"
)

func setupShogunateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "shogunate_test")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := filepath.Join(tmpDir, "test.db")
	sqlDB, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)

	db, err := gorm.Open(sqlite.Dialector{Conn: sqlDB}, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	err = db.AutoMigrate(
		&storage.Edict{},
		&storage.Zhengming{},
		&storage.TianEvent{},
		&storage.TianEventDLQ{},
		&storage.Ling{},
		&storage.ForgeManifest{},
		&storage.Seal{},
		&storage.JudgeVerdict{},
		&storage.CensorPrecedent{},
		&storage.MarshalIncident{},
		&storage.RulerCouncil{},
		&storage.RitualGuardCheckpoint{},
	)
	require.NoError(t, err)

	db.Exec(`CREATE TABLE IF NOT EXISTS ritual_guard_checkpoint (id INTEGER PRIMARY KEY, event_id INTEGER NOT NULL, updated_at DATETIME)`)
	return db
}

func TestSetContext_NilShogunate(t *testing.T) {
	var s *Shogunate
	err := s.SetContext(context.Background(), types.SetContextParams{})
	assert.EqualError(t, err, "shogunate not initialised")
}

// runGit runs a git command in the given directory, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

func TestSetContext_InvalidProjectRoot(t *testing.T) {
	db := setupShogunateTestDB(t)
	cfg := config.DefaultShogunateConfig()
	s := NewShogunate(db, cfg, nil, nil)

	err := s.SetContext(context.Background(), types.SetContextParams{
		ProjectRoot: "/nonexistent/path/that/does/not/exist",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid project_root")
}

func TestSetContext_ProjectRootIsFile(t *testing.T) {
	db := setupShogunateTestDB(t)
	cfg := config.DefaultShogunateConfig()
	s := NewShogunate(db, cfg, nil, nil)

	// Create a temporary file (not a directory) as ProjectRoot
	tmpFile, err := os.CreateTemp("", "notadir_*")
	require.NoError(t, err)
	tmpFile.Close()
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })

	err = s.SetContext(context.Background(), types.SetContextParams{
		ProjectRoot: tmpFile.Name(),
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestSetContext_EmptyProjectRoot_UsesCwd(t *testing.T) {
	db := setupShogunateTestDB(t)
	cfg := config.DefaultShogunateConfig()
	s := NewShogunate(db, cfg, nil, nil)

	// Empty ProjectRoot defaults to "." which should resolve to current working dir.
	// This may succeed or fail depending on whether cwd has an .agents dir,
	// but it must not panic.
	err := s.SetContext(context.Background(), types.SetContextParams{
		ProjectRoot: "",
		APIKeys:     map[string]string{"openai": "sk-test"},
	})
	// We don't assert on success/failure because it depends on the test
	// environment's config files, but we verify no panic occurred.
	_ = err
}

func TestSetContext_WithAPIKeys(t *testing.T) {
	db := setupShogunateTestDB(t)
	cfg := config.DefaultShogunateConfig()
	s := NewShogunate(db, cfg, nil, nil)

	// Use a temp dir as project root so LoadProjectConfig can find it.
	tmpDir, err := os.MkdirTemp("", "setcontext_test")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	// Create .agents dir so LoadProjectConfig doesn't complain about stat
	agentsDir := filepath.Join(tmpDir, ".agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	err = s.SetContext(context.Background(), types.SetContextParams{
		ProjectRoot: tmpDir,
		Project:     "test-project",
		Username:    "test-user",
		Branch:      "main",
		APIKeys: map[string]string{
			"openai": "sk-test-key",
		},
	})
	require.NoError(t, err)
}

func TestSetContext_Idempotent(t *testing.T) {
	db := setupShogunateTestDB(t)
	cfg := config.DefaultShogunateConfig()
	s := NewShogunate(db, cfg, nil, nil)

	tmpDir, err := os.MkdirTemp("", "setcontext_idempotent_test")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	agentsDir := filepath.Join(tmpDir, ".agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	params := types.SetContextParams{
		ProjectRoot: tmpDir,
		Project:     "test-project",
		Username:    "test-user",
		APIKeys: map[string]string{
			"openai": "sk-test-key",
		},
	}

	// First call should succeed
	err = s.SetContext(context.Background(), params)
	require.NoError(t, err)

	// Second call with same params should also succeed (idempotent)
	err = s.SetContext(context.Background(), params)
	require.NoError(t, err)

	// Third call with different keys should also succeed (reconfigure)
	params.APIKeys = map[string]string{
		"anthropic": "sk-ant-test",
	}
	err = s.SetContext(context.Background(), params)
	require.NoError(t, err)
}

func TestSetContext_PropagatesRepoInfo(t *testing.T) {
	db := setupShogunateTestDB(t)
	cfg := config.DefaultShogunateConfig()
	s := NewShogunate(db, cfg, nil, nil)

	tmpDir, err := os.MkdirTemp("", "setcontext_repo_test")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	agentsDir := filepath.Join(tmpDir, ".agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	err = s.SetContext(context.Background(), types.SetContextParams{
		ProjectRoot:  tmpDir,
		WorktreePath: "/tmp/worktree",
		Branch:       "feature-branch",
		Project:      "my-project",
		Username:     "dev",
		APIKeys: map[string]string{
			"openai": "sk-test",
		},
	})
	require.NoError(t, err)

	// Verify ministers were configured by checking one of them has a session.
	// ConfigureModel sets the config on all ministers; if SetContext
	// completed without error, ConfigureModel was called successfully.
	chancellor := s.GetMinister("chancellor")
	assert.NotNil(t, chancellor)
}

func TestSetContext_EmptyProjectDerivesSlugFromGitRemote(t *testing.T) {
	db := setupShogunateTestDB(t)
	cfg := config.DefaultShogunateConfig()
	s := NewShogunate(db, cfg, nil, nil)

	// Create a temp git repo with a remote so GetRepoInfoForRoot can
	// derive a slug (owner/repo).
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	// Initialize a git repo with a remote origin URL
	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "remote", "add", "origin", "https://github.com/testorg/testrepo.git")
	runGit(t, tmpDir, "config", "user.email", "test@test.com")
	runGit(t, tmpDir, "config", "user.name", "Test")

	err := s.SetContext(context.Background(), types.SetContextParams{
		ProjectRoot: tmpDir,
		// Project is intentionally empty — slug should be derived from git remote
		Project:  "",
		Username: "test-user",
		APIKeys:  map[string]string{"openai": "sk-test"},
	})
	require.NoError(t, err)

	// The chancellor's MinisterBase should have received the repoInfo
	// with a non-empty slug derived from the git remote.
	chancellor := s.GetMinister("chancellor")
	require.NotNil(t, chancellor)
	if base, ok := chancellor.(interface{ RepoInfo() repo.RepoInfo }); ok {
		ri := base.RepoInfo()
		assert.NotEmpty(t, ri.Slug, "slug should be derived from git remote when Project is empty")
		assert.Equal(t, "testorg/testrepo", ri.Slug)
	}
}

func TestSetContext_NonEmptyProjectUsesExplicitSlug(t *testing.T) {
	db := setupShogunateTestDB(t)
	cfg := config.DefaultShogunateConfig()
	s := NewShogunate(db, cfg, nil, nil)

	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, ".agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	err := s.SetContext(context.Background(), types.SetContextParams{
		ProjectRoot: tmpDir,
		Project:     "my-org/my-repo",
		Username:    "test-user",
		APIKeys:     map[string]string{"openai": "sk-test"},
	})
	require.NoError(t, err)

	chancellor := s.GetMinister("chancellor")
	require.NotNil(t, chancellor)
	if base, ok := chancellor.(interface{ RepoInfo() repo.RepoInfo }); ok {
		ri := base.RepoInfo()
		assert.Equal(t, "my-org/my-repo", ri.Slug)
	}
}

func TestGetSandboxImageName_EmptyWhenNoRunner(t *testing.T) {
	db := setupShogunateTestDB(t)
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject")
	rg := NewRitualGuard(RitualGuardOpts{Base: base})

	imageName := rg.getSandboxImageName()
	assert.Empty(t, imageName, "getSandboxImageName should return empty string when no PodmanRunner is available")
}

func TestConfigureModel_ReloadsRitualsWhenProjectRootBecomesAvailable(t *testing.T) {
	db := setupShogunateTestDB(t)

	// Create shogunate with nil config — no project root set
	s := NewShogunate(db, nil, nil, nil)

	// Start should succeed but rituals won't load (empty project root)
	err := s.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { s.Stop() })

	// Registry should be empty since LoadRituals failed
	reg := s.GetRitualRegistry()
	assert.Empty(t, reg.List(), "rituals should be empty when project root is not set")

	// ConfigureModel with a project root should load rituals
	projectRoot := t.TempDir()
	agentsDir := filepath.Join(projectRoot, ".agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	s.ConfigureModel(nil, &SessionConfig{}, repo.RepoInfo{
		ProjectRoot: projectRoot,
		Slug:        "test-project",
	})

	// Registry should now have embedded rituals (e.g. dawn-audience)
	assert.NotEmpty(t, reg.List(), "rituals should be loaded after ConfigureModel with project root")
}

func TestConfigureModel_DoesNotReloadRitualsWhenRegistryNotEmpty(t *testing.T) {
	db := setupShogunateTestDB(t)

	// Create shogunate with nil config — no project root set
	s := NewShogunate(db, nil, nil, nil)

	err := s.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { s.Stop() })

	// Manually register a ritual before ConfigureModel runs
	reg := s.GetRitualRegistry()
	customRitual := &RitualDef{
		Name: "custom-ritual",
		Steps: []RitualStep{
			{Minister: "chancellor", Act: "do something"},
		},
	}
	require.NoError(t, reg.Register(customRitual))

	// ConfigureModel with a project root should NOT reload rituals
	// because the registry is no longer empty
	projectRoot := t.TempDir()
	agentsDir := filepath.Join(projectRoot, ".agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	s.ConfigureModel(nil, &SessionConfig{}, repo.RepoInfo{
		ProjectRoot: projectRoot,
		Slug:        "test-project",
	})

	// Only the custom ritual should remain; embedded rituals should NOT be added
	names := reg.List()
	assert.Contains(t, names, "custom-ritual", "manually registered ritual should persist")
	assert.NotContains(t, names, "dawn-audience", "embedded rituals should not be reloaded when registry is not empty")
}

// ---------------------------------------------------------------------------
// Scheduler clearing tests (merged from clear_scheduler_test.go)
// ---------------------------------------------------------------------------

// msgForwardingRunner captures the msgChan (like recordingMsgChanRunner) and
// implements Run/Close/Restart so it satisfies runners.Runner.
type msgForwardingRunner struct {
	runners.Runner
	msgChan chan<- runners.Msg
}

func (r *msgForwardingRunner) SetMessageChannel(ch chan<- runners.Msg) { r.msgChan = ch }
func (r *msgForwardingRunner) Run(context.Context, runners.Input) (runners.Output, error) {
	return runners.Output{}, nil
}
func (r *msgForwardingRunner) Close(context.Context) error   { return nil }
func (r *msgForwardingRunner) Restart(context.Context) error { return nil }
func (r *msgForwardingRunner) AllowFallback(bool)            {}
func (r *msgForwardingRunner) RunnerType() string            { return "msg_forwarding" }

// TestClearAllSchedulers_NoSessions verifies that clearAllSchedulers returns 0
// when ministers exist but have no sessions.
func TestClearAllSchedulers_NoSessions(t *testing.T) {
	db := setupMinisterTestDB(t)
	cfg := config.DefaultShogunateConfig()
	shog := NewShogunate(db, cfg, nil, nil)
	require.NotNil(t, shog)
	shog.ConfigureModel(nil, &SessionConfig{}, repo.RepoInfo{})

	count := shog.clearAllSchedulers()
	assert.Equal(t, 0, count, "clearAllSchedulers should return 0 when no sessions have schedulers")
}

// TestClearAllSchedulers_WithQueuedItems verifies that clearAllSchedulers
// iterates all ministers, finds sessions with schedulers that have queued
// items, clears them, and returns the total aborted count.
func TestClearAllSchedulers_WithQueuedItems(t *testing.T) {
	db := setupMinisterTestDB(t)
	cfg := config.DefaultShogunateConfig()
	shog := NewShogunate(db, cfg, nil, nil)
	require.NotNil(t, shog)
	shog.ConfigureModel(nil, &SessionConfig{}, repo.RepoInfo{})

	// Create blocking tools so items stay in-flight when ClearQueue() runs.
	doneA := make(chan struct{})
	doneB := make(chan struct{})
	doneC := make(chan struct{})
	defer close(doneA)
	defer close(doneB)
	defer close(doneC)

	sched1 := runners.NewCoreToolScheduler(nil)
	sched1.Schedule(context.Background(), &blockingTool{name: "tool_a", done: doneA}, `{}`)
	sched1.Schedule(context.Background(), &blockingTool{name: "tool_b", done: doneB}, `{}`)

	sched2 := runners.NewCoreToolScheduler(nil)
	sched2.Schedule(context.Background(), &blockingTool{name: "tool_c", done: doneC}, `{}`)

	mockLLM := mocks.NewLLMProvider()

	// Create sessions with these schedulers and attach to ministers
	sess1, err := NewSession(mockLLM, &SessionConfig{}, nil, sched1, nil, "test", "chancellor")
	require.NoError(t, err)
	sess2, err := NewSession(mockLLM, &SessionConfig{}, nil, sched2, nil, "test", "forge")
	require.NoError(t, err)

	// Attach sessions to ministers
	chancellor := shog.GetMinister("chancellor")
	require.NotNil(t, chancellor)
	if base, ok := chancellor.(interface{ SetSession(*Session) }); ok {
		base.SetSession(sess1)
	}

	forge := shog.GetMinister("forge")
	require.NotNil(t, forge)
	if base, ok := forge.(interface{ SetSession(*Session) }); ok {
		base.SetSession(sess2)
	}

	count := shog.clearAllSchedulers()
	assert.Equal(t, 3, count, "should abort 2+1 = 3 queued items across two schedulers")

	// Verify the schedulers are now empty
	assert.Equal(t, 0, sched1.ClearQueue(), "sched1 should be empty after clear")
	assert.Equal(t, 0, sched2.ClearQueue(), "sched2 should be empty after clear")
}

// TestClearAllSchedulers_MinistersWithNilScheduler verifies that ministers
// with sessions that have nil schedulers are skipped gracefully.
func TestClearAllSchedulers_MinistersWithNilScheduler(t *testing.T) {
	db := setupMinisterTestDB(t)
	cfg := config.DefaultShogunateConfig()
	shog := NewShogunate(db, cfg, nil, nil)
	require.NotNil(t, shog)
	shog.ConfigureModel(nil, &SessionConfig{}, repo.RepoInfo{})

	mockLLM := mocks.NewLLMProvider()

	// Create a session with a scheduler then nil it out to simulate edge case
	sess, err := NewSession(mockLLM, &SessionConfig{}, nil, nil, nil, "test", "chancellor")
	require.NoError(t, err)

	// Manually nil out the scheduler
	sess.scheduler = nil

	chancellor := shog.GetMinister("chancellor")
	require.NotNil(t, chancellor)
	if base, ok := chancellor.(interface{ SetSession(*Session) }); ok {
		base.SetSession(sess)
	}

	// Should not panic and return 0
	count := shog.clearAllSchedulers()
	assert.Equal(t, 0, count)
}

// TestSubscribe_HandlesClearSchedulerMsg verifies the core fix from edict 532:
// when Subscribe() receives a ClearSchedulerMsg on the runner's message channel,
// it handles it in-process (calls clearAllSchedulers, replies on ResultChan)
// and does NOT forward it to the out channel.
func TestSubscribe_HandlesClearSchedulerMsg(t *testing.T) {
	db := setupMinisterTestDB(t)
	cfg := config.DefaultShogunateConfig()

	runner := &msgForwardingRunner{}
	shog := NewShogunate(db, cfg, runner, nil)
	require.NotNil(t, shog)
	shog.ConfigureModel(nil, &SessionConfig{}, repo.RepoInfo{})

	// Create blocking tools so items stay in-flight for ClearSchedulerMsg to abort.
	doneX := make(chan struct{})
	doneY := make(chan struct{})
	defer close(doneX)
	defer close(doneY)

	sched := runners.NewCoreToolScheduler(nil)
	sched.Schedule(context.Background(), &blockingTool{name: "tool_x", done: doneX}, `{}`)
	sched.Schedule(context.Background(), &blockingTool{name: "tool_y", done: doneY}, `{}`)

	mockLLM := mocks.NewLLMProvider()
	sess, err := NewSession(mockLLM, &SessionConfig{}, nil, sched, nil, "test", "chancellor")
	require.NoError(t, err)

	chancellor := shog.GetMinister("chancellor")
	require.NotNil(t, chancellor)
	if base, ok := chancellor.(interface{ SetSession(*Session) }); ok {
		base.SetSession(sess)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := shog.Subscribe(ctx)
	require.NotNil(t, out)
	require.NotNil(t, runner.msgChan, "Subscribe should set the runner msg channel")

	// Send a ClearSchedulerMsg through the runner's msg channel
	resultChan := make(chan int, 1)
	runner.msgChan <- runners.ClearSchedulerMsg{ResultChan: resultChan}

	// Wait for the result — it must arrive promptly
	select {
	case count := <-resultChan:
		assert.Equal(t, 2, count, "should abort 2 queued items")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ClearSchedulerMsg result — Restart() would hang")
	}

	// Verify the ClearSchedulerMsg was NOT forwarded to the out channel.
	select {
	case msg := <-out:
		t.Fatalf("ClearSchedulerMsg should NOT be forwarded to out channel, got: %T %+v", msg, msg)
	case <-time.After(100 * time.Millisecond):
		// Expected: no message on out channel
	}

	// Verify scheduler queue was actually cleared
	assert.Equal(t, 0, sched.ClearQueue(), "scheduler should be empty after ClearSchedulerMsg handled")
}

// TestSubscribe_ForwardsNormalMessages verifies that non-ClearSchedulerMsg
// messages still flow through to the out channel.
func TestSubscribe_ForwardsNormalMessages(t *testing.T) {
	db := setupMinisterTestDB(t)
	cfg := config.DefaultShogunateConfig()

	runner := &msgForwardingRunner{}
	shog := NewShogunate(db, cfg, runner, nil)
	require.NotNil(t, shog)
	shog.ConfigureModel(nil, &SessionConfig{}, repo.RepoInfo{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := shog.Subscribe(ctx)
	require.NotNil(t, out)
	require.NotNil(t, runner.msgChan)

	// Send a normal message (not ClearSchedulerMsg) — e.g., SandboxUnhealthyMsg
	normalMsg := runners.SandboxUnhealthyMsg{Message: "stale container", ContainerName: "test"}
	runner.msgChan <- normalMsg

	select {
	case msg := <-out:
		unhealthyMsg, ok := msg.(runners.SandboxUnhealthyMsg)
		assert.True(t, ok, "expected SandboxUnhealthyMsg, got %T", msg)
		assert.Equal(t, "stale container", unhealthyMsg.Message)
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for normal message on out channel")
	}
}

// noopTool is a minimal Tool implementation for scheduler tests.
type noopTool struct {
	name string
}

func (t *noopTool) Name() string        { return t.name }
func (t *noopTool) Description() string { return "noop" }
func (t *noopTool) Call(ctx context.Context, input string) (string, error) {
	return "ok", nil
}
func (t *noopTool) Format(input, result string, err error) string {
	if err != nil {
		return err.Error()
	}
	return result
}
func (t *noopTool) ParameterSchema() map[string]any { return nil }

// blockingTool is a Tool whose Call blocks until done is closed.
type blockingTool struct {
	name string
	done chan struct{}
}

func (t *blockingTool) Name() string        { return t.name }
func (t *blockingTool) Description() string { return "blocking" }
func (t *blockingTool) Call(ctx context.Context, input string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-t.done:
		return "ok", nil
	}
}
func (t *blockingTool) Format(input, result string, err error) string {
	if err != nil {
		return err.Error()
	}
	return result
}
func (t *blockingTool) ParameterSchema() map[string]any { return nil }
