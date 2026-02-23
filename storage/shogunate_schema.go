package storage

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// JSON is a custom type for storing JSON data in the database
type JSON map[string]interface{}

// Value implements driver.Valuer for JSON
func (j JSON) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan implements sql.Scanner for JSON
func (j *JSON) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal JSON value: %v", value)
	}
	return json.Unmarshal(bytes, j)
}

// StringArray is a custom type for storing string arrays in the database
type StringArray []string

// Value implements driver.Valuer for StringArray
func (s StringArray) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	return json.Marshal(s)
}

// Scan implements sql.Scanner for StringArray
func (s *StringArray) Scan(value interface{}) error {
	if value == nil {
		*s = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal StringArray value: %v", value)
	}
	return json.Unmarshal(bytes, s)
}

// EdictStatus represents the current status of an edict
type EdictStatus string

const (
	EdictActive    EdictStatus = "active"
	EdictBlocked   EdictStatus = "blocked"
	EdictSealed    EdictStatus = "sealed"
	EdictCancelled EdictStatus = "cancelled"
)

// Edict represents a high-level task/issue being processed by the Shogunate
type Edict struct {
	EdictID   string      `gorm:"primaryKey;column:edict_id"`
	SessionID string      `gorm:"column:session_id;index"`
	IssueRef  string      `gorm:"column:issue_ref"`
	Intent    string      `gorm:"column:intent"`
	Status    EdictStatus `gorm:"column:status"`
	CreatedAt time.Time   `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time   `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName returns the table name for Edict
func (Edict) TableName() string {
	return "edicts"
}

// ZhengmingQuestion represents a single question with predefined answer options
type ZhengmingQuestion struct {
	Text    string   `json:"text"`
	Options []string `json:"options"`
}

// ZhengmingQuestions is a slice of ZhengmingQuestion stored as JSON in the database
type ZhengmingQuestions []ZhengmingQuestion

// Value implements driver.Valuer for ZhengmingQuestions
func (q ZhengmingQuestions) Value() (driver.Value, error) {
	if q == nil {
		return nil, nil
	}
	return json.Marshal(q)
}

// Scan implements sql.Scanner for ZhengmingQuestions
func (q *ZhengmingQuestions) Scan(value interface{}) error {
	if value == nil {
		*q = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal ZhengmingQuestions value: %v", value)
	}
	return json.Unmarshal(bytes, q)
}

// ZhengmingWaiter blocks a tool goroutine until the user answers
type ZhengmingWaiter interface {
	WaitForAnswer(ctx context.Context, requestID string) (string, error)
}

// ZhengmingPriority represents the priority of a clarification request
type ZhengmingPriority string

const (
	PriorityNormal ZhengmingPriority = "normal"
	PriorityUrgent ZhengmingPriority = "urgent"
)

// ZhengmingStatus represents the status of a clarification request
type ZhengmingStatus string

const (
	ZhengmingPending  ZhengmingStatus = "pending"
	ZhengmingAnswered ZhengmingStatus = "answered"
	ZhengmingExpired  ZhengmingStatus = "expired"
)

// Zhengming represents a clarification request from a minister
type Zhengming struct {
	RequestID  string              `gorm:"primaryKey;column:request_id"`
	EdictID    string              `gorm:"column:edict_id;index"`
	MinisterID string              `gorm:"column:minister_id"`
	Questions  ZhengmingQuestions  `gorm:"column:question;type:text"`
	Answer     string              `gorm:"column:answer"`
	Priority   ZhengmingPriority   `gorm:"column:priority"`
	Status     ZhengmingStatus     `gorm:"column:status"`
	TimeoutAt  time.Time           `gorm:"column:timeout_at"`
	AnsweredAt *time.Time          `gorm:"column:answered_at"`
	CreatedAt  time.Time           `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt  time.Time           `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName returns the table name for Zhengming
func (Zhengming) TableName() string {
	return "zhengming_requests"
}

// TianEvent represents an event in the Tian ledger
type TianEvent struct {
	ID        uint      `gorm:"primaryKey;autoIncrement"`
	EdictID   string    `gorm:"column:edict_id;index"`
	EventType string    `gorm:"column:event_type"`
	Payload   JSON      `gorm:"column:payload;type:json"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
}

// TableName returns the table name for TianEvent
func (TianEvent) TableName() string {
	return "tian_events"
}

// TianEventDLQ represents a dead-letter queue entry for failed event processing
type TianEventDLQ struct {
	ID           uint      `gorm:"primaryKey;autoIncrement"`
	OriginalID   uint      `gorm:"column:original_id"`
	EdictID      string    `gorm:"column:edict_id;index"`
	EventType    string    `gorm:"column:event_type"`
	Payload      JSON      `gorm:"column:payload;type:json"`
	ErrorMessage string    `gorm:"column:error_message"`
	RetryCount   int       `gorm:"column:retry_count"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime"`
}

// TableName returns the table name for TianEventDLQ
func (TianEventDLQ) TableName() string {
	return "tian_event_dlq"
}

// LingStatus represents the status of a ling (sub-task)
type LingStatus string

const (
	LingPending    LingStatus = "pending"
	LingInProgress LingStatus = "in_progress"
	LingDone       LingStatus = "done"
	LingBlocked    LingStatus = "blocked"
)

// Ling represents a sub-task or step in an edict's execution plan
type Ling struct {
	LingID       string      `gorm:"primaryKey;column:ling_id"`
	EdictID      string      `gorm:"column:edict_id;index"`
	Description  string      `gorm:"column:description"`
	Dependencies StringArray `gorm:"column:dependencies;type:json"`
	Status       LingStatus  `gorm:"column:status"`
	CreatedAt    time.Time   `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt    time.Time   `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName returns the table name for Ling
func (Ling) TableName() string {
	return "lings"
}

// ManifestStatus represents the status of a forge manifest
type ManifestStatus string

const (
	ManifestStaged   ManifestStatus = "staged"
	ManifestLive     ManifestStatus = "live"
	ManifestQuenched ManifestStatus = "quenched"
	ManifestRejected ManifestStatus = "rejected"
)

// ForgeManifest represents a code change manifest from the Forge
type ForgeManifest struct {
	ManifestID string         `gorm:"primaryKey;column:manifest_id"`
	EdictID    string         `gorm:"column:edict_id;index"`
	LingID     string         `gorm:"column:ling_id"`
	FilePath   string         `gorm:"column:file_path"`
	FuncName   string         `gorm:"column:func_name"`
	ContentSHA string         `gorm:"column:content_sha"`
	CommitHash string         `gorm:"column:commit_hash"`
	Status     ManifestStatus `gorm:"column:status"`
	VerdictID  string         `gorm:"column:verdict_id"`
	CreatedAt  time.Time      `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt  time.Time      `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName returns the table name for ForgeManifest
func (ForgeManifest) TableName() string {
	return "forge_manifests"
}

// VerdictOutcome represents the outcome of a judge's verdict
type VerdictOutcome string

const (
	VerdictPassed VerdictOutcome = "passed"
	VerdictFailed VerdictOutcome = "failed"
)

// JudgeVerdict represents a test/CI verdict from the Judge
type JudgeVerdict struct {
	VerdictID  string         `gorm:"primaryKey;column:verdict_id"`
	ManifestID string         `gorm:"column:manifest_id;index"`
	TestSuite  string         `gorm:"column:test_suite"`
	Outcome    VerdictOutcome `gorm:"column:outcome"`
	Evidence   JSON           `gorm:"column:evidence;type:json"`
	CreatedAt  time.Time      `gorm:"column:created_at;autoCreateTime"`
}

// TableName returns the table name for JudgeVerdict
func (JudgeVerdict) TableName() string {
	return "judge_verdicts"
}

// PrecedentRuling represents the ruling of a censor precedent
type PrecedentRuling string

const (
	PrecedentApproved PrecedentRuling = "approved"
	PrecedentRejected PrecedentRuling = "rejected"
)

// CensorPrecedent represents an ethics review precedent from the Censor
type CensorPrecedent struct {
	PrecedentID   string          `gorm:"primaryKey;column:precedent_id"`
	ManifestID    string          `gorm:"column:manifest_id;index"`
	Principle     string          `gorm:"column:principle"`
	Ruling        PrecedentRuling `gorm:"column:ruling"`
	Justification string          `gorm:"column:justification"`
	CreatedAt     time.Time       `gorm:"column:created_at;autoCreateTime"`
}

// TableName returns the table name for CensorPrecedent
func (CensorPrecedent) TableName() string {
	return "censor_precedents"
}

// MarshalIncident represents a production incident tracked by the Marshal
type MarshalIncident struct {
	IncidentID string    `gorm:"primaryKey;column:incident_id"`
	CommitHash string    `gorm:"column:commit_hash;index"`
	EdictID    string    `gorm:"column:edict_id;index"`
	RCASummary string    `gorm:"column:rca_summary"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt  time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName returns the table name for MarshalIncident
func (MarshalIncident) TableName() string {
	return "marshal_incidents"
}

// RulerCouncil represents a high-stakes decision requiring human approval
type RulerCouncil struct {
	CouncilID  string    `gorm:"primaryKey;column:council_id"`
	EdictID    string    `gorm:"column:edict_id;index"`
	Decision   string    `gorm:"column:decision"`
	Approved   bool      `gorm:"column:approved"`
	ApprovedBy string    `gorm:"column:approved_by"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt  time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName returns the table name for RulerCouncil
func (RulerCouncil) TableName() string {
	return "ruler_councils"
}

// RitualGuardCheckpoint represents a checkpoint for the Ritual Guard's event processing
type RitualGuardCheckpoint struct {
	ID        uint      `gorm:"primaryKey;autoIncrement"`
	EventID   uint      `gorm:"column:event_id"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName returns the table name for RitualGuardCheckpoint
func (RitualGuardCheckpoint) TableName() string {
	return "ritual_guard_checkpoint"
}

// ShogunateSchema is the SQL DDL for creating Shogunate tables
const ShogunateSchema = `
-- Edicts table (high-level tasks/issues)
CREATE TABLE IF NOT EXISTS edicts (
    edict_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL DEFAULT '',
    issue_ref TEXT NOT NULL,
    intent TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE INDEX IF NOT EXISTS idx_edicts_status ON edicts(status);
CREATE INDEX IF NOT EXISTS idx_edicts_session ON edicts(session_id);

-- Zhengming requests table (clarification requests)
CREATE TABLE IF NOT EXISTS zhengming_requests (
    request_id TEXT PRIMARY KEY,
    edict_id TEXT NOT NULL,
    minister_id TEXT NOT NULL,
    question TEXT NOT NULL,
    answer TEXT NOT NULL DEFAULT '',
    priority TEXT NOT NULL DEFAULT 'normal',
    status TEXT NOT NULL DEFAULT 'pending',
    timeout_at INTEGER NOT NULL,
    answered_at INTEGER,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
    FOREIGN KEY (edict_id) REFERENCES edicts(edict_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_zhengming_edict ON zhengming_requests(edict_id);
CREATE INDEX IF NOT EXISTS idx_zhengming_status ON zhengming_requests(status);

-- Tian events table (event ledger)
CREATE TABLE IF NOT EXISTS tian_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    edict_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    FOREIGN KEY (edict_id) REFERENCES edicts(edict_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_tian_events_edict ON tian_events(edict_id);

-- Tian events dead letter queue
CREATE TABLE IF NOT EXISTS tian_event_dlq (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    original_id INTEGER NOT NULL,
    edict_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL DEFAULT '{}',
    error_message TEXT NOT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

-- Lings table (sub-tasks)
CREATE TABLE IF NOT EXISTS lings (
    ling_id TEXT PRIMARY KEY,
    edict_id TEXT NOT NULL,
    description TEXT NOT NULL,
    dependencies TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'pending',
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
    FOREIGN KEY (edict_id) REFERENCES edicts(edict_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_lings_edict ON lings(edict_id);

-- Forge manifests table (code changes)
CREATE TABLE IF NOT EXISTS forge_manifests (
    manifest_id TEXT PRIMARY KEY,
    edict_id TEXT NOT NULL,
    ling_id TEXT NOT NULL DEFAULT '',
    file_path TEXT NOT NULL,
    func_name TEXT NOT NULL,
    content_sha TEXT NOT NULL,
    commit_hash TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'staged',
    verdict_id TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
    FOREIGN KEY (edict_id) REFERENCES edicts(edict_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_forge_manifests_edict ON forge_manifests(edict_id);
CREATE INDEX IF NOT EXISTS idx_forge_manifests_status ON forge_manifests(status);

-- Judge verdicts table (test/CI results)
CREATE TABLE IF NOT EXISTS judge_verdicts (
    verdict_id TEXT PRIMARY KEY,
    manifest_id TEXT NOT NULL,
    test_suite TEXT NOT NULL,
    outcome TEXT NOT NULL,
    evidence TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    FOREIGN KEY (manifest_id) REFERENCES forge_manifests(manifest_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_judge_verdicts_manifest ON judge_verdicts(manifest_id);

-- Censor precedents table (ethics reviews)
CREATE TABLE IF NOT EXISTS censor_precedents (
    precedent_id TEXT PRIMARY KEY,
    manifest_id TEXT NOT NULL,
    principle TEXT NOT NULL,
    ruling TEXT NOT NULL,
    justification TEXT NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    FOREIGN KEY (manifest_id) REFERENCES forge_manifests(manifest_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_censor_precedents_manifest ON censor_precedents(manifest_id);

-- Marshal incidents table (production incidents)
CREATE TABLE IF NOT EXISTS marshal_incidents (
    incident_id TEXT PRIMARY KEY,
    commit_hash TEXT NOT NULL,
    edict_id TEXT NOT NULL DEFAULT '',
    rca_summary TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE INDEX IF NOT EXISTS idx_marshal_incidents_commit ON marshal_incidents(commit_hash);
CREATE INDEX IF NOT EXISTS idx_marshal_incidents_edict ON marshal_incidents(edict_id);

-- Ruler councils table (human approval decisions)
CREATE TABLE IF NOT EXISTS ruler_councils (
    council_id TEXT PRIMARY KEY,
    edict_id TEXT NOT NULL,
    decision TEXT NOT NULL,
    approved INTEGER NOT NULL DEFAULT 0,
    approved_by TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
    FOREIGN KEY (edict_id) REFERENCES edicts(edict_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_ruler_councils_edict ON ruler_councils(edict_id);

-- Ritual guard checkpoint table
CREATE TABLE IF NOT EXISTS ritual_guard_checkpoint (
    id INTEGER PRIMARY KEY,
    event_id INTEGER NOT NULL,
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

-- Ritual executions table (tracks running ritual instances)
CREATE TABLE IF NOT EXISTS ritual_executions (
    id TEXT PRIMARY KEY,
    ritual_name TEXT NOT NULL,
    edict_id TEXT NOT NULL,
    current_step INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'pending',
    data TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
    FOREIGN KEY (edict_id) REFERENCES edicts(edict_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_ritual_executions_edict ON ritual_executions(edict_id);
CREATE INDEX IF NOT EXISTS idx_ritual_executions_state ON ritual_executions(state);

-- Ritual step states table (tracks step state within executions)
CREATE TABLE IF NOT EXISTS ritual_step_states (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    execution_id TEXT NOT NULL,
    step_index INTEGER NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    retry_count INTEGER NOT NULL DEFAULT 0,
    message TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (execution_id) REFERENCES ritual_executions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_ritual_step_states_execution ON ritual_step_states(execution_id);
`
