package shogunate

import (
	"testing"
)

func TestParseForkRitual(t *testing.T) {
	yaml := `
name: fork-ritual
description: A ritual with fork/join
steps:
  - name: plan
    minister: sage
    task: Plan the work
    output_key: work_units
  - name: fix-all
    fork:
      over: work_units
      batch_size: 5
    work:
      - name: fix
        minister: forge
        act: Fix {{ .item.file }}
      - name: verify
        minister: judge
        act: Verify {{ .item.file }}
`

	ritual, err := ParseRitual([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseRitual() error = %v", err)
	}

	if len(ritual.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(ritual.Steps))
	}

	forkStep := ritual.Steps[1]
	if forkStep.Fork == nil {
		t.Fatal("expected fork step to have Fork defined")
	}
	if forkStep.Fork.Over != "work_units" {
		t.Errorf("expected fork over 'work_units', got %q", forkStep.Fork.Over)
	}
	if forkStep.Fork.BatchSize != 5 {
		t.Errorf("expected batch_size 5, got %d", forkStep.Fork.BatchSize)
	}
	if len(forkStep.Work) != 2 {
		t.Errorf("expected 2 work steps, got %d", len(forkStep.Work))
	}
	if forkStep.Work[0].Minister != "forge" {
		t.Errorf("expected work[0] minister 'forge', got %q", forkStep.Work[0].Minister)
	}
	if forkStep.Work[1].Minister != "judge" {
		t.Errorf("expected work[1] minister 'judge', got %q", forkStep.Work[1].Minister)
	}
}

func TestValidateForkRitual(t *testing.T) {
	tests := []struct {
		name    string
		ritual  *RitualDef
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid fork step",
			ritual: &RitualDef{
				Name: "fork-valid",
				Steps: []RitualStep{
					{
						Name: "fork-step",
						Fork: &ForkDef{Over: "work_units", BatchSize: 5},
						Work: []RitualStep{{Name: "work", Minister: "forge", Task: "do work"}},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "fork step missing work",
			ritual: &RitualDef{
				Name: "fork-no-work",
				Steps: []RitualStep{
					{
						Name: "fork-step",
						Fork: &ForkDef{Over: "work_units"},
					},
				},
			},
			wantErr: true,
			errMsg:  "requires work steps",
		},
		{
			name: "fork step missing over",
			ritual: &RitualDef{
				Name: "fork-no-over",
				Steps: []RitualStep{
					{
						Name: "fork-step",
						Fork: &ForkDef{},
						Work: []RitualStep{{Name: "work", Minister: "forge", Task: "do work"}},
					},
				},
			},
			wantErr: true,
			errMsg:  "requires 'over' field",
		},
		{
			name: "fork work step missing minister",
			ritual: &RitualDef{
				Name: "fork-work-no-minister",
				Steps: []RitualStep{
					{
						Name: "fork-step",
						Fork: &ForkDef{Over: "work_units"},
						Work: []RitualStep{{Name: "work", Task: "do work"}},
					},
				},
			},
			wantErr: true,
			errMsg:  "requires minister",
		},
		{
			name: "fork work step missing act",
			ritual: &RitualDef{
				Name: "fork-work-no-act",
				Steps: []RitualStep{
					{
						Name: "fork-step",
						Fork: &ForkDef{Over: "work_units"},
						Work: []RitualStep{{Name: "work", Minister: "forge"}},
					},
				},
			},
			wantErr: true,
			errMsg:  "requires act or task",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRitual(tt.ritual)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRitual() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				if !containsString(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			}
		})
	}
}
