package storage

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // SQLite driver
)

// DB wraps the database connection with additional functionality
type DB struct {
	conn *sql.DB
	path string
}

// InitDB initializes the SQLite database and creates tables if needed
func InitDB(dbPath string) (*DB, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database connection
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	conn.SetMaxOpenConns(1) // SQLite works best with single connection
	conn.SetMaxIdleConns(1)

	// Enable foreign keys (SQLite requires this per connection)
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := conn.Exec("PRAGMA journal_mode = WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	db := &DB{
		conn: conn,
		path: dbPath,
	}

	// Check current schema version and migrate if needed
	currentVersion, err := db.getSchemaVersion()
	if err != nil {
		// Schema version table doesn't exist, create fresh schema
		if _, err := conn.Exec(Schema); err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to create schema: %w", err)
		}
		slog.Debug("SQLite database initialized with fresh schema", "path", dbPath, "version", SchemaVersion)
	} else if currentVersion < SchemaVersion {
		// Run migrations
		if err := db.runMigrations(currentVersion); err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to run migrations: %w", err)
		}
		slog.Debug("SQLite database migrated", "path", dbPath, "from_version", currentVersion, "to_version", SchemaVersion)
	} else {
		slog.Debug("SQLite database initialized", "path", dbPath, "version", currentVersion)
	}

	// Register custom REGEXP function
	if err := db.registerRegexpFunction(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to register regexp function: %w", err)
	}

	return db, nil
}

// getSchemaVersion returns the current schema version from the database
func (db *DB) getSchemaVersion() (int, error) {
	var version int
	err := db.conn.QueryRow("SELECT MAX(version) FROM schema_version").Scan(&version)
	if err != nil {
		return 0, err
	}
	return version, nil
}

// runMigrations runs all necessary migrations from currentVersion to SchemaVersion
func (db *DB) runMigrations(currentVersion int) error {
	// Run migrations in order
	for v := currentVersion + 1; v <= SchemaVersion; v++ {
		slog.Info("Running database migration", "from_version", v-1, "to_version", v)

		var migrationSQL string
		switch v {
		case 2:
			migrationSQL = Migration1to2
		case 3:
			migrationSQL = Migration2to3
		case 4:
			migrationSQL = Migration3to4
		case 5:
			migrationSQL = Migration4to5
		case 6:
			migrationSQL = Migration5to6
		default:
			return fmt.Errorf("unknown migration version: %d", v)
		}

		if _, err := db.conn.Exec(migrationSQL); err != nil {
			return fmt.Errorf("failed to run migration to version %d: %w", v, err)
		}
	}

	return nil
}

// registerRegexpFunction adds REGEXP support to SQLite
func (db *DB) registerRegexpFunction() error {
	// Note: The sqlite driver from modernc.org/sqlite doesn't support
	// sql.Conn.Raw() for custom functions. We'll implement REGEXP
	// filtering in Go code after querying instead.
	// This is a placeholder - we'll handle regex in application layer.

	return nil
}

// Close closes the database connection
func (db *DB) Close() error {
	if db.conn != nil {
		return db.conn.Close()
	}
	return nil
}

// Conn returns the underlying database connection
func (db *DB) Conn() *sql.DB {
	return db.conn
}

// Path returns the database file path
func (db *DB) Path() string {
	return db.path
}

// Vacuum optimizes the database file
func (db *DB) Vacuum() error {
	_, err := db.conn.Exec("VACUUM")
	return err
}

// Stats returns database statistics
func (db *DB) Stats() (map[string]int64, error) {
	stats := make(map[string]int64)

	// Count repositories
	var repoCount int64
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM repositories").Scan(&repoCount); err != nil {
		return nil, err
	}
	stats["repositories"] = repoCount

	// Count branches
	var branchCount int64
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM branches").Scan(&branchCount); err != nil {
		return nil, err
	}
	stats["branches"] = branchCount

	// Count sessions
	var sessionCount int64
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&sessionCount); err != nil {
		return nil, err
	}
	stats["sessions"] = sessionCount

	// Count messages
	var messageCount int64
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM messages").Scan(&messageCount); err != nil {
		return nil, err
	}
	stats["messages"] = messageCount

	// Count prompt history
	var promptCount int64
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM prompt_history").Scan(&promptCount); err != nil {
		return nil, err
	}
	stats["prompt_history"] = promptCount

	// Count command history
	var commandCount int64
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM command_history").Scan(&commandCount); err != nil {
		return nil, err
	}
	stats["command_history"] = commandCount

	// Count workflows (if table exists)
	var workflowCount int64
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM workflows").Scan(&workflowCount); err == nil {
		stats["workflows"] = workflowCount
	}

	// Count workflow steps (if table exists)
	var stepCount int64
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM workflow_steps").Scan(&stepCount); err == nil {
		stats["workflow_steps"] = stepCount
	}

	return stats, nil
}
