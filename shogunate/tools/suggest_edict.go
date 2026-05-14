package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/afittestide/asimi/internal/utils"
	"github.com/afittestide/asimi/storage"
)

// SuggestEdictTool routes an edict suggestion to the Ruler via Zhengming.
// For suggestions over 500 chars, the Ruler reviews them in $EDITOR first
// (via ApproveDocTool); shorter ones go straight to a Zhengming prompt.
//
// Sage never creates edicts directly — only the Ruler does.
//
// NotifyFn is a getter, not a snapshot: it returns the *current* notify each
// time the tool runs. This matters in daemon mode where the live notify
// channel is installed lazily by shog.Subscribe() on each client connect, and
// can be swapped out when a client reconnects. A snapshot captured at
// Tools()-construction would be nil (or stale) and silently bypass the
// editor branch.
type SuggestEdictTool struct {
	Requester ZhengmingRequester
	NotifyFn  func() func(any)
	Username  string
	Project   string
}

func (t SuggestEdictTool) Name() string { return "suggest_edict" }

func (t SuggestEdictTool) Description() string {
	return `Suggest a new edict to the Ruler via Zhengming. Use this when you identify
an improvement opportunity, naming inconsistency, or refactoring need.
You cannot create edicts directly — only the Ruler can do that.
This creates a Zhengming request that the Ruler can approve or dismiss.
Returns immediately with status='suggested' - the edict will be created if approved via event.

For large suggestions (>500 chars), the Ruler reviews the text in $EDITOR.
If the tool returns status='ruler_modified', the Ruler has edited your suggestion.
Review the original, modified content, and diff. Then either:
- Call suggest_edict again with the modified content if you find it harmonized with your intent
- Respond in conversation explaining your concerns and suggesting changes if not harmonized`
}

func (t SuggestEdictTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		Suggestion string `json:"suggestion"`
		Summary    string `json:"summary"`
		Priority   string `json:"priority"`
		Evidence   string `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if params.Suggestion == "" {
		return "", fmt.Errorf("suggestion is required")
	}
	if params.Summary == "" {
		return "", fmt.Errorf("summary is required")
	}

	priority := storage.PriorityNormal
	if params.Priority == "urgent" {
		priority = storage.PriorityUrgent
	}

	questionText := params.Suggestion
	if params.Evidence != "" {
		questionText = fmt.Sprintf("%s\n\nEvidence: %s", params.Suggestion, params.Evidence)
	}

	// Large suggestions get an $EDITOR review pass via approve_doc before they
	// hit Zhengming. The Ruler can :wq to forward as-is, edit and :wq to bounce
	// it back to the Sage, or :q! to reject outright.
	var notify func(any)
	if t.NotifyFn != nil {
		notify = t.NotifyFn()
	}
	if len(questionText) > 500 && notify != nil {
		approveTool := ApproveDocTool{Notify: notify}
		approveInput := map[string]any{
			"content":     questionText,
			"description": "Review edict suggestion before submission",
		}
		approveInputJSON, _ := json.Marshal(approveInput)
		approveResult, err := approveTool.Call(ctx, string(approveInputJSON))
		if err != nil {
			return "", fmt.Errorf("approve_doc failed: %w", err)
		}

		var approveRes struct {
			Status  string `json:"status"`
			Diff    string `json:"diff,omitempty"`
			Content string `json:"content,omitempty"`
		}
		if err := json.Unmarshal([]byte(approveResult), &approveRes); err != nil {
			return "", fmt.Errorf("failed to parse approve_doc result: %w", err)
		}

		if approveRes.Status == "modified" {
			originalJSON, _ := json.Marshal(questionText)
			modifiedJSON, _ := json.Marshal(approveRes.Content)
			diffJSON, _ := json.Marshal(approveRes.Diff)
			return fmt.Sprintf(`{"status":"ruler_modified","original":%s,"modified":%s,"diff":%s}`,
				originalJSON, modifiedJSON, diffJSON), nil
		}
		// approved or rejected → fall through to zhengming with the original text
	}

	questions := storage.ZhengmingQuestions{{
		Text:    questionText,
		Summary: params.Summary,
		Options: []string{"Approve edict", "Reject"},
	}}

	key := storage.EdictKey{
		ID:       1, // court-scoped suggestions live under edict 1
		Username: t.Username,
		Project:  t.Project,
	}
	requestID, err := t.Requester.RequestZhengming(key, questions, priority)
	if err != nil {
		return "", fmt.Errorf("failed to suggest edict: %w", err)
	}

	return fmt.Sprintf(`{"status":"suggested","request_id":"%s"}`, requestID), nil
}

func (t SuggestEdictTool) Format(input, result string, err error) string {
	msg := utils.NewMsgBlockBuilder("SuggestEdict")
	msg.WriteLn()
	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		var params struct {
			Suggestion string `json:"suggestion"`
		}
		json.Unmarshal([]byte(input), &params)
		preview := params.Suggestion
		if len(preview) > 60 {
			preview = preview[:57] + "..."
		}
		msg.Writef("Suggested: %s", preview)
	}
	return msg.String() + "\n"
}

func (t SuggestEdictTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"suggestion": map[string]any{
				"type":        "string",
				"description": "What edict should the Ruler consider? Be specific about the change.",
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "A short one-line summary to help the ruler recall the edict",
			},
			"priority": map[string]any{
				"type":        "string",
				"enum":        []string{"normal", "urgent"},
				"description": "Priority level (default: normal)",
			},
			"evidence": map[string]any{
				"type":        "string",
				"description": "Supporting evidence: file:line references, patterns found, etc.",
			},
		},
		"required": []string{"suggestion", "summary"},
	}
}
