// Package utils provides shared utility functions for message formatting.
package utils

import (
	"fmt"
	"github.com/blang/semver"
	"github.com/rhysd/go-github-selfupdate/selfupdate"
	"log/slog"
	"strings"
)

const (
	githubOwner = "afittestide"
	githubRepo  = "asimi-cli"
)

var AsimiVersion = "0.6.0" // Update this before each release

// GetAsimiSlug gets the slug for asimi's repo
func GetAsimiSlug() string {
	return fmt.Sprintf("%s/%s", githubOwner, githubRepo)
}

// ParseVersion parses a version string, handling "v" prefix
func ParseVersion(v string) (semver.Version, error) {
	// Remove "v" prefix if present
	v = strings.TrimPrefix(v, "v")
	return semver.Parse(v)
}

// CheckForUpdates checks if a newer version is available on GitHub
func CheckForUpdates() (*selfupdate.Release, bool, error) {
	slug := GetAsimiSlug()
	slog.Debug("Checking updates", "project", slug)
	latest, found, err := selfupdate.DetectLatest(slug)
	if err != nil {
		return nil, false, fmt.Errorf("failed to detect latest version: %w", err)
	}

	if !found {
		return nil, false, fmt.Errorf("no release found")
	}

	current, err := ParseVersion(AsimiVersion)
	if err != nil {
		return nil, false, fmt.Errorf("invalid current version: %w", err)
	}

	if latest.Version.LTE(current) {
		slog.Debug("current version is up to date", "current", AsimiVersion, "latest", latest.Version)
		return latest, false, nil
	}

	return latest, true, nil
}
