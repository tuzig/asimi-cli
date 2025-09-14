package main

import (
	"context"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/tmc/langchaingo/tools"
)

// ToolCallStatus represents the status of a tool call
type ToolCallStatus string

const (
	StatusValidating         ToolCallStatus = "validating"
	StatusScheduled          ToolCallStatus = "scheduled"
	StatusExecuting          ToolCallStatus = "executing"
	StatusWaitingForApproval ToolCallStatus = "awaiting_approval"
	StatusSuccess            ToolCallStatus = "success"
	StatusError              ToolCallStatus = "error"
	StatusCancelled          ToolCallStatus = "cancelled"
)

// ToolCall represents a single tool call task
type ToolCall struct {
	ID     string
	Tool   tools.Tool
	Input  string
	Status ToolCallStatus
	Result string
	Error  error
}

// ToolCallResult is used to send the result of a tool call back to the caller
type ToolCallResult struct {
	Output string
	Error  error
}

// CoreToolScheduler manages a queue of tool calls and orchestrates their execution
type CoreToolScheduler struct {
	mu          sync.Mutex
	toolCalls   map[string]*ToolCall
	queue       []*ToolCall
	isBusy      bool
	resultChans map[string]chan ToolCallResult
	notify      func(any)
}

// NewCoreToolScheduler creates a new CoreToolScheduler
func NewCoreToolScheduler(toolNotify func(any)) *CoreToolScheduler {
	return &CoreToolScheduler{
		toolCalls:   make(map[string]*ToolCall),
		queue:       make([]*ToolCall, 0),
		resultChans: make(map[string]chan ToolCallResult),
		notify:      toolNotify,
	}
}

// Schedule adds a new tool call to the scheduler and returns a channel for the result
func (s *CoreToolScheduler) Schedule(tool tools.Tool, input string) <-chan ToolCallResult {
	slog.Info("scheduler.enqueue", "tool", tool.Name())
	s.mu.Lock()
	defer s.mu.Unlock()

	id := uuid.New().String()
	call := &ToolCall{
		ID:     id,
		Tool:   tool,
		Input:  input,
		Status: StatusScheduled,
	}
	s.toolCalls[id] = call
	s.queue = append(s.queue, call)

	resultChan := make(chan ToolCallResult, 1)
	s.resultChans[id] = resultChan

	if s.notify != nil {
		s.notify(ToolCallScheduledMsg{Call: call})
	}
	s.processQueue()

	return resultChan
}

func (s *CoreToolScheduler) processQueue() {
	if s.isBusy || len(s.queue) == 0 {
		slog.Info("scheduler.idle_or_empty", "busy", s.isBusy, "queued", len(s.queue))
		return
	}
	s.isBusy = true

	call := s.queue[0]
	s.queue = s.queue[1:]

	call.Status = StatusExecuting
	if s.notify != nil {
		s.notify(ToolCallExecutingMsg{Call: call})
	}

	go func() {
		// NOTE: We are calling the tool's Call method directly here.
		// The toolWrapper's Call method is what schedules the tool.
		// This means the tool passed to Schedule should be the unwrapped tool.
		slog.Info("scheduler.exec", "tool", call.Tool.Name())
		output, err := call.Tool.Call(context.Background(), call.Input)

		s.mu.Lock()
		defer s.mu.Unlock()

		resultChan := s.resultChans[call.ID]

		if err != nil {
			call.Status = StatusError
			call.Error = err
			if s.notify != nil {
				s.notify(ToolCallErrorMsg{Call: call})
			}
			if resultChan != nil {
				resultChan <- ToolCallResult{Error: err}
			}
		} else {
			call.Status = StatusSuccess
			call.Result = output
			if s.notify != nil {
				s.notify(ToolCallSuccessMsg{Call: call})
			}
			if resultChan != nil {
				resultChan <- ToolCallResult{Output: output}
			}
		}
		if resultChan != nil {
			close(resultChan)
			delete(s.resultChans, call.ID)
		}

		s.isBusy = false
		s.processQueue()
	}()
}

// Messages for bubbletea
type ToolCallScheduledMsg struct{ Call *ToolCall }
type ToolCallExecutingMsg struct{ Call *ToolCall }
type ToolCallWaitingForApprovalMsg struct{ Call *ToolCall }
type ToolCallSuccessMsg struct{ Call *ToolCall }
type ToolCallErrorMsg struct{ Call *ToolCall }
