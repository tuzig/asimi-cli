package court

import (
	"context"
	"database/sql"
	"log/slog"
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
	"github.com/afittestide/asimi/court/tools"
	"github.com/afittestide/asimi/storage"
	"reflect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	_ "modernc.org/sqlite"
)

func setupCourtTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "court_test")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := filepath.Join(tmpDir, "test.db")
	sqlDB, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)

	// Enable WAL mode and busy_timeout for better concurrency — the ritual
	// guard runs concurrently and performs writes, so without these PRAGMAs
	// SQLite returns "database is locked" errors.
	conn, err := sqlDB.Conn(context.Background())
	require.NoError(t, err)
	_, err = conn.ExecContext(context.Background(), "PRAGMA journal_mode = WAL")
	require.NoError(t, err)
	_, err = conn.ExecContext(context.Background(), "PRAGMA busy_timeout = 5000")
	require.NoError(t, err)
	conn.Close()

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
		&storage.Incident{},
		&storage.RulerCouncil{},
		&storage.RitualGuardCheckpoint{},
		&RitualExecution{},
		&RitualStepState{},
	)
	require.NoError(t, err)

	db.Exec(`CREATE TABLE IF NOT EXISTS ritual_guard_checkpoint (id INTEGER PRIMARY KEY, event_id INTEGER NOT NULL, updated_at DATETIME)`)
	return db
}

func TestSetContext_NilCourt(t *testing.T) {
	var s *Court
	err := s.SetContext(context.Background(), types.SetContextParams{})
	assert.EqualError(t, err, "court not initialised")
}

