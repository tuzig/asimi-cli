package storage

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// JSON is a custom type for storing JSON in SQLite
// It serializes to JSON and implements sql.Scanner and driver.Valuer interfaces
type JSON json.RawMessage

// Scan implements sql.Scanner for JSON
func (j *JSON) Scan(value interface{}) error {
	if value == nil {
		*j = []byte("null")
		return nil
	}
	switch v := value.(type) {
	case []byte:
		*j = v
	case string:
		*j = []byte(v)
	default:
		return errors.New("failed to unmarshal JSON value")
	}
	return nil
}

// Value implements driver.Valuer for JSON
func (j JSON) Value() (driver.Value, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return []byte(j), nil
}

// GormDataType specifies the GORM data type for JSON
func (JSON) GormDataType() string {
	return "text"
}

// StringArray is a custom type for storing string arrays in SQLite
// It serializes to JSON and implements sql.Scanner and driver.Valuer interfaces
type StringArray []string

// Scan implements sql.Scanner for StringArray
func (a *StringArray) Scan(value interface{}) error {
	if value == nil {
		*a = []string{}
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.New("failed to unmarshal StringArray value")
	}
	return json.Unmarshal(bytes, a)
}

// Value implements driver.Valuer for StringArray
func (a StringArray) Value() (driver.Value, error) {
	if len(a) == 0 {
		return "[]", nil
	}
	return json.Marshal(a)
}

// GormDataType specifies the GORM data type for StringArray
func (StringArray) GormDataType() string {
	return "text"
}

// EdictPhase represents the lifecycle state of an edict
type EdictPhase string

const (
	PhasePlanning  EdictPhase = "planning"
	PhaseForging   EdictPhase = "forging"
	PhaseJudgment  EdictPhase = "judgment"
	PhaseReview    EdictPhase = "review"
	PhaseMerged    EdictPhase = "merged"
	PhaseCancelled EdictPhase = "cancelled"
)

// ZhengmingPriority for clarification urgency
type ZhengmingPriority string

const (
	PriorityLow    ZhengmingPriority = "low"
	PriorityNormal ZhengmingPriority = "normal"
	PriorityUrgent ZhengmingPriority = "urgent"
)

// ZhengmingStatus for request lifecycle
type ZhengmingStatus string

const (
	ZhengmingPending   ZhengmingStatus = "pending"
	ZhengmingAnswered  ZhengmingStatus = "answered"
	ZhengmingEscalated ZhengmingStatus = "escalated"
)

// LingStatus for task order lifecycle
type LingStatus string

const (
	LingPending   LingStatus = "pending"
	LingCompleted LingStatus = "completed"
)

// ManifestStatus for code artifact lifecycle
type ManifestStatus string

const (
	ManifestStaging  ManifestStatus = "staging"
	ManifestPending  ManifestStatus = "pending"
	ManifestQuenched ManifestStatus = "quenched"
	ManifestRejected ManifestStatus = "rejected"
)

// VerdictOutcome from CI judgment
type VerdictOutcome string

const (
	VerdictPassed VerdictOutcome = "passed"
	VerdictFailed VerdictOutcome = "failed"
)

// PrecedentRuling from the Censor
type PrecedentRuling string

const (
	RulingReject PrecedentRuling = "reject"
	RulingWaive  PrecedentRuling = "waive"
)

// Edict is the source of Ren: all work originates here
type Edict struct {
	EdictID            string     `gorm:"primaryKey"`         // GitHub Issue number (e.g., "owner/repo#123")
	RenIntent          string     `gorm:"not null;type:text"` // Ruler's original description
	RenIntentVersion   int        `gorm:"default:1"`
	CurrentPhase       EdictPhase `gorm:"not null;default:'planning';index:idx_edicts_phase"`
	ChancellorSeal     bool       `gorm:"default:false"`
	CensorSeal         bool       `gorm:"default:false"`
	CancelledAt        *time.Time
	CancelledBy        string    `gorm:"size:255"`
	CancellationReason string    `gorm:"type:text"`
	LastActivityAt     time.Time `gorm:"autoUpdateTime;index:idx_edicts_stale"`
	CreatedAt          time.Time `gorm:"autoCreateTime"`
}

