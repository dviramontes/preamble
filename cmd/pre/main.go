package main

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

type config struct {
	Root    string
	Project string
}

type workspace struct {
	Name   string
	Path   string
	Branch string
	Num    int
}

var errUsage = errors.New("usage")

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, errUsage) {
			printUsage(os.Stderr)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "pre: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if len(args) == 0 || args[0] == "list" {
		return listCommand(cfg)
	}

	switch args[0] {
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return nil
	case "remove", "rm":
		target, confirm, force, err := parseRemoveArgs(args[1:])
		if err != nil {
			return err
		}
		return removeCommand(cfg, target, confirm, force)
	case "setup", "init":
		if len(args) > 2 {
			return errUsage
		}
		install := len(args) == 2 && args[1] == "--install"
		if len(args) == 2 && !install {
			return errUsage
		}
		return setupCommand(install)
	case "new":
		if len(args) > 2 {
			return errUsage
		}
		baseRef := ""
		if len(args) == 2 {
			baseRef = args[1]
		}
		path, err := newCommand(cfg, baseRef)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, path)
		return nil
	default:
		if len(args) != 1 {
			return errUsage
		}
		path, err := switchPathCommand(cfg, args[0])
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, path)
		return nil
	}
}

func setupCommand(install bool) error {
	if !install {
		printSetupInstructions(os.Stdout)
		return nil
	}

	if err := installZshWrapper(); err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "Installed pre zsh wrapper in ~/.functions.sh")
	fmt.Fprintln(os.Stdout, "Reload with: source ~/.functions.sh")
	return nil
}

func printSetupInstructions(out *os.File) {
	fmt.Fprintln(out, "pre shell setup")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "To enable cd behavior for 'pre <suffix>', add this wrapper to ~/.functions.sh:")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, zshWrapperBlock())
	fmt.Fprintln(out, "Then reload your shell:")
	fmt.Fprintln(out, "  source ~/.functions.sh")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Or run:")
	fmt.Fprintln(out, "  pre setup --install")
}

func installZshWrapper() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	functionsPath := filepath.Join(home, ".functions.sh")
	block := zshWrapperBlock()

	content, err := os.ReadFile(functionsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return os.WriteFile(functionsPath, []byte(block+"\n"), 0644)
		}
		return err
	}

	text := string(content)
	begin := "# >>> pre zsh wrapper >>>"
	end := "# <<< pre zsh wrapper <<<"

	startIdx := strings.Index(text, begin)
	endIdx := strings.Index(text, end)
	if startIdx >= 0 && endIdx > startIdx {
		replaceEnd := endIdx + len(end)
		if replaceEnd < len(text) && text[replaceEnd] == '\n' {
			replaceEnd++
		}
		updated := text[:startIdx] + block + "\n" + text[replaceEnd:]
		return os.WriteFile(functionsPath, []byte(updated), 0644)
	}

	if len(text) > 0 && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += "\n" + block + "\n"

	return os.WriteFile(functionsPath, []byte(text), 0644)
}

func zshWrapperBlock() string {
	return strings.TrimSpace(`
# >>> pre zsh wrapper >>>
pre() {
    local destination exit_code

    case "$1" in
        ""|list|help|-h|--help|setup|init)
            command pre "$@"
            return $?
            ;;
    esac

    destination="$(command pre "$@")"
    exit_code=$?

    if [ "$exit_code" -ne 0 ]; then
        [ -n "$destination" ] && printf '%s\n' "$destination"
        return "$exit_code"
    fi

    if [ -d "$destination" ]; then
        cd "$destination" || return 1
    elif [ -n "$destination" ]; then
        printf '%s\n' "$destination"
    fi
}
# <<< pre zsh wrapper <<<
`)
}

func loadConfig() (config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return config{}, err
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

	return config{Root: root, Project: project}, nil
}

func listCommand(cfg config) error {
	workspaces, err := collectWorkspaces(cfg)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "Available workspaces:")
	if len(workspaces) == 0 {
		fmt.Fprintln(os.Stdout, "  (none)")
		return nil
	}

	for _, ws := range workspaces {
		if ws.Branch != "" {
			fmt.Fprintf(os.Stdout, "  %s [%s]\n", ws.Name, ws.Branch)
			continue
		}
		fmt.Fprintf(os.Stdout, "  %s\n", ws.Name)
	}

	return nil
}

