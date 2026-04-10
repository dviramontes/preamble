package workspaces

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Config struct {
	Root    string
	Project string
}

type Workspace struct {
	Name   string
	Path   string
	Branch string
	Log    string
	Num    int
}

func LoadConfig() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}

	root := os.Getenv("PRE_ROOT")
	if root == "" {
		root = filepath.Join(home, "local", "work")
	}
	if strings.HasPrefix(root, "~/") {
		root = filepath.Join(home, root[2:])
	}

	project := os.Getenv("PRE_PROJECT")
	if project == "" {
		project = "ops"
	}

	return Config{Root: root, Project: project}, nil
}

func Collect(cfg Config) ([]Workspace, error) {
	stat, err := os.Stat(cfg.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("workspace root not found: %s", cfg.Root)
		}
		return nil, err
	}
	if !stat.IsDir() {
		return nil, fmt.Errorf("workspace root is not a directory: %s", cfg.Root)
	}

	entries, err := os.ReadDir(cfg.Root)
	if err != nil {
		return nil, err
	}

	pattern := regexp.MustCompile("^" + regexp.QuoteMeta(cfg.Project) + `-(\d{2})$`)
	var result []Workspace

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		matches := pattern.FindStringSubmatch(entry.Name())
		if len(matches) != 2 {
			continue
		}

		num, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}

		path := filepath.Join(cfg.Root, entry.Name())
		result = append(result, Workspace{
			Name:   entry.Name(),
			Path:   path,
			Branch: branchOrSHA(path),
			Log:    lastCommitLine(path),
			Num:    num,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Num < result[j].Num
	})

	return result, nil
}

func SwitchPath(cfg Config, target string) (string, error) {
	items, err := Collect(cfg)
	if err != nil {
		return "", err
	}

	name, err := NormalizeTarget(cfg.Project, target)
	if err != nil {
		return "", err
	}

	for _, ws := range items {
		if ws.Name == name {
			return ws.Path, nil
		}
	}

	return "", fmt.Errorf("no workspace found for %q", target)
}

func CreateNext(cfg Config, rawRef string) (string, error) {
	baseRepoPath := filepath.Join(cfg.Root, cfg.Project)
	stat, err := os.Stat(baseRepoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("base repo not found: %s", baseRepoPath)
		}
		return "", err
	}
	if !stat.IsDir() {
		return "", fmt.Errorf("base repo path is not a directory: %s", baseRepoPath)
	}

	items, err := Collect(cfg)
	if err != nil {
		return "", err
	}

	next := 1
	for _, ws := range items {
		if ws.Num >= next {
			next = ws.Num + 1
		}
	}

	newName := fmt.Sprintf("%s-%02d", cfg.Project, next)
	newPath := filepath.Join(cfg.Root, newName)
	if _, err := os.Stat(newPath); err == nil {
		return "", fmt.Errorf("workspace already exists: %s", newPath)
	}

	baseRef := ResolveBaseRef(rawRef)
	if err := verifyRefExists(baseRepoPath, baseRef); err != nil {
		return "", err
	}

	cmd := exec.Command("git", "-C", baseRepoPath, "worktree", "add", newPath, baseRef)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}

	return newPath, nil
}

func Remove(cfg Config, rawTarget string, force bool) (Workspace, error) {
	name, err := NormalizeTarget(cfg.Project, rawTarget)
	if err != nil {
		return Workspace{}, err
	}

	items, err := Collect(cfg)
	if err != nil {
		return Workspace{}, err
	}

	for _, ws := range items {
		if ws.Name == name {
			if err := RemovePath(cfg, ws.Path, force); err != nil {
				return Workspace{}, err
			}
			return ws, nil
		}
	}

	return Workspace{}, fmt.Errorf("no workspace found for %q", rawTarget)
}

func RemovePath(cfg Config, workspacePath string, force bool) error {
	baseRepoPath := filepath.Join(cfg.Root, cfg.Project)
	gitArgs := []string{"-C", baseRepoPath, "worktree", "remove"}
	if force {
		gitArgs = append(gitArgs, "--force")
	}
	gitArgs = append(gitArgs, workspacePath)

	cmd := exec.Command("git", gitArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return errors.New(msg)
		}
		return err
	}

	return nil
}

func ResolveBaseRef(rawRef string) string {
	if rawRef == "" {
		return "origin/main"
	}

	if strings.HasPrefix(rawRef, "origin/") ||
		strings.HasPrefix(rawRef, "refs/") ||
		strings.HasPrefix(rawRef, "HEAD") ||
		strings.HasPrefix(rawRef, "remotes/") {
		return rawRef
	}

	return "origin/" + rawRef
}

func NormalizeTarget(project, target string) (string, error) {
	if len(target) == 1 && target[0] >= '0' && target[0] <= '9' {
		return fmt.Sprintf("%s-0%s", project, target), nil
	}

	if len(target) == 2 && isAllDigits(target) {
		return fmt.Sprintf("%s-%s", project, target), nil
	}

	pattern := regexp.MustCompile("^" + regexp.QuoteMeta(project) + `-(\d{2})$`)
	if pattern.MatchString(target) {
		return target, nil
	}

	return "", fmt.Errorf("expected 1-2 digits or %s-XX", project)
}

func verifyRefExists(repoPath, ref string) error {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("base ref not found: %s (try: git -C %s fetch origin)", ref, repoPath)
	}
	return nil
}

func branchOrSHA(repoPath string) string {
	branch, err := gitOutput(repoPath, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err == nil && branch != "" {
		return branch
	}

	sha, err := gitOutput(repoPath, "rev-parse", "--short", "HEAD")
	if err == nil {
		return sha
	}

	return ""
}

func lastCommitLine(repoPath string) string {
	logLine, err := gitOutput(repoPath, "log", "-1", "--pretty=format:%s")
	if err != nil {
		return ""
	}

	return logLine
}

func gitOutput(repoPath string, args ...string) (string, error) {
	gitArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.Command("git", gitArgs...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
