package storage

import (
	"log/slog"

	"gorm.io/gorm"
)

// MigrateEdictIDToUint converts edict_id columns from TEXT to INTEGER across all
// GORM-managed shogunate tables. Existing string IDs get new auto-increment uint IDs.
// This is a no-op if the column is already INTEGER.
func MigrateEdictIDToUint(db *gorm.DB, logger *slog.Logger) error {
	// Check if edicts table exists and has TEXT edict_id
	var colType string
	row := db.Raw(`SELECT type FROM pragma_table_info('edicts') WHERE name = 'edict_id'`).Row()
	if row == nil {
		return nil // table doesn't exist yet, AutoMigrate will create it
	}
	if err := row.Scan(&colType); err != nil {
		return nil // table doesn't exist or no edict_id column
	}
	if colType == "integer" {
		return nil // already migrated
	}

	logger.Info("migrating edict_id from TEXT to INTEGER")

	// All tables with edict_id that need migration (SQLite copy-table pattern).
	// We rebuild edicts first since other tables reference it.
	return db.Transaction(func(tx *gorm.DB) error {
		stmts := []string{
			// 1. Rebuild edicts: assign running numbers via ROWID
			`CREATE TABLE edicts_new (
				edict_id INTEGER PRIMARY KEY AUTOINCREMENT,
				session_id TEXT,
				issue_ref TEXT,
				summary TEXT,
				intent TEXT,
				cancelled_at INTEGER,
				created_at DATETIME,
				updated_at DATETIME
			)`,
			`INSERT INTO edicts_new (edict_id, session_id, issue_ref, summary, intent, cancelled_at, created_at, updated_at)
				SELECT ROWID, session_id, '', summary, intent, cancelled_at, created_at, updated_at FROM edicts ORDER BY created_at`,

			// 2. Build a mapping table from old TEXT id to new INTEGER id
			`CREATE TEMP TABLE edict_id_map AS
				SELECT e.edict_id AS old_id, n.edict_id AS new_id
				FROM edicts e
				JOIN edicts_new n ON n.edict_id = (
					SELECT ROWID FROM edicts WHERE edict_id = e.edict_id
				)`,

			// 3. Migrate each referencing table
			// -- seals
			`UPDATE seals SET edict_id = (
				SELECT new_id FROM edict_id_map WHERE old_id = seals.edict_id
			) WHERE EXISTS (SELECT 1 FROM edict_id_map WHERE old_id = seals.edict_id)`,

			// -- tian_events
			`UPDATE tian_events SET edict_id = (
				SELECT new_id FROM edict_id_map WHERE old_id = tian_events.edict_id
			) WHERE EXISTS (SELECT 1 FROM edict_id_map WHERE old_id = tian_events.edict_id)`,

			// -- tian_event_dlq
			`UPDATE tian_event_dlq SET edict_id = (
				SELECT new_id FROM edict_id_map WHERE old_id = tian_event_dlq.edict_id
			) WHERE EXISTS (SELECT 1 FROM edict_id_map WHERE old_id = tian_event_dlq.edict_id)`,

			// -- zhengming_requests
			`UPDATE zhengming_requests SET edict_id = (
				SELECT new_id FROM edict_id_map WHERE old_id = zhengming_requests.edict_id
			) WHERE EXISTS (SELECT 1 FROM edict_id_map WHERE old_id = zhengming_requests.edict_id)`,

			// -- lings
			`UPDATE lings SET edict_id = (
				SELECT new_id FROM edict_id_map WHERE old_id = lings.edict_id
			) WHERE EXISTS (SELECT 1 FROM edict_id_map WHERE old_id = lings.edict_id)`,

			// -- forge_manifests
			`UPDATE forge_manifests SET edict_id = (
				SELECT new_id FROM edict_id_map WHERE old_id = forge_manifests.edict_id
			) WHERE EXISTS (SELECT 1 FROM edict_id_map WHERE old_id = forge_manifests.edict_id)`,

			// -- marshal_incidents
			`UPDATE marshal_incidents SET edict_id = (
				SELECT new_id FROM edict_id_map WHERE old_id = marshal_incidents.edict_id
			) WHERE EXISTS (SELECT 1 FROM edict_id_map WHERE old_id = marshal_incidents.edict_id)`,

			// -- ruler_councils
			`UPDATE ruler_councils SET edict_id = (
				SELECT new_id FROM edict_id_map WHERE old_id = ruler_councils.edict_id
			) WHERE EXISTS (SELECT 1 FROM edict_id_map WHERE old_id = ruler_councils.edict_id)`,

			// -- ritual_executions
			`UPDATE ritual_executions SET edict_id = (
				SELECT new_id FROM edict_id_map WHERE old_id = ritual_executions.edict_id
			) WHERE EXISTS (SELECT 1 FROM edict_id_map WHERE old_id = ritual_executions.edict_id)`,

			// 4. Swap edicts table
			`DROP TABLE edicts`,
			`ALTER TABLE edicts_new RENAME TO edicts`,

			// 5. Cleanup
			`DROP TABLE IF EXISTS edict_id_map`,
		}

		for _, stmt := range stmts {
			if err := tx.Exec(stmt).Error; err != nil {
				preview := stmt
				if len(preview) > 80 {
					preview = preview[:80]
				}
				logger.Error("migration statement failed", "error", err, "sql", preview)
				return err
			}
		}

		logger.Info("edict_id migration complete")
		return nil
	})
}