func switchPathCommand(cfg config, target string) (string, error) {
	workspaces, err := collectWorkspaces(cfg)
	if err != nil {
		return "", err
	}

	name, err := normalizeTarget(cfg.Project, target)
	if err != nil {
		return "", err
	}

	for _, ws := range workspaces {
		if ws.Name == name {
			return ws.Path, nil
		}
	}

	return "", fmt.Errorf("no workspace found for %q", target)
}

func newCommand(cfg config, rawRef string) (string, error) {
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

	workspaces, err := collectWorkspaces(cfg)
	if err != nil {
		return "", err
	}

	next := 1
	for _, ws := range workspaces {
		if ws.Num >= next {
			next = ws.Num + 1
		}
	}

	newName := fmt.Sprintf("%s-%02d", cfg.Project, next)
	newPath := filepath.Join(cfg.Root, newName)
	if _, err := os.Stat(newPath); err == nil {
		return "", fmt.Errorf("workspace already exists: %s", newPath)
	}

	baseRef := resolveBaseRef(rawRef)
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

func parseRemoveArgs(args []string) (string, bool, bool, error) {
	if len(args) == 0 {
		return "", false, false, errUsage
	}

	var target string
	confirm := false
	force := false

	for _, arg := range args {
		switch arg {
		case "--yes", "-y":
			confirm = true
		case "--force", "-f":
			force = true
		default:
			if target != "" {
				return "", false, false, errUsage
			}
			target = arg
		}
	}

	if target == "" {
		return "", false, false, errUsage
	}

	return target, confirm, force, nil
}

func removeCommand(cfg config, rawTarget string, confirm bool, force bool) error {
	if !confirm {
		return fmt.Errorf("refusing to remove %q without confirmation; rerun with --yes", rawTarget)
	}

	name, err := normalizeTarget(cfg.Project, rawTarget)
	if err != nil {
		return err
	}

	workspaces, err := collectWorkspaces(cfg)
	if err != nil {
		return err
	}

	var target workspace
	found := false
	for _, ws := range workspaces {
		if ws.Name == name {
			target = ws
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("no workspace found for %q", rawTarget)
	}

	baseRepoPath := filepath.Join(cfg.Root, cfg.Project)
	gitArgs := []string{"-C", baseRepoPath, "worktree", "remove"}
	if force {
		gitArgs = append(gitArgs, "--force")
	}
	gitArgs = append(gitArgs, target.Path)

	cmd := exec.Command("git", gitArgs...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if !force {
			return fmt.Errorf("%w (retry with --force to remove a dirty worktree)", err)
		}
		return err
	}

	fmt.Fprintf(os.Stdout, "Removed workspace: %s\n", target.Name)
	return nil
}

func resolveBaseRef(rawRef string) string {
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

func verifyRefExists(repoPath, ref string) error {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("base ref not found: %s (try: git -C %s fetch origin)", ref, repoPath)
	}
	return nil
}

func collectWorkspaces(cfg config) ([]workspace, error) {
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
	var workspaces []workspace

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
		workspaces = append(workspaces, workspace{
			Name:   entry.Name(),
			Path:   path,
			Branch: branchOrSHA(path),
			Num:    num,
		})
	}

	sort.Slice(workspaces, func(i, j int) bool {
		return workspaces[i].Num < workspaces[j].Num
	})

	return workspaces, nil
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

func gitOutput(repoPath string, args ...string) (string, error) {
	gitArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.Command("git", gitArgs...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func normalizeTarget(project, target string) (string, error) {
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

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func printUsage(out *os.File) {
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  pre               List workspaces")
	fmt.Fprintln(out, "  pre list          List workspaces")
	fmt.Fprintln(out, "  pre <suffix>      Print workspace path (08, 8, or ops-08)")
	fmt.Fprintln(out, "  pre new [base-ref] Create next workspace from base ref")
	fmt.Fprintln(out, "  pre remove <suffix> --yes [--force] Remove a workspace")
	fmt.Fprintln(out, "  pre rm <suffix> --yes [-f]          Alias for remove")
	fmt.Fprintln(out, "  pre setup          Print zsh wrapper setup instructions")
	fmt.Fprintln(out, "  pre setup --install Install zsh wrapper in ~/.functions.sh")
	fmt.Fprintln(out, "  pre init [--install] Alias for setup")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Defaults:")
	fmt.Fprintln(out, "  PRE_ROOT=$HOME/local/work")
	fmt.Fprintln(out, "  PRE_PROJECT=ops")
}
