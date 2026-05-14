package workspaces

import (
	"os"
	"os/exec"
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

func TestParseWorktreeList(t *testing.T) {
	output := `worktree /tmp/project
HEAD 1111111111111111111111111111111111111111
branch refs/heads/main

worktree /tmp/project-23
HEAD 2222222222222222222222222222222222222222
branch refs/heads/OPS-2321

worktree /tmp/project-review
HEAD 3333333333333333333333333333333333333333
detached
`

	items := parseWorktreeList("project", output)

	if len(items) != 3 {
		t.Fatalf("parseWorktreeList length = %d, want 3", len(items))
	}

	if items[0].Name != "project" || items[0].Branch != "main" || items[0].Num != 0 {
		t.Fatalf("parseWorktreeList[0] = %#v, want base worktree on main", items[0])
	}
	if items[1].Name != "project-23" || items[1].Branch != "OPS-2321" || items[1].Num != 23 {
		t.Fatalf("parseWorktreeList[1] = %#v, want project-23 on OPS-2321", items[1])
	}
	if items[2].Name != "project-review" || items[2].Branch != "3333333333333333333333333333333333333333" || items[2].Num != 0 {
		t.Fatalf("parseWorktreeList[2] = %#v, want detached project-review", items[2])
	}
}

func TestHasChanges(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "pre@example.test")
	runGit(t, repo, "config", "user.name", "pre test")

	writeFile(t, filepath.Join(repo, "tracked.txt"), "clean\n")
	runGit(t, repo, "add", "tracked.txt")
	runGit(t, repo, "commit", "-m", "initial")

	if hasChanges(repo) {
		t.Fatal("hasChanges() = true for clean repo, want false")
	}

	writeFile(t, filepath.Join(repo, "tracked.txt"), "unstaged\n")
	if !hasChanges(repo) {
		t.Fatal("hasChanges() = false for unstaged modification, want true")
	}

	runGit(t, repo, "add", "tracked.txt")
	if !hasChanges(repo) {
		t.Fatal("hasChanges() = false for staged modification, want true")
	}

	runGit(t, repo, "commit", "-m", "update tracked")
	writeFile(t, filepath.Join(repo, "untracked.txt"), "new\n")
	if !hasChanges(repo) {
		t.Fatal("hasChanges() = false for untracked file, want true")
	}
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()

	gitArgs := append([]string{"-C", repo}, args...)
	cmd := exec.Command("git", gitArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
