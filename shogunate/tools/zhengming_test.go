package tools

import (
	"context"
	"testing"

	"github.com/afittestide/asimi/storage"
)

// mockRequester captures the EdictKey passed to RequestZhengming
type mockRequester struct {
	capturedKey storage.EdictKey
}

func (m *mockRequester) RequestZhengming(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (string, error) {
	m.capturedKey = key
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
	if mock.capturedKey.EdictID != 42 {
		t.Errorf("expected edict_id 42, got %d", mock.capturedKey.EdictID)
	}
}
