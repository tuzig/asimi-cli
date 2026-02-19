package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/afittestide/asimi/internal/utils"
	"github.com/afittestide/asimi/storage"
)

// ZhengmingRequester provides clarification request capabilities
type ZhengmingRequester interface {
	RequestZhengming(edictID, question string, priority storage.ZhengmingPriority) (requestID string, err error)
}

// ZhengmingNotifyFunc is a callback for notifying about zhengming requests
type ZhengmingNotifyFunc func(requestID, edictID, ministerID, question string, priority storage.ZhengmingPriority)

// RequestZhengmingTool requests clarification from the user.
type RequestZhengmingTool struct {
	Requester ZhengmingRequester
	Notify    ZhengmingNotifyFunc
}

func (t RequestZhengmingTool) Name() string {
	return "request_zhengming"
}

func (t RequestZhengmingTool) Description() string {
	return "Request clarification from the user (Zhengming - 正名) when requirements are ambiguous. Use this when you need more information before proceeding with an edict. The edict will be halted until the user responds."
}

// TODO: It should lock and wait for the ruler response
func (t RequestZhengmingTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID  string `json:"edict_id"`
		Question string `json:"question"`
		Priority string `json:"priority"` // "low", "normal", "urgent"
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.EdictID == "" {
		return "", fmt.Errorf("edict_id is required")
	}
	if params.Question == "" {
		return "", fmt.Errorf("question is required")
	}

	// Default priority
	priority := storage.PriorityNormal
	if params.Priority != "" {
		priority = storage.ZhengmingPriority(params.Priority)
	}

	requestID, err := t.Requester.RequestZhengming(params.EdictID, params.Question, priority)
	if err != nil {
		return "", fmt.Errorf("request zhengming: %w", err)
	}

	// Notify if callback is set
	if t.Notify != nil {
		t.Notify(requestID, params.EdictID, "chancellor", params.Question, priority)
	}

	return fmt.Sprintf(`{"status":"pending","request_id":"%s"}`, requestID), nil
}

func (t RequestZhengmingTool) Format(input, result string, err error) string {
	var params struct {
		Question string `json:"question"`
	}
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("Zhengming")
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		// Truncate question if too long
		q := params.Question
		if len(q) > 50 {
			q = q[:47] + "..."
		}
		msg.WriteString(q)
	}

	return msg.String() + "\n"
}

// TODO: update the schema so it can contain multiple questions and every
// question contains 2-4 possible answers with the recommended one in the
// first stop
func (t RequestZhengmingTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{
				"type":        "string",
				"description": "The edict ID this question relates to",
			},
			"question": map[string]any{
				"type":        "string",
				"description": "The clarification question to ask the user",
			},
			"priority": map[string]any{
				"type":        "string",
				"enum":        []string{"low", "normal", "urgent"},
				"description": "Priority of the clarification request",
			},
		},
		"required": []string{"edict_id", "question"},
	}
}
