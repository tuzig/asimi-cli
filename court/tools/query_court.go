package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/afittestide/asimi/internal/utils"
	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// QueryCourtTool returns a broad snapshot of court state — active edicts,
// seals per edict, and any pending zhengming requests. Read-only.
type QueryCourtTool struct {
	DB       *gorm.DB
	Username string
	Project  string
}

// Name returns the tool identifier.
func (t QueryCourtTool) Name() string { return "query_court" }

// Description returns a human-readable explanation of what the tool does.
func (t QueryCourtTool) Description() string {
	return `Query the current state of the court. Returns active edicts, their seal status,
recent manifests, verdicts, and precedents. Use this for a broad overview
of what's happening in the Court.`
}

// Call executes the court query and returns a JSON snapshot.
func (t QueryCourtTool) Call(ctx context.Context, input string) (string, error) {
	var params struct {
		Scope   string `json:"scope"` // "active", "all", or specific edict_id
		EdictID uint   `json:"edict_id"`
	}
	// Input may be empty for a default scope query; ignore unmarshal errors.
	_ = json.Unmarshal([]byte(input), &params) //nolint:errcheck // intentional: empty input is valid

	result := make(map[string]interface{})

	var edicts []storage.Edict
	query := t.DB.Order("created_at DESC").Limit(20)
	if params.EdictID != 0 {
		query = query.Where("id = ? AND username = ? AND project = ?", params.EdictID, t.Username, t.Project)
	} else if params.Scope != "all" {
		query = query.Where("status NOT IN ? AND username = ? AND project = ?", []string{"sealed", "cancelled"}, t.Username, t.Project)
	}
	query.Find(&edicts)

	edictSummaries := make([]map[string]interface{}, len(edicts))
	sealService := storage.NewSealService(t.DB)
	for i, e := range edicts {
		status, err := sealService.GetEdictStatus(storage.EdictKey{ID: e.ID, Username: e.Username, Project: e.Project})
		if err != nil {
			status = storage.EdictActive
		}
		edictSummaries[i] = map[string]interface{}{
			"edict_id": e.ID,
			"status":   string(status),
			"intent":   truncateForCourt(e.Intent, 120),
		}
	}
	result["edicts"] = edictSummaries

	sealSummaries := make([]map[string]interface{}, len(edicts))
	for i, e := range edicts {
		var seals []storage.Seal
		t.DB.Where("edict_id = ? AND username = ? AND project = ?", e.ID, e.Username, e.Project).Order("sealed_at ASC").Find(&seals)
		sealList := make([]map[string]interface{}, len(seals))
		for j, seal := range seals {
			sealList[j] = map[string]interface{}{
				"minister_id": seal.MinisterID,
				"sealed_at":   seal.SealedAt,
				"stale_at":    seal.StaleAt,
			}
		}
		sealSummaries[i] = map[string]interface{}{
			"edict_id": e.ID,
			"seals":    sealList,
		}
	}
	result["seals"] = sealSummaries

	var zhengming []storage.Zhengming
	t.DB.Where("status = ? AND username = ? AND project = ?", storage.ZhengmingPending, t.Username, t.Project).
		Order("created_at DESC").Limit(10).Find(&zhengming)
	if len(zhengming) > 0 {
		zhSummaries := make([]map[string]interface{}, len(zhengming))
		for i, z := range zhengming {
			zhSummaries[i] = map[string]interface{}{
				"request_id":  z.RequestID,
				"edict_id":    z.EdictID,
				"minister_id": z.MinisterID,
				"questions":   z.Questions,
				"priority":    string(z.Priority),
			}
		}
		result["pending_zhengming"] = zhSummaries
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ") //nolint:errcheck // result is always marshallable
	return string(resultJSON), nil
}

// Format renders the query result for display in the TUI.
func (t QueryCourtTool) Format(input, result string, err error) string {
	msg := utils.NewMsgBlockBuilder("QueryCourt")
	msg.WriteLn()
	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		var res map[string]interface{}
		// Best-effort parse; format degrades gracefully on failure.
		_ = json.Unmarshal([]byte(result), &res) //nolint:errcheck // best-effort for display
		if edicts, ok := res["edicts"].([]interface{}); ok {
			msg.Writef("Found %d edicts", len(edicts))
		}
	}
	return msg.String() + "\n"
}

// ParameterSchema returns the JSON schema for the tool's input parameters.
func (t QueryCourtTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"edict_id": map[string]any{
				"type":        "integer",
				"description": "Optional: focus on a specific edict",
			},
			"scope": map[string]any{
				"type":        "string",
				"enum":        []string{"active", "all"},
				"description": "Scope of query: 'active' (default) or 'all'",
			},
		},
	}
}

func truncateForCourt(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
