package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/afittestide/asimi/internal/utils"
)

// ApproveDocInput is the input for the ApproveDocTool
type ApproveDocInput struct {
	Content     string `json:"content"`
	Filename    string `json:"filename,omitempty"`
	Description string `json:"description,omitempty"`
}

// EditorRequest is sent via Notify to ask the TUI to open a file in $EDITOR.
// The caller blocks on ResultChan until the editor exits.
type EditorRequest struct {
	Cmd        *exec.Cmd
	ResultChan chan error
}

// ApproveDocTool opens documents in $EDITOR for review, auto-approves if unchanged, returns diff if modified.
// Notify must be set to send an EditorRequest to the TUI so tea.ExecProcess can run the editor.
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

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	// Create temp file with original content
	tmpFile, err := os.CreateTemp("", "approve_doc_*.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	if err := os.WriteFile(tmpPath, []byte(params.Content), 0644); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	// Ask the TUI to run the editor via tea.ExecProcess and block until done
	cmd := exec.Command(editor, tmpPath)
	resultChan := make(chan error, 1)
	t.Notify(EditorRequest{Cmd: cmd, ResultChan: resultChan})

	// Wait for editor to finish (TUI sends result after tea.ExecProcess callback)
	select {
	case <-ctx.Done():
		os.Remove(tmpPath)
		return "", ctx.Err()
	case editorErr := <-resultChan:
		if editorErr != nil {
			os.Remove(tmpPath)
			return "", fmt.Errorf("editor failed: %w", editorErr)
		}
	}

	// Read modified content
	modifiedContent, err := os.ReadFile(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to read modified file: %w", err)
	}

	// Compare with original
	if string(modifiedContent) == params.Content {
		os.Remove(tmpPath)
		return `{"status":"approved"}`, nil
	}

	// Compute diff (must happen before removing tmpPath)
	diffCmd := exec.Command("diff", "-u", "-", tmpPath)
	diffCmd.Stdin = strings.NewReader(params.Content)
	diffOutput, err := diffCmd.Output()
	os.Remove(tmpPath)
	if err != nil {
		// diff returns exit code 1 when files differ, but we still get output
		if len(diffOutput) == 0 {
			return "", fmt.Errorf("diff failed: %w", err)
		}
	}

	result := map[string]any{
		"status":  "modified",
		"diff":    string(diffOutput),
		"content": string(modifiedContent),
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
				"description": "Document content to review",
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
		if res.Status == "approved" {
			msg.WriteString("Approved (no changes)")
		} else {
			msg.WriteString("Modified (diff returned)")
		}
	}

	return msg.String() + "\n"
}
