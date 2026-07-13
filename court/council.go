package court

import (
	"fmt"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// CreateCouncilDecision creates a new council decision record for an edict
func CreateCouncilDecision(db *gorm.DB, councilID string, key storage.EdictKey, decision string) error {
	council := storage.RulerCouncil{
		CouncilID: councilID,
		EdictID:   key.ID,
		Username:  key.Username,
		Project:   key.Project,
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
func GetPendingCouncilDecisions(db *gorm.DB, key storage.EdictKey) ([]storage.RulerCouncil, error) {
	var decisions []storage.RulerCouncil
	err := db.Where("edict_id = ? AND username = ? AND project = ? AND approved = ?",
		key.ID, key.Username, key.Project, false).
		Order("created_at ASC").
		Find(&decisions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get pending council decisions: %w", err)
	}
	return decisions, nil
}

// GetCouncilDecision retrieves a specific council decision by ID
func GetCouncilDecision(db *gorm.DB, councilID string, key storage.EdictKey) (*storage.RulerCouncil, error) {
	var council storage.RulerCouncil
	err := db.Where("council_id = ? AND username = ? AND project = ?",
		councilID, key.Username, key.Project).First(&council).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get council decision: %w", err)
	}
	return &council, nil
}

// GetCouncilDecisionsForEdict retrieves all council decisions for an edict
func GetCouncilDecisionsForEdict(db *gorm.DB, key storage.EdictKey) ([]storage.RulerCouncil, error) {
	var decisions []storage.RulerCouncil
	err := db.Where("edict_id = ? AND username = ? AND project = ?",
		key.ID, key.Username, key.Project).
		Order("created_at ASC").
		Find(&decisions).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get council decisions: %w", err)
	}
	return decisions, nil
}
