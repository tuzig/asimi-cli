package shogunate

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/afittestide/asimi/storage"
)

// findFirstIncompleteStep returns the 0-based index of the first incomplete step,
// or -1 if all steps are complete. A step is considered incomplete if:
//   - its message is empty (never executed)
//   - it has a retry count > 0 (failed and retried)
//   - its message contains error patterns: "context canceled", "timeout", "aborted"
//   - the step index is beyond the recorded stepStates (never reached)
func findFirstIncompleteStep(stepStates []RitualStepState, totalSteps int) int {
	for i := 0; i < totalSteps; i++ {
		if i >= len(stepStates) {
			// Step was never reached
			return i
		}
		ss := stepStates[i]
		if ss.Message == "" || ss.RetryCount > 0 {
			return i
		}
		if strings.Contains(ss.Message, "context canceled") ||
			strings.Contains(ss.Message, "timeout") ||
			strings.Contains(ss.Message, "aborted") {
			return i
		}
	}
	return -1
}

// recoveryResult captures the outcome of recovery detection and confirmation.
type recoveryResult struct {
	previousExecutionID string
	fromStep            int
}

// recoverFromPreviousExec checks for an aborted/paused ritual execution for the
// given edict, confirms recovery via zhengming if needed, and applies recovery
// state to the provided exec. Returns a recoveryResult when recovery was
// applied, or an error if the user passed or zhengming failed.
func (r *RitualRunner) recoverFromPreviousExec(ctx context.Context, ritualName string, key storage.EdictKey, def *RitualDef, exec *RitualExecution) (recoveryResult, error) {
	// Look up previous aborted/paused execution for this ritual
	var previousExec *RitualExecution
	var recoveryData storage.JSON
	var recoveryFirstIncompleteStep = -1
	var skipZhengmingPrompt bool
	if err := r.db.Where("edict_id = ? AND username = ? AND project = ? AND ritual_name = ? AND state IN (?, ?, ?, ?)", key.ID, key.Username, key.Project, ritualName, RitualStateAborted, RitualStateStopped, RitualStateFailed, RitualStateRecovering).
		Order("updated_at DESC").
		First(&previousExec).Error; err == nil {
		// Found aborted execution - attempt recovery
		r.logger.Info("found aborted ritual execution for recovery",
			"ritual", ritualName,
			"edict_id", key.ID,
			"previous_execution_id", previousExec.ID,
			"state", previousExec.State)

		// If state is "recovering", user already approved - skip zhengming prompt
		skipZhengmingPrompt = previousExec.State == RitualStateRecovering

		// Load step states to determine which steps completed
		var stepStates []RitualStepState
		if err := r.db.Where("execution_id = ?", previousExec.ID).Order("step_index").Find(&stepStates).Error; err == nil {
			// Find first incomplete step (message is empty, has error, or step was not reached)
			firstIncompleteStep := findFirstIncompleteStep(stepStates, len(def.Steps))

			// Only recover if there are incomplete steps
			if firstIncompleteStep > 0 && firstIncompleteStep < len(def.Steps) {
				r.logger.Info("resuming ritual from incomplete step",
					"ritual", ritualName,
					"from_step", firstIncompleteStep,
					"previous_execution_id", previousExec.ID)
				recoveryData = previousExec.Data
				recoveryFirstIncompleteStep = firstIncompleteStep
			} else if firstIncompleteStep == 0 {
				// First step never completed - start fresh
				r.logger.Info("first step incomplete, starting fresh",
					"ritual", ritualName,
					"previous_execution_id", previousExec.ID)
			} else {
				// All steps completed (-1) - start fresh
				r.logger.Info("all steps completed in previous execution, starting fresh",
					"ritual", ritualName,
					"previous_execution_id", previousExec.ID)
			}
		}
	}

	// No recovery needed
	if recoveryFirstIncompleteStep <= 0 {
		return recoveryResult{}, nil
	}

	// Confirm recovery via zhengming (unless skipZhengmingPrompt is set or no zhengming available)
	if !skipZhengmingPrompt && r.getMinister != nil {
		minister := r.getMinister("chancellor")
		if minister != nil {
			type zhengmingRequester interface {
				RequestZhengming(storage.EdictKey, storage.ZhengmingQuestions, storage.ZhengmingPriority) (string, error)
				WaitForZhengming(context.Context, string) (string, error)
			}
			requester, ok := minister.(zhengmingRequester)
			if ok {
				questions := storage.ZhengmingQuestions{
					{
						Text: fmt.Sprintf("Ritual %q was aborted at step %d. Resume or start fresh?", ritualName, recoveryFirstIncompleteStep),
						Options: []string{
							fmt.Sprintf("Recover from step %d", recoveryFirstIncompleteStep),
							"Start fresh",
						},
					},
				}

				requestID, err := requester.RequestZhengming(key, questions, storage.PriorityUrgent)
				if err != nil {
					r.logger.Warn("failed to request zhengming for recovery", "error", err)
					return recoveryResult{}, nil
				}

				answer, err := requester.WaitForZhengming(ctx, requestID)
				if err != nil {
					r.logger.Warn("failed waiting for zhengming answer", "error", err)
					return recoveryResult{}, nil
				}

				if answer != questions[0].Options[0] {
					r.logger.Info("user chose not to recover", "answer", answer)
					return recoveryResult{}, nil
				}
			}
		}
	}
	// Apply recovery state
	exec.RecoveryMode = true
	exec.PreviousExecutionID = previousExec.ID
	exec.CurrentStep = recoveryFirstIncompleteStep
	exec.Data = recoveryData
	r.logger.Info("applied recovery state",
		"ritual", ritualName,
		"from_step", recoveryFirstIncompleteStep,
		"previous_execution_id", previousExec.ID)

	// Mark the previous "recovering" execution as completed to prevent zombie state
	if previousExec.State == RitualStateRecovering {
		previousExec.State = RitualStateCompleted
		r.db.Save(previousExec)
		r.logger.Info("marked previous recovering execution as completed",
			"previous_execution_id", previousExec.ID)
	}

	return recoveryResult{
		previousExecutionID: previousExec.ID,
		fromStep:            recoveryFirstIncompleteStep,
	}, nil
}

