package shogunate

import (
	"fmt"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// CreateCouncilDecision creates a new council decision record for an edict
func CreateCouncilDecision(db *gorm.DB, councilID string, edictID uint, decision string) error {
	council := storage.RulerCouncil{
		CouncilID: councilID,
		EdictID:   edictID,
		Decision:  decision,
		Approved:  false,
	}

	if err := db.Create(&council).Error; err != nil {
		return fmt.Errorf("failed to create council decision: %w", err)
	}
	return nil
}

// ApproveCouncilDecision marks a council decision as approved
func ApproveCouncilDecision(db *gorm.DB, councilID, approvedBy string) error {
	result := db.Model(&storage.RulerCouncil{}).
		Where("council_id = ?", councilID).
		Updates(map[string]interface{}{
			"approved":    true,
			"approved_by": approvedBy,
		})
	if result.Error != nil {
		return fmt.Errorf("failed to approve council decision: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("council decision not found: %s", councilID)
	}
	return nil
}

// GetPendingCouncilDecisions retrieves all unapproved council decisions for an edict
func GetPendingCouncilDecisions(db *gorm.DB, edictID uint) ([]storage.RulerCouncil, error) {
	var decisions []storage.RulerCouncil
	err := db.Where("edict_id = ? AND approved = ?", edictID, false).
		Order("created_at ASC").
		Find(&decisions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get pending council decisions: %w", err)
	}
	return decisions, nil
}

// GetCouncilDecision retrieves a specific council decision by ID
func GetCouncilDecision(db *gorm.DB, councilID string) (*storage.RulerCouncil, error) {
	var council storage.RulerCouncil
	err := db.Where("council_id = ?", councilID).First(&council).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get council decision: %w", err)
	}
	return &council, nil
}

// GetCouncilDecisionsForEdict retrieves all council decisions for an edict
func GetCouncilDecisionsForEdict(db *gorm.DB, edictID uint) ([]storage.RulerCouncil, error) {
	var decisions []storage.RulerCouncil
	err := db.Where("edict_id = ?", edictID).
		Order("created_at ASC").
		Find(&decisions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get council decisions: %w", err)
	}
	return decisions, nil
}
