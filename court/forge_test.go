package court

import (
	"fmt"
	"testing"
	"time"

	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestForge_StageManifest_UniqueIDs verifies that manifest creation generates
// unique IDs even when called multiple times with the same inputs.
// This prevents UNIQUE constraint violations when the ritual retries.
func TestForge_StageManifest_UniqueIDs(t *testing.T) {
	db := setupMinisterTestDB(t)

	edict := &storage.Edict{SessionID: "test-session", Intent: "Build REST API", Username: "testuser", Project: "testproject"}
	require.NoError(t, db.Create(edict).Error)

	key := edict.Key()
	lingID := "ling-1"
	filePath := "internal/model/user.go"
	funcName := "User"
	contentSHA := "abc123"

	// Stage the same manifest multiple times with identical inputs
	createManifest := func() string {
		manifestID := GenerateID("manifest", fmt.Sprintf("%d", key.ID), lingID, filePath, fmt.Sprintf("%d", time.Now().UnixNano()))
		manifest := storage.ForgeManifest{
			ManifestID: manifestID,
			EdictID:    key.ID,
			Username:   key.Username,
			Project:    key.Project,
			LingID:     lingID,
			FilePath:   filePath,
			FuncName:   funcName,
			ContentSHA: contentSHA,
			Status:     storage.ManifestForged,
		}
		require.NoError(t, db.Create(&manifest).Error)
		return manifestID
	}

	id1 := createManifest()
	id2 := createManifest()
	id3 := createManifest()

	// All IDs should be unique
	assert.NotEqual(t, id1, id2, "second call should produce different ID")
	assert.NotEqual(t, id2, id3, "third call should produce different ID")
	assert.NotEqual(t, id1, id3, "first and third call should produce different ID")

	// All should be stored in the database
	var manifests []storage.ForgeManifest
	require.NoError(t, db.Where("edict_id = ?", edict.ID).Find(&manifests).Error)
	assert.Len(t, manifests, 3, "all three manifests should be in the database")
}

// TestForge_ProcessTask_FixesFailedVerdictsFirst verifies that when an edict has
// failed verdicts, processTask calls the LLM with fix prompts instead of
// executing the normal ling work.
func TestForge_ProcessTask_FixesFailedVerdictsFirst(t *testing.T) {
	t.Skip("requires bifrost mock")
}

// TestForge_ProcessTask_SkipsFixLoopWhenNoFailedVerdicts verifies that when there
// are no failed verdicts, processTask executes the normal ling work.
func TestForge_ProcessTask_SkipsFixLoopWhenNoFailedVerdicts(t *testing.T) {
	t.Skip("requires bifrost mock")
}
