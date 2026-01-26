package e2e

import (
	"fmt"
	"strings"
	"sync"
)

// ProgressRecord represents a captured progress message
type ProgressRecord struct {
	StepIndex int
	StepName  string
	Status    string
	Message   string
}

// ProgressCapture captures workflow progress messages for assertions
type ProgressCapture struct {
	mu       sync.Mutex
	messages []ProgressRecord
}

// NewProgressCapture creates a new ProgressCapture
func NewProgressCapture() *ProgressCapture {
	return &ProgressCapture{
		messages: make([]ProgressRecord, 0),
	}
}

// Capture returns a function that can be used as the onProgress callback
// Compatible with: func(stepIndex int, stepState StepState, message string)
// Since we can't import the main package, we accept an interface
func (p *ProgressCapture) Capture(stepIndex int, stepName, status, message string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.messages = append(p.messages, ProgressRecord{
		StepIndex: stepIndex,
		StepName:  stepName,
		Status:    status,
		Message:   message,
	})
}

// GetMessages returns all captured progress messages
func (p *ProgressCapture) GetMessages() []ProgressRecord {
	p.mu.Lock()
	defer p.mu.Unlock()

	messages := make([]ProgressRecord, len(p.messages))
	copy(messages, p.messages)
	return messages
}

// Count returns the number of captured messages
func (p *ProgressCapture) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.messages)
}

// Reset clears all captured messages
func (p *ProgressCapture) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messages = nil
}

// HasMessage checks if any message contains the given substring
func (p *ProgressCapture) HasMessage(substring string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, record := range p.messages {
		if strings.Contains(record.Message, substring) {
			return true
		}
	}
	return false
}

// HasStepWithStatus checks if a step with the given name reached the given status
func (p *ProgressCapture) HasStepWithStatus(stepName, status string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, record := range p.messages {
		if record.StepName == stepName && record.Status == status {
			return true
		}
	}
	return false
}

// GetMessagesForStep returns all messages for a specific step
func (p *ProgressCapture) GetMessagesForStep(stepName string) []ProgressRecord {
	p.mu.Lock()
	defer p.mu.Unlock()

	var messages []ProgressRecord
	for _, record := range p.messages {
		if record.StepName == stepName {
			messages = append(messages, record)
		}
	}
	return messages
}

// AssertStepCompleted checks that a step was completed
func (p *ProgressCapture) AssertStepCompleted(stepName string) error {
	if p.HasStepWithStatus(stepName, "completed") {
		return nil
	}
	return fmt.Errorf("step %q was not completed", stepName)
}

// AssertStepFailed checks that a step failed
func (p *ProgressCapture) AssertStepFailed(stepName string) error {
	if p.HasStepWithStatus(stepName, "failed") {
		return nil
	}
	return fmt.Errorf("step %q did not fail", stepName)
}

// GetStepOrder returns the order in which steps were executed
func (p *ProgressCapture) GetStepOrder() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	seen := make(map[string]bool)
	var order []string

	for _, record := range p.messages {
		if record.StepName != "" && !seen[record.StepName] {
			seen[record.StepName] = true
			order = append(order, record.StepName)
		}
	}

	return order
}

// String returns a human-readable representation of all captured messages
func (p *ProgressCapture) String() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	var sb strings.Builder
	for i, record := range p.messages {
		sb.WriteString(fmt.Sprintf("[%d] Step %d (%s) [%s]: %s\n",
			i, record.StepIndex, record.StepName, record.Status, record.Message))
	}
	return sb.String()
}
