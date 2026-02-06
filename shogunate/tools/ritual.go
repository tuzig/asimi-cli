package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/afittestide/asimi/internal/utils"
)

// RitualStarter provides the ability to start rituals
type RitualStarter interface {
	// StartRitual starts a ritual execution and returns the execution ID
	StartRitual(ctx context.Context, ritualName, edictID string, inputs map[string]string) (executionID string, err error)
}

// RitualNotifyFunc is called to notify the UI about ritual status
type RitualNotifyFunc func(ritualName, executionID, edictID, status string)

// InvokeRitualTool starts a YAML-defined ritual workflow
type InvokeRitualTool struct {
	Starter RitualStarter
	Logger  *slog.Logger
	Notify  RitualNotifyFunc
}

func (t InvokeRitualTool) Name() string {
	return "invoke_ritual"
}

func (t InvokeRitualTool) Description() string {
	return `Start a YAML-defined ritual workflow for an edict.
Rituals are predefined workflows that orchestrate ministers through a series of steps.
Use list_rituals to see available rituals, or specify a ritual name directly.
Common rituals: implement, fix, refactor, review.`
}

func (t InvokeRitualTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		RitualName string            `json:"ritual_name"`
		EdictID    string            `json:"edict_id"`
		Inputs     map[string]string `json:"inputs"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.RitualName == "" {
		return "", fmt.Errorf("ritual_name is required")
	}
	if params.EdictID == "" {
		return "", fmt.Errorf("edict_id is required")
	}

	if params.Inputs == nil {
		params.Inputs = make(map[string]string)
	}
	// Add edict_id to inputs for template expansion
	params.Inputs["edict_id"] = params.EdictID

	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}

	executionID, err := t.Starter.StartRitual(ctx, params.RitualName, params.EdictID, params.Inputs)
	if err != nil {
		// Notify: failed
		if t.Notify != nil {
			t.Notify(params.RitualName, "", params.EdictID, "failed")
		}
		return "", fmt.Errorf("failed to start ritual: %w", err)
	}

	// Notify: started
	if t.Notify != nil {
		t.Notify(params.RitualName, executionID, params.EdictID, "started")
	}

	logger.Info("ritual started",
		"ritual", params.RitualName,
		"execution_id", executionID,
		"edict_id", params.EdictID)

	result := map[string]any{
		"status":       "started",
		"execution_id": executionID,
		"ritual_name":  params.RitualName,
		"edict_id":     params.EdictID,
	}
	resultJSON, _ := json.Marshal(result)
	return string(resultJSON), nil
}

func (t InvokeRitualTool) Format(input, result string, err error) string {
	var params struct {
		RitualName string `json:"ritual_name"`
	}
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("Ritual")
	msg.Writef(" %s", params.RitualName)
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		var res struct {
			ExecutionID string `json:"execution_id"`
		}
		json.Unmarshal([]byte(result), &res)
		execID := res.ExecutionID
		if len(execID) > 8 {
			execID = execID[:8]
		}
		msg.Writef("Started [%s]", execID)
	}

	return msg.String() + "\n"
}

func (t InvokeRitualTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ritual_name": map[string]any{
				"type":        "string",
				"description": "Name of the ritual to invoke (e.g., 'implement', 'fix', 'refactor')",
			},
			"edict_id": map[string]any{
				"type":        "string",
				"description": "The edict ID this ritual is processing",
			},
			"inputs": map[string]any{
				"type":        "object",
				"description": "Optional inputs for the ritual (key-value pairs)",
			},
		},
		"required": []string{"ritual_name", "edict_id"},
	}
}
