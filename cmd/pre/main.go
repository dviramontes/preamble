package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dviramontes/preamble/internal/workspaces"
	"github.com/gdamore/tcell/v3"
	z "github.com/tekugo/zeichenwerk"
)

type config = workspaces.Config

type workspace = workspaces.Workspace

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
	return workspaces.LoadConfig()
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

func detectCurrentWorkspaceIndex(workspaces []workspace) int {
	cwd, err := os.Getwd()
	if err != nil {
		return -1
	}

	for idx, ws := range workspaces {
		if cwd == ws.Path || strings.HasPrefix(cwd, ws.Path+string(filepath.Separator)) {
			return idx
		}
	}

	return -1
}

func selectWorkspaceInteractive(cfg config, workspaces []workspace) (string, error) {
	if len(workspaces) == 0 {
		return "", printWorkspaceList(workspaces)
	}

	controller, err := newPickerController(cfg, workspaces)
	if err != nil {
		return "", err
	}

	if err := controller.ui.Run(); err != nil {
		return "", err
	}

	if controller.cancelled || controller.selected == "" {
		return "", nil
	}

	return controller.selected, nil
}

type pickerController struct {
	cfg        config
	ui         *z.UI
	meta       *z.Static
	list       *z.List
	listMeta   *z.Static
	name       *z.Static
	summary    *z.Static
	path       *z.Static
	branch     *z.Static
	commit     *z.Static
	cwd        *z.Static
	notice     *z.Static
	footer     *z.Static
	workspaces []workspace
	current    int
	selected   string
	cancelled  bool
}

func newPickerController(cfg config, workspaces []workspace) (*pickerController, error) {
	currentIndex := detectCurrentWorkspaceIndex(workspaces)
	builder := z.NewBuilder(z.TokyoNightTheme())
	builder.
		Flex("picker-root", false, "stretch", 1).Padding(1, 2).
		Flex("picker-header", false, "stretch", 0).Border("round").Padding(1, 2).
		Static("picker-title", "pre").Font("bold").Foreground("$cyan").
		Static("picker-subtitle", "workspace TUI for git worktrees").Foreground("$fg1").
		Static("picker-meta", "").Foreground("$gray").
		End().
		Grid("picker-main", 1, 2, false).Hint(0, -1).Columns(34, -1).
		Cell(0, 0, 1, 1).
		Flex("picker-list-pane", false, "stretch", 1).Border("round").Padding(1, 1).
		Static("picker-list-title", "Workspaces").Font("bold").
		Static("picker-list-meta", "").Foreground("$gray").
		List("picker-list", workspaceLines(workspaces, currentIndex)...).Hint(0, -1).
		End().
		Cell(1, 0, 1, 1).
		Flex("picker-detail-pane", false, "stretch", 1).Border("round").Padding(1, 2).
		Static("picker-name", "").Font("bold").Foreground("$cyan").
		Static("picker-summary", "").Foreground("$fg1").
		HRule("thin").
		Static("picker-path", "").
		Static("picker-branch", "").
		Static("picker-commit", "").
		Static("picker-cwd", "").Foreground("$gray").
		HRule("thin").
		Flex("picker-actions", true, "start", 2).
		Button("picker-open", "Open").
		Button("picker-delete", "Delete").
		Button("picker-refresh", "Refresh").
		Button("picker-quit", "Quit").
		End().
		End().
		End().
		Static("picker-notice", "").Foreground("$yellow").
		Static("picker-footer", "Enter/o open  a actions  d/x delete  r refresh  Tab move focus  q quit").Foreground("$gray").
		End()

	ui := builder.Build()
	controller := &pickerController{
		cfg:        cfg,
		ui:         ui,
		meta:       z.Find(ui, "picker-meta").(*z.Static),
		list:       z.Find(ui, "picker-list").(*z.List),
		listMeta:   z.Find(ui, "picker-list-meta").(*z.Static),
		name:       z.Find(ui, "picker-name").(*z.Static),
		summary:    z.Find(ui, "picker-summary").(*z.Static),
		path:       z.Find(ui, "picker-path").(*z.Static),
		branch:     z.Find(ui, "picker-branch").(*z.Static),
		commit:     z.Find(ui, "picker-commit").(*z.Static),
		cwd:        z.Find(ui, "picker-cwd").(*z.Static),
		notice:     z.Find(ui, "picker-notice").(*z.Static),
		footer:     z.Find(ui, "picker-footer").(*z.Static),
		workspaces: workspaces,
		current:    currentIndex,
	}

	z.OnSelect(controller.list, func(_ z.Widget, index int) bool {
		controller.updateSelection(index)
		return true
	})
	z.OnActivate(controller.list, func(_ z.Widget, index int) bool {
		controller.openWorkspace(index)
		return true
	})
	z.OnKey(controller.list, controller.handleListKey)
	z.Find(ui, "picker-open").On(z.EvtActivate, func(_ z.Widget, _ z.Event, _ ...any) bool {
		controller.openWorkspace(controller.list.Selected())
		return true
	})
	z.Find(ui, "picker-delete").On(z.EvtActivate, func(_ z.Widget, _ z.Event, _ ...any) bool {
		controller.openDeleteDialog()
		return true
	})
	z.Find(ui, "picker-refresh").On(z.EvtActivate, func(_ z.Widget, _ z.Event, _ ...any) bool {
		controller.refreshWorkspaces(controller.list.Selected())
		return true
	})
	z.Find(ui, "picker-quit").On(z.EvtActivate, func(_ z.Widget, _ z.Event, _ ...any) bool {
		controller.cancelled = true
		controller.ui.Quit()
		return true
	})

	if currentIndex >= 0 {
		controller.list.Select(currentIndex)
	} else {
		controller.list.Select(0)
	}
	controller.refreshMeta()
	controller.updateSelection(controller.list.Selected())

	return controller, nil
}

