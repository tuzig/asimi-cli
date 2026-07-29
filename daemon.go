package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/afittestide/asimi/court"
	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/daemon"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/storage"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

// initDaemonShared wires shared infrastructure (logger, config, DB)
// via fx and returns a daemon.Shared ready for daemon.Run.
func initDaemonShared() (*daemon.Shared, error) {
	logBaseName = "asimi-daemon"
	initLogger()

	var shared *daemon.Shared
	fxOptions := []fx.Option{
		fx.NopLogger,
		fx.Provide(
			ProvideLogger,
			ProvideRepoInfo,
			ProvideConfig,
			ProvideStorage,
			ProvideGormDB,
			provideDaemonShared,
		),
		fx.Populate(&shared),
	}
	app := fx.New(fxOptions...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := app.Start(ctx); err != nil {
		return nil, fmt.Errorf("daemon: fx start: %w", err)
	}

	// Keep the fx app alive — it's stopped via the daemon's shutdown
	// path. The fx lifecycle hooks close storage/runner on Stop.
	// We run app.Stop in a goroutine that waits for the caller to
	// finish, but since daemon.Run blocks until shutdown, we instead
	// rely on process exit for cleanup. The SQLite WAL is already
	// flushed; the OS reclaims fds on exit.
	return shared, nil
}

// provideDaemonShared creates a daemon.Shared with the given dependencies.
func provideDaemonShared(db *gorm.DB, sdb *storage.DB, cfg *config.Config, logger *slog.Logger) *daemon.Shared {
	return &daemon.Shared{
		DB:              db,
		Storage:         sdb,
		Config:          cfg,
		Logger:          logger,
		NewSessionStore: newDaemonSessionStore,
		IsolatedHost:    cli.IsolatedHost,
	}
}

// newDaemonSessionStore adapts the root NewSessionStore to the
// daemon.SessionStoreFactory signature.
func newDaemonSessionStore(sdb *storage.DB, repoInfo repo.RepoInfo, maxSessions, maxAgeDays int) (court.SessionPersister, error) {
	return NewSessionStore(sdb, repoInfo, maxSessions, maxAgeDays)
}

// silenceUnused keeps time imported — it's used for the fx stop timeout
// if we ever wire graceful shutdown through the daemon package.
var _ = time.Second
