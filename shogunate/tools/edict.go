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
	GetEdict(edictID uint) (*storage.Edict, error)
	AppendToIntent(edictID uint, clarification string) error
	EmitEvent(edictID uint, eventType storage.ShogunateEvent, payload storage.JSON) error
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

	// Verify edict exists
	if _, err := t.Manager.GetEdict(params.EdictID); err != nil {
		return "", fmt.Errorf("get edict: %w", err)
	}

	if err := t.Manager.AppendToIntent(params.EdictID, params.Clarification); err != nil {
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
}

func (t GetEdictStatusTool) Name() string {
	return "get_edict_status"
}

func (t GetEdictStatusTool) Description() string {
	return "Gets the current status and phase of an edict. Use this to check progress on a work order."
}

func (t GetEdictStatusTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID uint `json:"edict_id"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.EdictID == 0 {
		return "", fmt.Errorf("edict_id is required")
	}

	edict, err := t.Manager.GetEdict(params.EdictID)
	if err != nil {
		return "", fmt.Errorf("get edict: %w", err)
	}

	result := map[string]any{
		"edict_id": edict.EdictID,
		"status":   string(edict.Status),
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

	var edicts []storage.Edict
	query := t.DB.Order("created_at DESC").Limit(params.Limit)
	if params.Status != "" {
		query = query.Where("status = ?", params.Status)
	}

	if err := query.Find(&edicts).Error; err != nil {
		return "", fmt.Errorf("list edicts: %w", err)
	}

	var results []map[string]any
	for _, e := range edicts {
		results = append(results, map[string]any{
			"edict_id": e.EdictID,
			"status":   string(e.Status),
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

	// Validate status transition
	var targetStatus storage.EdictStatus
	switch params.Status {
	case "active":
		targetStatus = storage.EdictActive
	case "cancelled":
		targetStatus = storage.EdictCancelled
	case "blocked":
		targetStatus = storage.EdictBlocked
	case "sealed":
		targetStatus = storage.EdictSealed
	default:
		return "", fmt.Errorf("invalid status: %s (valid: active, cancelled, blocked, sealed)", params.Status)
	}

	// Verify edict exists and get current status
	var edict storage.Edict
	if err := t.DB.First(&edict, "edict_id = ?", params.EdictID).Error; err != nil {
		return "", fmt.Errorf("get edict: %w", err)
	}

	// Validate transition from blocked
	if edict.Status == storage.EdictBlocked {
		if targetStatus != storage.EdictActive && targetStatus != storage.EdictCancelled {
			return "", fmt.Errorf("blocked edicts can only be transitioned to active (unblock) or cancelled (reject)")
		}
	}

	// Perform transition
	if err := t.DB.Model(&storage.Edict{}).
		Where("edict_id = ? AND status = ?", params.EdictID, edict.Status).
		Update("status", targetStatus).Error; err != nil {
		return "", fmt.Errorf("transition edict: %w", err)
	}

	result := map[string]any{
		"edict_id":        params.EdictID,
		"previous_status": string(edict.Status),
		"new_status":      string(targetStatus),
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
