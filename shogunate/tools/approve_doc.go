package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/afittestide/asimi/internal/utils"
	"github.com/pmezard/go-difflib/difflib"
)

// ApproveDocInput is the input for the ApproveDocTool
type ApproveDocInput struct {
	Content     string `json:"content"`
	Filename    string `json:"filename,omitempty"`
	Description string `json:"description,omitempty"`
}

// EditorRequest is sent via Notify to ask the host (TUI or RPC client) to open
// content in $EDITOR. The receiver owns the temp-file lifecycle: it writes the
// content, runs the editor, and returns the modified bytes on ResultChan.
//
// This shape is intentionally serialization-friendly (no *exec.Cmd) so a daemon
// can bridge it to a remote TUI over RPC.
type EditorRequest struct {
	Content    string
	Filename   string // hint for extension/syntax highlighting (optional)
	ResultChan chan EditorResult
}

// EditorResult carries the post-edit content back to the tool, or an error if
// the editor failed to launch.
type EditorResult struct {
	Content  string // modified bytes; empty if Saved == false
	Saved    bool   // false when the user quit without writing (vi :q!)
	Err      error
}

// ApproveDocTool opens documents in $EDITOR for review. Returns status='approved'
// when content is unchanged, status='modified' with a diff when edited, or
// status='rejected' when the user quit without saving.
//
// Notify must be set to send an EditorRequest to the host that owns the terminal.
type ApproveDocTool struct {
	Notify func(any)
}

func (t ApproveDocTool) Name() string {
	return "approve_doc"
}

func (t ApproveDocTool) Description() string {
	return "Opens a document in $EDITOR for review. If the content is unchanged after editing, returns status='approved'. If modified, returns status='modified' with the diff. Use this for reviewing large documents (edict suggestions, strategic plans) that cannot be properly viewed in the TUI."
}

func (t ApproveDocTool) Call(ctx context.Context, input string) (string, error) {
	var params ApproveDocInput
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.Content == "" {
		return "", fmt.Errorf("content is required")
	}

	if t.Notify == nil {
		return "", fmt.Errorf("approve_doc requires TUI notify to open editor")
	}

	resultChan := make(chan EditorResult, 1)
	t.Notify(EditorRequest{
		Content:    params.Content,
		Filename:   params.Filename,
		ResultChan: resultChan,
	})

	var res EditorResult
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res = <-resultChan:
	}
	if res.Err != nil {
		return "", fmt.Errorf("editor failed: %w", res.Err)
	}

	if !res.Saved {
		return `{"status":"rejected"}`, nil
	}

	if strings.TrimRight(res.Content, " \t\n\r") == strings.TrimRight(params.Content, " \t\n\r") {
		return `{"status":"approved"}`, nil
	}

	diffOutput, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(params.Content),
		B:        difflib.SplitLines(res.Content),
		FromFile: "original",
		ToFile:   "modified",
		Context:  3,
	})

	result := map[string]any{
		"status":  "modified",
		"diff":    diffOutput,
		"content": res.Content,
	}
	resultJSON, _ := json.Marshal(result)
	return string(resultJSON), nil
}

func (t ApproveDocTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "Document content to review, formatted as markdown",
			},
			"filename": map[string]any{
				"type":        "string",
				"description": "Suggested filename (optional)",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Context for the review (optional)",
			},
		},
		"required": []string{"content"},
	}
}

// Format formats an approve_doc tool call for display
func (t ApproveDocTool) Format(input, result string, err error) string {
	var params ApproveDocInput
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("ApproveDoc")
	if params.Description != "" {
		msg.Writef(" %s", params.Description)
	}
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		var res struct {
			Status string `json:"status"`
		}
		json.Unmarshal([]byte(result), &res)
		switch res.Status {
		case "approved":
			msg.WriteString("Approved (no changes)")
		case "rejected":
			msg.WriteString("Rejected (quit without saving)")
		default:
			msg.WriteString("Modified (diff returned)")
		}
	}

	return msg.String() + "\n"
}
