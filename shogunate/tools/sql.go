package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/afittestide/asimi/internal/utils"
)

// CommandExecutor executes shell commands (for testability)
type CommandExecutor func(name string, args ...string) *exec.Cmd

// DefaultCommandExecutor is the default command executor using exec.Command
var DefaultCommandExecutor CommandExecutor = exec.Command

// AsimiSQLTool executes SQL queries against the Shogunate database.
type AsimiSQLTool struct {
	DBPath      string
	ExecCommand CommandExecutor
}

func (t AsimiSQLTool) Name() string {
	return "asimisql"
}

func (t AsimiSQLTool) Description() string {
	return `Execute SQL against the Shogunate database. Use for edict status transitions and reviewing precedents:

Edict Status Transitions (via transition_edict tool, not direct SQL):
- Status is derived from seals and zhengming tables
- Use transition_edict tool to cancel edicts or grant ruler seal
- Statuses: active (default), blocked (pending zhengming), sealed (ruler seal), cancelled (cancelled_at set)

Find Review Suggestions (Censor Precedents):
- SELECT * FROM censor_precedents ORDER BY created_at DESC LIMIT 10;
- SELECT principle, ruling, justification FROM censor_precedents WHERE ruling = 'rejected';

Query Manifests and Verdicts:
- SELECT * FROM forge_manifests WHERE status = 'forged' ORDER BY created_at DESC;
- SELECT * FROM judge_verdicts WHERE outcome = 'failed';

Statuses:
- Edicts: active, blocked, sealed, cancelled (derived from seals/zhengming)
- Manifests: forged, live, quenched, rejected
- Verdicts: passed, failed
- Precedents: approved, rejected`
}

func (t AsimiSQLTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.Query == "" {
		return "", fmt.Errorf("query is required")
	}

	executor := t.ExecCommand
	if executor == nil {
		executor = DefaultCommandExecutor
	}

	// Execute via sqlite3 command
	// #nosec G204 - dbPath is controlled by the application
	cmd := executor("sqlite3", t.DBPath, params.Query)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("sqlite3 error: %w: %s", err, string(output))
	}

	result := strings.TrimSpace(string(output))
	if result == "" {
		return `{"status":"ok"}`, nil
	}
	return result, nil
}

func (t AsimiSQLTool) Format(input, result string, err error) string {
	var params struct {
		Query string `json:"query"`
	}
	json.Unmarshal([]byte(input), &params)

	q := params.Query
	if len(q) > 40 {
		q = q[:37] + "..."
	}

	msg := utils.NewMsgBlockBuilder("SQL")
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		msg.WriteString(q)
	}

	return msg.String() + "\n"
}

func (t AsimiSQLTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "SQL query to execute",
			},
		},
		"required": []string{"query"},
	}
}
