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
	"unicode"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

type config struct {
	Root    string
	Project string
}

type workspace struct {
	Name   string
	Path   string
	Branch string
	Log    string
	Num    int
}

var errUsage = errors.New("usage")

const (
	ansiReset = "\x1b[0m"
	ansiCyan  = "\x1b[36m"
	ansiGreen = "\x1b[32m"
)

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

	if len(args) == 0 {
		return defaultCommand(cfg)
	}

	switch args[0] {
	case "list":
		return listCommand(cfg)
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

func defaultCommand(cfg config) error {
	workspaces, err := collectWorkspaces(cfg)
	if err != nil {
		return err
	}

	if !isInteractiveSession() {
		return printWorkspaceList(workspaces)
	}

	path, err := selectWorkspaceInteractive(cfg, workspaces)
	if err != nil {
		return err
	}

	if path != "" {
		if err := emitSelectionPath(path); err != nil {
			return err
		}
	}

	return nil
}

func emitSelectionPath(path string) error {
	selectionFile := os.Getenv("PRE_SELECTION_FILE")
	if selectionFile == "" {
		fmt.Fprintln(os.Stdout, path)
		return nil
	}

	return os.WriteFile(selectionFile, []byte(path+"\n"), 0600)
}

func isInteractiveSession() bool {
	stderr, err := os.Stderr.Stat()
	if err != nil {
		return false
	}

	if (stderr.Mode() & os.ModeCharDevice) == 0 {
		return false
	}

	return !strings.EqualFold(os.Getenv("TERM"), "dumb")
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
    local destination exit_code selection_file

    case "$1" in
        list|help|-h|--help|setup|init|remove|rm)
            command pre "$@"
            return $?
            ;;
    esac

    if [ -z "$1" ]; then
        selection_file=$(mktemp 2>/dev/null) || return 1
        PRE_SELECTION_FILE="$selection_file" command pre "$@"
        exit_code=$?
        destination=$(tr -d '\n' < "$selection_file")
        rm -f "$selection_file"
    else
        destination="$(command pre "$@")"
        exit_code=$?
    fi

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

	return printWorkspaceList(workspaces)
}

func printWorkspaceList(workspaces []workspace) error {

	fmt.Fprintln(os.Stdout, "Available workspaces:")
	if len(workspaces) == 0 {
		fmt.Fprintln(os.Stdout, "  (none)")
		return nil
	}

	color := colorEnabledFor(os.Stdout)

	for _, ws := range workspaces {
		fmt.Fprintf(os.Stdout, "  %s\n", formatWorkspaceDisplay(ws, color))
	}

	return nil
}

type workspaceItem struct {
	workspace workspace
}

func (w workspaceItem) Title() string {
	return formatWorkspaceDisplay(w.workspace, colorEnabledFor(os.Stderr))
}

func (w workspaceItem) Description() string {
	return ""
}

func (w workspaceItem) FilterValue() string {
	return strings.Join([]string{w.workspace.Name, w.workspace.Branch, w.workspace.Path}, " ")
}

func formatWorkspaceDisplay(ws workspace, color bool) string {
	branch := ws.Branch
	if branch == "" {
		branch = "unknown"
	}
	branch = truncateWithDots(branch, 28)

	logLine := ws.Log
	if logLine == "" {
		logLine = "no commits"
	}
	logLine = truncateWithDots(logLine, 52)

	worktreeText := fmt.Sprintf("[%s]", ws.Name)
	branchText := fmt.Sprintf("(%s)", branch)

	if color {
		worktreeText = colorize(worktreeText, ansiCyan)
		branchText = colorize(branchText, ansiGreen)
	}

	return fmt.Sprintf("%s %s : %s", worktreeText, branchText, logLine)
}

func truncateWithDots(value string, max int) string {
	if max <= 0 {
		return ""
	}

	runes := []rune(value)
	if len(runes) <= max {
		return value
	}

	if max <= 3 {
		return string(runes[:max])
	}

	return string(runes[:max-3]) + "..."
}

func colorEnabledFor(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}

	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}

	stat, err := f.Stat()
	if err != nil {
		return false
	}

	return (stat.Mode() & os.ModeCharDevice) != 0
}

func colorize(value string, ansiColor string) string {
	return ansiColor + value + ansiReset
}

type pickerModel struct {
	cfg              config
	list             list.Model
	selected         string
	cancelled        bool
	actionMode       bool
	actionTarget     workspace
	confirmingDelete bool
	deleteTarget     workspace
	notice           string
}

func newPickerModel(cfg config, workspaces []workspace) pickerModel {
	items := itemsFromWorkspaces(workspaces)

	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetHeight(1)
	delegate.SetSpacing(0)
	l := list.New(items, delegate, 80, 16)
	l.Title = "Choose workspace (enter=actions, d/x=delete, q=quit)"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowPagination(false)
	l.SetShowHelp(false)

	m := pickerModel{cfg: cfg, list: l}
	m.refreshTitle()
	return m
}

func (m *pickerModel) refreshTitle() {
	if m.confirmingDelete {
		m.list.Title = "Delete? y=yes n=no f=force"
		return
	}

	if m.actionMode {
		m.list.Title = "Action: enter/o=open d/x=delete n/q=cancel"
		return
	}

	if m.notice != "" {
		m.list.Title = "Notice: " + truncateWithDots(m.notice, 40)
		return
	}

	m.list.Title = "Choose workspace (enter=actions, d/x=delete, q=quit)"
}