// promptForAbortedRituals scans for aborted/stopped/failed ritual executions
// and prompts the user (via zhengming) to recover, mark as completed, or pass.
// It runs at startup and sets recoveryComplete when done.
func (rg *RitualGuard) promptForAbortedRituals(ctx context.Context) {
	defer func() {
		rg.recoveryMu.Lock()
		rg.recoveryComplete = true
		rg.recoveryMu.Unlock()
	}()

	if rg.db == nil {
		rg.logger.Debug("no db available, skipping aborted ritual recovery")
		return
	}

	if rg.getMinister == nil {
		rg.logger.Debug("no getMinister function, skipping aborted ritual recovery")
		return
	}

	// Query aborted/stopped/failed rituals, excluding edict_id=0
	var abortedExecs []RitualExecution
	err := rg.db.Where("edict_id != 0 AND state IN (?, ?, ?, ?)",
		RitualStateAborted, RitualStateStopped, RitualStateFailed, RitualStateRecovering).
		Order("updated_at DESC").
		Limit(5).
		Find(&abortedExecs).Error
	if err != nil {
		rg.logger.Warn("failed to query aborted rituals", "error", err)
		return
	}

	if len(abortedExecs) == 0 {
		rg.logger.Debug("no aborted rituals found")
		return
	}

	// First pass: auto-complete aborted rituals for sealed/cancelled edicts
	for i := range abortedExecs {
		exec := &abortedExecs[i]
		var edict storage.Edict
		if err := rg.db.First(&edict, "id = ? AND username = ? AND project = ?", exec.EdictID, exec.Username, exec.Project).Error; err != nil {
			rg.logger.Warn("failed to find edict for aborted ritual", "edict_id", exec.EdictID, "error", err)
			continue
		}
		var sealCount int64
		rg.db.Model(&storage.Seal{}).Where("edict_id = ? AND username = ? AND project = ?", exec.EdictID, exec.Username, exec.Project).Count(&sealCount)
		if sealCount > 0 || edict.CancelledAt != nil {
			exec.State = RitualStateCompleted
			rg.db.Save(exec)
			rg.logger.Info("auto-completed aborted ritual for sealed/cancelled edict",
				"execution_id", exec.ID,
				"edict_id", exec.EdictID)
		}
	}

	// Get chancellor for zhengming
	chancellor := rg.getMinister("chancellor")
	if chancellor == nil {
		rg.logger.Warn("chancellor not available for recovery prompts")
		return
	}

	type zhengmingRequester interface {
		RequestZhengming(storage.EdictKey, storage.ZhengmingQuestions, storage.ZhengmingPriority) (string, error)
		WaitForZhengming(context.Context, string) (string, error)
	}
	requester, ok := chancellor.(zhengmingRequester)
	if !ok {
		rg.logger.Warn("chancellor does not support zhengming")
		return
	}

	for _, exec := range abortedExecs {
		// Check context before each iteration
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Skip already completed rituals (from first pass)
		if exec.State == RitualStateCompleted {
			continue
		}

		// Check if the edict is sealed or cancelled
		var edict storage.Edict
		if err := rg.db.First(&edict, "id = ? AND username = ? AND project = ?", exec.EdictID, exec.Username, exec.Project).Error; err != nil {
			rg.logger.Warn("failed to find edict for aborted ritual", "edict_id", exec.EdictID, "error", err)
			continue
		}

		// Determine first incomplete step
		var stepStates []RitualStepState
		rg.db.Where("execution_id = ?", exec.ID).Order("step_index").Find(&stepStates)
		incompleteStep := findFirstIncompleteStep(stepStates, exec.CurrentStep+1)

		// Build description text, preferring summary over intent
		description := edict.Summary
		if description == "" {
			description = edict.Intent
		}
		// Truncate long descriptions
		if len(description) > 60 {
			description = description[:57] + "..."
		}

		// Build zhengming question
		var options []string
		if incompleteStep >= 0 {
			options = []string{
				fmt.Sprintf("Recover from step %d", incompleteStep),
				"Mark as completed",
				"Pass",
			}
		} else {
			options = []string{
				"Mark as completed",
				"Pass",
			}
		}

		questions := storage.ZhengmingQuestions{
			{
				Text:    fmt.Sprintf("Ritual %q (e%d: %s) was aborted at step %d. What should we do?", exec.RitualName, exec.EdictID, description, incompleteStep),
				Options: options,
			},
		}

		key := storage.EdictKey{ID: exec.EdictID, Username: exec.Username, Project: exec.Project}
		requestID, err := requester.RequestZhengming(key, questions, storage.PriorityUrgent)
		if err != nil {
			rg.logger.Warn("failed to request zhengming for aborted ritual", "execution_id", exec.ID, "error", err)
			continue
		}

		answer, err := requester.WaitForZhengming(ctx, requestID)
		if err != nil {
			rg.logger.Warn("failed waiting for zhengming answer", "execution_id", exec.ID, "error", err)
			continue
		}

		switch {
		case strings.HasPrefix(answer, "Recover from step "):
			stepStr := strings.TrimPrefix(answer, "Recover from step ")
			stepIdx, err := strconv.Atoi(stepStr)
			if err != nil {
				rg.logger.Warn("invalid recovery step in answer", "answer", answer, "error", err)
				continue
			}
			exec.State = RitualStateRecovering
			exec.CurrentStep = stepIdx
			rg.db.Save(&exec)
			rg.logger.Info("set ritual to recovering",
				"execution_id", exec.ID,
				"from_step", stepIdx)

		case answer == "Mark as completed":
			exec.State = RitualStateCompleted
			rg.db.Save(&exec)
			rg.logger.Info("marked aborted ritual as completed",
				"execution_id", exec.ID)

		case answer == "Pass":
			// Leave in current state
			rg.logger.Info("user passed on aborted ritual",
				"execution_id", exec.ID)

		default:
			rg.logger.Warn("unknown zhengming answer for aborted ritual", "answer", answer)
		}
	}
}
