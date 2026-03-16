package shogunate

import (
	"fmt"
	"time"

	"github.com/afittestide/asimi/storage"
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
func (s *SealService) GrantSeal(edictID, ministerID string, metadata storage.JSON) error {
	if edictID == "" {
		return fmt.Errorf("edict_id is required")
	}
	if ministerID == "" {
		return fmt.Errorf("minister_id is required")
	}

	sealID := uuid.New().String()
	seal := storage.Seal{
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
func (s *SealService) GetSeals(edictID string) ([]storage.Seal, error) {
	var seals []storage.Seal
	err := s.db.Where("edict_id = ?", edictID).
		Order("sealed_at ASC").
		Find(&seals).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get seals: %w", err)
	}
	return seals, nil
}

// HasSeal checks if a specific minister has sealed an edict
func (s *SealService) HasSeal(edictID, ministerID string) (bool, error) {
	var count int64
	err := s.db.Model(&storage.Seal{}).
		Where("edict_id = ? AND minister_id = ?", edictID, ministerID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check seal: %w", err)
	}
	return count > 0, nil
}

// GetMissingSeals returns the list of required seals that are missing
func (s *SealService) GetMissingSeals(edictID string) ([]string, error) {
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
func (s *SealService) IsPendingAscension(edictID string) (bool, error) {
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
func (s *SealService) GetSealStatus(edictID string) (map[string]bool, error) {
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
