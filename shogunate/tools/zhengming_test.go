package tools

import (
	"context"
	"testing"

	"github.com/afittestide/asimi/storage"
)

// mockRequester captures the EdictKey passed to RequestZhengming
type mockRequester struct {
	capturedKey              storage.EdictKey
	capturedCallerMinisterID string
}

func (m *mockRequester) RequestZhengming(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority, callerMinisterID string) (string, error) {
	m.capturedKey = key
	m.capturedCallerMinisterID = callerMinisterID
	return "req-123", nil
}

func TestRequestZhengmingTool_KeyIncludesUsernameAndProject(t *testing.T) {
	mock := &mockRequester{}
	tool := RequestZhengmingTool{
		MinisterID: "chancellor",
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
		MinisterID: "sage",
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

	if mock.capturedCallerMinisterID != "sage" {
		t.Errorf("expected callerMinisterID 'sage', got %q", mock.capturedCallerMinisterID)
	}
}

func TestRequestZhengmingTool_CallPassesMinisterIDAsCallerMinisterID_ForDifferentMinisters(t *testing.T) {
	tests := []struct {
		ministerID string
	}{
		{"chancellor"},
		{"sage"},
		{"strategist"},
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
