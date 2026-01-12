package shogunate

import (
	"fmt"

	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// marshalConn implements MarshalConn - monitors production
type marshalConn struct {
	baseConn
}

// NewMarshalConn creates a new Marshal connection
func NewMarshalConn(db *gorm.DB) MarshalConn {
	return &marshalConn{
		baseConn: baseConn{db: db, ministerID: "marshal"},
	}
}

// GetEdict retrieves an edict by ID
func (c *marshalConn) GetEdict(edictID string) (*storage.Edict, error) {
	return c.getEdict(edictID)
}

// GetManifestByCommit finds a manifest by its git commit hash
func (c *marshalConn) GetManifestByCommit(commitHash string) (*storage.ForgeManifest, error) {
	var manifest storage.ForgeManifest
	if err := c.db.First(&manifest, "commit_hash = ?", commitHash).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("manifest not found for commit: %s", commitHash)
		}
		return nil, fmt.Errorf("failed to get manifest: %w", err)
	}
	return &manifest, nil
}

// LogIncident records a production crash incident
func (c *marshalConn) LogIncident(incidentID, edictID, commitHash, rcaSummary string) error {
	incident := storage.MarshalIncident{
		IncidentID: incidentID,
		EdictID:    edictID,
		CommitHash: commitHash,
		RCASummary: rcaSummary,
	}

	if err := c.db.Create(&incident).Error; err != nil {
		return fmt.Errorf("failed to log incident: %w", err)
	}
	return nil
}

// GetIncident retrieves an incident by ID
func (c *marshalConn) GetIncident(incidentID string) (*storage.MarshalIncident, error) {
	var incident storage.MarshalIncident
	if err := c.db.First(&incident, "incident_id = ?", incidentID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("incident not found: %s", incidentID)
		}
		return nil, fmt.Errorf("failed to get incident: %w", err)
	}
	return &incident, nil
}

// MarkHotfixApproved approves a hotfix for an incident
func (c *marshalConn) MarkHotfixApproved(incidentID string) error {
	result := c.db.Model(&storage.MarshalIncident{}).
		Where("incident_id = ?", incidentID).
		Update("hotfix_approved", true)
	if result.Error != nil {
		return fmt.Errorf("failed to approve hotfix: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("incident not found: %s", incidentID)
	}
	return nil
}
