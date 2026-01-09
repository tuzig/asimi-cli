package storage

import (
	"time"

	"github.com/tmc/langchaingo/llms"
)

// Schema version for migrations
const SchemaVersion = 2

// SessionConfig holds session persistence configuration
type SessionConfig struct {
	Enabled      bool
	MaxSessions  int
	MaxAgeDays   int
	ListLimit    int
	AutoSave     bool
	SaveInterval int
}

// HistoryConfig holds persistent history configuration
type HistoryConfig struct {
	Enabled      bool
	MaxSessions  int // Used as max entries for history
	MaxAgeDays   int
	ListLimit    int
	AutoSave     bool
	SaveInterval int
}

// Repository represents a Git repository (host/org/project)
type Repository struct {
	ID      int64  `gorm:"primaryKey;autoIncrement"`
	Host    string `gorm:"not null;uniqueIndex:idx_repo_unique,priority:1"`
	Org     string `gorm:"not null;uniqueIndex:idx_repo_unique,priority:2"`
	Project string `gorm:"not null;uniqueIndex:idx_repo_unique,priority:3"`
}

// TableName overrides the table name
func (Repository) TableName() string {
	return "repositories"
}

// Branch represents a Git branch within a repository
type Branch struct {
	ID           int64       `gorm:"primaryKey;autoIncrement"`
	RepositoryID int64       `gorm:"not null;uniqueIndex:idx_branch_unique,priority:1"`
	Name         string      `gorm:"not null;uniqueIndex:idx_branch_unique,priority:2"`
	Repository   *Repository `gorm:"foreignKey:RepositoryID;constraint:OnDelete:CASCADE"`
}

// TableName overrides the table name
func (Branch) TableName() string {
	return "branches"
}

// DBSession maps directly to the sessions table
type DBSession struct {
	ID          string  `gorm:"primaryKey"`
	BranchID    int64   `gorm:"not null;index:idx_sessions_branch"`
	CreatedAt   int64   `gorm:"not null;index:idx_sessions_created"`
	LastUpdated int64   `gorm:"not null;index:idx_sessions_updated"`
	FirstPrompt string  `gorm:"not null"`
	Provider    string  `gorm:"not null"`
	Model       string  `gorm:"not null"`
	WorkingDir  string  `gorm:"not null"`
	Branch      *Branch `gorm:"foreignKey:BranchID;constraint:OnDelete:CASCADE"`
}

// TableName overrides the table name
func (DBSession) TableName() string {
	return "sessions"
}

// SessionData contains the persistable session fields (used for API)
type SessionData struct {
	ID           string
	CreatedAt    time.Time
	LastUpdated  time.Time
	FirstPrompt  string
	Provider     string
	Model        string
	WorkingDir   string
	ProjectSlug  string
	Messages     []llms.MessageContent
	ContextFiles map[string]string
	MessageCount int
}

// Message represents a single message in a conversation
type Message struct {
	ID        int64      `gorm:"primaryKey;autoIncrement"`
	SessionID string     `gorm:"not null;index:idx_messages_session_seq,priority:1"`
	Sequence  int        `gorm:"not null;index:idx_messages_session_seq,priority:2"`
	Role      string     `gorm:"not null"`
	Content   string     `gorm:"not null"`
	CreatedAt int64      `gorm:"not null;index:idx_messages_created"`
	Session   *DBSession `gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
}

// TableName overrides the table name
func (Message) TableName() string {
	return "messages"
}

// PromptHistory represents a user prompt for autocomplete
type PromptHistory struct {
	ID        int64   `gorm:"primaryKey;autoIncrement"`
	BranchID  int64   `gorm:"not null;index:idx_prompt_history_branch_ts,priority:1"`
	Prompt    string  `gorm:"not null"`
	Timestamp int64   `gorm:"not null;index:idx_prompt_history_branch_ts,priority:2"`
	Branch    *Branch `gorm:"foreignKey:BranchID;constraint:OnDelete:CASCADE"`
}

// TableName overrides the table name
func (PromptHistory) TableName() string {
	return "prompt_history"
}

// CommandHistory represents a slash command for history
type CommandHistory struct {
	ID        int64   `gorm:"primaryKey;autoIncrement"`
	BranchID  int64   `gorm:"not null;index:idx_command_history_branch_ts,priority:1"`
	Command   string  `gorm:"not null"`
	Timestamp int64   `gorm:"not null;index:idx_command_history_branch_ts,priority:2"`
	Branch    *Branch `gorm:"foreignKey:BranchID;constraint:OnDelete:CASCADE"`
}

// TableName overrides the table name
func (CommandHistory) TableName() string {
	return "command_history"
}

// SchemaVersion tracks schema migrations
type SchemaVersionRecord struct {
	Version   int   `gorm:"primaryKey"`
	AppliedAt int64 `gorm:"not null"`
}

// TableName overrides the table name
func (SchemaVersionRecord) TableName() string {
	return "schema_version"
}

// WorkflowState represents the overall state of a workflow
type WorkflowState string

const (
	WorkflowStatePending   WorkflowState = "pending"
	WorkflowStateRunning   WorkflowState = "running"
	WorkflowStateCompleted WorkflowState = "completed"
	WorkflowStateAborted   WorkflowState = "aborted"
	WorkflowStateFailed    WorkflowState = "failed"
)

// StepStatus represents the status of a workflow step
type StepStatus string

const (
	StepStatusPending   StepStatus = "pending"
	StepStatusRunning   StepStatus = "running"
	StepStatusCompleted StepStatus = "completed"
	StepStatusFailed    StepStatus = "failed"
	StepStatusSkipped   StepStatus = "skipped"
)

// WorkflowData represents a workflow stored in the database
type WorkflowData struct {
	ID          string        `gorm:"primaryKey"`
	BranchID    int64         `gorm:"not null;index:idx_workflows_branch_updated,priority:1"`
	Name        string        `gorm:"not null"`
	CurrentStep int           `gorm:"not null;default:0"`
	State       WorkflowState `gorm:"not null;default:'pending';index:idx_workflows_state"`
	MaxRetries  int           `gorm:"not null;default:3"`
	Data        string        `gorm:"not null;default:'{}'"`
	CreatedAt   int64         `gorm:"not null"`
	UpdatedAt   int64         `gorm:"not null;index:idx_workflows_branch_updated,priority:2"`
	Branch      *Branch       `gorm:"foreignKey:BranchID;constraint:OnDelete:CASCADE"`
}

// TableName overrides the table name
func (WorkflowData) TableName() string {
	return "workflows"
}

// WorkflowStepData represents a workflow step stored in the database
type WorkflowStepData struct {
	ID             int64         `gorm:"primaryKey;autoIncrement"`
	WorkflowID     string        `gorm:"not null;uniqueIndex:idx_workflow_step_unique,priority:1"`
	StepIndex      int           `gorm:"not null;uniqueIndex:idx_workflow_step_unique,priority:2"`
	Name           string        `gorm:"not null"`
	Status         StepStatus    `gorm:"not null;default:'pending'"`
	RetryCount     int           `gorm:"not null;default:0"`
	Message        string        `gorm:"not null;default:''"`
	PromptTemplate string        `gorm:"not null;default:''"`
	PrepareData    string        `gorm:"not null;default:'{}'"`
	Workflow       *WorkflowData `gorm:"foreignKey:WorkflowID;constraint:OnDelete:CASCADE"`
}

// TableName overrides the table name
func (WorkflowStepData) TableName() string {
	return "workflow_steps"
}

// HistoryEntry represents a single history item (prompt or command)
type HistoryEntry struct {
	Content   string
	Timestamp time.Time
}
