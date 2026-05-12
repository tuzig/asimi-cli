package rpc

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/afittestide/asimi/shogunate/tools"
)

func TestEditorRequestRoundTrip(t *testing.T) {
	pa, pb := net.Pipe()
	daemon := New(pa, Options{})
	tui := New(pb, Options{})

	fake := &fakeProgram{ch: make(chan any, 1)}
	RegisterEditorHandler(tui, fake)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = daemon.Serve() }()
	go func() { defer wg.Done(); _ = tui.Serve() }()
	defer func() {
		_ = daemon.Close()
		_ = tui.Close()
		wg.Wait()
	}()

	// Simulate the TUI: pretend the user edited the content.
	go func() {
		msg := <-fake.ch
		req, ok := msg.(tools.EditorRequest)
		if !ok {
			t.Errorf("unexpected msg type: %T", msg)
			return
		}
		req.ResultChan <- tools.EditorResult{Content: req.Content + " (edited)", Saved: true}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := RequestEditor(ctx, daemon, "draft edict body", "edict.md")
	if err != nil {
		t.Fatalf("RequestEditor: %v", err)
	}
	if !res.Saved {
		t.Fatalf("Saved = false; wanted true")
	}
	if res.Content != "draft edict body (edited)" {
		t.Fatalf("unexpected content roundtrip: %q", res.Content)
	}
}

func TestEditorRequestQuitWithoutSaving(t *testing.T) {
	pa, pb := net.Pipe()
	daemon := New(pa, Options{})
	tui := New(pb, Options{})

	fake := &fakeProgram{ch: make(chan any, 1)}
	RegisterEditorHandler(tui, fake)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = daemon.Serve() }()
	go func() { defer wg.Done(); _ = tui.Serve() }()
	defer func() {
		_ = daemon.Close()
		_ = tui.Close()
		wg.Wait()
	}()

	go func() {
		msg := <-fake.ch
		req := msg.(tools.EditorRequest)
		req.ResultChan <- tools.EditorResult{Saved: false}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := RequestEditor(ctx, daemon, "draft", "")
	if err != nil {
		t.Fatalf("RequestEditor: %v", err)
	}
	if res.Saved {
		t.Fatalf("Saved = true; wanted false (vi :q!)")
	}
}

func TestPumpShogunateEventsInterceptsEditor(t *testing.T) {
	pa, pb := net.Pipe()
	daemon := New(pa, Options{})
	tui := New(pb, Options{})

	fake := &fakeProgram{ch: make(chan any, 1)}
	RegisterEditorHandler(tui, fake)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = daemon.Serve() }()
	go func() { defer wg.Done(); _ = tui.Serve() }()
	defer func() {
		_ = daemon.Close()
		_ = tui.Close()
		wg.Wait()
	}()

	events := make(chan any, 4)
	pumpCtx, cancelPump := context.WithCancel(context.Background())
	go PumpShogunateEvents(pumpCtx, daemon, events)
	defer cancelPump()

	go func() {
		msg := <-fake.ch
		req := msg.(tools.EditorRequest)
		req.ResultChan <- tools.EditorResult{Content: "modified body", Saved: true}
	}()

	respCh := make(chan tools.EditorResult, 1)
	events <- tools.EditorRequest{Content: "original body", Filename: "x.md", ResultChan: respCh}

	select {
	case got := <-respCh:
		if !got.Saved || got.Content != "modified body" {
			t.Fatalf("unexpected result: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("response never delivered")
	}
}
