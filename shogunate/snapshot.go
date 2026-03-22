package shogunate

import (
	"sort"
	"time"

	"github.com/afittestide/asimi/storage"
)

// RitualEntry represents a ritual execution (active or historical) for dashboard display.
type RitualEntry struct {
	RitualName  string
	EdictID     uint
	State       RitualState
	CurrentStep int
	TotalSteps  int
	StepName    string
	Age         time.Duration
	StartedAt   time.Time
}

// EventEntry represents a single event from the tian_events ledger.
type EventEntry struct {
	Time      time.Time
	EventType string
	Detail    string
	EdictID   uint
}

// ZhengmingEntry represents a pending clarification request for dashboard display.
type ZhengmingEntry struct {
	RequestID  string
	EdictID    uint
	MinisterID string
	Questions  []string // Summary or truncated Text for each question
	Priority   string
	Status     string
	CreatedAt  time.Time
	TimeoutAt  time.Time
}

// Snapshot captures the current state of the shogunate for dashboard display.
type Snapshot struct {
	Rituals   []RitualEntry
	Events    []EventEntry
	Zhengming []ZhengmingEntry
	TakenAt   time.Time
}

// TakeSnapshot returns a point-in-time snapshot of live rituals and recent events.
func (s *Shogunate) TakeSnapshot() Snapshot {
	snap := Snapshot{TakenAt: time.Now()}

	// Rituals from the ritual runner (all states)
	if rr := s.GetRitualRunner(); rr != nil {
		execs, err := rr.ListExecutions(0)
		if err == nil {
			for _, ex := range execs {
				age := time.Since(ex.CreatedAt)
				if ex.State != RitualStatePending && ex.State != RitualStateRunning {
					// For completed rituals, age = duration from creation to last update
					age = ex.UpdatedAt.Sub(ex.CreatedAt)
				}
				entry := RitualEntry{
					RitualName:  ex.RitualName,
					EdictID:     ex.EdictID,
					State:       ex.State,
					CurrentStep: ex.CurrentStep,
					Age:         age,
					StartedAt:   ex.CreatedAt,
				}
				// Get step info from the ritual definition
				if def := rr.registry.Get(ex.RitualName); def != nil {
					entry.TotalSteps = len(def.Steps)
					if ex.CurrentStep >= 0 && ex.CurrentStep < len(def.Steps) {
						entry.StepName = def.Steps[ex.CurrentStep].Name
					}
				}
				// Try to get step name from step states in DB
				/* TODO: why do we need this?
				var stepState RitualStepState
				if err := s.db.Where("execution_id = ? AND step_index = ?", ex.ID, ex.CurrentStep).
					First(&stepState).Error; err == nil && stepState.Name != "" {
					entry.StepName = stepState.Name
				}
				*/
				snap.Rituals = append(snap.Rituals, entry)
			}
			// Sort: active rituals first, then by age descending (most recent first)
			sort.Slice(snap.Rituals, func(i, j int) bool {
				iActive := snap.Rituals[i].State == RitualStatePending || snap.Rituals[i].State == RitualStateRunning
				jActive := snap.Rituals[j].State == RitualStatePending || snap.Rituals[j].State == RitualStateRunning
				if iActive != jActive {
					return iActive
				}
				return snap.Rituals[i].StartedAt.After(snap.Rituals[j].StartedAt)
			})
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

	// Pending zhengming (clarification requests)
	var pending []storage.Zhengming
	if err := s.db.Where("status = ?", storage.ZhengmingPending).
		Order("priority DESC, created_at ASC").Limit(20).Find(&pending).Error; err == nil {
		for _, z := range pending {
			var questions []string
			for _, q := range z.Questions {
				text := q.Summary
				if text == "" {
					text = q.Text
					if len(text) > 60 {
						text = text[:57] + "..."
					}
				}
				questions = append(questions, text)
			}
			snap.Zhengming = append(snap.Zhengming, ZhengmingEntry{
				RequestID:  z.RequestID,
				EdictID:    z.EdictID,
				MinisterID: z.MinisterID,
				Questions:  questions,
				Priority:   string(z.Priority),
				Status:     string(z.Status),
				CreatedAt:  z.CreatedAt,
				TimeoutAt:  z.TimeoutAt,
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
