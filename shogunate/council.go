package shogunate

import (
	"fmt"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// CouncilMember represents a member to seed into the RulerCouncil
type CouncilMember struct {
	Username        string
	CanOverride     bool
	EscalationOrder int
}

// SeedRulerCouncil populates the ruler council with initial members
func SeedRulerCouncil(db *gorm.DB, members []CouncilMember) error {
	for _, m := range members {
		council := storage.RulerCouncil{
			Username:        m.Username,
			IsActive:        true,
			CanOverride:     m.CanOverride,
			EscalationOrder: m.EscalationOrder,
		}

		// Upsert: create or update
		result := db.Where("username = ?", m.Username).
			Assign(map[string]interface{}{
				"is_active":        true,
				"can_override":     m.CanOverride,
				"escalation_order": m.EscalationOrder,
			}).
			FirstOrCreate(&council)

		if result.Error != nil {
			return fmt.Errorf("failed to seed council member %s: %w", m.Username, result.Error)
		}
	}
	return nil
}

// GetActiveCouncilMembers retrieves all active council members ordered by escalation
func GetActiveCouncilMembers(db *gorm.DB) ([]storage.RulerCouncil, error) {
	var members []storage.RulerCouncil
	err := db.Where("is_active = ?", true).
		Order("escalation_order ASC").
		Find(&members).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get council members: %w", err)
	}
	return members, nil
}

// DeactivateCouncilMember marks a council member as inactive
func DeactivateCouncilMember(db *gorm.DB, username string) error {
	result := db.Model(&storage.RulerCouncil{}).
		Where("username = ?", username).
		Update("is_active", false)
	if result.Error != nil {
		return fmt.Errorf("failed to deactivate council member: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("council member not found: %s", username)
	}
	return nil
}

// GetOverrideMembers retrieves council members who can override rejections
func GetOverrideMembers(db *gorm.DB) ([]storage.RulerCouncil, error) {
	var members []storage.RulerCouncil
	err := db.Where("is_active = ? AND can_override = ?", true, true).
		Order("escalation_order ASC").
		Find(&members).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get override members: %w", err)
	}
	return members, nil
}
