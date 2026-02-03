package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/afittestide/asimi/internal/utils"
)

// MinisterResult captures the result of a minister task invocation.
// This interface avoids circular dependency with the shogunate package.
type MinisterResult interface {
	GetMinisterID() string
	GetSealed() bool
	GetOutput() string
	GetErr() error
}

// MinisterInvoker provides the ability to invoke ministers
type MinisterInvoker interface {
	// InvokeMinister sends a task to a minister and waits for a result
	InvokeMinister(ctx context.Context, ministerID, edictID, work string, timeout time.Duration) (MinisterResult, error)
}

// InvokeMinisterTool allows the Chancellor to invoke any registered minister for an edict.
type InvokeMinisterTool struct {
	// TODO: Simplify, the Invoker adds nothing...
	Invoker MinisterInvoker
	Logger  *slog.Logger
}

func (t InvokeMinisterTool) Name() string {
	return "invoke_minister"
}

func (t InvokeMinisterTool) Description() string {
	return `Invoke a minister by ID to execute its logic for an edict.
	Ministers process edicts through their specialized phase logic
	(e.g., strategist for planning, forge for code generation, judge for testing and verification, censor for review, marshal for deployment).
	Provide specific task instructions for what the minister should do.`
}

func (t InvokeMinisterTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		MinisterID string `json:"minister_id"`
		EdictID    string `json:"edict_id"`
		Work       string `json:"task"` // JSON field is "task" for backwards compatibility
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.MinisterID == "" {
		return "", fmt.Errorf("minister_id is required")
	}
	if params.EdictID == "" {
		return "", fmt.Errorf("edict_id is required")
	}
	if params.Work == "" {
		return "", fmt.Errorf("task is required")
	}

	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}

	res, err := t.Invoker.InvokeMinister(ctx, params.MinisterID, params.EdictID, params.Work, 5*time.Minute)
	if err != nil {
		logger.Error("task failed",
			"minister", params.MinisterID,
			"edict_id", params.EdictID,
			"error", err)
		return "", fmt.Errorf("minister %s failed: %w", params.MinisterID, err)
	}

	if res.GetErr() != nil {
		logger.Error("task returned error",
			"minister", params.MinisterID,
			"edict_id", params.EdictID,
			"error", res.GetErr())
		return "", fmt.Errorf("minister %s failed: %w", params.MinisterID, res.GetErr())
	}

	logger.Info("task completed",
		"minister", params.MinisterID,
		"edict_id", params.EdictID,
		"sealed", res.GetSealed(),
		"output_len", len(res.GetOutput()))

	result := map[string]any{
		"minister_id": params.MinisterID,
		"edict_id":    params.EdictID,
		"status":      "completed",
		"sealed":      res.GetSealed(),
		"output":      res.GetOutput(),
	}
	resultJSON, _ := json.Marshal(result)
	return string(resultJSON), nil
}

func (t InvokeMinisterTool) Format(input, result string, err error) string {
	var params struct {
		MinisterID string `json:"minister_id"`
		Task       string `json:"task"`
	}
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("InvokeMinister")
	msg.Writef(" %s", params.MinisterID)
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		taskPreview := params.Task
		if len(taskPreview) > 30 {
			taskPreview = taskPreview[:27] + "..."
		}
		msg.Writef("[%s]", taskPreview)
	}

	return msg.String() + "\n"
}

func (t InvokeMinisterTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"minister_id": map[string]any{
				"type":        "string",
				"description": "The minister to invoke (strategist, forge, judge, censor or marshal)",
			},
			"edict_id": map[string]any{
				"type":        "string",
				"description": "The edict ID to process",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "Specific instructions for the minister to execute",
			},
		},
		"required": []string{"minister_id", "edict_id", "task"},
	}
}
