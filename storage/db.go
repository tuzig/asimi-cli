package storage

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite" // Pure-Go SQLite driver for GORM
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB wraps the GORM database connection
type DB struct {
	conn *gorm.DB
	path string
}

// InitDB initializes the SQLite database with GORM and runs migrations
func InitDB(dbPath string) (*DB, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Configure GORM logger (silent by default)
	gormLogger := logger.Default.LogMode(logger.Silent)

	// Open database with GORM
	conn, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Get underlying sql.DB to configure connection pool
	sqlDB, err := conn.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying database: %w", err)
	}

	// SQLite works best with single connection
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	// Enable foreign keys and WAL mode
	if err := conn.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	if err := conn.Exec("PRAGMA journal_mode = WAL").Error; err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Set busy timeout to handle concurrent access (5 seconds)
	if err := conn.Exec("PRAGMA busy_timeout = 5000").Error; err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	db := &DB{
		conn: conn,
		path: dbPath,
	}

	// Check if this is an existing database with schema already set up
	// We detect this by checking if the schema_version table exists and has data
	existingSchema := db.hasExistingSchema()

	if existingSchema {
		// Check if schema needs to be upgraded
		currentVersion, err := db.getSchemaVersion()
		if err != nil {
			slog.Warn("Could not read schema version, assuming current", "error", err)
			currentVersion = SchemaVersion
		}

		if currentVersion < SchemaVersion {
			// Run AutoMigrate to add new tables/columns
			// Note: GORM's AutoMigrate is additive - it won't drop columns or tables
			// For destructive migrations, add custom migration logic here
			slog.Debug("Upgrading database schema", "from", currentVersion, "to", SchemaVersion)
			if err := db.autoMigrate(); err != nil {
				sqlDB.Close()
				return nil, fmt.Errorf("failed to run migrations: %w", err)
			}

			// Record new schema version
			if err := db.ensureSchemaVersion(); err != nil {
				sqlDB.Close()
				return nil, fmt.Errorf("failed to update schema version: %w", err)
			}
			slog.Debug("SQLite database upgraded", "path", dbPath, "version", SchemaVersion)
		} else {
			slog.Debug("SQLite database opened (existing schema)", "path", dbPath, "version", currentVersion)
		}
	} else {
		// Fresh database - run auto-migration for all models
		if err := db.autoMigrate(); err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("failed to run migrations: %w", err)
		}

		// Record schema version
		if err := db.ensureSchemaVersion(); err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("failed to set schema version: %w", err)
		}
		slog.Debug("SQLite database initialized with GORM", "path", dbPath, "version", SchemaVersion)
	}

	return db, nil
}

// hasExistingSchema checks if the database already has tables set up
func (db *DB) hasExistingSchema() bool {
	// Check if schema_version table exists and has records
	var count int64
	result := db.conn.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_version'").Scan(&count)
	if result.Error != nil || count == 0 {
		return false
	}

	// Check if there's at least one version record
	result = db.conn.Raw("SELECT COUNT(*) FROM schema_version").Scan(&count)
	return result.Error == nil && count > 0
}

// autoMigrate runs GORM auto-migration for all models
func (db *DB) autoMigrate() error {
	return db.conn.AutoMigrate(
		&Repository{},
		&Branch{},
		&DBSession{},
		&Message{},
		&PromptHistory{},
		&CommandHistory{},
		&WorkflowData{},
		&WorkflowStepData{},
		&SchemaVersionRecord{},
	)
}

// ensureSchemaVersion ensures the schema version is recorded
func (db *DB) ensureSchemaVersion() error {
	var record SchemaVersionRecord
	result := db.conn.First(&record, SchemaVersion)
	if result.Error == gorm.ErrRecordNotFound {
		record = SchemaVersionRecord{
			Version:   SchemaVersion,
			AppliedAt: time.Now().Unix(),
		}
		return db.conn.Create(&record).Error
	}
	return result.Error
}

