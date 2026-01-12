package shogunate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// ZhengmingConn provides clarification request capabilities
type ZhengmingConn interface {
	RequestZhengming(edictID, question string, priority storage.ZhengmingPriority) (requestID string, err error)
	IsZhengmingPending(edictID string) (bool, error)
}

// EventEmitter emits events to the Ritual Guard's ledger
type EventEmitter interface {
	EmitEvent(edictID, eventType string, payload storage.JSON) error
}

// ChancellorConn has full access - the only cross-domain reader
type ChancellorConn interface {
	ZhengmingConn
	EventEmitter

	// Edict management
	GetEdict(edictID string) (*storage.Edict, error)
	CreateEdict(edictID, renIntent string) error
	UpdatePhase(edictID string, phase storage.EdictPhase) error
	SetChancellorSeal(edictID string, sealed bool) error
	SetCensorSeal(edictID string, sealed bool) error
	CancelEdict(edictID, cancelledBy, reason string) error

	// Zhengming management
	GetPendingZhengming(edictID string) ([]storage.ZhengmingRequest, error)
	AnswerZhengming(requestID, answer string) error
	AppendToRenIntent(edictID, clarification string) error

	// Cross-domain reads (Chancellor privilege)
	GetAllManifestsForEdict(edictID string) ([]storage.ForgeManifest, error)
	GetAllLingForEdict(edictID string) ([]storage.Ling, error)

	// Regression handling
	ResetLingStatus(lingID string, status storage.LingStatus) error
}

// StrategistConn decomposes edicts into ling
type StrategistConn interface {
	ZhengmingConn
	GetEdict(edictID string) (*storage.Edict, error)
	InsertLing(ling *storage.Ling) error
	GetLingForEdict(edictID string) ([]storage.Ling, error)
	LingExistsForEdict(edictID string) (bool, error)
}

// ForgeConn creates code manifests
type ForgeConn interface {
	ZhengmingConn
	EventEmitter
	GetEdict(edictID string) (*storage.Edict, error)
	GetPendingLing(edictID string) ([]storage.Ling, error)
	MarkLingCompleted(lingID string) error
	StageManifest(edictID, lingID, filePath, qualifiedName, patchHash string) (manifestID string, err error)
	ActivateManifest(manifestID, commitHash string) error
	DeleteStagedManifest(manifestID string) error
	GetRejectedManifests(edictID string) ([]storage.ForgeManifest, error)
}

// JudgeConn judges code through CI
type JudgeConn interface {
	ZhengmingConn
	EventEmitter
	GetPendingManifests(edictID string) ([]storage.ForgeManifest, error)
	AllManifestsQuenched(edictID string) (bool, error)
	InsertVerdict(manifestID, testSuite string, outcome storage.VerdictOutcome, evidence storage.JSON) (verdictID string, err error)
	UpdateManifestStatus(manifestID string, status storage.ManifestStatus, verdictID string) error
}

// CensorConn enforces code ethics
type CensorConn interface {
	ZhengmingConn
	GetQuenchedManifests(edictID string) ([]storage.ForgeManifest, error)
	NoRejections(edictID string) (bool, error)
	LogPrecedent(manifestID, principle string, ruling storage.PrecedentRuling, justification string) (precedentID string, err error)
	RejectManifest(manifestID string) error
	GetPrecedentsForManifest(manifestID string) ([]storage.CensorPrecedent, error)
	QueryPrecedentsByPrinciple(principle string, limit int) ([]storage.CensorPrecedent, error)
}

// MarshalConn monitors production
type MarshalConn interface {
	ZhengmingConn
	GetEdict(edictID string) (*storage.Edict, error)
	GetManifestByCommit(commitHash string) (*storage.ForgeManifest, error)
	LogIncident(incidentID, edictID, commitHash, rcaSummary string) error
	GetIncident(incidentID string) (*storage.MarshalIncident, error)
	MarkHotfixApproved(incidentID string) error
}

// RitualGuardConn provides event stream access
type RitualGuardConn interface {
	GetEventsFrom(fromEventID int64, limit int) ([]storage.TianEvent, error)
	AcknowledgeEvent(eventID int64) error
	GetLastAcknowledgedEvent() (int64, error)
	SaveCheckpoint(eventID int64) error
	LoadCheckpoint() (int64, error)
	MoveToDLQ(event storage.TianEvent, errMsg string, retryCount int) error
}

// baseConn provides shared functionality for all minister connections
type baseConn struct {
	db         *gorm.DB
	ministerID string
}

// generateID creates a unique ID using SHA256
func generateID(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// generateIdempotencyKey creates a deterministic key for deduplication
func generateIdempotencyKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// RequestZhengming creates a clarification request
func (c *baseConn) RequestZhengming(edictID, question string, priority storage.ZhengmingPriority) (string, error) {
	requestID := generateID("zhengming", edictID, c.ministerID, question, time.Now().String())

	req := storage.ZhengmingRequest{
		RequestID:  requestID,
		EdictID:    edictID,
		MinisterID: c.ministerID,
		Question:   question,
		Priority:   priority,
		Status:     storage.ZhengmingPending,
		TimeoutAt:  time.Now().Add(24 * time.Hour), // Default 24h timeout
	}

	if priority == storage.PriorityUrgent {
		req.TimeoutAt = time.Now().Add(1 * time.Hour)
	}

	if err := c.db.Create(&req).Error; err != nil {
		return "", fmt.Errorf("failed to create zhengming request: %w", err)
	}
	return requestID, nil
}

// IsZhengmingPending checks if there are pending clarification requests for an edict
func (c *baseConn) IsZhengmingPending(edictID string) (bool, error) {
	var count int64
	err := c.db.Model(&storage.ZhengmingRequest{}).
		Where("edict_id = ? AND status = ?", edictID, storage.ZhengmingPending).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check pending zhengming: %w", err)
	}
	return count > 0, nil
}

// EmitEvent records an event in the Tian ledger
func (c *baseConn) EmitEvent(edictID, eventType string, payload storage.JSON) error {
	event := storage.TianEvent{
		EdictID:   edictID,
		EventType: eventType,
		Payload:   payload,
	}
	if err := c.db.Create(&event).Error; err != nil {
		return fmt.Errorf("failed to emit event: %w", err)
	}
	return nil
}

// getEdict retrieves an edict by ID (shared helper)
func (c *baseConn) getEdict(edictID string) (*storage.Edict, error) {
	var edict storage.Edict
	if err := c.db.First(&edict, "edict_id = ?", edictID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("edict not found: %s", edictID)
		}
		return nil, fmt.Errorf("failed to get edict: %w", err)
	}
	return &edict, nil
}
