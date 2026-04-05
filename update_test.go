package main

import (
	"testing"
)

func TestGetUpdateCommand(t *testing.T) {
	cmd := GetUpdateCommand()
	if cmd == "" {
		t.Error("GetUpdateCommand() returned empty string")
	}
	// Should return either brew command or asimi update
	if cmd != "brew upgrade asimi" && cmd != "asimi update" {
		t.Errorf("GetUpdateCommand() = %v, want 'brew upgrade asimi' or 'asimi update'", cmd)
	}
}

func TestAutoCheckForUpdates(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{
			name:    "dev version skips check",
			version: "dev",
			want:    false,
		},
		{
			name:    "empty version skips check",
			version: "",
			want:    false,
		},
		// Note: We can't test actual update checking without hitting GitHub API
		// which would be flaky in tests
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AutoCheckForUpdates(tt.version)
			if got != tt.want {
				t.Errorf("AutoCheckForUpdates() = %v, want %v", got, tt.want)
			}
		})
	}
}
