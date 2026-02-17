package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/afittestide/asimi/internal/utils"
	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// EdictManager provides edict management capabilities
type EdictManager interface {
	CreateEdict(edictID, intent string) error
	GetEdict(edictID string) (*storage.Edict, error)
	EmitEvent(edictID, eventType string, payload storage.JSON) error
}

// CreateEdictTool creates a new edict from the user's request.
type CreateEdictTool struct {
	Manager EdictManager
}

func (t CreateEdictTool) Name() string {
	return "create_edict"
}

func (t CreateEdictTool) Description() string {
	return "Creates a new edict (work order) from the user's request. Use this when the user asks you to implement a feature, fix a bug, or make changes to the codebase. The edict_id should be a unique identifier like 'issue-123' or 'feature-user-auth'."
}

func (t CreateEdictTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID string `json:"edict_id"`
		Intent  string `json:"intent"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.EdictID == "" {
		return "", fmt.Errorf("edict_id is required")
	}
	if params.Intent == "" {
		return "", fmt.Errorf("intent is required")
	}

	if err := t.Manager.CreateEdict(params.EdictID, params.Intent); err != nil {
		return "", fmt.Errorf("create edict: %w", err)
	}

	// Emit event for edict assignment
	t.Manager.EmitEvent(params.EdictID, "edict_assigned", storage.JSON{"source": "chancellor"})

	return fmt.Sprintf(`{"status":"created","edict_id":"%s"}`, params.EdictID), nil
}

func (t CreateEdictTool) Format(input, result string, err error) string {
	var params struct {
		EdictID string `json:"edict_id"`
	}
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("CreateEdict")
	msg.Writef(" %s", params.EdictID)
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		msg.WriteString("Created")
	}

	return msg.String() + "\n"
}

func (t CreateEdictTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{
				"type":        "string",
				"description": "Unique identifier for the edict (e.g., 'issue-123', 'feature-auth')",
			},
			"intent": map[string]any{
				"type":        "string",
				"description": "The user's intent - what they want to accomplish",
			},
		},
		"required": []string{"edict_id", "intent"},
	}
}

// GetEdictStatusTool retrieves the status of an edict.
type GetEdictStatusTool struct {
	Manager EdictManager
}

func (t GetEdictStatusTool) Name() string {
	return "get_edict_status"
}

func (t GetEdictStatusTool) Description() string {
	return "Gets the current status and phase of an edict. Use this to check progress on a work order."
}

func (t GetEdictStatusTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID string `json:"edict_id"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.EdictID == "" {
		return "", fmt.Errorf("edict_id is required")
	}

	edict, err := t.Manager.GetEdict(params.EdictID)
	if err != nil {
		return "", fmt.Errorf("get edict: %w", err)
	}

	result := map[string]any{
		"edict_id": edict.EdictID,
		"phase":    string(edict.CurrentPhase),
		"intent":   edict.Intent,
	}

	resultJSON, _ := json.Marshal(result)
	return string(resultJSON), nil
}

func (t GetEdictStatusTool) Format(input, result string, err error) string {
	var params struct {
		EdictID string `json:"edict_id"`
	}
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("EdictStatus")
	msg.Writef(" %s", params.EdictID)
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		var res struct {
			Phase string `json:"phase"`
		}
		json.Unmarshal([]byte(result), &res)
		msg.Writef("[%s]", res.Phase)
	}

	return msg.String() + "\n"
}

func (t GetEdictStatusTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{
				"type":        "string",
				"description": "The edict ID to check status for",
			},
		},
		"required": []string{"edict_id"},
	}
}

// ListEdictsTool lists all edicts with optional filtering.
type ListEdictsTool struct {
	DB *gorm.DB
}

func (t ListEdictsTool) Name() string {
	return "list_edicts"
}

func (t ListEdictsTool) Description() string {
	return "Lists all edicts, optionally filtered by phase. Use this to see what work orders exist."
}

func (t ListEdictsTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		Phase string `json:"phase"`
		Limit int    `json:"limit"`
	}
	json.Unmarshal([]byte(input), &params)

	if params.Limit <= 0 {
		params.Limit = 20
	}

	var edicts []storage.Edict
	query := t.DB.Order("created_at DESC").Limit(params.Limit)
	if params.Phase != "" {
		query = query.Where("current_phase = ?", params.Phase)
	}

	if err := query.Find(&edicts).Error; err != nil {
		return "", fmt.Errorf("list edicts: %w", err)
	}

	var results []map[string]any
	for _, e := range edicts {
		results = append(results, map[string]any{
			"edict_id": e.EdictID,
			"phase":    string(e.CurrentPhase),
			"intent":   truncateString(e.Intent, 100),
		})
	}

	resultJSON, _ := json.Marshal(results)
	return string(resultJSON), nil
}

func (t ListEdictsTool) Format(input, result string, err error) string {
	msg := utils.NewMsgBlockBuilder("ListEdicts")
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		var edicts []map[string]any
		json.Unmarshal([]byte(result), &edicts)
		msg.Writef("Found %d edicts", len(edicts))
	}

	return msg.String() + "\n"
}

func (t ListEdictsTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"phase": map[string]any{
				"type":        "string",
				"enum":        []string{"brewing", "planning", "forging", "judging", "censoring", "deploying", "sealed", "cancelled"},
				"description": "Filter by phase (optional)",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of edicts to return (default: 20)",
			},
		},
	}
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
