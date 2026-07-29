package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/afittestide/asimi/internal/utils"
	"github.com/afittestide/asimi/storage"
)

// --- ConsultMinisterTool ---

// ConsultMinisterTool allows any minister to consult any registered minister for an edict.
type ConsultMinisterTool struct {
	Ctx         ToolContext
	Consultant  MinisterConsultant
	MinisterIDs []string
}

func (t ConsultMinisterTool) Name() string {
	return "consult_minister"
}

func (t ConsultMinisterTool) Description() string {
	examples := "war, forge, judge, chancellor"
	if len(t.MinisterIDs) > 0 {
		// Use the last two as examples, rest as a plain list
		examples = strings.Join(t.MinisterIDs, ", ")
	}
	return fmt.Sprintf(`Consult a minister by ID to get its perspective or execute its logic for an edict.
	Ministers process edicts through their specialized phase logic
	(e.g., %s).
	Provide specific task instructions for what the minister should do.
	A minister cannot consult itself — if you need to perform a task within your own domain, do it directly.`, examples)
}

func (t ConsultMinisterTool) Call(ctx context.Context, input string) (string, error) {
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

	if params.MinisterID == t.Ctx.MinisterID {
		return "", fmt.Errorf("a minister cannot consult itself (caller: %s, target: %s) — use direct execution instead", t.Ctx.MinisterID, params.MinisterID)
	}

	key := storage.EdictKey{
		ID:       params.EdictID,
		Username: t.Ctx.Username,
		Project:  t.Ctx.Project,
	}

	return t.Consultant.ConsultMinister(ctx, t.Ctx.MinisterID, params.MinisterID, key, params.Work)
}

func (t ConsultMinisterTool) Format(input, result string, err error) string {
	var params struct {
		MinisterID string `json:"minister_id"`
		Task       string `json:"task"`
	}
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("ConsultMinister")
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

func (t ConsultMinisterTool) ParameterSchema() map[string]any {
	var examples string
	if len(t.MinisterIDs) > 0 {
		examples = strings.Join(t.MinisterIDs, ", ")
	} else {
		examples = "war, forge, judge, chancellor"
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"minister_id": map[string]any{
				"type":        "string",
				"description": fmt.Sprintf("The minister to consult (must be different from yourself) (%s)", examples),
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
	//TODO: the tool needs to listen to this event and ???
	return `Starts a ritual for an existing edict. Returns immediately with 'requested' status; the ritual runs asynchronously and completion is reported via events (ritual_completed, ritual_failed).`
}

func (t InvokeRitualTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		RitualName string         `json:"ritual_name"`
		EdictID    uint           `json:"edict_id"`
		Inputs     map[string]any `json:"inputs"`
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
		params.Inputs = make(map[string]any)
	}
	params.Inputs["edict_id"] = fmt.Sprintf("%d", key.ID)
	// Convert to map[string]string for StartRitual
	inputs := make(map[string]string, len(params.Inputs))
	for k, v := range params.Inputs {
		inputs[k] = fmt.Sprintf("%v", v)
	}
	if err := t.Launcher.StartRitual(params.RitualName, key, inputs); err != nil {
		return "", err
	}

	result := map[string]any{
		"status":      "requested and reported. stay quiet and trust the ritual to wake you up when done",
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
				"type":                 "object",
				"description":          "Optional inputs for the ritual (key-value pairs)",
				"AdditionalProperties": true,
			},
		},
		"required": []string{"ritual_name"},
	}
}
