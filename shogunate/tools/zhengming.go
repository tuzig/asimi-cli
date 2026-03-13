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
	RequestZhengming(edictID string, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (requestID string, err error)
}

// ZhengmingNotifyFunc is a callback for notifying about zhengming requests
type ZhengmingNotifyFunc func(requestID, edictID, ministerID string, questions []storage.ZhengmingQuestion, priority storage.ZhengmingPriority)

// RequestZhengmingTool requests clarification from the user.
type RequestZhengmingTool struct {
	MinisterID string
	Requester  ZhengmingRequester
	Notify     ZhengmingNotifyFunc
}

func (t RequestZhengmingTool) Name() string {
	return "request_zhengming"
}

func (t RequestZhengmingTool) Description() string {
	return "Request clarification from the user (Zhengming - 正名) when requirements are ambiguous. Use this when you need more information before proceeding with an edict. The tool returns immediately with status='pending'."
}

func (t RequestZhengmingTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID   string                      `json:"edict_id"`
		Questions []storage.ZhengmingQuestion `json:"questions"`
		Priority  string                      `json:"priority"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if len(params.Questions) == 0 {
		return "", fmt.Errorf("questions array is required and must not be empty")
	}
	for i, q := range params.Questions {
		if q.Text == "" {
			return "", fmt.Errorf("question %d: text is required", i)
		}
		if len(q.Options) < 2 || len(q.Options) > 4 {
			return "", fmt.Errorf("question %d: must have 2-4 options, got %d", i, len(q.Options))
		}
	}

	priority := storage.PriorityNormal
	if params.Priority != "" {
		priority = storage.ZhengmingPriority(params.Priority)
	}

	requestID, err := t.Requester.RequestZhengming(params.EdictID, storage.ZhengmingQuestions(params.Questions), priority)
	if err != nil {
		return "", fmt.Errorf("request zhengming: %w", err)
	}

	// Notify TUI with structured questions
	if t.Notify != nil {
		t.Notify(requestID, params.EdictID, t.MinisterID, params.Questions, priority)
	}

	// Return immediately with pending status - no blocking
	return fmt.Sprintf(`{"status":"pending","request_id":"%s"}`, requestID), nil
}

func (t RequestZhengmingTool) Format(input, result string, err error) string {
	var params struct {
		Questions []storage.ZhengmingQuestion `json:"questions"`
	}
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("Zhengming")
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		for _, q := range params.Questions {
			text := q.Text
			if len(text) > 50 {
				text = text[:47] + "..."
			}
			msg.WriteString(text)
		}
		if len(params.Questions) == 0 {
			msg.WriteString("(no questions)")
		}
	}

	return msg.String() + "\n"
}

func (t RequestZhengmingTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{
				"type":        "string",
				"description": "The edict ID this question relates to",
			},
			"questions": map[string]any{
				"type":        "array",
				"description": "Array of questions with 2-4 answer options",
				"items": map[string]any{
					"type":  "object",
					"title": "Question",
					"properties": map[string]any{
						"text": map[string]any{
							"type":        "string",
							"description": "The question text",
						},
						"options": map[string]any{
							"type":        "array",
							"description": "2-4 answer options, recommended first",
							"items":       map[string]any{"type": "string"},
							"minItems":    2,
							"maxItems":    4,
						},
					},
					"required":             []string{"text", "options"},
					"additionalProperties": false,
				},
			},
			"priority": map[string]any{
				"type":        "string",
				"enum":        []string{"low", "normal", "urgent"},
				"description": "Priority level",
			},
		},
		"required":             []string{"questions"},
		"additionalProperties": false,
	}
}
