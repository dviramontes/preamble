package workspaces

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveBaseRef(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "default", input: "", want: "origin/main"},
		{name: "branch name", input: "feature", want: "origin/feature"},
		{name: "origin ref", input: "origin/dev", want: "origin/dev"},
		{name: "refs ref", input: "refs/heads/dev", want: "refs/heads/dev"},
		{name: "head ref", input: "HEAD~1", want: "HEAD~1"},
		{name: "remotes ref", input: "remotes/upstream/main", want: "remotes/upstream/main"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveBaseRef(tt.input); got != tt.want {
				t.Fatalf("ResolveBaseRef(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeTarget(t *testing.T) {
	tests := []struct {
		name    string
		project string
		target  string
		want    string
		wantErr bool
	}{
		{name: "single digit", project: "project", target: "8", want: "project-08"},
		{name: "two digits", project: "project", target: "08", want: "project-08"},
		{name: "full name", project: "project", target: "project-08", want: "project-08"},
		{name: "bad prefix", project: "project", target: "foo-08", wantErr: true},
		{name: "bad text", project: "project", target: "abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeTarget(tt.project, tt.target)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeTarget(%q, %q) error = nil, want error", tt.project, tt.target)
				}
				return
			}

			if err != nil {
				t.Fatalf("NormalizeTarget(%q, %q) unexpected error: %v", tt.project, tt.target, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeTarget(%q, %q) = %q, want %q", tt.project, tt.target, got, tt.want)
			}
		})
	}
}

func TestCollectSortsMatchingWorkspaces(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"project-10", "project-02", "project-01", "skip-me"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", name, err)
		}
	}

	items, err := Collect(Config{Root: root, Project: "project"})
	if err != nil {
		t.Fatalf("Collect unexpected error: %v", err)
	}

	if len(items) != 3 {
		t.Fatalf("Collect length = %d, want 3", len(items))
	}

	want := []string{"project-01", "project-02", "project-10"}
	for i, name := range want {
		if items[i].Name != name {
			t.Fatalf("Collect[%d].Name = %q, want %q", i, items[i].Name, name)
		}
	}
}
