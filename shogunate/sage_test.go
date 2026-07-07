package shogunate

import (
	"fmt"
	"testing"
	"time"

	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
)

// TestNoRejections_SupersededRejected verifies that old rejected manifests
// superseded by newer quenched ones for the same file_path do not cause
// noRejections to return false.
func TestNoRejections_SupersededRejected(t *testing.T) {
	db := setupMinisterTestDB(t)
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject")
	sage := NewSage(base, nil)

	edict, err := CreateEdictForTest(db, "Feature edict")
	assert.NoError(t, err)

	key := storage.EdictKey{ID: edict.ID, Username: base.username, Project: base.project}

	// Old rejected manifest for foo.go
	oldRejected := storage.ForgeManifest{
		ManifestID: GenerateID("manifest", fmt.Sprintf("%d", edict.ID), "test", "foo.go", "old"),
		EdictID:    edict.ID,
		Username:   base.username,
		Project:    base.project,
		FilePath:   "foo.go",
		Status:     storage.ManifestRejected,
	}
	assert.NoError(t, db.Create(&oldRejected).Error)

	time.Sleep(10 * time.Millisecond)

	// Newer quenched manifest for foo.go (supersedes the rejected one)
	newQuenched := storage.ForgeManifest{
		ManifestID: GenerateID("manifest", fmt.Sprintf("%d", edict.ID), "test", "foo.go", "new"),
		EdictID:    edict.ID,
		Username:   base.username,
		Project:    base.project,
		FilePath:   "foo.go",
		Status:     storage.ManifestQuenched,
	}
	assert.NoError(t, db.Create(&newQuenched).Error)

	noRej, err := sage.noRejections(key)
	assert.NoError(t, err)
	assert.True(t, noRej, "Should report no rejections when old rejected is superseded by quenched")
}

// TestNoRejections_LatestRejected verifies that if the latest manifest for a
// file is rejected, noRejections returns false even if an older one was quenched.
func TestNoRejections_LatestRejected(t *testing.T) {
	db := setupMinisterTestDB(t)
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject")
	sage := NewSage(base, nil)

	edict, err := CreateEdictForTest(db, "Feature edict")
	assert.NoError(t, err)

	key := storage.EdictKey{ID: edict.ID, Username: base.username, Project: base.project}

	// Old quenched manifest for foo.go
	oldQuenched := storage.ForgeManifest{
		ManifestID: GenerateID("manifest", fmt.Sprintf("%d", edict.ID), "test", "foo.go", "old"),
		EdictID:    edict.ID,
		Username:   base.username,
		Project:    base.project,
		FilePath:   "foo.go",
		Status:     storage.ManifestQuenched,
	}
	assert.NoError(t, db.Create(&oldQuenched).Error)

	time.Sleep(10 * time.Millisecond)

	// Newer rejected manifest for foo.go
	newRejected := storage.ForgeManifest{
		ManifestID: GenerateID("manifest", fmt.Sprintf("%d", edict.ID), "test", "foo.go", "new"),
		EdictID:    edict.ID,
		Username:   base.username,
		Project:    base.project,
		FilePath:   "foo.go",
		Status:     storage.ManifestRejected,
	}
	assert.NoError(t, db.Create(&newRejected).Error)

	noRej, err := sage.noRejections(key)
	assert.NoError(t, err)
	assert.False(t, noRej, "Should report rejections when latest manifest is rejected")
}
