package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/afittestide/asimi/storage"
)

// fakePrecedentStore satisfies PrecedentStore for testing RecordPrecedentTool.
type fakePrecedentStore struct {
	manifests []storage.ForgeManifest
	sealed    bool
	rejected  []string
	logged    []string
}

func (f *fakePrecedentStore) GetQuenchedManifests(key storage.EdictKey) ([]storage.ForgeManifest, error) {
	return f.manifests, nil
}

func (f *fakePrecedentStore) LogPrecedent(manifestID, principle string, ruling storage.PrecedentRuling, justification string) (string, error) {
	f.logged = append(f.logged, manifestID)
	return "ok", nil
}

func (f *fakePrecedentStore) RejectManifest(key storage.EdictKey, manifestID string) error {
	f.rejected = append(f.rejected, manifestID)
	return nil
}

func (f *fakePrecedentStore) GetPrecedentsForManifest(username, project, manifestID string) ([]storage.CensorPrecedent, error) {
	return nil, nil
}

func (f *fakePrecedentStore) QueryPrecedentsByPrinciple(username, project, principle string, limit int) ([]storage.CensorPrecedent, error) {
	return nil, nil
}

func (f *fakePrecedentStore) GrantSeal(key storage.EdictKey, metadata storage.JSON) error {
	f.sealed = true
	return nil
}

func TestRecordPrecedentTool_NoReasoningEcho(t *testing.T) {
	store := &fakePrecedentStore{
		manifests: []storage.ForgeManifest{
			{ManifestID: "abc123"},
		},
	}
	tool := RecordPrecedentTool{
		Store:    store,
		Username: "testuser",
		Project:  "testproject",
	}

	longReasoning := "This is a very long reasoning that should NOT appear in the tool output because the Sage already wrote it as conversational text."
	result, err := tool.Call(context.Background(),
		`{"edict_id": 5, "approved": true, "reasoning": "`+longReasoning+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(result, longReasoning) {
		t.Errorf("result should not echo reasoning, got: %s", result)
	}

	want := "Recorded precedent (approved) for edict 5"
	if result != want {
		t.Errorf("result = %q, want %q", result, want)
	}
}

func TestRecordPrecedentTool_RejectedNoReasoningEcho(t *testing.T) {
	store := &fakePrecedentStore{
		manifests: []storage.ForgeManifest{
			{ManifestID: "abc123"},
		},
	}
	tool := RecordPrecedentTool{
		Store:    store,
		Username: "testuser",
		Project:  "testproject",
	}

	longReasoning := "Code has issues that need addressing."
	result, err := tool.Call(context.Background(),
		`{"edict_id": 5, "approved": false, "reasoning": "`+longReasoning+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(result, longReasoning) {
		t.Errorf("result should not echo reasoning, got: %s", result)
	}

	want := "Recorded precedent (rejected) for edict 5"
	if result != want {
		t.Errorf("result = %q, want %q", result, want)
	}
}

func TestRecordPrecedentTool_Format(t *testing.T) {
	tool := RecordPrecedentTool{}
	formatted := tool.Format("", "Recorded precedent (approved) for edict 5", nil)
	want := "Record Precedent: Recorded precedent (approved) for edict 5\n"
	if formatted != want {
		t.Errorf("Format() = %q, want %q", formatted, want)
	}
}
