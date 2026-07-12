package config

import "testing"

func TestDefaultSessionConfig(t *testing.T) {
	got := DefaultSessionConfig()

	want := &SessionConfig{
		MaxSessions: 50,
		MaxAgeDays:  30,
	}

	if got.MaxSessions != want.MaxSessions {
		t.Errorf("MaxSessions = %d, want %d", got.MaxSessions, want.MaxSessions)
	}
	if got.MaxAgeDays != want.MaxAgeDays {
		t.Errorf("MaxAgeDays = %d, want %d", got.MaxAgeDays, want.MaxAgeDays)
	}
}

func TestDefaultSessionConfig_Table(t *testing.T) {
	got := DefaultSessionConfig()

	tests := []struct {
		name string
		got  int
		want int
	}{
		{"MaxSessions", got.MaxSessions, 50},
		{"MaxAgeDays", got.MaxAgeDays, 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %d, want %d", tt.got, tt.want)
			}
		})
	}
}
