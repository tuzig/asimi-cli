package court

import (
	"context"
	"sync"
	"testing"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/mocks"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestForgeChannelID_Routing verifies that streaming messages from Forge are routed to ChannelID="forge".
// This test MUST FAIL before the fix (when Forge hardcodes ChannelID="chancellor")
// and MUST PASS after the fix (when Forge uses its own ChannelID).
//
// The bug: Forge's streamTask() called CreateSessionWithOpts with
// ChannelID="chancellor" instead of ChannelID="forge", causing streaming notifications to
// route to the Chancellor tab instead of the Forge tab.
func TestForgeChannelID_Routing(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create mock LLM bridge
	mockLLM := mocks.NewLLMProvider()

	// Create Forge with mock LLM client
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject", nil)
	forge := NewForge(base)
	forge.SetMinisterConfig(mockLLM, &SessionConfig{LLM: config.LLMConfig{Provider: "test", Model: "test"}}, repo.RepoInfo{})

	// Collect all streaming notifications
	var mu sync.Mutex
	var streamMsgs []StreamChunkMsg

	forge.SetNotify(func(msg any) {
		mu.Lock()
		defer mu.Unlock()
		switch m := msg.(type) {
		case StreamChunkMsg:
			streamMsgs = append(streamMsgs, m)
		}
	})

	// Create done channel and task (no lings — Forge uses streamTask directly)
	doneCh := make(chan Result, 1)
	task := &Task{
		Ctx:        ctx,
		EdictKey:   storage.EdictKey{ID: 1, Username: "testuser", Project: "testproject"},
		Work:       "process the task",
		Scratchpad: "# Test",
		Done:       doneCh,
	}

	// Process the task
	forge.processTask(ctx, task)

	// Wait for completion
	result := <-doneCh
	require.NoError(t, result.Err, "processTask should not return an error")

	// Verify all streaming chunks were routed to ChannelID="forge"
	mu.Lock()
	defer mu.Unlock()

	require.NotEmpty(t, streamMsgs, "should have received at least one StreamChunkMsg")

	for i, msg := range streamMsgs {
		assert.Equal(t, "forge", msg.ChannelID, "StreamChunkMsg[%d] should have ChannelID='forge', got ChannelID='%s' (routing bug)", i, msg.ChannelID)
	}
}

// TestForgeChannelID_DirectStreamTask verifies ChannelID routing when Forge uses streamTask directly
// (no lings, so Forge uses streamTask directly).
func TestForgeChannelID_DirectStreamTask(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx := context.Background()

	// Create mock LLM bridge
	mockLLM := mocks.NewLLMProvider()

	// Create Forge with mock LLM client
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject", nil)
	forge := NewForge(base)
	forge.SetMinisterConfig(mockLLM, &SessionConfig{LLM: config.LLMConfig{Provider: "test", Model: "test"}}, repo.RepoInfo{})

	// Collect all streaming notifications
	var mu sync.Mutex
	var streamMsgs []StreamChunkMsg

	forge.SetNotify(func(msg any) {
		mu.Lock()
		defer mu.Unlock()
		if m, ok := msg.(StreamChunkMsg); ok {
			streamMsgs = append(streamMsgs, m)
		}
	})

	// Create done channel and task (NO pending lings, so it uses streamTask directly)
	doneCh := make(chan Result, 1)
	task := &Task{
		Ctx:        ctx,
		EdictKey:   storage.EdictKey{ID: 1, Username: "testuser", Project: "testproject"},
		Work:       "direct stream task",
		Scratchpad: "# Test",
		Done:       doneCh,
	}

	// Process the task
	forge.processTask(ctx, task)

	// Wait for completion
	result := <-doneCh
	require.NoError(t, result.Err, "processTask should not return an error")

	// Verify all streaming chunks were routed to ChannelID="forge"
	mu.Lock()
	defer mu.Unlock()

	require.NotEmpty(t, streamMsgs, "should have received at least one StreamChunkMsg")

	for i, msg := range streamMsgs {
		assert.Equal(t, "forge", msg.ChannelID, "StreamChunkMsg[%d] should have ChannelID='forge', got ChannelID='%s' (routing bug)", i, msg.ChannelID)
	}
}

// TestJudgeChannelID_Routing verifies that streaming messages from Judge are routed to ChannelID="judge".
// This test MUST FAIL before the fix (when Judge hardcodes ChannelID="chancellor")
// and MUST PASS after the fix (when Judge uses ChannelID="judge").
func TestJudgeChannelID_Routing(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create mock LLM bridge
	mockLLM := mocks.NewLLMProvider()

	// Create Judge with mock LLM client
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject", nil)
	judge := NewJudge(base, nil)
	judge.SetMinisterConfig(mockLLM, &SessionConfig{LLM: config.LLMConfig{Provider: "test", Model: "test"}}, repo.RepoInfo{})

	// Collect all streaming notifications
	var mu sync.Mutex
	var streamMsgs []StreamChunkMsg

	judge.SetNotify(func(msg any) {
		mu.Lock()
		defer mu.Unlock()
		if m, ok := msg.(StreamChunkMsg); ok {
			streamMsgs = append(streamMsgs, m)
		}
	})

	// Create done channel and task
	doneCh := make(chan Result, 1)
	task := &Task{
		Ctx:        ctx,
		EdictKey:   storage.EdictKey{ID: 1, Username: "testuser", Project: "testproject"},
		Work:       "judge the manifests",
		Scratchpad: "# Review",
		Done:       doneCh,
	}

	// Process the task
	judge.processTask(ctx, task)

	// Wait for completion
	result := <-doneCh
	require.NoError(t, result.Err, "processTask should not return an error")

	// Verify all streaming chunks were routed to ChannelID="judge"
	mu.Lock()
	defer mu.Unlock()

	require.NotEmpty(t, streamMsgs, "should have received at least one StreamChunkMsg")

	for i, msg := range streamMsgs {
		assert.Equal(t, "judge", msg.ChannelID, "StreamChunkMsg[%d] should have ChannelID='judge', got ChannelID='%s' (routing bug)", i, msg.ChannelID)
	}
}

// TestSageChannelID_Routing verifies that streaming messages from Sage are routed to ChannelID="sage".
// This test MUST FAIL before the fix (when Sage hardcodes ChannelID="chancellor" in streamTask)
// and MUST PASS after the fix (when Sage uses ChannelID="sage").
func TestSageChannelID_Routing(t *testing.T) {
	db := setupMinisterTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create mock LLM bridge
	mockLLM := mocks.NewLLMProvider()

	// Create Sage with mock LLM client
	base := NewMinisterBase(db, nil, nil, "testuser", "testproject", nil)
	sage := NewSage(base)
	sage.SetMinisterConfig(mockLLM, &SessionConfig{LLM: config.LLMConfig{Provider: "test", Model: "test"}}, repo.RepoInfo{})

	// Collect all streaming notifications
	var mu sync.Mutex
	var streamMsgs []StreamChunkMsg

	sage.SetNotify(func(msg any) {
		mu.Lock()
		defer mu.Unlock()
		if m, ok := msg.(StreamChunkMsg); ok {
			streamMsgs = append(streamMsgs, m)
		}
	})

	// Create done channel and task
	doneCh := make(chan Result, 1)
	task := &Task{
		Ctx:        ctx,
		EdictKey:   storage.EdictKey{ID: 1, Username: "testuser", Project: "testproject"},
		Work:       "review the diff",
		Scratchpad: "# Review",
		Done:       doneCh,
	}

	// Process the task
	sage.processTask(ctx, task)

	// Wait for completion
	result := <-doneCh
	require.NoError(t, result.Err, "processTask should not return an error")

	// Verify all streaming chunks were routed to ChannelID="sage"
	mu.Lock()
	defer mu.Unlock()

	require.NotEmpty(t, streamMsgs, "should have received at least one StreamChunkMsg")

	for i, msg := range streamMsgs {
		assert.Equal(t, "sage", msg.ChannelID, "StreamChunkMsg[%d] should have ChannelID='sage', got ChannelID='%s' (routing bug)", i, msg.ChannelID)
	}
}
