package rpc

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/internal/shogunateapi"
	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/storage"
	"github.com/maximhq/bifrost/core/schemas"
)

// fakeShogunate embeds shogunateapi.Client so any method the test
// doesn't stub panics at runtime (reaching the nil embedded interface).
// Only the methods exercised by TestShogunateRPCLoopback are defined.
type fakeShogunate struct {
	shogunateapi.Client

	mu         sync.Mutex
	hasIDs     map[string]bool
	resetLog   []string
	edicts     map[uint]*storage.Edict
	seals      map[uint][]storage.Seal
	sealNotes  map[uint]string
	fallback   bool
	cancels    []string
	clears     []string
	messages   []string // "target/role/content"
	ctxFiles   []string // "target/path/content"
	rollbacks  []string // "target/snapshot"
	zhengAns   []string // "reqID/answer"
	sessions   map[string]shogunate.SessionState
	compacted  []string // "target/prompt"
	shellCmds  []runners.Input
	events     []struct {
		Key       storage.EdictKey
		EventType storage.ShogunateEvent
		Payload   storage.JSON
	}
	prompts   []string // "target/message"
	restored  []string // "tabType/N"
}

func newFakeShogunate() *fakeShogunate {
	return &fakeShogunate{
		hasIDs:    map[string]bool{},
		edicts:    map[uint]*storage.Edict{},
		seals:     map[uint][]storage.Seal{},
		sealNotes: map[uint]string{},
		sessions:  map[string]shogunate.SessionState{},
	}
}

func (f *fakeShogunate) HasMinister(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hasIDs[id]
}

func (f *fakeShogunate) ResetMinisterSession(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resetLog = append(f.resetLog, id)
}

func (f *fakeShogunate) EdictKey(edictID uint) storage.EdictKey {
	return storage.EdictKey{ID: edictID, Username: "alice", Project: "asimi"}
}

func (f *fakeShogunate) CourtEdictKey() storage.EdictKey {
	return storage.EdictKey{ID: 1, Username: "alice", Project: "asimi"}
}

func (f *fakeShogunate) CreateEdict(issueRef, intent string) (*storage.Edict, error) {
	return f.makeEdict(issueRef, intent)
}

func (f *fakeShogunate) CreateEdictSilent(issueRef, intent string) (*storage.Edict, error) {
	return f.makeEdict(issueRef, intent)
}

func (f *fakeShogunate) makeEdict(issueRef, intent string) (*storage.Edict, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := uint(len(f.edicts) + 1)
	e := &storage.Edict{ID: id, Username: "alice", Project: "asimi", IssueRef: issueRef, Intent: intent}
	f.edicts[id] = e
	return e, nil
}

func (f *fakeShogunate) GetEdict(edictID uint) (*storage.Edict, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.edicts[edictID]
	if !ok {
		return nil, errors.New("edict not found")
	}
	return e, nil
}

func (f *fakeShogunate) GrantRulerSeal(edictID uint, notes string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.edicts[edictID]; !ok {
		return errors.New("edict not found")
	}
	f.sealNotes[edictID] = notes
	return nil
}

func (f *fakeShogunate) ListActiveEdicts() ([]storage.ActiveEdict, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]storage.ActiveEdict, 0, len(f.edicts))
	for _, e := range f.edicts {
		out = append(out, storage.ActiveEdict{Edict: *e})
	}
	return out, nil
}

func (f *fakeShogunate) HandleZhengmingResponse(ctx context.Context, requestID, answer string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.zhengAns = append(f.zhengAns, requestID+"/"+answer)
	return nil
}

func (f *fakeShogunate) CancelZhengming(requestID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels = append(f.cancels, requestID)
}

func (f *fakeShogunate) AllowRunnerFallback(allow bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fallback = allow
}

func (f *fakeShogunate) AddSessionMessage(target, role, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, target+"/"+role+"/"+content)
	return nil
}

func (f *fakeShogunate) ClearSessionHistory(target string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clears = append(f.clears, target)
	return nil
}

func (f *fakeShogunate) RollbackSession(target string, snap int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rollbacks = append(f.rollbacks, target)
	return nil
}

func (f *fakeShogunate) AddSessionContextFile(target, path, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ctxFiles = append(f.ctxFiles, target+"/"+path+"/"+content)
	return nil
}

func (f *fakeShogunate) CompactSession(ctx context.Context, target, prompt string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.compacted = append(f.compacted, target+"/"+prompt)
	return "summary for " + target, nil
}

func (f *fakeShogunate) SessionState(target string) shogunate.SessionState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions[target]
}

func (f *fakeShogunate) GetEdictSeals(key storage.EdictKey) ([]storage.Seal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seals[key.ID], nil
}

