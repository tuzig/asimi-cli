package court

import (
	"context"
	"sync"
	"testing"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/court/tools"
	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestZhengmingRouting_Sage verifies that when Sage's request_zhengming tool
// is invoked, the ZhengmingPendingMsg carries MinisterID="sage" — not "chancellor".
//
// Before the fix (edict 489), the shared RequestZhengmingTool in the registry
// omitted MinisterID, and the Requester was always the Chancellor, so all
// zhengming notifications routed to MinisterID="chancellor" regardless of
// which minister actually asked the question.
func TestZhengmingRouting_Sage(t *testing.T) {
	db := setupMinisterTestDB(t)

	// Create a MinisterBase acting as the zhengming requester (chancellor's role)
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject", nil)

	// Capture ZhengmingPendingMsg notifications
	var mu sync.Mutex
	var pendingMsgs []ZhengmingPendingMsg

	base.SetNotify(func(msg any) {
		mu.Lock()
		defer mu.Unlock()
		if m, ok := msg.(ZhengmingPendingMsg); ok {
			pendingMsgs = append(pendingMsgs, m)
		}
	})

	// Build tool registry — request_zhengming is now a factory extra tool,
	// resolved per-minister via ExtraTools(ministerID, names).
	registry := tools.NewToolRegistry()
	tools.RegisterBuiltinTools(registry, tools.ToolRegistrationOpts{
		Ctx: tools.ToolContext{
			RepoInfo: &repo.RepoInfo{ProjectRoot: "/tmp"},
			Username: "testuser",
			Project:  "testproject",
			DB:       db,
		},
		ZhengmingRequester: base,
		WaitForZhengming:    nil, // no WaitForAnswer — Call returns immediately with "pending" status
	})

	// Get Sage's extra tools — request_zhengming is a factory tool
	sageExtras := registry.ExtraTools("sage", []string{"request_zhengming"})

	// Find the request_zhengming tool
	var zhengmingTool tools.Tool
	for _, tool := range sageExtras {
		if tool.Name() == "request_zhengming" {
			zhengmingTool = tool
			break
		}
	}
	require.NotNil(t, zhengmingTool, "sage should have request_zhengming tool")

	// Call the tool
	input := `{"edict_id": 1, "questions": [{"text": "Approve?", "options": ["Yes", "No"]}]}`
	_, err := zhengmingTool.Call(context.Background(), input)
	require.NoError(t, err, "request_zhengming Call should not error")

	// Verify the ZhengmingPendingMsg has MinisterID="sage"
	mu.Lock()
	defer mu.Unlock()

	require.Len(t, pendingMsgs, 1, "should have received one ZhengmingPendingMsg")
	assert.Equal(t, "sage", pendingMsgs[0].MinisterID,
		"ZhengmingPendingMsg.MinisterID should be 'sage', not 'chancellor' — the tool's MinisterID must flow through to the pending message")
}

// TestZhengmingRouting_Strategist verifies that when Strategist's request_zhengming
// tool is invoked, the ZhengmingPendingMsg carries MinisterID="strategist".
func TestZhengmingRouting_Strategist(t *testing.T) {
	db := setupMinisterTestDB(t)

	// Create a MinisterBase acting as the zhengming requester
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject", nil)

	// Capture ZhengmingPendingMsg notifications
	var mu sync.Mutex
	var pendingMsgs []ZhengmingPendingMsg

	base.SetNotify(func(msg any) {
		mu.Lock()
		defer mu.Unlock()
		if m, ok := msg.(ZhengmingPendingMsg); ok {
			pendingMsgs = append(pendingMsgs, m)
		}
	})

	// Build tool registry — request_zhengming is now a factory extra tool
	registry := tools.NewToolRegistry()
	tools.RegisterBuiltinTools(registry, tools.ToolRegistrationOpts{
		Ctx: tools.ToolContext{
			RepoInfo: &repo.RepoInfo{ProjectRoot: "/tmp"},
			Username: "testuser",
			Project:  "testproject",
			DB:       db,
		},
		ZhengmingRequester: base,
		WaitForZhengming:    nil, // no WaitForAnswer — Call returns immediately
	})

	// Get Strategist's extra tools
	strategistExtras := registry.ExtraTools("strategist", []string{"request_zhengming"})

	// Find the request_zhengming tool
	var zhengmingTool tools.Tool
	for _, tool := range strategistExtras {
		if tool.Name() == "request_zhengming" {
			zhengmingTool = tool
			break
		}
	}
	require.NotNil(t, zhengmingTool, "strategist should have request_zhengming tool")

	// Call the tool
	input := `{"edict_id": 2, "questions": [{"text": "Which approach?", "options": ["A", "B"]}]}`
	_, err := zhengmingTool.Call(context.Background(), input)
	require.NoError(t, err, "request_zhengming Call should not error")

	// Verify the ZhengmingPendingMsg has MinisterID="strategist"
	mu.Lock()
	defer mu.Unlock()

	require.Len(t, pendingMsgs, 1, "should have received one ZhengmingPendingMsg")
	assert.Equal(t, "strategist", pendingMsgs[0].MinisterID,
		"ZhengmingPendingMsg.MinisterID should be 'strategist', not 'chancellor'")
}

// TestZhengmingRouting_DBRecord verifies that the storage.Zhengming DB record
// also carries the correct MinisterID from the calling tool, not the requester's ID.
func TestZhengmingRouting_DBRecord(t *testing.T) {
	db := setupMinisterTestDB(t)

	// Create a MinisterBase acting as the zhengming requester
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject", nil)
	base.SetNotify(func(msg any) {}) // discard notifications

	// Build tool registry — request_zhengming is now a factory extra tool
	registry := tools.NewToolRegistry()
	tools.RegisterBuiltinTools(registry, tools.ToolRegistrationOpts{
		Ctx: tools.ToolContext{
			RepoInfo: &repo.RepoInfo{ProjectRoot: "/tmp"},
			Username: "testuser",
			Project:  "testproject",
			DB:       db,
		},
		ZhengmingRequester: base,
		WaitForZhengming:    nil, // no WaitForAnswer — Call returns immediately
	})

	// Get Sage's extra tools and find request_zhengming
	sageExtras := registry.ExtraTools("sage", []string{"request_zhengming"})
	var zhengmingTool tools.Tool
	for _, tool := range sageExtras {
		if tool.Name() == "request_zhengming" {
			zhengmingTool = tool
			break
		}
	}
	require.NotNil(t, zhengmingTool, "sage should have request_zhengming tool")

	// Call the tool
	input := `{"edict_id": 3, "questions": [{"text": "OK?", "options": ["Yes", "No"]}]}`
	_, err := zhengmingTool.Call(context.Background(), input)
	require.NoError(t, err, "request_zhengming Call should not error")

	// Verify the DB record has MinisterID="sage"
	var zhengming storage.Zhengming
	err = db.Where("edict_id = ? AND username = ? AND project = ?", 3, "testuser", "testproject").First(&zhengming).Error
	require.NoError(t, err, "should find the zhengming DB record")
	assert.Equal(t, "sage", zhengming.MinisterID,
		"storage.Zhengming.MinisterID should be 'sage', not 'chancellor'")
}
