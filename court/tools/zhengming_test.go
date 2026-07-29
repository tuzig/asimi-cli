package tools

import (
	"context"
	"fmt"
	"testing"

	"github.com/afittestide/asimi/storage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// mockRequester captures the EdictKey passed to RequestZhengming
type mockRequester struct {
	capturedKey              storage.EdictKey
	capturedCallerMinisterID string
}

func (m *mockRequester) RequestZhengming(ctx context.Context, key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority, callerMinisterID string) (string, error) {
	m.capturedKey = key
	m.capturedCallerMinisterID = callerMinisterID
	return "req-123", nil
}

func TestRequestZhengmingTool_KeyIncludesUsernameAndProject(t *testing.T) {
	mock := &mockRequester{}
	tool := RequestZhengmingTool{
		MinisterID: "secretary",
		Requester:  mock,
		Username:   "daonb",
		Project:    "afittestide-asimi-cli",
	}

	input := `{
		"edict_id": 42,
		"questions": [{"text": "Which approach?", "options": ["A", "B"]}]
	}`

	_, err := tool.Call(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.capturedKey.Username != "daonb" {
		t.Errorf("expected username 'daonb', got %q", mock.capturedKey.Username)
	}
	if mock.capturedKey.Project != "afittestide-asimi-cli" {
		t.Errorf("expected project 'afittestide-asimi-cli', got %q", mock.capturedKey.Project)
	}
	if mock.capturedKey.ID != 42 {
		t.Errorf("expected edict_id 42, got %d", mock.capturedKey.ID)
	}
}

func TestRequestZhengmingTool_CallPassesMinisterIDAsCallerMinisterID(t *testing.T) {
	mock := &mockRequester{}
	tool := RequestZhengmingTool{
		MinisterID: "chancellor",
		Requester:  mock,
		Username:   "daonb",
		Project:    "afittestide-asimi-cli",
	}

	input := `{
		"edict_id": 99,
		"questions": [{"text": "Approve?", "options": ["Yes", "No"]}]
	}`

	_, err := tool.Call(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.capturedCallerMinisterID != "chancellor" {
		t.Errorf("expected callerMinisterID 'chancellor', got %q", mock.capturedCallerMinisterID)
	}
}

func TestRequestZhengmingTool_CallPassesMinisterIDAsCallerMinisterID_ForDifferentMinisters(t *testing.T) {
	tests := []struct {
		ministerID string
	}{
		{"secretary"},
		{"chancellor"},
		{"war"},
		{"judge"},
		{"forge"},
	}

	for _, tt := range tests {
		t.Run(tt.ministerID, func(t *testing.T) {
			mock := &mockRequester{}
			tool := RequestZhengmingTool{
				MinisterID: tt.ministerID,
				Requester:  mock,
				Username:   "daonb",
				Project:    "afittestide-asimi-cli",
			}

			input := `{
				"edict_id": 1,
				"questions": [{"text": "OK?", "options": ["Yes", "No"]}]
			}`

			_, err := tool.Call(context.Background(), input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if mock.capturedCallerMinisterID != tt.ministerID {
				t.Errorf("expected callerMinisterID %q, got %q", tt.ministerID, mock.capturedCallerMinisterID)
			}
		})
	}
}

func setupSuggestEdictTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := db.AutoMigrate(&storage.Edict{}, &storage.Seal{}, &storage.Zhengming{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

func TestSuggestEdictTool_PassesEdictIDInKey(t *testing.T) {
	db := setupSuggestEdictTestDB(t)

	// Create an existing edict
	edict := storage.Edict{
		ID:       10,
		Username: "sageuser",
		Project:  "myproject",
		Intent:   "Original intent",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	mock := &mockRequester{}
	tool := SuggestEdictTool{
		Ctx: ToolContext{
			DB:        db,
			Username:  "sageuser",
			Project:   "myproject",
			MinisterID: "chancellor",
		},
		Requester: mock,
	}

	input := `{
		"suggestion": "Add more tests",
		"summary": "improve test coverage",
		"edict_id": 10
	}`

	_, err := tool.Call(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.capturedKey.ID != 10 {
		t.Errorf("expected edict_id 10 in key, got %d", mock.capturedKey.ID)
	}
	if mock.capturedKey.Username != "sageuser" {
		t.Errorf("expected username 'sageuser', got %q", mock.capturedKey.Username)
	}
	if mock.capturedKey.Project != "myproject" {
		t.Errorf("expected project 'myproject', got %q", mock.capturedKey.Project)
	}
	if mock.capturedCallerMinisterID != "chancellor" {
		t.Errorf("expected callerMinisterID 'chancellor', got %q", mock.capturedCallerMinisterID)
	}
}

func TestSuggestEdictTool_EdictIDZeroDefaultsToNewEdict(t *testing.T) {
	db := setupSuggestEdictTestDB(t)
	mock := &mockRequester{}
	tool := SuggestEdictTool{
		Ctx: ToolContext{
			DB:         db,
			Username:   "sageuser",
			Project:    "myproject",
			MinisterID: "secretary",
		},
		Requester: mock,
	}

	input := `{
		"suggestion": "Create a new edict for something",
		"summary": "new edict"
	}`

	_, err := tool.Call(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.capturedKey.ID != 0 {
		t.Errorf("expected edict_id 0 for new edict, got %d", mock.capturedKey.ID)
	}
	if mock.capturedCallerMinisterID != "secretary" {
		t.Errorf("expected callerMinisterID 'secretary', got %q", mock.capturedCallerMinisterID)
	}
}

func TestSuggestEdictTool_NonexistentEdictReturnsError(t *testing.T) {
	db := setupSuggestEdictTestDB(t)
	mock := &mockRequester{}
	tool := SuggestEdictTool{
		Ctx: ToolContext{
			DB:        db,
			Username:  "sageuser",
			Project:   "myproject",
			MinisterID: "chancellor",
		},
		Requester: mock,
	}

	input := `{
		"suggestion": "Refine something",
		"summary": "refinement",
		"edict_id": 999
	}`

	_, err := tool.Call(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for nonexistent edict, got nil")
	}
}

func TestSuggestEdictTool_WrongUserProjectReturnsError(t *testing.T) {
	db := setupSuggestEdictTestDB(t)

	// Create an edict belonging to a different user/project
	edict := storage.Edict{
		ID:       5,
		Username: "otheruser",
		Project:  "otherproject",
		Intent:   "Someone else's edict",
	}
	if err := db.Create(&edict).Error; err != nil {
		t.Fatalf("failed to create edict: %v", err)
	}

	mock := &mockRequester{}
	tool := SuggestEdictTool{
		Ctx: ToolContext{
			DB:        db,
			Username:  "sageuser",
			Project:   "myproject",
			MinisterID: "chancellor",
		},
		Requester: mock,
	}

	input := fmt.Sprintf(`{
		"suggestion": "Refine edict",
		"summary": "refinement",
		"edict_id": %d
	}`, edict.ID)

	_, err := tool.Call(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for edict belonging to different user/project, got nil")
	}
}
