package storage

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SealService manages the seal chain for edicts
type SealService struct {
	db *gorm.DB
}

// NewSealService creates a new seal service
func NewSealService(db *gorm.DB) *SealService {
	return &SealService{db: db}
}

// GrantSeal records a minister's seal on an edict
func (s *SealService) GrantSeal(key EdictKey, ministerID string, metadata JSON) error {
	if key.ID == 0 {
		return fmt.Errorf("id is required")
	}
	if ministerID == "" {
		return fmt.Errorf("minister_id is required")
	}

	sealID := uuid.New().String()
	seal := Seal{
		SealID:     sealID,
		EdictID:    key.ID,
		Username:   key.Username,
		Project:    key.Project,
		MinisterID: ministerID,
		SealedAt:   time.Now(),
		Metadata:   metadata,
	}

	if err := s.db.Create(&seal).Error; err != nil {
		return fmt.Errorf("failed to grant seal: %w", err)
	}

	return nil
}

// GetSeals retrieves all seals for an edict
func (s *SealService) GetSeals(key EdictKey) ([]Seal, error) {
	var seals []Seal
	err := s.db.Where("edict_id = ? AND username = ? AND project = ?", key.ID, key.Username, key.Project).
		Order("sealed_at ASC").
		Find(&seals).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get seals: %w", err)
	}
	return seals, nil
}

// HasSeal checks if a specific minister has sealed an edict
func (s *SealService) HasSeal(key EdictKey, ministerID string) (bool, error) {
	var count int64
	err := s.db.Model(&Seal{}).
		Where("edict_id = ? AND username = ? AND project = ? AND minister_id = ?", key.ID, key.Username, key.Project, ministerID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check seal: %w", err)
	}
	return count > 0, nil
}

// GetMissingSeals returns the list of required seals that are missing
func (s *SealService) GetMissingSeals(key EdictKey) ([]string, error) {
	requiredMinisters := []string{"judge", "sage", "ruler"}
	var missing []string

	for _, ministerID := range requiredMinisters {
		hasSeal, err := s.HasSeal(key, ministerID)
		if err != nil {
			return nil, err
		}
		if !hasSeal {
			missing = append(missing, ministerID)
		}
	}

	return missing, nil
}

// IsPendingAscension checks if an edict has judge and sage seals but is awaiting ruler seal
func (s *SealService) IsPendingAscension(key EdictKey) (bool, error) {
	hasJudge, err := s.HasSeal(key, "judge")
	if err != nil {
		return false, err
	}

	hasSage, err := s.HasSeal(key, "sage")
	if err != nil {
		return false, err
	}

	hasRuler, err := s.HasSeal(key, "ruler")
	if err != nil {
		return false, err
	}

	return hasJudge && hasSage && !hasRuler, nil
}

// GetSealStatus returns a map of minister IDs to their seal status
func (s *SealService) GetSealStatus(key EdictKey) (map[string]bool, error) {
	status := make(map[string]bool)
	requiredMinisters := []string{"judge", "sage", "ruler"}

	for _, ministerID := range requiredMinisters {
		hasSeal, err := s.HasSeal(key, ministerID)
		if err != nil {
			return nil, err
		}
		status[ministerID] = hasSeal
	}

	return status, nil
}

// GetEdictStatus derives the status of an edict from seals and zhengming tables
func (s *SealService) GetEdictStatus(key EdictKey) (EdictStatus, error) {
	var edict Edict
	if err := s.db.First(&edict, "id = ? AND username = ? AND project = ?", key.ID, key.Username, key.Project).Error; err != nil {
		return "", fmt.Errorf("get edict: %w", err)
	}
	if edict.CancelledAt != nil {
		return EdictCancelled, nil
	}

	hasRuler, err := s.HasSeal(key, "ruler")
	if err != nil {
		return "", err
	}
	if hasRuler {
		return EdictSealed, nil
	}

	var zhengmingCount int64
	err = s.db.Model(&Zhengming{}).
		Where("edict_id = ? AND username = ? AND project = ? AND status = ?", key.ID, key.Username, key.Project, ZhengmingPending).
		Count(&zhengmingCount).Error
	if err != nil {
		return "", fmt.Errorf("check zhengming: %w", err)
	}
	if zhengmingCount > 0 {
		return EdictBlocked, nil
	}

	return EdictActive, nil
}

// IsEdictSealed checks if an edict has all three seals (judge, sage, ruler)
func (s *SealService) IsEdictSealed(key EdictKey) (bool, error) {
	status, err := s.GetEdictStatus(key)
	if err != nil {
		return false, err
	}
	return status == EdictSealed, nil
}

// IsEdictBlocked checks if an edict has pending zhengming requests
func (s *SealService) IsEdictBlocked(key EdictKey) (bool, error) {
	status, err := s.GetEdictStatus(key)
	if err != nil {
		return false, err
	}
	return status == EdictBlocked, nil
}

// IsEdictCancelled checks if an edict is cancelled
func (s *SealService) IsEdictCancelled(key EdictKey) (bool, error) {
	status, err := s.GetEdictStatus(key)
	if err != nil {
		return false, err
	}
	return status == EdictCancelled, nil
}

// UnsealedEdict is an edict with its minister seal status
type UnsealedEdict struct {
	Edict
	HasJudgeSeal bool
	HasSageSeal  bool
}

// ListUnsealedEdicts returns all edicts the ruler hasn't sealed yet, with judge/sage seal status
func (s *SealService) ListUnsealedEdicts(username, project string) ([]UnsealedEdict, error) {
	var edicts []Edict
	err := s.db.Raw(`
		SELECT e.* FROM edicts e
		WHERE e.username = ? AND e.project = ?
		AND e.cancelled_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM seals s WHERE s.edict_id = e.id AND s.username = e.username AND s.project = e.project AND s.minister_id = 'ruler')
		ORDER BY e.id DESC`, username, project).Scan(&edicts).Error
	if err != nil {
		return nil, fmt.Errorf("list pending seals: %w", err)
	}

	// Fetch all non-ruler seals for these edicts in one query
	var seals []Seal
	edictIDs := make([]uint, len(edicts))
	for i, e := range edicts {
		edictIDs[i] = e.ID
	}
	if len(edictIDs) > 0 {
		s.db.Where("edict_id IN ? AND username = ? AND project = ? AND minister_id IN ('judge','sage')",
			edictIDs, username, project).Find(&seals)
	}

	// Build seal lookup: edictID -> set of minister_ids
	sealMap := make(map[uint]map[string]bool)
	for _, seal := range seals {
		if sealMap[seal.EdictID] == nil {
			sealMap[seal.EdictID] = make(map[string]bool)
		}
		sealMap[seal.EdictID][seal.MinisterID] = true
	}

	result := make([]UnsealedEdict, len(edicts))
	for i, e := range edicts {
		result[i] = UnsealedEdict{
			Edict:        e,
			HasJudgeSeal: sealMap[e.ID]["judge"],
			HasSageSeal:  sealMap[e.ID]["sage"],
		}
	}
	return result, nil
}
