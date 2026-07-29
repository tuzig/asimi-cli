package daemon

import (
	"log/slog"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// Shared holds shared resources for the daemon process.
type Shared struct {
	DB              *gorm.DB
	Storage         *storage.DB
	Config          *config.Config
	Logger          *slog.Logger
	NewSessionStore SessionStoreFactory
	IsolatedHost    bool
}
