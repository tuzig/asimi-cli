package utils

import (
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{
			name:    "version with v prefix",
			version: "v0.1.0",
			wantErr: false,
		},
		{
			name:    "version without v prefix",
			version: "0.1.0",
			wantErr: false,
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseVersion(tt.version)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseVersion() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
