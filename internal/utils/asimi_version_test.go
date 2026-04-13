package utils

import (
	"os"
	"testing"
)

func TestGetAsimiSlug(t *testing.T) {
	slug := GetAsimiSlug()
	if slug != "afittestide/asimi-cli" {
		t.Errorf("GetAsimiSlug() = %q, want %q", slug, "afittestide/asimi-cli")
	}
}

func TestCheckForUpdates(t *testing.T) {
	// Skip unless CI=true to avoid network-dependent tests during local development
	if os.Getenv("CI") == "" {
		t.Skip("Skipping network-dependent test (set CI=true to run)")
	}

	// This test makes real HTTP calls to GitHub API
	// It verifies the full flow works end-to-end
	release, updated, err := CheckForUpdates()
	if err != nil {
		// "no release found" is acceptable if repo has no releases
		if err.Error() != "no release found" {
			t.Fatalf("CheckForUpdates() error = %v", err)
		}
	}

	if !updated {
		// Current version is up to date - also valid
		t.Log("Current version is up to date")
	}

	if release != nil {
		t.Logf("Update available: %s (current: %s)", release.Version, AsimiVersion)
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		wantErr   bool
		wantMajor uint64
		wantMinor uint64
		wantPatch uint64
	}{
		{
			name:      "version with v prefix",
			version:   "v0.1.0",
			wantErr:   false,
			wantMajor: 0,
			wantMinor: 1,
			wantPatch: 0,
		},
		{
			name:      "version without v prefix",
			version:   "0.1.0",
			wantErr:   false,
			wantMajor: 0,
			wantMinor: 1,
			wantPatch: 0,
		},
		{
			name:      "complex version",
			version:   "v1.2.3-beta.1",
			wantErr:   false,
			wantMajor: 1,
			wantMinor: 2,
			wantPatch: 3,
		},
		{
			name:    "invalid version",
			version: "invalid",
			wantErr: true,
		},
		{
			name:    "empty version",
			version: "",
			wantErr: true,
		},
		{
			name:    "partial version",
			version: "1.2",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := ParseVersion(tt.version)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if v.Major != tt.wantMajor {
					t.Errorf("ParseVersion() major = %v, want %v", v.Major, tt.wantMajor)
				}
				if v.Minor != tt.wantMinor {
					t.Errorf("ParseVersion() minor = %v, want %v", v.Minor, tt.wantMinor)
				}
				if v.Patch != tt.wantPatch {
					t.Errorf("ParseVersion() patch = %v, want %v", v.Patch, tt.wantPatch)
				}
			}
		})
	}
}