func itemsFromWorkspaces(workspaces []workspace) []list.Item {
	items := make([]list.Item, 0, len(workspaces))
	for _, ws := range workspaces {
		items = append(items, workspaceItem{workspace: ws})
	}
	return items
}

func (m pickerModel) Init() tea.Cmd {
	return nil
}

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.actionMode {
			switch {
			case isEnterKey(msg) || isOpenKey(msg):
				m.selected = m.actionTarget.Path
				m.actionMode = false
				m.refreshTitle()
				return m, tea.Quit
			case isDeleteRequest(msg):
				m.confirmingDelete = true
				m.deleteTarget = m.actionTarget
				m.actionMode = false
				m.notice = ""
				m.refreshTitle()
				return m, nil
			case isNoKey(msg):
				m.actionMode = false
				m.notice = "action cancelled"
				m.refreshTitle()
				return m, nil
			}

			return m, nil
		}

		if m.confirmingDelete {
			switch {
			case isYesKey(msg):
				if err := removeWorkspacePath(m.cfg, m.deleteTarget.Path, false); err != nil {
					m.notice = fmt.Sprintf("delete failed: %s", truncateWithDots(err.Error(), 80))
				} else {
					m.notice = fmt.Sprintf("removed %s", m.deleteTarget.Name)
					if refreshed, err := collectWorkspaces(m.cfg); err == nil {
						m.list.SetItems(itemsFromWorkspaces(refreshed))
					}
				}
				m.confirmingDelete = false
				m.refreshTitle()
				return m, nil
			case isForceKey(msg):
				if err := removeWorkspacePath(m.cfg, m.deleteTarget.Path, true); err != nil {
					m.notice = fmt.Sprintf("force delete failed: %s", truncateWithDots(err.Error(), 80))
				} else {
					m.notice = fmt.Sprintf("removed %s (forced)", m.deleteTarget.Name)
					if refreshed, err := collectWorkspaces(m.cfg); err == nil {
						m.list.SetItems(itemsFromWorkspaces(refreshed))
					}
				}
				m.confirmingDelete = false
				m.refreshTitle()
				return m, nil
			case isNoKey(msg):
				m.confirmingDelete = false
				m.notice = "delete cancelled"
				m.refreshTitle()
				return m, nil
			}
			return m, nil
		}

		switch {
		case isEnterKey(msg):
			selected, ok := m.list.SelectedItem().(workspaceItem)
			if ok {
				m.actionMode = true
				m.actionTarget = selected.workspace
				m.notice = ""
				m.refreshTitle()
			} else {
				m.notice = "no workspace selected"
				m.refreshTitle()
			}
			return m, nil
		case isQuitKey(msg):
			m.cancelled = true
			return m, tea.Quit
		}

		if isDeleteRequest(msg) {
			selected, ok := m.list.SelectedItem().(workspaceItem)
			if ok {
				m.confirmingDelete = true
				m.deleteTarget = selected.workspace
				m.notice = ""
				m.refreshTitle()
			} else {
				m.notice = "no workspace selected"
				m.refreshTitle()
			}
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m pickerModel) View() string {
	return m.list.View()
}

func isEnterKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyEnter || strings.EqualFold(msg.String(), "enter")
}

func isOpenKey(msg tea.KeyMsg) bool {
	return isRuneKey(msg, 'o')
}

func isQuitKey(msg tea.KeyMsg) bool {
	key := strings.ToLower(msg.String())
	return key == "q" || key == "esc" || key == "ctrl+c"
}

func isDeleteRequest(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyCtrlD, tea.KeyDelete, tea.KeyBackspace:
		return true
	}

	return isRuneKey(msg, 'd') || isRuneKey(msg, 'x') || strings.EqualFold(msg.String(), "delete") || strings.EqualFold(msg.String(), "ctrl+d")
}

func isYesKey(msg tea.KeyMsg) bool {
	return isRuneKey(msg, 'y') || strings.EqualFold(msg.String(), "yes")
}

func isNoKey(msg tea.KeyMsg) bool {
	key := strings.ToLower(msg.String())
	return isRuneKey(msg, 'n') || key == "no" || key == "q" || key == "esc" || key == "ctrl+c"
}

func isForceKey(msg tea.KeyMsg) bool {
	return isRuneKey(msg, 'f') || strings.EqualFold(msg.String(), "force")
}

func isRuneKey(msg tea.KeyMsg, expected rune) bool {
	if msg.Type != tea.KeyRunes || len(msg.Runes) == 0 {
		return false
	}

	return unicode.ToLower(msg.Runes[0]) == unicode.ToLower(expected)
}

func selectWorkspaceInteractive(cfg config, workspaces []workspace) (string, error) {
	if len(workspaces) == 0 {
		return "", printWorkspaceList(workspaces)
	}

	program := tea.NewProgram(newPickerModel(cfg, workspaces), tea.WithInputTTY(), tea.WithOutput(os.Stderr))
	result, err := program.Run()
	if err != nil {
		return "", err
	}

	model, ok := result.(pickerModel)
	if !ok {
		return "", fmt.Errorf("unexpected picker model type")
	}

	if model.cancelled || model.selected == "" {
		return "", nil
	}

	return model.selected, nil
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

	if err := removeWorkspacePath(cfg, target.Path, force); err != nil {
		if !force {
			return fmt.Errorf("%w (retry with --force to remove a dirty worktree)", err)
		}
		return err
	}

	fmt.Fprintf(os.Stdout, "Removed workspace: %s\n", target.Name)
	return nil
}

func removeWorkspacePath(cfg config, workspacePath string, force bool) error {
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
			Log:    lastCommitLine(path),
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
