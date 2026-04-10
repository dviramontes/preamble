package main

import (
	"errors"
	"testing"
)

func TestParseRemoveArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantTarget  string
		wantConfirm bool
		wantForce   bool
		wantErr     bool
	}{
		{name: "target only", args: []string{"08"}, wantTarget: "08"},
		{name: "confirm and force", args: []string{"08", "--yes", "--force"}, wantTarget: "08", wantConfirm: true, wantForce: true},
		{name: "short flags", args: []string{"-y", "-f", "08"}, wantTarget: "08", wantConfirm: true, wantForce: true},
		{name: "missing target", args: []string{"--yes"}, wantErr: true},
		{name: "duplicate targets", args: []string{"08", "09"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, confirm, force, err := parseRemoveArgs(tt.args)
			if tt.wantErr {
				if !errors.Is(err, errUsage) {
					t.Fatalf("parseRemoveArgs(%v) error = %v, want errUsage", tt.args, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseRemoveArgs(%v) unexpected error: %v", tt.args, err)
			}
			if target != tt.wantTarget || confirm != tt.wantConfirm || force != tt.wantForce {
				t.Fatalf("parseRemoveArgs(%v) = (%q, %v, %v), want (%q, %v, %v)", tt.args, target, confirm, force, tt.wantTarget, tt.wantConfirm, tt.wantForce)
			}
		})
	}
}