// getSchemaVersion returns the current schema version from the database
func (db *DB) getSchemaVersion() (int, error) {
	var record SchemaVersionRecord
	result := db.conn.Order("version DESC").First(&record)
	if result.Error != nil {
		return 0, result.Error
	}
	return record.Version, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	sqlDB, err := db.conn.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Conn returns the underlying GORM connection
func (db *DB) Conn() *gorm.DB {
	return db.conn
}

// Path returns the database file path
func (db *DB) Path() string {
	return db.path
}

// Vacuum optimizes the database file
func (db *DB) Vacuum() error {
	return db.conn.Exec("VACUUM").Error
}

// Stats returns database statistics
func (db *DB) Stats() (map[string]int64, error) {
	stats := make(map[string]int64)

	var count int64

	if err := db.conn.Model(&Repository{}).Count(&count).Error; err != nil {
		return nil, err
	}
	stats["repositories"] = count

	if err := db.conn.Model(&Branch{}).Count(&count).Error; err != nil {
		return nil, err
	}
	stats["branches"] = count

	if err := db.conn.Model(&DBSession{}).Count(&count).Error; err != nil {
		return nil, err
	}
	stats["sessions"] = count

	if err := db.conn.Model(&Message{}).Count(&count).Error; err != nil {
		return nil, err
	}
	stats["messages"] = count

	if err := db.conn.Model(&PromptHistory{}).Count(&count).Error; err != nil {
		return nil, err
	}
	stats["prompt_history"] = count

	if err := db.conn.Model(&CommandHistory{}).Count(&count).Error; err != nil {
		return nil, err
	}
	stats["command_history"] = count

	if err := db.conn.Model(&WorkflowData{}).Count(&count).Error; err == nil {
		stats["workflows"] = count
	}

	if err := db.conn.Model(&WorkflowStepData{}).Count(&count).Error; err == nil {
		stats["workflow_steps"] = count
	}

	return stats, nil
}

// GetOrCreateRepository gets an existing repository or creates a new one
func (db *DB) GetOrCreateRepository(host, org, project string) (int64, error) {
	var repo Repository
	result := db.conn.Where(Repository{Host: host, Org: org, Project: project}).
		FirstOrCreate(&repo)

	if result.Error != nil {
		return 0, fmt.Errorf("failed to get or create repository: %w", result.Error)
	}

	return repo.ID, nil
}

// GetOrCreateBranch gets an existing branch or creates a new one
func (db *DB) GetOrCreateBranch(repositoryID int64, name string) (int64, error) {
	var branch Branch
	result := db.conn.Where(Branch{RepositoryID: repositoryID, Name: name}).
		FirstOrCreate(&branch)

	if result.Error != nil {
		return 0, fmt.Errorf("failed to get or create branch: %w", result.Error)
	}

	return branch.ID, nil
}

// GetRepository retrieves a repository by host, org and project
func (db *DB) GetRepository(host, org, project string) (*Repository, error) {
	var repo Repository
	result := db.conn.Where("host = ? AND org = ? AND project = ?", host, org, project).First(&repo)

	if result.Error == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if result.Error != nil {
		return nil, fmt.Errorf("failed to get repository: %w", result.Error)
	}

	return &repo, nil
}

// GetBranch retrieves a branch by repository ID and name
func (db *DB) GetBranch(repositoryID int64, name string) (*Branch, error) {
	var branch Branch
	result := db.conn.Where("repository_id = ? AND name = ?", repositoryID, name).First(&branch)

	if result.Error == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if result.Error != nil {
		return nil, fmt.Errorf("failed to get branch: %w", result.Error)
	}

	return &branch, nil
}

// ListRepositories returns all repositories
func (db *DB) ListRepositories() ([]Repository, error) {
	var repos []Repository
	result := db.conn.Order("host, org, project").Find(&repos)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to list repositories: %w", result.Error)
	}
	return repos, nil
}

// ListBranches returns all branches for a repository
func (db *DB) ListBranches(repositoryID int64) ([]Branch, error) {
	var branches []Branch
	result := db.conn.Where("repository_id = ?", repositoryID).Order("name").Find(&branches)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to list branches: %w", result.Error)
	}
	return branches, nil
}

// DeleteRepository deletes a repository and all its branches/sessions (CASCADE)
func (db *DB) DeleteRepository(id int64) error {
	result := db.conn.Delete(&Repository{}, id)
	if result.Error != nil {
		return fmt.Errorf("failed to delete repository: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("repository not found")
	}
	return nil
}

// DeleteBranch deletes a branch and all its sessions (CASCADE)
func (db *DB) DeleteBranch(id int64) error {
	result := db.conn.Delete(&Branch{}, id)
	if result.Error != nil {
		return fmt.Errorf("failed to delete branch: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("branch not found")
	}
	return nil
}
