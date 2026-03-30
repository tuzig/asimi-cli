package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/utils"
	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// EdictManager provides edict management capabilities
type EdictManager interface {
	GetEdict(key storage.EdictKey) (*storage.Edict, error)
	AppendToIntent(key storage.EdictKey, clarification string) error
	EmitEvent(key storage.EdictKey, eventType storage.ShogunateEvent, payload storage.JSON) error
}

// UpdateEdictTool refines an existing edict's intent (Chancellor only).
// Only the Ruler creates edicts via SubmitEdict; the Chancellor refines them.
type UpdateEdictTool struct {
	Manager EdictManager
}

func (t UpdateEdictTool) Name() string {
	return "update_edict"
}

func (t UpdateEdictTool) Description() string {
	return "Refine an existing edict's intent with additional context, clarification, or refined understanding. Use this after brewing to sharpen the edict before orchestrating ministers. Only the Ruler creates edicts; you refine them."
}

func (t UpdateEdictTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID       uint   `json:"edict_id"`
		Username      string `json:"username"`
		Project       string `json:"project"`
		Clarification string `json:"clarification"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.EdictID == 0 {
		return "", fmt.Errorf("edict_id is required")
	}
	if params.Clarification == "" {
		return "", fmt.Errorf("clarification is required")
	}

	key := storage.EdictKey{EdictID: params.EdictID, Username: params.Username, Project: params.Project}

	if _, err := t.Manager.GetEdict(key); err != nil {
		return "", fmt.Errorf("get edict: %w", err)
	}

	if err := t.Manager.AppendToIntent(key, params.Clarification); err != nil {
		return "", fmt.Errorf("update edict: %w", err)
	}

	return fmt.Sprintf(`{"status":"updated","edict_id":%d}`, params.EdictID), nil
}

func (t UpdateEdictTool) Format(input, result string, err error) string {
	var params struct {
		EdictID uint `json:"edict_id"`
	}
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("UpdateEdict")
	msg.Writef(" %d", params.EdictID)
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		msg.WriteString("Updated")
	}

	return msg.String() + "\n"
}

func (t UpdateEdictTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{
				"type":        "integer",
				"description": "The edict ID to refine",
			},
			"clarification": map[string]any{
				"type":        "string",
				"description": "Additional context or refined understanding to append to the edict's intent",
			},
		},
		"required": []string{"edict_id", "clarification"},
	}
}

// GetEdictStatusTool retrieves the status of an edict.
type GetEdictStatusTool struct {
	Manager EdictManager
	DB      *gorm.DB
}

func (t GetEdictStatusTool) Name() string {
	return "get_edict_status"
}

func (t GetEdictStatusTool) Description() string {
	return "Gets the current status and phase of an edict. Use this to check progress on a work order."
}

func (t GetEdictStatusTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID  uint   `json:"edict_id"`
		Username string `json:"username"`
		Project  string `json:"project"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.EdictID == 0 {
		return "", fmt.Errorf("edict_id is required")
	}

	if t.DB == nil {
		return "", fmt.Errorf("database connection not initialized")
	}

	key := storage.EdictKey{EdictID: params.EdictID, Username: params.Username, Project: params.Project}
	edict, err := t.Manager.GetEdict(key)
	if err != nil {
		return "", fmt.Errorf("get edict: %w", err)
	}

	sealService := storage.NewSealService(t.DB)
	status, err := sealService.GetEdictStatus(edict.Key())
	if err != nil {
		status = storage.EdictActive
	}

	result := map[string]any{
		"edict_id": edict.EdictID,
		"status":   string(status),
		"intent":   edict.Intent,
	}

	resultJSON, _ := json.Marshal(result)
	return string(resultJSON), nil
}

func (t GetEdictStatusTool) Format(input, result string, err error) string {
	var params struct {
		EdictID uint `json:"edict_id"`
	}
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("EdictStatus")
	msg.Writef(" %d", params.EdictID)
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		var res struct {
			Status string `json:"status"`
		}
		json.Unmarshal([]byte(result), &res)
		msg.Writef("[%s]", res.Status)
	}

	return msg.String() + "\n"
}

