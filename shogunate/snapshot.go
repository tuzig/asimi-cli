package shogunate

import (
	"time"

	"github.com/afittestide/asimi/storage"
)

// LiveRitual represents a currently running or pending ritual execution.
type LiveRitual struct {
	RitualName  string
	EdictID     string
	State       RitualState
	CurrentStep int
	TotalSteps  int
	StepName    string
	Age         time.Duration
}

// EventEntry represents a single event from the tian_events ledger.
type EventEntry struct {
	Time      time.Time
	EventType string
	Detail    string
	EdictID   string
}

// Snapshot captures the current state of the shogunate for dashboard display.
type Snapshot struct {
	LiveRituals []LiveRitual
	Events      []EventEntry
	TakenAt     time.Time
}

// TakeSnapshot returns a point-in-time snapshot of live rituals and recent events.
func (s *Shogunate) TakeSnapshot() Snapshot {
	snap := Snapshot{TakenAt: time.Now()}

	// Live rituals from the ritual runner
	if rr := s.GetRitualRunner(); rr != nil {
		execs, err := rr.ListExecutions("")
		if err == nil {
			for _, ex := range execs {
				if ex.State != RitualStatePending && ex.State != RitualStateRunning {
					continue
				}
				lr := LiveRitual{
					RitualName:  ex.RitualName,
					EdictID:     ex.EdictID,
					State:       ex.State,
					CurrentStep: ex.CurrentStep,
					Age:         time.Since(ex.CreatedAt),
				}
				// Get step info from the ritual definition
				if def := rr.registry.Get(ex.RitualName); def != nil {
					lr.TotalSteps = len(def.Steps)
					if ex.CurrentStep >= 0 && ex.CurrentStep < len(def.Steps) {
						lr.StepName = def.Steps[ex.CurrentStep].Minister
					}
				}
				// Try to get step name from step states in DB
				var stepState RitualStepState
				if err := s.db.Where("execution_id = ? AND step_index = ?", ex.ID, ex.CurrentStep).
					First(&stepState).Error; err == nil && stepState.Name != "" {
					lr.StepName = stepState.Name
				}
				snap.LiveRituals = append(snap.LiveRituals, lr)
			}
		}
	}

	// Recent events from tian_events
	var events []storage.TianEvent
	if err := s.db.Order("created_at DESC").Limit(50).Find(&events).Error; err == nil {
		for _, ev := range events {
			snap.Events = append(snap.Events, EventEntry{
				Time:      ev.CreatedAt,
				EventType: string(ev.EventType),
				Detail:    extractDetail(ev),
				EdictID:   ev.EdictID,
			})
		}
	}

	return snap
}

func extractDetail(ev storage.TianEvent) string {
	if ev.Payload == nil {
		return ""
	}
	payload := map[string]interface{}(ev.Payload)
	// Try common payload fields for a human-readable detail
	if name, ok := payload["ritual_name"].(string); ok {
		return name
	}
	if name, ok := payload["minister"].(string); ok {
		return name
	}
	if name, ok := payload["step_name"].(string); ok {
		return name
	}
	return ""
}
