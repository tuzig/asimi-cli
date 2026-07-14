package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/afittestide/asimi/internal/utils"
)

// extractBinaryFromTarball reads a gzip-compressed tarball from r and returns
// the contents of the first regular file named "asimi".
func extractBinaryFromTarball(r io.Reader) ([]byte, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to open gzip: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tarball: %w", err)
		}
		base := filepath.Base(hdr.Name)
		if base == "asimi" && hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("asimi binary not found in tarball")
}

// fetchChecksum downloads checksums.txt from the release and returns the
// expected SHA256 for the given asset name. Returns empty string if
// checksums.txt is not available (graceful degradation for older releases).
func fetchChecksum(ctx context.Context, checksumURL, assetName string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create checksum request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download checksums: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil // checksums.txt not available — skip verification
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read checksums: %w", err)
	}

	for _, line := range strings.Split(string(body), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == assetName {
			return parts[0], nil
		}
	}
	return "", nil
}

// SelfUpdate performs the self-update to the latest version
func SelfUpdate(currentVersion string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	latest, hasUpdate, err := utils.CheckForUpdates(ctx)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latest.AssetURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Extract the asimi binary from the tarball
	binaryContent, err := extractBinaryFromTarball(resp.Body)
	if err != nil {
		return err
	}

	// Verify checksum if checksums.txt is available
	assetName := filepath.Base(latest.AssetURL)
	checksumURL := strings.Replace(latest.AssetURL, assetName, "checksums.txt", 1)
	expectedHash, err := fetchChecksum(ctx, checksumURL, assetName)
	if err != nil {
		slog.Warn("failed to fetch checksums, skipping verification", "error", err)
	}
	if expectedHash != "" {
		actualHash := sha256.Sum256(binaryContent)
		if hex.EncodeToString(actualHash[:]) != expectedHash {
			return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, hex.EncodeToString(actualHash[:]))
		}
		slog.Debug("checksum verified", "hash", expectedHash)
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

// AutoCheckForUpdates checks for updates with the given context controlling
// timeout and cancellation. Returns true if an update is available.
func AutoCheckForUpdates(ctx context.Context, currentVersion string) bool {
	// Skip if version is "dev" or empty
	if currentVersion == "" || currentVersion == "dev" {
		return false
	}

	latest, hasUpdate, err := utils.CheckForUpdates(ctx)
	if err != nil {
		slog.Debug("update check failed", "error", err)
		return false
	}
	if hasUpdate {
		slog.Info("update available",
			"current", currentVersion,
			"latest", latest.Version,
			"url", latest.URL,
		)
	}
	return hasUpdate
}

// GetUpdateCommand returns the command string to update asimi
func GetUpdateCommand() string {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		return "brew upgrade asimi"
	}
	return "asimi update"
}
