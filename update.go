package main

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"github.com/afittestide/asimi/internal/utils"
	"github.com/rhysd/go-github-selfupdate/selfupdate"
)

// SelfUpdate performs the self-update to the latest version
func SelfUpdate(currentVersion string) error {
	current, err := utils.ParseVersion(currentVersion)
	if err != nil {
		return fmt.Errorf("invalid current version: %w", err)
	}

	slug := utils.GetAsimiSlug()

	latest, err := selfupdate.UpdateSelf(current, slug)
	if err != nil {
		return fmt.Errorf("failed to update: %w", err)
	}

	if latest.Version.Equals(current) {
		slog.Info("already up to date", "version", currentVersion)
		return nil
	}

	slog.Info("successfully updated", "from", currentVersion, "to", latest.Version)
	return nil
}

// AutoCheckForUpdates checks for updates in the background (non-blocking)
// Returns true if an update is available
func AutoCheckForUpdates(currentVersion string) bool {
	// Skip if version is "dev" or empty
	if currentVersion == "" || currentVersion == "dev" {
		return false
	}

	// Check in background with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan bool, 1)
	go func() {
		latest, hasUpdate, err := utils.CheckForUpdates()
		if err != nil {
			slog.Debug("update check failed", "error", err)
			done <- false
			return
		}
		if hasUpdate {
			slog.Info("update available",
				"current", currentVersion,
				"latest", latest.Version,
				"url", latest.URL,
			)
		}
		done <- hasUpdate
	}()

	select {
	case hasUpdate := <-done:
		return hasUpdate
	case <-ctx.Done():
		slog.Debug("update check timed out")
		return false
	}
}

// GetUpdateCommand returns the command string to update asimi
func GetUpdateCommand() string {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		// Check if installed via Homebrew
		// This is a simple heuristic - could be improved
		return "brew upgrade asimi"
	}
	return "asimi update"
}