func (t GetEdictStatusTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{
				"type":        "integer",
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
	return "Lists all edicts, optionally filtered by status. Use this to see what work orders exist."
}

func (t ListEdictsTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		Status string `json:"status"`
		Limit  int    `json:"limit"`
	}
	json.Unmarshal([]byte(input), &params)

	if params.Limit <= 0 {
		params.Limit = 20
	}

	if t.DB == nil {
		return "", fmt.Errorf("database connection not initialized")
	}

	var edicts []storage.Edict
	query := t.DB.Order("created_at DESC").Limit(params.Limit)
	if err := query.Find(&edicts).Error; err != nil {
		return "", fmt.Errorf("list edicts: %w", err)
	}

	// Filter by status if specified (using derived status)
	sealService := storage.NewSealService(t.DB)
	var results []map[string]any
	for _, e := range edicts {
		status, err := sealService.GetEdictStatus(e.Key())
		if err != nil {
			status = storage.EdictActive
		}

		// Apply status filter
		if params.Status != "" && string(status) != params.Status {
			continue
		}

		results = append(results, map[string]any{
			"edict_id": e.EdictID,
			"status":   string(status),
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
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"active", "blocked", "sealed", "cancelled"},
				"description": "Filter by status (optional)",
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

// TransitionEdictTool transitions an edict to a new status (e.g., unblock or reject).
type TransitionEdictTool struct {
	DB *gorm.DB
}

func (t TransitionEdictTool) Name() string {
	return "transition_edict"
}

func (t TransitionEdictTool) Description() string {
	return "Transitions an edict to a new status. Use this to unblock (active) or reject (cancelled) blocked edicts after review."
}

func (t TransitionEdictTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID uint   `json:"edict_id"`
		Status  string `json:"status"`
		Reason  string `json:"reason,omitempty"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.EdictID == 0 {
		return "", fmt.Errorf("edict_id is required")
	}
	if params.Status == "" {
		return "", fmt.Errorf("status is required")
	}

	if t.DB == nil {
		return "", fmt.Errorf("database connection not initialized")
	}

	// Validate status
	switch params.Status {
	case "active", "cancelled", "blocked", "sealed":
		// valid
	default:
		return "", fmt.Errorf("invalid status: %s (valid: active, cancelled, blocked, sealed)", params.Status)
	}

	// Verify edict exists
	var edict storage.Edict
	if err := t.DB.First(&edict, "edict_id = ?", params.EdictID).Error; err != nil {
		return "", fmt.Errorf("get edict: %w", err)
	}

	key := edict.Key()
	sealService := storage.NewSealService(t.DB)
	currentStatus, err := sealService.GetEdictStatus(key)
	if err != nil {
		return "", fmt.Errorf("get edict status: %w", err)
	}

	if currentStatus == storage.EdictBlocked {
		if params.Status != "active" && params.Status != "cancelled" {
			return "", fmt.Errorf("blocked edicts can only be transitioned to active (unblock) or cancelled (reject)")
		}
	}

	switch params.Status {
	case "cancelled":
		now := time.Now()
		if err := t.DB.Model(&storage.Edict{}).
			Where("edict_id = ? AND username = ? AND project = ?", key.EdictID, key.Username, key.Project).
			Update("cancelled_at", now).Error; err != nil {
			return "", fmt.Errorf("cancel edict: %w", err)
		}
	case "sealed":
		if err := sealService.GrantSeal(key, "ruler", storage.JSON{"transitioned_by": "transition_edict"}); err != nil {
			return "", fmt.Errorf("seal edict: %w", err)
		}
	case "active", "blocked":
		// These are derived states - cannot be directly set
		// "active" is the default when no ruler seal and no pending zhengming
		// "blocked" requires a pending zhengming request
		return "", fmt.Errorf("cannot directly set status to %s - this is a derived state", params.Status)
	}

	result := map[string]any{
		"edict_id":        params.EdictID,
		"previous_status": string(currentStatus),
		"new_status":      params.Status,
	}
	if params.Reason != "" {
		result["reason"] = params.Reason
	}

	resultJSON, _ := json.Marshal(result)
	return string(resultJSON), nil
}

func (t TransitionEdictTool) Format(input, result string, err error) string {
	var params struct {
		EdictID uint   `json:"edict_id"`
		Status  string `json:"status"`
	}
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("TransitionEdict")
	msg.Writef(" %d", params.EdictID)
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		msg.Writef("Transitioned to [%s]", params.Status)
	}

	return msg.String() + "\n"
}

func (t TransitionEdictTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{
				"type":        "integer",
				"description": "The edict ID to transition",
			},
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"active", "cancelled", "blocked", "sealed"},
				"description": "New status: active (unblock), cancelled (reject), blocked, or sealed",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Optional reason for the transition",
			},
		},
		"required": []string{"edict_id", "status"},
	}
}