// TestCourt_ConnDone_NeverClosed verifies that the in-process Court's
// ConnDone returns a channel that is never closed (edict 552).
func TestCourt_ConnDone_NeverClosed(t *testing.T) {
	db := setupCourtTestDB(t)
	cfg := config.DefaultCourtConfig()
	s := NewCourt(db, cfg, nil, nil)

	done := s.ConnDone()
	select {
	case <-done:
		t.Fatal("ConnDone should return a never-closed channel for in-process mode")
	default:
		// Good — channel is open and never closes
	}
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
	db := setupCourtTestDB(t)
	cfg := config.DefaultCourtConfig()
	s := NewCourt(db, cfg, nil, nil)

	err := s.SetContext(context.Background(), types.SetContextParams{
		ProjectRoot: "/nonexistent/path/that/does/not/exist",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid project_root")
}

func TestSetContext_ProjectRootIsFile(t *testing.T) {
	db := setupCourtTestDB(t)
	cfg := config.DefaultCourtConfig()
	s := NewCourt(db, cfg, nil, nil)

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
	db := setupCourtTestDB(t)
	cfg := config.DefaultCourtConfig()
	s := NewCourt(db, cfg, nil, nil)

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
	db := setupCourtTestDB(t)
	cfg := config.DefaultCourtConfig()
	s := NewCourt(db, cfg, nil, nil)

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
	db := setupCourtTestDB(t)
	cfg := config.DefaultCourtConfig()
	s := NewCourt(db, cfg, nil, nil)

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
	db := setupCourtTestDB(t)
	cfg := config.DefaultCourtConfig()
	s := NewCourt(db, cfg, nil, nil)

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
	db := setupCourtTestDB(t)
	cfg := config.DefaultCourtConfig()
	s := NewCourt(db, cfg, nil, nil)

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
	db := setupCourtTestDB(t)
	cfg := config.DefaultCourtConfig()
	s := NewCourt(db, cfg, nil, nil)

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
	db := setupCourtTestDB(t)
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject")
	rg := NewRitualGuard(RitualGuardOpts{Base: base})

	imageName := rg.getSandboxImageName()
	assert.Empty(t, imageName, "getSandboxImageName should return empty string when no PodmanRunner is available")
}

func TestConfigureModel_ReloadsRitualsWhenProjectRootBecomesAvailable(t *testing.T) {
	db := setupCourtTestDB(t)

	// Create court with nil config — no project root set
	s := NewCourt(db, nil, nil, nil)

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
	db := setupCourtTestDB(t)

	// Create court with nil config — no project root set
	s := NewCourt(db, nil, nil, nil)

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
	cfg := config.DefaultCourtConfig()
	court := NewCourt(db, cfg, nil, nil)
	require.NotNil(t, court)
	court.ConfigureModel(nil, &SessionConfig{}, repo.RepoInfo{})

	count := court.clearAllSchedulers()
	assert.Equal(t, 0, count, "clearAllSchedulers should return 0 when no sessions have schedulers")
}

// TestClearAllSchedulers_WithQueuedItems verifies that clearAllSchedulers
// iterates all ministers, finds sessions with schedulers that have queued
// items, clears them, and returns the total aborted count.
func TestClearAllSchedulers_WithQueuedItems(t *testing.T) {
	db := setupMinisterTestDB(t)
	cfg := config.DefaultCourtConfig()
	court := NewCourt(db, cfg, nil, nil)
	require.NotNil(t, court)
	court.ConfigureModel(nil, &SessionConfig{}, repo.RepoInfo{})

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
	chancellor := court.GetMinister("chancellor")
	require.NotNil(t, chancellor)
	if base, ok := chancellor.(interface{ SetSession(*Session) }); ok {
		base.SetSession(sess1)
	}

	forge := court.GetMinister("forge")
	require.NotNil(t, forge)
	if base, ok := forge.(interface{ SetSession(*Session) }); ok {
		base.SetSession(sess2)
	}

	count := court.clearAllSchedulers()
	assert.Equal(t, 3, count, "should abort 2+1 = 3 queued items across two schedulers")

	// Verify the schedulers are now empty
	assert.Equal(t, 0, sched1.ClearQueue(), "sched1 should be empty after clear")
	assert.Equal(t, 0, sched2.ClearQueue(), "sched2 should be empty after clear")
}

// TestClearAllSchedulers_MinistersWithNilScheduler verifies that ministers
// with sessions that have nil schedulers are skipped gracefully.
func TestClearAllSchedulers_MinistersWithNilScheduler(t *testing.T) {
	db := setupMinisterTestDB(t)
	cfg := config.DefaultCourtConfig()
	court := NewCourt(db, cfg, nil, nil)
	require.NotNil(t, court)
	court.ConfigureModel(nil, &SessionConfig{}, repo.RepoInfo{})

	mockLLM := mocks.NewLLMProvider()

	// Create a session with a scheduler then nil it out to simulate edge case
	sess, err := NewSession(mockLLM, &SessionConfig{}, nil, nil, nil, "test", "chancellor")
	require.NoError(t, err)

	// Manually nil out the scheduler
	sess.scheduler = nil

	chancellor := court.GetMinister("chancellor")
	require.NotNil(t, chancellor)
	if base, ok := chancellor.(interface{ SetSession(*Session) }); ok {
		base.SetSession(sess)
	}

	// Should not panic and return 0
	count := court.clearAllSchedulers()
	assert.Equal(t, 0, count)
}

// TestSubscribe_HandlesClearSchedulerMsg verifies the core fix from edict 532:
// when Subscribe() receives a ClearSchedulerMsg on the runner's message channel,
// it handles it in-process (calls clearAllSchedulers, replies on ResultChan)
// and does NOT forward it to the out channel.
func TestSubscribe_HandlesClearSchedulerMsg(t *testing.T) {
	db := setupMinisterTestDB(t)
	cfg := config.DefaultCourtConfig()

	runner := &msgForwardingRunner{}
	court := NewCourt(db, cfg, runner, nil)
	require.NotNil(t, court)
	court.ConfigureModel(nil, &SessionConfig{}, repo.RepoInfo{})

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

	chancellor := court.GetMinister("chancellor")
	require.NotNil(t, chancellor)
	if base, ok := chancellor.(interface{ SetSession(*Session) }); ok {
		base.SetSession(sess)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := court.Subscribe(ctx)
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
	cfg := config.DefaultCourtConfig()

	runner := &msgForwardingRunner{}
	court := NewCourt(db, cfg, runner, nil)
	require.NotNil(t, court)
	court.ConfigureModel(nil, &SessionConfig{}, repo.RepoInfo{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := court.Subscribe(ctx)
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

// ---------------------------------------------------------------------------
// Zhengming answer event-handler tests (edict 574)
// ---------------------------------------------------------------------------

// TestZhengmingAnswered_ChatDoesNotCreateEdict is the regression test for the
// bug fixed in edict 574: the "Chat" sentinel (AnswerChat) — emitted when
// the Ruler dismisses a zhengming prompt — was not recognized by the
// zhengming_answered event handler and fell through to the catch-all path
// that created a bogus edict with "Chat" as its intent.
//
// This test publishes a zhengming_answered event with AnswerChat and verifies
// no edict is created.
func TestZhengmingAnswered_ChatDoesNotCreateEdict(t *testing.T) {
	db := setupCourtTestDB(t)
	cfg := config.DefaultCourtConfig()
	s := NewCourt(db, cfg, nil, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	s.ctx, s.cancel = ctx, cancel
	defer cancel()

	// Start the ritual guard event loop so dispatched events reach subscribers
	go s.ritualGuard.Run(ctx)

	// Publish a zhengming_answered event with AnswerChat
	s.PublishEvent(storage.EdictKey{ID: 0, Username: cfg.Username, Project: cfg.Project},
		storage.EventZhengmingAnswered, storage.JSON{
			"request_id": "test-chat-1",
			"answer":     tools.AnswerChat,
		})

	// Give the event loop time to process
	time.Sleep(100 * time.Millisecond)

	// No edict with intent "Chat" should exist
	var edicts []storage.Edict
	err := db.Where("intent = ?", tools.AnswerChat).Find(&edicts).Error
	require.NoError(t, err)
	assert.Empty(t, edicts, "no edict should be created for Chat answer")
}

// TestZhengmingAnswered_RejectDoesNotCreateEdict verifies that AnswerReject
// is handled as a rejection, not a catch-all edict creation.
func TestZhengmingAnswered_RejectDoesNotCreateEdict(t *testing.T) {
	db := setupCourtTestDB(t)
	cfg := config.DefaultCourtConfig()
	s := NewCourt(db, cfg, nil, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	s.ctx, s.cancel = ctx, cancel
	defer cancel()

	go s.ritualGuard.Run(ctx)

	s.PublishEvent(storage.EdictKey{ID: 0, Username: cfg.Username, Project: cfg.Project},
		storage.EventZhengmingAnswered, storage.JSON{
			"request_id": "test-reject-1",
			"answer":     tools.AnswerReject,
		})

	time.Sleep(100 * time.Millisecond)

	var edicts []storage.Edict
	err := db.Where("intent = ?", tools.AnswerReject).Find(&edicts).Error
	require.NoError(t, err)
	assert.Empty(t, edicts, "no edict should be created for Reject answer")
}

// TestZhengmingAnswered_ApproveEdictCreatesEdict verifies that AnswerApproveEdict
// triggers edict creation from the stored zhengming suggestion.
func TestZhengmingAnswered_ApproveEdictCreatesEdict(t *testing.T) {
	db := setupCourtTestDB(t)
	cfg := config.DefaultCourtConfig()
	s := NewCourt(db, cfg, nil, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	s.ctx, s.cancel = ctx, cancel
	defer cancel()

	go s.ritualGuard.Run(ctx)

	// Store a zhengming request with a suggestion
	req := storage.Zhengming{
		RequestID:  "test-approve-1",
		EdictID:    0,
		Username:   cfg.Username,
		Project:    cfg.Project,
		MinisterID: "sage",
		Questions: storage.ZhengmingQuestions{{
			Text:    "Refactor the zhengming constants",
			Summary: "refactor constants",
			Options: []string{tools.AnswerApproveEdict, tools.AnswerReject},
		}},
		Status:   storage.ZhengmingPending,
		Priority: storage.PriorityNormal,
	}
	require.NoError(t, db.Create(&req).Error)

	s.PublishEvent(storage.EdictKey{ID: 0, Username: cfg.Username, Project: cfg.Project},
		storage.EventZhengmingAnswered, storage.JSON{
			"request_id": "test-approve-1",
			"answer":     tools.AnswerApproveEdict,
		})

	// Poll for the edict to appear (async handler: CreateEdict + db.Save for summary)
	var edict storage.Edict
	require.Eventually(t, func() bool {
		return db.Where("intent = ?", "Refactor the zhengming constants").First(&edict).Error == nil
	}, 2*time.Second, 50*time.Millisecond, "edict should be created from the approved suggestion")
	assert.Equal(t, "Refactor the zhengming constants", edict.Intent)

	// Summary is set via a separate db.Save call after CreateEdict, which may
	// race with our read. Poll for it.
	require.Eventually(t, func() bool {
		var fresh storage.Edict
		if db.First(&fresh, edict.ID).Error != nil {
			return false
		}
		return fresh.Summary == "refactor constants"
	}, 2*time.Second, 50*time.Millisecond, "edict summary should be set to the suggestion summary")
}

// TestZhengmingAnswered_AnswerConstants verifies that the constant values
// haven't drifted from their expected string sentinels. This guards against
// accidental renames that would break the TUI ↔ court contract.
func TestZhengmingAnswered_AnswerConstants(t *testing.T) {
	assert.Equal(t, "Chat", tools.AnswerChat)
	assert.Equal(t, "Approve edict", tools.AnswerApproveEdict)
	assert.Equal(t, "Reject", tools.AnswerReject)
	assert.Equal(t, "Approve and proceed", tools.AnswerApproveAndProceed)
	assert.Equal(t, "Let me clarify", tools.AnswerLetMeClarify)
}

// TestZhengmingAnswered_ApproveEdictRefinesExistingEdict verifies that when
// a zhengming request has EdictID > 0, approving it appends the suggestion
// to the existing edict's intent instead of creating a new edict.
func TestZhengmingAnswered_ApproveEdictRefinesExistingEdict(t *testing.T) {
	db := setupCourtTestDB(t)
	cfg := config.DefaultCourtConfig()
	s := NewCourt(db, cfg, nil, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	s.ctx, s.cancel = ctx, cancel
	defer cancel()

	go s.ritualGuard.Run(ctx)

	// Create an existing edict with the right user/project scoping
	edict := storage.Edict{
		ID:       42,
		Username: cfg.Username,
		Project:  cfg.Project,
		Intent:   "Original intent",
	}
	require.NoError(t, db.Create(&edict).Error)

	// Store a zhengming request linked to the existing edict
	req := storage.Zhengming{
		RequestID:  "test-refine-1",
		EdictID:    42,
		Username:   cfg.Username,
		Project:    cfg.Project,
		MinisterID: "sage",
		Questions: storage.ZhengmingQuestions{{
			Text:    "Add better error handling",
			Summary: "refine error handling",
			Options: []string{tools.AnswerApproveEdict, tools.AnswerReject},
		}},
		Status:   storage.ZhengmingPending,
		Priority: storage.PriorityNormal,
	}
	require.NoError(t, db.Create(&req).Error)

	s.PublishEvent(storage.EdictKey{ID: 42, Username: cfg.Username, Project: cfg.Project},
		storage.EventZhengmingAnswered, storage.JSON{
			"request_id": "test-refine-1",
			"answer":     tools.AnswerApproveEdict,
		})

	// Poll for the intent to be updated (async handler)
	require.Eventually(t, func() bool {
		var fresh storage.Edict
		if db.First(&fresh, 42).Error != nil {
			return false
		}
		return strings.Contains(fresh.Intent, "Original intent") &&
			strings.Contains(fresh.Intent, "Add better error handling")
	}, 2*time.Second, 50*time.Millisecond, "edict intent should contain both original and appended suggestion")

	// Verify no new edict was created (edict 1 is the auto-created court infrastructure edict)
	var count int64
	db.Model(&storage.Edict{}).Where("id != ? AND id != ?", 1, 42).Count(&count)
	assert.Equal(t, int64(0), count, "no new edict should be created when refining")
}

// TestZhengmingAnswered_NonSentinelAnswerWithEdictID0CreatesEdict verifies that
// a system-ritual answer (edict_id=0, non-sentinel answer) still creates an
// edict — this is the legitimate catch-all path that should NOT be broken
// by the bug fix.
func TestZhengmingAnswered_NonSentinelAnswerWithEdictID0CreatesEdict(t *testing.T) {
	db := setupCourtTestDB(t)
	cfg := config.DefaultCourtConfig()
	s := NewCourt(db, cfg, nil, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	s.ctx, s.cancel = ctx, cancel
	defer cancel()

	go s.ritualGuard.Run(ctx)

	customAnswer := "Let's build a new feature"
	s.PublishEvent(storage.EdictKey{ID: 0, Username: cfg.Username, Project: cfg.Project},
		storage.EventZhengmingAnswered, storage.JSON{
			"request_id": "test-custom-1",
			"answer":     customAnswer,
		})

	time.Sleep(100 * time.Millisecond)

	var edicts []storage.Edict
	err := db.Where("intent = ?", customAnswer).Find(&edicts).Error
	require.NoError(t, err)
	assert.Len(t, edicts, 1, "a non-sentinel answer with edict_id=0 should create an edict")
}

// TestZhengmingAnswered_ChatAndNonSentinelIsolation verifies that when a
// Chat answer and a legitimate answer are both processed, only the
// legitimate answer creates an edict. Events are published sequentially to
// avoid SQLite write-lock contention.
func TestZhengmingAnswered_ChatAndNonSentinelIsolation(t *testing.T) {
	db := setupCourtTestDB(t)
	cfg := config.DefaultCourtConfig()
	s := NewCourt(db, cfg, nil, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	s.ctx, s.cancel = ctx, cancel
	defer cancel()

	go s.ritualGuard.Run(ctx)

	// Publish Chat first
	s.PublishEvent(storage.EdictKey{ID: 0, Username: cfg.Username, Project: cfg.Project},
		storage.EventZhengmingAnswered, storage.JSON{
			"request_id": "isolation-chat",
			"answer":     tools.AnswerChat,
		})
	time.Sleep(100 * time.Millisecond)

	// Then publish a legitimate answer
	legitAnswer := "Build the database migration tool"
	s.PublishEvent(storage.EdictKey{ID: 0, Username: cfg.Username, Project: cfg.Project},
		storage.EventZhengmingAnswered, storage.JSON{
			"request_id": "isolation-legit",
			"answer":     legitAnswer,
		})
	time.Sleep(150 * time.Millisecond)

	// Chat should NOT have created an edict
	var chatEdicts []storage.Edict
	err := db.Where("intent = ?", tools.AnswerChat).Find(&chatEdicts).Error
	require.NoError(t, err)
	assert.Empty(t, chatEdicts, "no edict should be created for Chat answer")

	// The legitimate answer SHOULD have created an edict
	var legitEdicts []storage.Edict
	err = db.Where("intent = ?", legitAnswer).Find(&legitEdicts).Error
	require.NoError(t, err)
	assert.Len(t, legitEdicts, 1, "one edict should be created from the legitimate answer")
}

// ---------------------------------------------------------------------------
// HostChecker wiring tests (edict 631)
// ---------------------------------------------------------------------------

// TestBuildToolRegistry_WiresHostChecker verifies that buildToolRegistry
// extracts CheckHostCommand from the chancellor's MinisterBase and stores
// it as s.hostChecker so the shell tool can honor run_on_host /
// safe_run_on_host config patterns.
func TestBuildToolRegistry_WiresHostChecker(t *testing.T) {
	db := setupMinisterTestDB(t)
	cfg := config.DefaultCourtConfig()
	s := NewCourt(db, cfg, nil, nil)
	require.NotNil(t, s)

	// ConfigureModel with a SessionConfig that has RunOnHost patterns
	s.ConfigureModel(nil, &SessionConfig{
		Sandbox: config.SandboxConfig{
			RunOnHost: []string{"^gh "},
		},
	}, repo.RepoInfo{})

	// hostChecker should be non-nil after buildToolRegistry
	require.NotNil(t, s.hostChecker, "hostChecker should be wired from chancellor's CheckHostCommand")

	// Verify it actually matches patterns from the config
	runOnHost, needsApproval := s.hostChecker("gh issue list")
	assert.True(t, runOnHost, "command matching RunOnHost should return runOnHost=true")
	assert.True(t, needsApproval, "RunOnHost patterns require approval")

	runOnHost, needsApproval = s.hostChecker("ls -la")
	assert.False(t, runOnHost, "non-matching command should return runOnHost=false")
}

// TestUpdateProjectRootTools_PreservesHostChecker verifies that
// updateProjectRootTools re-registers the shell tool with the stored
// hostChecker (and msgChan) rather than nil values.
func TestUpdateProjectRootTools_PreservesHostChecker(t *testing.T) {
	db := setupMinisterTestDB(t)
	cfg := config.DefaultCourtConfig()

	runner := &msgForwardingRunner{}
	s := NewCourt(db, cfg, runner, nil)
	require.NotNil(t, s)

	// Set up a msgChan via Subscribe (which calls SetRunnerMessageChannel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = s.Subscribe(ctx)
	require.NotNil(t, s.msgChan, "msgChan should be set after Subscribe")

	// ConfigureModel to wire hostChecker
	s.ConfigureModel(nil, &SessionConfig{
		Sandbox: config.SandboxConfig{
			RunOnHost: []string{"^docker "},
		},
	}, repo.RepoInfo{})
	require.NotNil(t, s.hostChecker)

	// Call updateProjectRootTools and verify the shell tool has the hostChecker
	s.updateProjectRootTools("/tmp")

	// Get the registered shell tool from the registry
	forgePerm, _ := tools.ParsePermissions("rwxr---w-")
	ts := s.toolRegistry.ForPermissions(forgePerm)
	var shellTool *tools.RunShellCommand
	for _, t := range ts {
		if st, ok := t.(*tools.RunShellCommand); ok {
			shellTool = st
			break
		}
	}
	require.NotNil(t, shellTool, "run_shell_command should be registered after updateProjectRootTools")

	// Verify the shell tool's internal shouldRunOnHost field is non-nil.
	// This is the critical check: updateProjectRootTools must pass s.hostChecker
	// (not nil) when constructing the RunShellCommand. We use reflection because
	// shouldRunOnHost is an unexported field on tools.RunShellCommand.
	shellVal := reflect.ValueOf(shellTool).Elem()
	shouldRunOnHost := shellVal.FieldByName("shouldRunOnHost")
	require.True(t, shouldRunOnHost.IsValid(), "RunShellCommand should have shouldRunOnHost field")
	assert.False(t, shouldRunOnHost.IsNil(), "shell tool's shouldRunOnHost must be non-nil after updateProjectRootTools")

	// Verify the shell tool's internal msgChan field is non-nil.
	msgChanField := shellVal.FieldByName("msgChan")
	require.True(t, msgChanField.IsValid(), "RunShellCommand should have msgChan field")
	assert.False(t, msgChanField.IsNil(), "shell tool's msgChan must be non-nil after updateProjectRootTools")
}
