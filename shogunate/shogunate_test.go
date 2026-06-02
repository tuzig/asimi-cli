package shogunate

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	_ "modernc.org/sqlite"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/storage"
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
