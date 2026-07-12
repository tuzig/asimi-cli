package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/afittestide/asimi/internal/utils"
	"github.com/afittestide/asimi/storage"
)

// --- InvokeMinisterTool ---

// InvokeMinisterTool allows the Chancellor to invoke any registered minister for an edict.
type InvokeMinisterTool struct {
	Ctx     ToolContext
	Invoker MinisterInvoker
}

func (t InvokeMinisterTool) Name() string {
	return "invoke_minister"
}

func (t InvokeMinisterTool) Description() string {
	return `Invoke a minister by ID to execute its logic for an edict.
	Ministers process edicts through their specialized phase logic
	(e.g., strategist for planning, forge for code generation, judge for testing and verification, sage for review).
	Provide specific task instructions for what the minister should do.`
}

func (t InvokeMinisterTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		MinisterID string `json:"minister_id"`
		EdictID    uint   `json:"edict_id"`
		Work       string `json:"task"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.MinisterID == "" {
		return "", fmt.Errorf("minister_id is required")
	}
	if params.EdictID == 0 {
		return "", fmt.Errorf("edict_id is required")
	}
	if params.Work == "" {
		return "", fmt.Errorf("task is required")
	}

	key := storage.EdictKey{
		ID:       params.EdictID,
		Username: t.Ctx.Username,
		Project:  t.Ctx.Project,
	}

	return t.Invoker.InvokeMinister(ctx, params.MinisterID, key, params.Work)
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
				"description": "The minister to invoke (strategist, forge, judge, or sage)",
			},
			"edict_id": map[string]any{
				"type":        "integer",
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

// --- InvokeRitualTool ---

// InvokeRitualTool starts a YAML-defined ritual workflow.
type InvokeRitualTool struct {
	Ctx      ToolContext
	Launcher RitualLauncher
}

func (t InvokeRitualTool) Name() string {
	return "enact_ritual"
}

func (t InvokeRitualTool) Description() string {
	return `Execute a YAML-defined ritual workflow for an existing edict. Blocks until the ritual completes or fails.
Rituals are predefined workflows that orchestrate ministers and commands through a series of steps.`
}

func (t InvokeRitualTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		RitualName string            `json:"ritual_name"`
		EdictID    uint              `json:"edict_id"`
		Inputs     map[string]string `json:"inputs"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.RitualName == "" {
		return "", fmt.Errorf("ritual_name is required")
	}

	key := storage.EdictKey{
		ID:       params.EdictID,
		Username: t.Ctx.Username,
		Project:  t.Ctx.Project,
	}

	if params.Inputs == nil {
		params.Inputs = make(map[string]string)
	}
	params.Inputs["edict_id"] = fmt.Sprintf("%d", key.ID)

	if err := t.Launcher.StartRitual(params.RitualName, key, params.Inputs); err != nil {
		return "", err
	}

	result := map[string]any{
		"status":      "enacted and reported. stay quiet and trust the ritual to wake you up when done",
		"ritual_name": params.RitualName,
		"edict_id":    key.ID,
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
			Status      string `json:"status"`
			ExecutionID string `json:"execution_id"`
		}
		json.Unmarshal([]byte(result), &res)
		execID := res.ExecutionID
		if len(execID) > 8 {
			execID = execID[:8]
		}
		switch res.Status {
		case "completed":
			msg.Writef("Completed [%s]", execID)
		case "failed":
			msg.Writef("Failed [%s]", execID)
		default:
			msg.Writef("%s [%s]", res.Status, execID)
		}
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
				"type":        "integer",
				"description": "The edict ID this ritual is processing (optional for unbound rituals, like reviews)",
			},
			"inputs": map[string]any{
				"type":        "object",
				"description": "Optional inputs for the ritual (key-value pairs)",
			},
		},
		"required": []string{"ritual_name"},
	}
}