func workspaceLines(workspaces []workspace, currentIndex int) []string {
	lines := make([]string, 0, len(workspaces))
	for idx, ws := range workspaces {
		prefix := "  "
		if idx == currentIndex {
			prefix = "* "
		}
		lines = append(lines, prefix+formatWorkspaceDisplay(ws, false))
	}
	return lines
}

func (c *pickerController) handleListKey(_ z.Widget, ev *tcell.EventKey) bool {
	switch {
	case isOpenKey(ev):
		c.openWorkspace(c.list.Selected())
		return true
	case isActionKey(ev):
		c.openActionsDialog()
		return true
	case isDeleteRequest(ev):
		c.openDeleteDialog()
		return true
	case isRefreshKey(ev):
		c.refreshWorkspaces(c.list.Selected())
		return true
	default:
		return false
	}
}

func (c *pickerController) currentWorkspace() (workspace, int, bool) {
	index := c.list.Selected()
	if index < 0 || index >= len(c.workspaces) {
		return workspace{}, -1, false
	}
	return c.workspaces[index], index, true
}

func (c *pickerController) openWorkspace(index int) {
	if index < 0 || index >= len(c.workspaces) {
		c.setNotice("no workspace selected")
		return
	}

	c.selected = c.workspaces[index].Path
	c.cancelled = false
	c.ui.Quit()
}

func (c *pickerController) refreshMeta() {
	current := "outside workspace"
	if c.current >= 0 && c.current < len(c.workspaces) {
		current = c.workspaces[c.current].Name
	}

	setStaticText(c.meta, fmt.Sprintf("project=%s  root=%s  workspaces=%d  current=%s", c.cfg.Project, truncateWithDots(c.cfg.Root, 36), len(c.workspaces), current))
	setStaticText(c.listMeta, fmt.Sprintf("%d entries  |  * current cwd", len(c.workspaces)))
}

func (c *pickerController) openActionsDialog() {
	ws, _, ok := c.currentWorkspace()
	if !ok {
		c.setNotice("no workspace selected")
		return
	}

	builder := z.NewBuilder(c.ui.Theme())
	builder.
		Dialog("picker-actions", "Workspace actions").
		Flex("picker-actions-body", false, "stretch", 1).Padding(0, 1).
		Static("picker-actions-name", ws.Name).Font("bold").
		Static("picker-actions-path", truncateWithDots(ws.Path, 72)).Foreground("$fg1").
		Flex("picker-actions-buttons", true, "end", 2).
		Button("picker-actions-open", "Open").
		Button("picker-actions-delete", "Delete").
		Button("picker-actions-cancel", "Cancel").
		End().
		End()

	dialog := builder.Container()
	z.OnKey(dialog, func(_ z.Widget, ev *tcell.EventKey) bool {
		switch {
		case isEnterKey(ev), isOpenKey(ev):
			c.ui.Close()
			c.openWorkspace(c.list.Selected())
			return true
		case isDeleteRequest(ev):
			c.ui.Close()
			c.openDeleteDialog()
			return true
		case isCancelKey(ev):
			c.ui.Close()
			c.setNotice("action cancelled")
			return true
		default:
			return false
		}
	})
	z.Find(dialog, "picker-actions-open").On(z.EvtActivate, func(_ z.Widget, _ z.Event, _ ...any) bool {
		c.ui.Close()
		c.openWorkspace(c.list.Selected())
		return true
	})
	z.Find(dialog, "picker-actions-delete").On(z.EvtActivate, func(_ z.Widget, _ z.Event, _ ...any) bool {
		c.ui.Close()
		c.openDeleteDialog()
		return true
	})
	z.Find(dialog, "picker-actions-cancel").On(z.EvtActivate, func(_ z.Widget, _ z.Event, _ ...any) bool {
		c.ui.Close()
		c.setNotice("action cancelled")
		return true
	})

	c.ui.Popup(-1, -1, 0, 0, dialog)
}

