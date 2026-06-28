package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/afittestide/asimi/storage"
)

// PrecedentStore is the surface the precedent/manifest tools need from the
// Sage. Kept narrow so tests can fake it without dragging in MinisterBase.
type PrecedentStore interface {
	GetQuenchedManifests(key storage.EdictKey) ([]storage.ForgeManifest, error)
	LogPrecedent(manifestID, principle string, ruling storage.PrecedentRuling, justification string) (string, error)
	RejectManifest(key storage.EdictKey, manifestID string) error
	GetPrecedentsForManifest(username, project, manifestID string) ([]storage.CensorPrecedent, error)
	QueryPrecedentsByPrinciple(username, project, principle string, limit int) ([]storage.CensorPrecedent, error)
	GrantSeal(key storage.EdictKey, metadata storage.JSON) error
}

// FailureRecorder lets tools flag soft failures into a Sage's failure
// accumulator without depending on the shogunate package's context key.
type FailureRecorder func(ctx context.Context, reason string)

// RecordPrecedentTool records the Sage's ethics review outcome for every
// quenched manifest on an edict, granting or withholding the Sage's seal.
type RecordPrecedentTool struct {
	Store        PrecedentStore
	Username     string
	Project      string
	AddFailure   FailureRecorder
}

func (t RecordPrecedentTool) Name() string { return "record_precedent" }

func (t RecordPrecedentTool) Description() string {
	return "Records an ethics review outcome with reasoning. Input: JSON with 'edict_id', 'approved' (boolean), and 'reasoning'."
}

func (t RecordPrecedentTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID   uint   `json:"edict_id"`
		Approved  bool   `json:"approved"`
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.EdictID == 0 || params.Reasoning == "" {
		return "", fmt.Errorf("edict_id and reasoning are required")
	}

	key := storage.EdictKey{ID: params.EdictID, Username: t.Username, Project: t.Project}

	manifests, err := t.Store.GetQuenchedManifests(key)
	if err != nil {
		return "", err
	}

	ruling := storage.PrecedentApproved
	if !params.Approved {
		ruling = storage.PrecedentRejected
	}

	for _, m := range manifests {
		if _, err := t.Store.LogPrecedent(m.ManifestID, "ethics_review", ruling, params.Reasoning); err != nil {
			return "", fmt.Errorf("failed to log precedent: %w", err)
		}
		if !params.Approved {
			if err := t.Store.RejectManifest(key, m.ManifestID); err != nil {
				return "", fmt.Errorf("failed to reject manifest: %w", err)
			}
		}
	}

	status := "approved"
	if !params.Approved {
		status = "rejected"
		if t.AddFailure != nil {
			t.AddFailure(ctx, fmt.Sprintf("rejected edict %d: %s", key.ID, params.Reasoning))
		}
	} else {
		if err := t.Store.GrantSeal(key, storage.JSON{"reason": params.Reasoning}); err != nil {
			return "", fmt.Errorf("failed to grant seal: %w", err)
		}
	}
	return fmt.Sprintf("Recorded precedent (%s) for edict %d", status, key.ID), nil
}

func (t RecordPrecedentTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id":  map[string]any{"type": "integer", "description": "The edict ID"},
			"approved":  map[string]any{"type": "boolean", "description": "Whether the code is approved"},
			"reasoning": map[string]any{"type": "string", "description": "The reasoning for the decision"},
		},
		"required": []string{"edict_id", "approved", "reasoning"},
	}
}

func (t RecordPrecedentTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("Record Precedent: Error: %v\n", err)
	}
	return fmt.Sprintf("Record Precedent: %s\n", result)
}

// ListQuenchedManifestsTool lists manifests ready for ethics review.
type ListQuenchedManifestsTool struct {
	Store    PrecedentStore
	Username string
	Project  string
}

func (t ListQuenchedManifestsTool) Name() string { return "list_quenched_manifests" }

func (t ListQuenchedManifestsTool) Description() string {
	return "Lists manifests that passed testing and are ready for ethics review. Input: JSON with 'edict_id'."
}

func (t ListQuenchedManifestsTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID uint `json:"edict_id"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.EdictID == 0 {
		return "", fmt.Errorf("edict_id is required")
	}

	key := storage.EdictKey{ID: params.EdictID, Username: t.Username, Project: t.Project}
	manifests, err := t.Store.GetQuenchedManifests(key)
	if err != nil {
		return "", err
	}

	if len(manifests) == 0 {
		return "No quenched manifests found", nil
	}

	result, err := json.MarshalIndent(manifests, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to format manifests: %w", err)
	}
	return string(result), nil
}

func (t ListQuenchedManifestsTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{"type": "integer", "description": "The edict ID to list manifests for"},
		},
		"required": []string{"edict_id"},
	}
}

func (t ListQuenchedManifestsTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("List Quenched Manifests: Error: %v\n", err)
	}
	return fmt.Sprintf("List Quenched Manifests: %s\n", result)
}

// QueryPrecedentsTool searches precedents by principle.
type QueryPrecedentsTool struct {
	Store    PrecedentStore
	Username string
	Project  string
}

func (t QueryPrecedentsTool) Name() string { return "query_precedents" }

func (t QueryPrecedentsTool) Description() string {
	return "Searches precedents by principle for case law lookup. Input: JSON with 'principle' and optional 'limit'."
}

func (t QueryPrecedentsTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		Principle string `json:"principle"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.Principle == "" {
		return "", fmt.Errorf("principle is required")
	}
	if params.Limit == 0 {
		params.Limit = 10
	}

	precedents, err := t.Store.QueryPrecedentsByPrinciple(t.Username, t.Project, params.Principle, params.Limit)
	if err != nil {
		return "", err
	}

	if len(precedents) == 0 {
		return "No precedents found for this principle", nil
	}

	result, err := json.MarshalIndent(precedents, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to format precedents: %w", err)
	}
	return string(result), nil
}

func (t QueryPrecedentsTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"principle": map[string]any{"type": "string", "description": "The principle to search for"},
			"limit":     map[string]any{"type": "integer", "description": "Maximum number of results (default 10)"},
		},
		"required": []string{"principle"},
	}
}

func (t QueryPrecedentsTool) Format(input, result string, err error) string {
	if err != nil {
		return fmt.Sprintf("Query Precedents: Error: %v\n", err)
	}
	return fmt.Sprintf("Query Precedents: %s\n", result)
}
