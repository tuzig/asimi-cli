package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/afittestide/asimi/internal/utils"
)

// SelfUpdate performs the self-update to the latest version
func SelfUpdate(currentVersion string) error {
	latest, hasUpdate, err := utils.CheckForUpdates()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	if !hasUpdate {
		slog.Info("already up to date", "version", currentVersion)
		return nil
	}

	if latest.AssetURL == "" {
		return fmt.Errorf("no matching asset found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// Download the tar.gz
	resp, err := http.Get(latest.AssetURL)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Extract the asimi binary from the tarball
	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to open gzip: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	var binaryContent []byte
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tarball: %w", err)
		}
		base := filepath.Base(hdr.Name)
		if base == "asimi" && hdr.Typeflag == tar.TypeReg {
			binaryContent, err = io.ReadAll(tr)
			if err != nil {
				return fmt.Errorf("failed to read binary from tarball: %w", err)
			}
			break
		}
	}

	if binaryContent == nil {
		return fmt.Errorf("asimi binary not found in tarball")
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Write to a temp file in the same directory, then rename
	dir := filepath.Dir(execPath)
	tmpFile, err := os.CreateTemp(dir, ".asimi-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(binaryContent); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write binary: %w", err)
	}
	tmpFile.Close()

	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to chmod binary: %w", err)
	}

	if err := os.Rename(tmpPath, execPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to replace binary: %w", err)
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
		return "brew upgrade asimi"
	}
	return "asimi update"
}