func (f *fakeShogunate) PublishEvent(key storage.EdictKey, et storage.ShogunateEvent, payload storage.JSON) uint {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, struct {
		Key       storage.EdictKey
		EventType storage.ShogunateEvent
		Payload   storage.JSON
	}{key, et, payload})
	return uint(len(f.events))
}

func (f *fakeShogunate) RunShellCommand(ctx context.Context, in runners.Input) (runners.Output, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shellCmds = append(f.shellCmds, in)
	return runners.Output{Output: "ran: " + in.Command, ExitCode: "0"}, nil
}

func (f *fakeShogunate) SubmitPrompt(target string, p *shogunate.Prompt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts = append(f.prompts, target+"/"+p.Message)
	return nil
}

func (f *fakeShogunate) RestoreMinisterSession(tabType string, msgs []schemas.ChatMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restored = append(f.restored, tabType+"/"+intString(len(msgs)))
	return nil
}

func intString(n int) string {
	switch n {
	case 0:
		return "0"
	case 1:
		return "1"
	case 2:
		return "2"
	case 3:
		return "3"
	default:
		return "many"
	}
}

// TestShogunateRPCLoopback exercises the wire-safe subset of the
// Shogunate RPC surface end-to-end over net.Pipe.
func TestShogunateRPCLoopback(t *testing.T) {
	impl := newFakeShogunate()
	impl.hasIDs["chancellor"] = true
	impl.hasIDs["sage"] = true

	pa, pb := net.Pipe()
	server := New(pa, Options{})
	clientConn := New(pb, Options{})

	RegisterShogunateHandlers(server, impl)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = server.Serve() }()
	go func() { defer wg.Done(); _ = clientConn.Serve() }()
	defer func() {
		_ = clientConn.Close()
		_ = server.Close()
		wg.Wait()
	}()

	client := NewShogunateClient(clientConn)

	if !client.HasMinister("chancellor") {
		t.Error("HasMinister(chancellor) = false")
	}
	if client.HasMinister("unknown") {
		t.Error("HasMinister(unknown) = true")
	}

	client.ResetMinisterSession("sage")
	impl.mu.Lock()
	if got := impl.resetLog; len(got) != 1 || got[0] != "sage" {
		t.Errorf("resetLog = %v", got)
	}
	impl.mu.Unlock()

	if k := client.EdictKey(42); k.ID != 42 || k.Username != "alice" {
		t.Errorf("EdictKey = %+v", k)
	}
	if k := client.CourtEdictKey(); k.ID != 1 {
		t.Errorf("CourtEdictKey = %+v", k)
	}

	e, err := client.CreateEdict("#7", "add feature")
	if err != nil {
		t.Fatalf("CreateEdict: %v", err)
	}
	if e.Intent != "add feature" || e.ID == 0 {
		t.Errorf("edict = %+v", e)
	}

	got, err := client.GetEdict(e.ID)
	if err != nil {
		t.Fatalf("GetEdict: %v", err)
	}
	if got.Intent != "add feature" {
		t.Errorf("got = %+v", got)
	}

	if _, err := client.GetEdict(99); err == nil {
		t.Error("GetEdict(99): want error")
	}

	if err := client.GrantRulerSeal(e.ID, "looks good"); err != nil {
		t.Fatalf("GrantRulerSeal: %v", err)
	}
	impl.mu.Lock()
	if impl.sealNotes[e.ID] != "looks good" {
		t.Errorf("sealNotes[%d] = %q", e.ID, impl.sealNotes[e.ID])
	}
	impl.mu.Unlock()

	active, err := client.ListActiveEdicts()
	if err != nil {
		t.Fatalf("ListActiveEdicts: %v", err)
	}
	if len(active) != 1 || active[0].ID != e.ID {
		t.Errorf("active = %+v", active)
	}

	client.CancelZhengming("req-1")
	impl.mu.Lock()
	if len(impl.cancels) != 1 || impl.cancels[0] != "req-1" {
		t.Errorf("cancels = %v", impl.cancels)
	}
	impl.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.HandleZhengmingResponse(ctx, "req-2", "yes"); err != nil {
		t.Fatalf("HandleZhengmingResponse: %v", err)
	}
	impl.mu.Lock()
	if len(impl.zhengAns) != 1 || impl.zhengAns[0] != "req-2/yes" {
		t.Errorf("zhengAns = %v", impl.zhengAns)
	}
	impl.mu.Unlock()

	client.AllowRunnerFallback(true)
	impl.mu.Lock()
	if !impl.fallback {
		t.Error("AllowRunnerFallback not applied")
	}
	impl.mu.Unlock()

	if err := client.ClearSessionHistory("ruling"); err != nil {
		t.Fatalf("ClearSessionHistory: %v", err)
	}
	if err := client.RollbackSession("ruling", 5); err != nil {
		t.Fatalf("RollbackSession: %v", err)
	}
	if err := client.AddSessionMessage("ruling", "human", "hi"); err != nil {
		t.Fatalf("AddSessionMessage: %v", err)
	}
	impl.mu.Lock()
	if len(impl.clears) != 1 || impl.clears[0] != "ruling" {
		t.Errorf("clears = %v", impl.clears)
	}
	if len(impl.rollbacks) != 1 {
		t.Errorf("rollbacks = %v", impl.rollbacks)
	}
	if len(impl.messages) != 1 || impl.messages[0] != "ruling/human/hi" {
		t.Errorf("messages = %v", impl.messages)
	}
	impl.mu.Unlock()

	// AddSessionContextFile, CompactSession, SessionState, GetEdictSeals,
	// PublishEvent, RunShellCommand, SubmitPrompt, RestoreMinisterSession.

	if err := client.AddSessionContextFile("ruling", "a.go", "package x"); err != nil {
		t.Fatalf("AddSessionContextFile: %v", err)
	}
	impl.mu.Lock()
	if len(impl.ctxFiles) != 1 || impl.ctxFiles[0] != "ruling/a.go/package x" {
		t.Errorf("ctxFiles = %v", impl.ctxFiles)
	}
	impl.mu.Unlock()

	summary, err := client.CompactSession(ctx, "ruling", "summarise")
	if err != nil {
		t.Fatalf("CompactSession: %v", err)
	}
	if summary != "summary for ruling" {
		t.Errorf("summary = %q", summary)
	}

	impl.mu.Lock()
	impl.sessions["ruling"] = shogunate.SessionState{
		Exists:              true,
		ChannelID:           "ruling",
		MessageCount:        3,
		MessageSnapshot:     2,
		ContextUsagePercent: 0.42,
		ContextInfo:         shogunate.ContextInfo{Model: "claude", TotalTokens: 200000, UsedTokens: 42000},
		ContextFiles:        map[string]string{"a.go": "package x"},
	}
	impl.mu.Unlock()
	state := client.SessionState("ruling")
	if !state.Exists || state.ChannelID != "ruling" || state.MessageCount != 3 ||
		state.ContextInfo.Model != "claude" || state.ContextInfo.UsedTokens != 42000 ||
		state.ContextFiles["a.go"] != "package x" {
		t.Errorf("SessionState round-trip: %+v", state)
	}

	// PublishEvent + GetEdictSeals.
	payload := storage.JSON{"note": "shipping"}
	id := client.PublishEvent(storage.EdictKey{ID: 9, Username: "alice", Project: "asimi"}, storage.EventEdictCreated, payload)
	if id == 0 {
		t.Error("PublishEvent: zero ID")
	}
	impl.mu.Lock()
	if len(impl.events) != 1 || impl.events[0].EventType != storage.EventEdictCreated || impl.events[0].Payload["note"] != "shipping" {
		t.Errorf("events = %+v", impl.events)
	}
	impl.seals[9] = []storage.Seal{{SealID: "s1", EdictID: 9, MinisterID: "judge"}}
	impl.mu.Unlock()
	seals, err := client.GetEdictSeals(storage.EdictKey{ID: 9})
	if err != nil {
		t.Fatalf("GetEdictSeals: %v", err)
	}
	if len(seals) != 1 || seals[0].MinisterID != "judge" {
		t.Errorf("seals = %+v", seals)
	}

	// RunShellCommand.
	out, err := client.RunShellCommand(ctx, runners.Input{Command: "echo hi", Description: "test"})
	if err != nil {
		t.Fatalf("RunShellCommand: %v", err)
	}
	if out.Output != "ran: echo hi" || out.ExitCode != "0" {
		t.Errorf("RunShellCommand out = %+v", out)
	}

	// SubmitPrompt (ctx should be rebuilt server-side).
	if err := client.SubmitPrompt("chancellor", &shogunate.Prompt{
		Message:      "hello shogun",
		EdictKey:     storage.EdictKey{ID: 42},
		ChannelID:    "ruling",
		ContextFiles: map[string]string{"x.md": "hi"},
	}); err != nil {
		t.Fatalf("SubmitPrompt: %v", err)
	}
	impl.mu.Lock()
	if len(impl.prompts) != 1 || impl.prompts[0] != "chancellor/hello shogun" {
		t.Errorf("prompts = %v", impl.prompts)
	}
	impl.mu.Unlock()

	// RestoreMinisterSession.
	msgs := []schemas.ChatMessage{{Role: schemas.ChatMessageRoleUser}, {Role: schemas.ChatMessageRoleAssistant}}
	if err := client.RestoreMinisterSession("ruling", msgs); err != nil {
		t.Fatalf("RestoreMinisterSession: %v", err)
	}
	impl.mu.Lock()
	if len(impl.restored) != 1 || impl.restored[0] != "ruling/2" {
		t.Errorf("restored = %v", impl.restored)
	}
	impl.mu.Unlock()
}
