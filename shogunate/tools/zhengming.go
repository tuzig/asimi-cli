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
	RequestZhengming(key storage.EdictKey, questions storage.ZhengmingQuestions, priority storage.ZhengmingPriority) (requestID string, err error)
}

// RequestZhengmingTool requests clarification from the user.
// WaitForAnswer blocks until the user responds, returning the actual answer.
type RequestZhengmingTool struct {
	MinisterID     string
	Requester      ZhengmingRequester
	WaitForAnswer  func(ctx context.Context, requestID string) (string, error)
	Username       string
	Project        string
}

func (t RequestZhengmingTool) Name() string {
	return "request_zhengming"
}

func (t RequestZhengmingTool) Description() string {
	return `Request clarification from the user (Zhengming - 正名).
	Example input: 
	{"questions":[{"text":"Which approach?","options":["Option A","Option B"]}]}`
}

func (t RequestZhengmingTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		EdictID   uint                        `json:"edict_id"`
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

	key := storage.EdictKey{ID: params.EdictID, Username: t.Username, Project: t.Project}
	requestID, err := t.Requester.RequestZhengming(key, storage.ZhengmingQuestions(params.Questions), priority)
	if err != nil {
		return "", fmt.Errorf("request zhengming: %w", err)
	}

	if t.WaitForAnswer == nil {
		return fmt.Sprintf(`{"status":"pending","request_id":"%s"}`, requestID), nil
	}

	answer, err := t.WaitForAnswer(ctx, requestID)
	if err != nil {
		return "", fmt.Errorf("waiting for zhengming answer: %w", err)
	}

	return fmt.Sprintf(`{"status":"answered","request_id":"%s","answer":"%s"}`, requestID, answer), nil
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
		"type":        "object",
		"description": "Clarification request containing one or more questions",
		"properties": map[string]any{
			"edict_id": map[string]any{
				"type":        "integer",
				"description": "The ID of the edict being worked on",
			},
			"questions": map[string]any{
				"type":        "array",
				"description": "Array of questions with 2-4 answer options",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text": map[string]any{
							"type":        "string",
							"description": "The question text",
						},
						"summary": map[string]any{
							"type":        "string",
							"description": "Optional short display text for the UI; text is used if empty",
						},
						"options": map[string]any{
							"type":        "array",
							"description": "2-4 predefined answer choices. Put the recommended choice first.",
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
				"enum":        []string{"normal", "urgent"},
				"description": "Priority level",
			},
		},
		"required":             []string{"questions"},
		"additionalProperties": false,
	}
}