// TableName specifies the table name for Edict
func (Edict) TableName() string {
	return "edicts"
}

// ZhengmingRequest halts work until the Ruler clarifies
type ZhengmingRequest struct {
	RequestID   string            `gorm:"primaryKey;size:64"`
	EdictID     string            `gorm:"not null;index:idx_zhengming_edict;size:255"` // References edicts.edict_id
	MinisterID  string            `gorm:"not null;size:50"`                            // 'strategist', 'forge', etc.
	Question    string            `gorm:"not null;type:text"`
	Priority    ZhengmingPriority `gorm:"not null;default:'normal'"`
	Status      ZhengmingStatus   `gorm:"not null;default:'pending';index:idx_zhengming_status"`
	Answer      string            `gorm:"type:text"`
	TimeoutAt   time.Time         `gorm:"not null;index:idx_zhengming_timeout"`
	EscalatedTo string            `gorm:"size:50"` // 'council' or 'chancellor'
	CreatedAt   time.Time         `gorm:"autoCreateTime"`
	AnsweredAt  *time.Time
}

// TableName specifies the table name for ZhengmingRequest
func (ZhengmingRequest) TableName() string {
	return "zhengming_requests"
}

// TianEvent is the Ritual Guard's event ledger
type TianEvent struct {
	EventID   int64     `gorm:"primaryKey;autoIncrement"`
	EdictID   string    `gorm:"index:idx_tian_events_edict;size:255"`
	EventType string    `gorm:"not null;size:100"` // 'edict_assigned', 'commit_pushed', etc.
	Payload   JSON      `gorm:"type:text"`
	CreatedAt time.Time `gorm:"autoCreateTime;index:idx_tian_events_edict"`
}

// TableName specifies the table name for TianEvent
func (TianEvent) TableName() string {
	return "tian_events"
}

// TianEventDLQ is the dead letter queue for failed events
type TianEventDLQ struct {
	DLQID             int64     `gorm:"primaryKey;autoIncrement"`
	EventID           int64     `gorm:"not null;index:idx_tian_dlq_event"`
	EdictID           string    `gorm:"size:255"`
	EventType         string    `gorm:"not null;size:100"`
	Payload           JSON      `gorm:"type:text"`
	ErrorMessage      string    `gorm:"not null;type:text"`
	RetryCount        int       `gorm:"not null;default:0"`
	CreatedAt         time.Time `gorm:"autoCreateTime;index:idx_tian_dlq_created"`
	OriginalCreatedAt time.Time `gorm:"not null"`
}

// TableName specifies the table name for TianEventDLQ
func (TianEventDLQ) TableName() string {
	return "tian_events_dlq"
}

// Ling (令) is a task order issued by the Strategist
type Ling struct {
	LingID         string      `gorm:"primaryKey;size:64"`
	EdictID        string      `gorm:"not null;index:idx_ling_edict_status,priority:1;size:255"`
	Description    string      `gorm:"not null;type:text"`
	Dependencies   StringArray `gorm:"type:text"` // JSON array of LingID references
	Status         LingStatus  `gorm:"not null;default:'pending';index:idx_ling_edict_status,priority:2"`
	IdempotencyKey string      `gorm:"uniqueIndex;size:64"` // sha256(edict_id + ren_intent_version + description)
	CreatedAt      time.Time   `gorm:"autoCreateTime"`
}

// TableName specifies the table name for Ling
func (Ling) TableName() string {
	return "ling"
}

