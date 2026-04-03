package storage

import (
	"log/slog"
	"strings"

	"gorm.io/gorm"
)

// MigrateEdictCompositePK adds username and project columns to the edicts table
// and all referencing tables, then rebuilds edicts with a composite primary key
// (edict_id, username, project). Existing rows get default values.
// This is a no-op if the columns already exist.
func MigrateEdictCompositePK(db *gorm.DB, logger *slog.Logger) error {
	// Check if edicts table already has the username column
	var colName string
	row := db.Raw(`SELECT name FROM pragma_table_info('edicts') WHERE name = 'username'`).Row()
	if row != nil {
		if err := row.Scan(&colName); err == nil && colName == "username" {
			return nil // already migrated
		}
	}

	// Check if the edicts table exists at all
	var tableCount int
	db.Raw(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='edicts'`).Scan(&tableCount)
	if tableCount == 0 {
		return nil // table doesn't exist yet, AutoMigrate will create it
	}

	logger.Info("migrating edicts to composite primary key (edict_id, username, project)")

	return db.Transaction(func(tx *gorm.DB) error {
		// 1. Rebuild edicts with composite PK
		criticalStmts := []string{
			`CREATE TABLE edicts_new (
				edict_id INTEGER NOT NULL,
				username TEXT NOT NULL DEFAULT '',
				project TEXT NOT NULL DEFAULT '',
				session_id TEXT,
				issue_ref TEXT,
				summary TEXT,
				intent TEXT,
				cancelled_at INTEGER,
				created_at DATETIME,
				updated_at DATETIME,
				PRIMARY KEY (edict_id, username, project)
			)`,
			`INSERT INTO edicts_new (edict_id, username, project, session_id, issue_ref, summary, intent, cancelled_at, created_at, updated_at)
				SELECT edict_id, 'daonb', 'asimi-cli', session_id, issue_ref, summary, intent, cancelled_at, created_at, updated_at FROM edicts`,
			`DROP TABLE edicts`,
			`ALTER TABLE edicts_new RENAME TO edicts`,
		}

		for _, stmt := range criticalStmts {
			if err := tx.Exec(stmt).Error; err != nil {
				logger.Error("composite PK migration failed", "error", err)
				return err
			}
		}

		// 2. Add username and project columns to FK tables (ignore if already exists)
		alterStmts := []string{
			`ALTER TABLE seals ADD COLUMN username TEXT NOT NULL DEFAULT 'daonb'`,
			`ALTER TABLE seals ADD COLUMN project TEXT NOT NULL DEFAULT 'asimi-cli'`,
			`ALTER TABLE tian_events ADD COLUMN username TEXT NOT NULL DEFAULT 'daonb'`,
			`ALTER TABLE tian_events ADD COLUMN project TEXT NOT NULL DEFAULT 'asimi-cli'`,
			`ALTER TABLE tian_event_dlq ADD COLUMN username TEXT NOT NULL DEFAULT 'daonb'`,
			`ALTER TABLE tian_event_dlq ADD COLUMN project TEXT NOT NULL DEFAULT 'asimi-cli'`,
			`ALTER TABLE zhengming_requests ADD COLUMN username TEXT NOT NULL DEFAULT 'daonb'`,
			`ALTER TABLE zhengming_requests ADD COLUMN project TEXT NOT NULL DEFAULT 'asimi-cli'`,
			`ALTER TABLE lings ADD COLUMN username TEXT NOT NULL DEFAULT 'daonb'`,
			`ALTER TABLE lings ADD COLUMN project TEXT NOT NULL DEFAULT 'asimi-cli'`,
			`ALTER TABLE forge_manifests ADD COLUMN username TEXT NOT NULL DEFAULT 'daonb'`,
			`ALTER TABLE forge_manifests ADD COLUMN project TEXT NOT NULL DEFAULT 'asimi-cli'`,
			`ALTER TABLE marshal_incidents ADD COLUMN username TEXT NOT NULL DEFAULT 'daonb'`,
			`ALTER TABLE marshal_incidents ADD COLUMN project TEXT NOT NULL DEFAULT 'asimi-cli'`,
			`ALTER TABLE ruler_councils ADD COLUMN username TEXT NOT NULL DEFAULT 'daonb'`,
			`ALTER TABLE ruler_councils ADD COLUMN project TEXT NOT NULL DEFAULT 'asimi-cli'`,
			`ALTER TABLE ritual_executions ADD COLUMN username TEXT NOT NULL DEFAULT 'daonb'`,
			`ALTER TABLE ritual_executions ADD COLUMN project TEXT NOT NULL DEFAULT 'asimi-cli'`,
		}

		for _, stmt := range alterStmts {
			if err := tx.Exec(stmt).Error; err != nil {
				if strings.Contains(err.Error(), "duplicate column") {
					continue // column already exists, skip
				}
				logger.Error("composite PK migration failed on ALTER", "error", err)
				return err
			}
		}

		logger.Info("composite PK migration complete")
		return nil
	})
}
