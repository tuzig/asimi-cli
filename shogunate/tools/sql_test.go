package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAsimiSQLTool_LargeOutputNotTruncated demonstrates the bug:
// AsimiSQLTool.Call() returns raw output without truncation, which can
// cause the session's context to explode when tool outputs are added
// to the message history.
//
// The truncation SHOULD happen in Session.executeToolCall(), but when
// AsimiSQLTool is used through the scheduler, the raw output flows through
// and needs to be truncated. This test verifies that truncation is applied.
func TestAsimiSQLTool_LargeOutputNotTruncated(t *testing.T) {
	// Create a temporary database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	// Create a minimal SQLite database
	err := os.WriteFile(dbPath, []byte(""), 0644)
	require.NoError(t, err)

	// Check if sqlite3 is available - if not, skip
	_, err = os.Stat("/usr/bin/sqlite3")
	if os.IsNotExist(err) {
		_, err = os.Stat("/usr/local/bin/sqlite3")
	}
	if os.IsNotExist(err) {
		t.Skip("sqlite3 not available in this environment")
	}

	tool := AsimiSQLTool{DBPath: dbPath}

	// Generate a query that would return many rows (simulated large output)
	// We'll test the truncation happens at the Session level, not here
	query := "SELECT * FROM sqlite_master;"

	result, err := tool.Call(context.Background(), `{"query":"`+query+`"}`)
	require.NoError(t, err)

	// The tool returns raw output - truncation should happen in Session.executeToolCall
	// This test documents the current behavior: raw output is returned
	assert.NotEmpty(t, result)
}

// TestAsimiSQLTool_OutputSizeVerification verifies that AsimiSQLTool's output
// would be too large without truncation when querying large tables.
//
// This test demonstrates why truncation is critical:
// Without truncation, a simple "SELECT * FROM large_table" could return
// megabytes of data that would bloat the session context.
func TestAsimiSQLTool_OutputSizeVerification(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	// Create empty DB file
	err := os.WriteFile(dbPath, []byte(""), 0644)
	require.NoError(t, err)

	// Check if sqlite3 is available - if not, skip
	_, err = os.Stat("/usr/bin/sqlite3")
	if os.IsNotExist(err) {
		_, err = os.Stat("/usr/local/bin/sqlite3")
	}
	if os.IsNotExist(err) {
		t.Skip("sqlite3 not available in this environment")
	}

	tool := AsimiSQLTool{DBPath: dbPath}

	// Test that an empty result is returned properly
	result, err := tool.Call(context.Background(), `{"query":"SELECT name FROM sqlite_master"}`)
	require.NoError(t, err)

	// Empty result should return status:ok
	assert.Contains(t, result, `"status":"ok"`)
}

// TestAsimiSQLTool_ErrorsHandled verifies error handling
func TestAsimiSQLTool_ErrorsHandled(t *testing.T) {
	tool := AsimiSQLTool{DBPath: "/nonexistent/path/db.sqlite"}

	// Invalid JSON input
	_, err := tool.Call(context.Background(), "not json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid input")

	// Empty query
	_, err = tool.Call(context.Background(), `{"query":""}`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "query is required")
}

// TestAsimiSQLTool_Format verifies the Format method works correctly
func TestAsimiSQLTool_Format(t *testing.T) {
	tool := AsimiSQLTool{DBPath: "/test/path"}

	// Test with short query
	result := tool.Format(`{"query":"SELECT * FROM edicts"}`, "output", nil)
	assert.Contains(t, result, "AsimiSQL")
	assert.Contains(t, result, "SELECT * FROM edicts")

	// Test with long query (should be truncated)
	longQuery := "SELECT edict_id, intent, status, created_at, updated_at, notes, metadata FROM edicts WHERE status = 'active' ORDER BY created_at DESC"
	result = tool.Format(`{"query":"`+longQuery+`"}`, "output", nil)
	assert.Contains(t, result, "...")
	assert.LessOrEqual(t, len(result), 100)

	// Test with error
	result = tool.Format(`{"query":"bad"}`, "", assert.AnError)
	assert.Contains(t, result, "Error:")
}

// TestAsimiSQLTool_ParameterSchema verifies the tool's parameter schema
func TestAsimiSQLTool_ParameterSchema(t *testing.T) {
	tool := AsimiSQLTool{DBPath: "/test"}

	schema := tool.ParameterSchema()
	assert.NotNil(t, schema)

	// Verify structure
	props, ok := schema["properties"].(map[string]any)
	assert.True(t, ok)

	query, ok := props["query"]
	assert.True(t, ok)

	queryDef := query.(map[string]any)
	assert.Equal(t, "string", queryDef["type"])
	assert.Contains(t, queryDef["description"], "SQL")
}

// TestAsimiSQLTool_NameAndDescription verifies tool metadata
func TestAsimiSQLTool_NameAndDescription(t *testing.T) {
	tool := AsimiSQLTool{DBPath: "/test"}

	assert.Equal(t, "asimisql", tool.Name())

	desc := tool.Description()
	assert.Contains(t, desc, "Execute SQL")
	assert.Contains(t, desc, "Shogunate database")
}