// ForgeManifest tracks code artifacts (Earth is immutable)
type ForgeManifest struct {
	ManifestID     string         `gorm:"primaryKey;size:64"`
	EdictID        string         `gorm:"not null;index:idx_forge_manifest_edict_status,priority:1;size:255"`
	LingID         string         `gorm:"index:idx_forge_manifest_ling;size:64"`
	CommitHash     string         `gorm:"uniqueIndex;size:64"` // Empty until git commit succeeds
	FilePath       string         `gorm:"not null;size:1000"`
	QualifiedName  string         `gorm:"not null;size:500"`
	Status         ManifestStatus `gorm:"not null;default:'staging';index:idx_forge_manifest_edict_status,priority:2"`
	VerdictID      string         `gorm:"index;size:64"`
	IdempotencyKey string         `gorm:"uniqueIndex;size:64"` // sha256(edict_id + ling_id + file_path + patch_hash)
	CreatedAt      time.Time      `gorm:"autoCreateTime"`
}

// TableName specifies the table name for ForgeManifest
func (ForgeManifest) TableName() string {
	return "forge_manifests"
}

// JudgeVerdict is Heaven's voice—immutable once rendered
type JudgeVerdict struct {
	VerdictID      string         `gorm:"primaryKey;size:64"`
	ManifestID     string         `gorm:"not null;index:idx_verdicts_manifest;size:64"`
	TestSuite      string         `gorm:"not null;size:255"`
	Outcome        VerdictOutcome `gorm:"not null"`
	Evidence       JSON           `gorm:"type:text"`
	IdempotencyKey string         `gorm:"uniqueIndex;size:64"` // sha256(manifest_id + test_suite + ci_run_id)
	CreatedAt      time.Time      `gorm:"autoCreateTime"`
}

// TableName specifies the table name for JudgeVerdict
func (JudgeVerdict) TableName() string {
	return "judge_verdicts"
}

// CensorPrecedent logs ethics decisions—queryable and permanent
type CensorPrecedent struct {
	PrecedentID    string          `gorm:"primaryKey;size:64"`
	ManifestID     string          `gorm:"not null;index:idx_precedents_manifest;size:64"`
	Principle      string          `gorm:"not null;size:255"` // e.g., "golangci:govet"
	Ruling         PrecedentRuling `gorm:"not null"`
	Justification  string          `gorm:"type:text"`
	IdempotencyKey string          `gorm:"uniqueIndex;size:64"` // sha256(manifest_id + principle + commit_hash)
	CreatedAt      time.Time       `gorm:"autoCreateTime"`
}

// TableName specifies the table name for CensorPrecedent
func (CensorPrecedent) TableName() string {
	return "censor_precedents"
}

// MarshalIncident tracks production crashes
type MarshalIncident struct {
	IncidentID         string    `gorm:"primaryKey;size:255"` // Sentry crash ID
	EdictID            string    `gorm:"index;size:255"`
	CommitHash         string    `gorm:"not null;size:64"`
	RCASummary         string    `gorm:"not null;type:text"`
	ZhengmingRequestID string    `gorm:"index;size:64"`
	HotfixApproved     bool      `gorm:"default:false"`
	CreatedAt          time.Time `gorm:"autoCreateTime"`
}

// TableName specifies the table name for MarshalIncident
func (MarshalIncident) TableName() string {
	return "marshal_incidents"
}

// RulerCouncil members can answer Zhengming and override rejections
type RulerCouncil struct {
	Username        string `gorm:"primaryKey;size:255"` // GitHub login
	IsActive        bool   `gorm:"default:true"`
	CanOverride     bool   `gorm:"default:false"`
	EscalationOrder int    `gorm:"not null"`
}

// TableName specifies the table name for RulerCouncil
func (RulerCouncil) TableName() string {
	return "ruler_council"
}

// RitualGuardCheckpoint tracks the last processed event for crash recovery
type RitualGuardCheckpoint struct {
	ID        int       `gorm:"primaryKey"`
	EventID   int64     `gorm:"not null"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

// TableName specifies the table name for RitualGuardCheckpoint
func (RitualGuardCheckpoint) TableName() string {
	return "ritual_guard_checkpoint"
}
