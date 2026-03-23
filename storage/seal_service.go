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
func (s *SealService) GrantSeal(edictID uint, ministerID string, metadata JSON) error {
	if edictID == 0 {
		return fmt.Errorf("edict_id is required")
	}
	if ministerID == "" {
		return fmt.Errorf("minister_id is required")
	}

	sealID := uuid.New().String()
	seal := Seal{
		SealID:     sealID,
		EdictID:    edictID,
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
func (s *SealService) GetSeals(edictID uint) ([]Seal, error) {
	var seals []Seal
	err := s.db.Where("edict_id = ?", edictID).
		Order("sealed_at ASC").
		Find(&seals).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get seals: %w", err)
	}
	return seals, nil
}

// HasSeal checks if a specific minister has sealed an edict
func (s *SealService) HasSeal(edictID uint, ministerID string) (bool, error) {
	var count int64
	err := s.db.Model(&Seal{}).
		Where("edict_id = ? AND minister_id = ?", edictID, ministerID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check seal: %w", err)
	}
	return count > 0, nil
}

// GetMissingSeals returns the list of required seals that are missing
func (s *SealService) GetMissingSeals(edictID uint) ([]string, error) {
	requiredMinisters := []string{"judge", "sage", "ruler"}
	var missing []string

	for _, ministerID := range requiredMinisters {
		hasSeal, err := s.HasSeal(edictID, ministerID)
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
func (s *SealService) IsPendingAscension(edictID uint) (bool, error) {
	hasJudge, err := s.HasSeal(edictID, "judge")
	if err != nil {
		return false, err
	}

	hasSage, err := s.HasSeal(edictID, "sage")
	if err != nil {
		return false, err
	}

	hasRuler, err := s.HasSeal(edictID, "ruler")
	if err != nil {
		return false, err
	}

	return hasJudge && hasSage && !hasRuler, nil
}

// GetSealStatus returns a map of minister IDs to their seal status
func (s *SealService) GetSealStatus(edictID uint) (map[string]bool, error) {
	status := make(map[string]bool)
	requiredMinisters := []string{"judge", "sage", "ruler"}

	for _, ministerID := range requiredMinisters {
		hasSeal, err := s.HasSeal(edictID, ministerID)
		if err != nil {
			return nil, err
		}
		status[ministerID] = hasSeal
	}

	return status, nil
}

// GetEdictStatus derives the status of an edict from seals and zhengming tables
// Status derivation rules:
// - cancelled: edict.CancelledAt is not null
// - sealed: has ruler seal (implies judge and sage seals too)
// - blocked: has pending zhengming request
// - active: default (no ruler seal, not blocked, not cancelled)
func (s *SealService) GetEdictStatus(edictID uint) (EdictStatus, error) {
	// First check if edict is cancelled
	var edict Edict
	if err := s.db.First(&edict, "edict_id = ?", edictID).Error; err != nil {
		return "", fmt.Errorf("get edict: %w", err)
	}
	if edict.CancelledAt != nil {
		return EdictCancelled, nil
	}

	// Check if edict is sealed (has ruler seal)
	hasRuler, err := s.HasSeal(edictID, "ruler")
	if err != nil {
		return "", err
	}
	if hasRuler {
		return EdictSealed, nil
	}

	// Check if edict is blocked (has pending zhengming)
	var zhengmingCount int64
	err = s.db.Model(&Zhengming{}).
		Where("edict_id = ? AND status = ?", edictID, ZhengmingPending).
		Count(&zhengmingCount).Error
	if err != nil {
		return "", fmt.Errorf("check zhengming: %w", err)
	}
	if zhengmingCount > 0 {
		return EdictBlocked, nil
	}

	// Default: active
	return EdictActive, nil
}

// IsEdictSealed checks if an edict has all three seals (judge, sage, ruler)
func (s *SealService) IsEdictSealed(edictID uint) (bool, error) {
	status, err := s.GetEdictStatus(edictID)
	if err != nil {
		return false, err
	}
	return status == EdictSealed, nil
}

// IsEdictBlocked checks if an edict has pending zhengming requests
func (s *SealService) IsEdictBlocked(edictID uint) (bool, error) {
	status, err := s.GetEdictStatus(edictID)
	if err != nil {
		return false, err
	}
	return status == EdictBlocked, nil
}

// IsEdictCancelled checks if an edict is cancelled
func (s *SealService) IsEdictCancelled(edictID uint) (bool, error) {
	status, err := s.GetEdictStatus(edictID)
	if err != nil {
		return false, err
	}
	return status == EdictCancelled, nil
}
