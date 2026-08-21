package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestCurrentVersion(t *testing.T) {
	originalVersion := version
	t.Cleanup(func() { version = originalVersion })

	version = "1.2.3"
	if got, want := currentVersion(), "1.2.3"; got != want {
		t.Fatalf("currentVersion() = %q, want %q", got, want)
	}
}

func TestFormatWorkspaceDisplayShowsDirtyMarker(t *testing.T) {
	ws := workspace{
		Name:   "project-08",
		Branch: "feature",
		Log:    "last commit",
		Dirty:  true,
	}

	got := formatWorkspaceDisplay(ws, false)
	want := `/\ 08 -> feature`
	if got != want {
		t.Fatalf("formatWorkspaceDisplay() = %q, want %q", got, want)
	}
}

func TestFormatWorkspaceDisplayKeepsCleanRowsAligned(t *testing.T) {
	ws := workspace{
		Name:   "project-08",
		Branch: "feature",
		Log:    "last commit",
	}

	got := formatWorkspaceDisplay(ws, false)
	want := "   08 -> feature"
	if got != want {
		t.Fatalf("formatWorkspaceDisplay() = %q, want %q", got, want)
	}
}

func TestFormatWorkspaceDisplayUsesSuffixForNumberedWorkspaces(t *testing.T) {
	ws := workspace{
		Name:   "project-23",
		Branch: "OPS-2321",
		Log:    "last commit",
		Num:    23,
	}

	got := formatWorkspaceDisplay(ws, false)
	want := "   23 -> OPS-2321"
	if got != want {
		t.Fatalf("formatWorkspaceDisplay() = %q, want %q", got, want)
	}
}

func TestInstallZshWrapperIsPersistentAndIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := installZshWrapper(); err != nil {
		t.Fatalf("installZshWrapper() unexpected error: %v", err)
	}
	if err := installZshWrapper(); err != nil {
		t.Fatalf("second installZshWrapper() unexpected error: %v", err)
	}

	wrapperPath := filepath.Join(home, ".config", "preamble", "pre.zsh")
	wrapper, err := os.ReadFile(wrapperPath)
	if err != nil {
		t.Fatalf("read wrapper: %v", err)
	}
	if !strings.Contains(string(wrapper), "pre()") {
		t.Fatalf("wrapper does not define pre(): %s", wrapper)
	}

	zshrc, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil {
		t.Fatalf("read .zshrc: %v", err)
	}
	sourceLine := `[ -f "$HOME/.config/preamble/pre.zsh" ] && source "$HOME/.config/preamble/pre.zsh"`
	if got := strings.Count(string(zshrc), sourceLine); got != 1 {
		t.Fatalf(".zshrc source line count = %d, want 1\n%s", got, zshrc)
	}
}