func (c *pickerController) openDeleteDialog() {
	ws, index, ok := c.currentWorkspace()
	if !ok {
		c.setNotice("no workspace selected")
		return
	}

	builder := z.NewBuilder(c.ui.Theme())
	builder.
		Dialog("picker-delete", "Remove workspace?").
		Flex("picker-delete-body", false, "stretch", 1).Padding(0, 1).
		Static("picker-delete-name", ws.Name).Font("bold").
		Static("picker-delete-branch", fmt.Sprintf("branch: %s", fallbackWorkspaceBranch(ws))).Foreground("$fg1").
		Static("picker-delete-log", truncateWithDots(ws.Log, 72)).Foreground("$fg1").
		Static("picker-delete-path", truncateWithDots(ws.Path, 72)).Foreground("$fg1").
		Flex("picker-delete-buttons", true, "end", 2).
		Button("picker-delete-remove", "Remove").
		Button("picker-delete-force", "Force").
		Button("picker-delete-cancel", "Cancel").
		End().
		End()

	dialog := builder.Container()
	perform := func(force bool) bool {
		c.ui.Close()
		c.removeWorkspace(ws, index, force)
		return true
	}
	z.OnKey(dialog, func(_ z.Widget, ev *tcell.EventKey) bool {
		switch {
		case isEnterKey(ev), isYesKey(ev):
			return perform(false)
		case isForceKey(ev):
			return perform(true)
		case isCancelKey(ev):
			c.ui.Close()
			c.setNotice("delete cancelled")
			return true
		default:
			return false
		}
	})
	z.Find(dialog, "picker-delete-remove").On(z.EvtActivate, func(_ z.Widget, _ z.Event, _ ...any) bool {
		return perform(false)
	})
	z.Find(dialog, "picker-delete-force").On(z.EvtActivate, func(_ z.Widget, _ z.Event, _ ...any) bool {
		return perform(true)
	})
	z.Find(dialog, "picker-delete-cancel").On(z.EvtActivate, func(_ z.Widget, _ z.Event, _ ...any) bool {
		c.ui.Close()
		c.setNotice("delete cancelled")
		return true
	})

	c.ui.Popup(-1, -1, 0, 0, dialog)
}

func (c *pickerController) removeWorkspace(ws workspace, index int, force bool) {
	if err := removeWorkspacePath(c.cfg, ws.Path, force); err != nil {
		prefix := "delete failed"
		if force {
			prefix = "force delete failed"
		}
		c.setNotice(fmt.Sprintf("%s: %s", prefix, truncateWithDots(err.Error(), 72)))
		return
	}

	refreshed, err := collectWorkspaces(c.cfg)
	if err != nil {
		c.setNotice(fmt.Sprintf("removed %s; refresh failed: %s", ws.Name, truncateWithDots(err.Error(), 48)))
		return
	}

	c.workspaces = refreshed
	c.current = detectCurrentWorkspaceIndex(refreshed)
	c.list.SetItems(workspaceLines(refreshed, c.current))
	c.refreshMeta()
	if len(refreshed) == 0 {
		c.updateSelection(-1)
		if force {
			c.setNotice(fmt.Sprintf("removed %s (forced)", ws.Name))
		} else {
			c.setNotice(fmt.Sprintf("removed %s", ws.Name))
		}
		return
	}

	if index >= len(refreshed) {
		index = len(refreshed) - 1
	}
	c.list.Select(index)
	c.updateSelection(index)
	if force {
		c.setNotice(fmt.Sprintf("removed %s (forced)", ws.Name))
	} else {
		c.setNotice(fmt.Sprintf("removed %s", ws.Name))
	}
}

func (c *pickerController) refreshWorkspaces(preferredIndex int) {
	refreshed, err := collectWorkspaces(c.cfg)
	if err != nil {
		c.setNotice(fmt.Sprintf("refresh failed: %s", truncateWithDots(err.Error(), 64)))
		return
	}

	c.workspaces = refreshed
	c.current = detectCurrentWorkspaceIndex(refreshed)
	c.list.SetItems(workspaceLines(refreshed, c.current))
	c.refreshMeta()
	if len(refreshed) == 0 {
		c.updateSelection(-1)
		c.setNotice("no workspaces found")
		return
	}

	if preferredIndex < 0 {
		preferredIndex = 0
	}
	if preferredIndex >= len(refreshed) {
		preferredIndex = len(refreshed) - 1
	}
	c.list.Select(preferredIndex)
	c.updateSelection(preferredIndex)
	c.setNotice("workspace list refreshed")
}

func (c *pickerController) updateSelection(index int) {
	if index < 0 || index >= len(c.workspaces) {
		setStaticText(c.name, "No workspace selected")
		setStaticText(c.summary, "Pick workspace from left pane")
		setStaticText(c.path, "path: -")
		setStaticText(c.branch, "branch: -")
		setStaticText(c.commit, "commit: -")
		setStaticText(c.cwd, "cwd: -")
		setStaticText(c.footer, "Enter/o open  a actions  d/x delete  q quit")
		return
	}

	ws := c.workspaces[index]
	state := fmt.Sprintf("suffix=%02d", ws.Num)
	if index == c.current {
		state += "  |  current cwd"
	}
	setStaticText(c.name, ws.Name)
	setStaticText(c.summary, state)
	setStaticText(c.path, fmt.Sprintf("path: %s", truncateWithDots(ws.Path, 68)))
	setStaticText(c.branch, fmt.Sprintf("branch: %s", truncateWithDots(fallbackWorkspaceBranch(ws), 68)))
	setStaticText(c.commit, fmt.Sprintf("commit: %s", truncateWithDots(fallbackWorkspaceLog(ws), 68)))
	setStaticText(c.cwd, fmt.Sprintf("cwd: %s", currentWorkspaceLabel(index == c.current)))
	setStaticText(c.footer, "Enter/o open  a actions  d/x delete  r refresh  Tab move focus  q quit")
}

func (c *pickerController) setNotice(message string) {
	setStaticText(c.notice, message)
}

func setStaticText(label *z.Static, text string) {
	label.SetText(text)
	label.Refresh()
}

func fallbackWorkspaceBranch(ws workspace) string {
	if ws.Branch == "" {
		return "unknown"
	}
	return ws.Branch
}

func fallbackWorkspaceLog(ws workspace) string {
	if ws.Log == "" {
		return "no commits"
	}
	return truncateWithDots(ws.Log, 52)
}

func currentWorkspaceLabel(current bool) string {
	if current {
		return "selected workspace contains current shell"
	}
	return "selected workspace is not current shell"
}

func isOpenKey(ev *tcell.EventKey) bool {
	return isRuneKey(ev, 'o')
}

func isEnterKey(ev *tcell.EventKey) bool {
	return ev.Key() == tcell.KeyEnter || strings.EqualFold(ev.Name(), "Enter")
}

func isActionKey(ev *tcell.EventKey) bool {
	return isRuneKey(ev, 'a')
}

func isDeleteRequest(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyCtrlD, tcell.KeyDelete, tcell.KeyBackspace, tcell.KeyBackspace2:
		return true
	}

	return isRuneKey(ev, 'd') || isRuneKey(ev, 'x')
}

func isRefreshKey(ev *tcell.EventKey) bool {
	return isRuneKey(ev, 'r')
}

func isYesKey(ev *tcell.EventKey) bool {
	return isRuneKey(ev, 'y')
}

func isForceKey(ev *tcell.EventKey) bool {
	return isRuneKey(ev, 'f')
}

func isCancelKey(ev *tcell.EventKey) bool {
	if isRuneKey(ev, 'n') || isRuneKey(ev, 'q') {
		return true
	}

	switch ev.Key() {
	case tcell.KeyEscape, tcell.KeyCtrlC:
		return true
	default:
		return false
	}
}

func isRuneKey(ev *tcell.EventKey, expected rune) bool {
	if ev.Key() != tcell.KeyRune {
		return false
	}

	runes := []rune(ev.Str())
	if len(runes) == 0 {
		return false
	}

	return strings.EqualFold(ev.Str(), string(expected))
}

func switchPathCommand(cfg config, target string) (string, error) {
	return workspaces.SwitchPath(cfg, target)
}

func newCommand(cfg config, rawRef string) (string, error) {
	return workspaces.CreateNext(cfg, rawRef)
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

	target, err := workspaces.Remove(cfg, rawTarget, force)
	if err != nil {
		if !force {
			return fmt.Errorf("%w (retry with --force to remove a dirty worktree)", err)
		}
		return err
	}

	fmt.Fprintf(os.Stdout, "Removed workspace: %s\n", target.Name)
	return nil
}

func removeWorkspacePath(cfg config, workspacePath string, force bool) error {
	return workspaces.RemovePath(cfg, workspacePath, force)
}

func resolveBaseRef(rawRef string) string {
	return workspaces.ResolveBaseRef(rawRef)
}

func verifyRefExists(repoPath, ref string) error {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("base ref not found: %s (try: git -C %s fetch origin)", ref, repoPath)
	}
	return nil
}

func collectWorkspaces(cfg config) ([]workspace, error) {
	return workspaces.Collect(cfg)
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
	return workspaces.NormalizeTarget(project, target)
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
