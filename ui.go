package main

import (
	"sort"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type item struct {
	Name    string
	Kind    string
	Service *Service
	Action  *Action
}

type projectGroup struct {
	Name      string
	RootPath  string
	Worktrees []Context
}

type model struct {
	roots         []string
	runtime       *runtimeManager
	allContexts   []Context
	projects      []projectGroup
	projectIdx    int
	worktreeIdx   int
	items         []item
	itemIdx       int
	focus         int
	width         int
	height        int
	logs          string
	status        string
	wizard        *wizard
	inspector     bool
	inspectScroll int
}

type tickMsg time.Time
type operationMsg struct {
	status string
	logs   string
	err    error
}
type shellDoneMsg struct{ err error }

func newModel(roots []string, runtime *runtimeManager) model {
	contexts, err := discoverContexts(roots)
	m := model{roots: roots, runtime: runtime, allContexts: contexts, projects: groupProjects(configuredContexts(contexts))}
	if err != nil {
		m.status = err.Error()
	}
	m.sortWorktrees()
	m.rebuildItems()
	return m
}

func groupProjects(contexts []Context) []projectGroup {
	index := make(map[string]int)
	var groups []projectGroup
	for _, ctx := range contexts {
		position, ok := index[ctx.RootPath]
		if !ok {
			position = len(groups)
			index[ctx.RootPath] = position
			groups = append(groups, projectGroup{Name: ctx.Project, RootPath: ctx.RootPath})
		}
		groups[position].Worktrees = append(groups[position].Worktrees, ctx)
	}
	sort.Slice(groups, func(i, j int) bool { return strings.ToLower(groups[i].Name) < strings.ToLower(groups[j].Name) })
	return groups
}

func (m model) Init() tea.Cmd { return tickCmd() }

func tickCmd() tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) currentProject() *projectGroup {
	if m.projectIdx < 0 || m.projectIdx >= len(m.projects) {
		return nil
	}
	return &m.projects[m.projectIdx]
}

func (m *model) currentContext() *Context {
	project := m.currentProject()
	if project == nil || m.worktreeIdx < 0 || m.worktreeIdx >= len(project.Worktrees) {
		return nil
	}
	return &project.Worktrees[m.worktreeIdx]
}

func (m *model) rebuildItems() {
	m.items = nil
	ctx := m.currentContext()
	if ctx == nil || ctx.ConfigErr != nil {
		m.itemIdx = 0
		return
	}
	for i := range ctx.Config.Services {
		m.items = append(m.items, item{Name: ctx.Config.Services[i].Name, Kind: "service", Service: &ctx.Config.Services[i]})
	}
	for i := range ctx.Config.Actions {
		m.items = append(m.items, item{Name: ctx.Config.Actions[i].Name, Kind: "action", Action: &ctx.Config.Actions[i]})
	}
	if m.itemIdx >= len(m.items) {
		m.itemIdx = maxInt(0, len(m.items)-1)
	}
	m.refreshLogs()
}

func (m *model) refreshLogs() {
	ctx := m.currentContext()
	if ctx == nil || len(m.items) == 0 {
		m.logs = ""
		return
	}
	selected := m.items[m.itemIdx]
	name := selected.Name
	if selected.Kind == "action" {
		name = "action-" + name
	}
	m.logs = sanitizeTerminalText(m.runtime.Logs(*ctx, name))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if saved, ok := msg.(wizardSavedMsg); ok {
		if saved.err != nil {
			m.wizard.error = saved.err.Error()
			return m, nil
		}
		m.wizard = nil
		m.rescan()
		m.status = "created " + saved.path
		return m, nil
	}
	if added, ok := msg.(wizardAddRootMsg); ok {
		roots, err := m.runtime.addRoot(m.roots, added.path)
		if err != nil {
			m.wizard.error = err.Error()
			return m, nil
		}
		m.roots = roots
		m.rescan()
		m.wizard = newWizard(m.allContexts)
		m.status = "added discovery root " + added.path
		return m, nil
	}
	if m.wizard != nil {
		command, closeWizard := m.wizard.update(msg)
		if closeWizard {
			m.wizard = nil
			m.status = "plugin wizard closed"
		}
		return m, command
	}
	if m.inspector {
		if _, ok := msg.(tickMsg); ok {
			m.runtime.Refresh()
			m.refreshLogs()
			return m, tickCmd()
		}
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "i", "esc", "q":
				m.inspector = false
				m.inspectScroll = 0
			case "j", "down":
				m.inspectScroll++
			case "k", "up":
				m.inspectScroll = maxInt(0, m.inspectScroll-1)
			case "g", "home":
				m.inspectScroll = 0
			case "G", "end":
				m.inspectScroll = 1 << 20
			}
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		m.runtime.Refresh()
		m.sortWorktrees()
		m.refreshLogs()
		return m, tickCmd()
	case operationMsg:
		m.status = sanitizeTerminalText(msg.status)
		if msg.err != nil {
			m.status += ": " + sanitizeTerminalText(msg.err.Error())
		}
		if msg.logs != "" {
			m.logs = sanitizeTerminalText(msg.logs)
		}
		m.sortWorktrees()
		return m, nil
	case shellDoneMsg:
		if msg.err != nil {
			m.status = "shell exited: " + msg.err.Error()
		} else {
			m.status = "shell closed"
		}
		return m, nil
	case tea.KeyMsg:
		pressed := msg.String()
		if pressed == "q" || pressed == "ctrl+c" {
			return m, tea.Quit
		}
		if pressed == ":" {
			return m, m.openShell()
		}
		if pressed == "n" {
			m.wizard = newWizard(m.allContexts)
			return m, nil
		}
		if pressed == "i" {
			m.inspector = true
			m.inspectScroll = 0
			return m, nil
		}
		if command := m.actionShortcut(pressed); command != nil {
			return m, command
		}
		switch pressed {
		case "h", "left":
			m.focus = maxInt(0, m.focus-1)
		case "l", "right", "tab":
			m.focus = minInt(2, m.focus+1)
		case "j", "down":
			m.move(1)
		case "k", "up":
			m.move(-1)
		case "g", "home":
			m.jump(false)
		case "G", "end":
			m.jump(true)
		case "enter", "s":
			return m, m.activateSelected()
		case "x":
			return m, m.stopSelected()
		case "r":
			return m, m.restartSelected()
		case "S":
			return m, m.startAll()
		case "X":
			return m, m.stopAll()
		case "R":
			m.rescan()
			m.status = "rescanned projects and worktrees"
		}
	}
	return m, nil
}

func (m *model) rescan() {
	all, err := discoverContexts(m.roots)
	if err != nil {
		m.status = err.Error()
		return
	}
	selectedID := ""
	if ctx := m.currentContext(); ctx != nil {
		selectedID = ctx.ID
	}
	m.allContexts = all
	m.projects = groupProjects(configuredContexts(all))
	m.projectIdx, m.worktreeIdx = 0, 0
	for projectIdx := range m.projects {
		for worktreeIdx := range m.projects[projectIdx].Worktrees {
			if m.projects[projectIdx].Worktrees[worktreeIdx].ID == selectedID {
				m.projectIdx, m.worktreeIdx = projectIdx, worktreeIdx
			}
		}
	}
	m.itemIdx = 0
	m.sortWorktrees()
	m.rebuildItems()
}

func (m *model) sortWorktrees() {
	selectedID := ""
	if ctx := m.currentContext(); ctx != nil {
		selectedID = ctx.ID
	}
	for projectIdx := range m.projects {
		sort.SliceStable(m.projects[projectIdx].Worktrees, func(i, j int) bool {
			iRunning := worktreeRunning(m.runtime, m.projects[projectIdx].Worktrees[i])
			jRunning := worktreeRunning(m.runtime, m.projects[projectIdx].Worktrees[j])
			if iRunning != jRunning {
				return iRunning
			}
			return strings.ToLower(m.projects[projectIdx].Worktrees[i].Worktree) < strings.ToLower(m.projects[projectIdx].Worktrees[j].Worktree)
		})
	}
	if selectedID != "" {
		for projectIdx := range m.projects {
			for worktreeIdx := range m.projects[projectIdx].Worktrees {
				if m.projects[projectIdx].Worktrees[worktreeIdx].ID == selectedID {
					m.projectIdx, m.worktreeIdx = projectIdx, worktreeIdx
					return
				}
			}
		}
	}
}

func (m *model) move(delta int) {
	switch m.focus {
	case 0:
		m.projectIdx = clamp(m.projectIdx+delta, 0, maxInt(0, len(m.projects)-1))
		m.worktreeIdx, m.itemIdx = 0, 0
		m.rebuildItems()
	case 1:
		project := m.currentProject()
		if project != nil {
			m.worktreeIdx = clamp(m.worktreeIdx+delta, 0, maxInt(0, len(project.Worktrees)-1))
			m.itemIdx = 0
			m.rebuildItems()
		}
	case 2:
		m.itemIdx = clamp(m.itemIdx+delta, 0, maxInt(0, len(m.items)-1))
		m.refreshLogs()
	}
}

func (m *model) jump(end bool) {
	value := func(length int) int {
		if end {
			return maxInt(0, length-1)
		}
		return 0
	}
	switch m.focus {
	case 0:
		m.projectIdx = value(len(m.projects))
		m.worktreeIdx, m.itemIdx = 0, 0
		m.rebuildItems()
	case 1:
		if project := m.currentProject(); project != nil {
			m.worktreeIdx = value(len(project.Worktrees))
			m.itemIdx = 0
			m.rebuildItems()
		}
	case 2:
		m.itemIdx = value(len(m.items))
		m.refreshLogs()
	}
}

func (m model) activateSelected() tea.Cmd {
	ctx, selected := m.selection()
	if ctx == nil || selected == nil {
		return nil
	}
	if selected.Service != nil {
		return func() tea.Msg {
			status, err := m.runtime.Start(*ctx, *selected.Service)
			return operationMsg{status: status, err: err}
		}
	}
	return m.runAction(*ctx, *selected.Action)
}

func (m model) stopSelected() tea.Cmd {
	ctx, selected := m.selection()
	if ctx == nil || selected == nil || selected.Service == nil {
		return nil
	}
	return func() tea.Msg {
		status, err := m.runtime.Stop(*ctx, *selected.Service)
		return operationMsg{status: status, err: err}
	}
}

func (m model) restartSelected() tea.Cmd {
	ctx, selected := m.selection()
	if ctx == nil || selected == nil || selected.Service == nil {
		return nil
	}
	return func() tea.Msg {
		status, err := m.runtime.Restart(*ctx, *selected.Service)
		return operationMsg{status: status, err: err}
	}
}

func (m model) startAll() tea.Cmd {
	ctx := m.currentContext()
	if ctx == nil {
		return nil
	}
	return func() tea.Msg {
		var statuses []string
		for _, svc := range ctx.Config.Services {
			status, err := m.runtime.Start(*ctx, svc)
			if err != nil {
				return operationMsg{status: "start all stopped at " + svc.Name, err: err}
			}
			statuses = append(statuses, status)
		}
		return operationMsg{status: strings.Join(statuses, "; ")}
	}
}

func (m model) stopAll() tea.Cmd {
	ctx := m.currentContext()
	if ctx == nil {
		return nil
	}
	return func() tea.Msg {
		var statuses []string
		for i := len(ctx.Config.Services) - 1; i >= 0; i-- {
			status, err := m.runtime.Stop(*ctx, ctx.Config.Services[i])
			if err != nil {
				return operationMsg{status: "stop all stopped at " + ctx.Config.Services[i].Name, err: err}
			}
			statuses = append(statuses, status)
		}
		return operationMsg{status: strings.Join(statuses, "; ")}
	}
}

func (m model) runAction(ctx Context, action Action) tea.Cmd {
	return func() tea.Msg {
		logs, status, err := m.runtime.RunAction(ctx, action)
		return operationMsg{status: status, logs: logs, err: err}
	}
}

func (m model) actionShortcut(pressed string) tea.Cmd {
	ctx := m.currentContext()
	if ctx == nil {
		return nil
	}
	for _, action := range ctx.Config.Actions {
		if action.Key != "" && action.Key == pressed {
			return m.runAction(*ctx, action)
		}
	}
	return nil
}

func (m model) openShell() tea.Cmd {
	ctx := m.currentContext()
	if ctx == nil {
		return nil
	}
	cmd, err := m.runtime.Shell(*ctx)
	if err != nil {
		return func() tea.Msg { return shellDoneMsg{err: err} }
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return shellDoneMsg{err: err} })
}

func (m model) selection() (*Context, *item) {
	ctx := m.currentContext()
	if ctx == nil || len(m.items) == 0 || m.itemIdx >= len(m.items) {
		return ctx, nil
	}
	return ctx, &m.items[m.itemIdx]
}

func (m model) View() string {
	if m.wizard != nil {
		return fitFrame(m.wizard.view(m.width, m.height), m.width, m.height)
	}
	if m.inspector {
		return fitFrame(m.inspectorView(), m.width, m.height)
	}
	if len(m.projects) == 0 {
		return fitFrame("No configured projects found. Press n to add a root or create a project plugin, R to rescan, or q to quit.", m.width, m.height)
	}
	ctx := m.currentContext()
	viewportWidth, viewportHeight := m.width, m.height
	if viewportWidth <= 0 {
		viewportWidth = 80
	}
	if viewportHeight <= 0 {
		viewportHeight = 24
	}
	layoutHeight := maxInt(1, viewportHeight-3)
	// Never paint the terminal's final row or column. Touching either edge can
	// trigger terminal auto-wrap/scroll, desynchronizing Bubble Tea's diff
	// renderer and leaving fragments from earlier frames on screen.
	contentWidth := maxInt(1, viewportWidth-2)
	projectHeight := 4
	if layoutHeight < 9 {
		projectHeight = 3
	}

	projectLines := make([]string, len(m.projects))
	for i, project := range m.projects {
		running := 0
		for _, worktree := range project.Worktrees {
			if worktreeRunning(m.runtime, worktree) {
				running++
			}
		}
		label := project.Name + dimStyle.Render(" "+itoa(running)+"/"+itoa(len(project.Worktrees)))
		projectLines[i] = selectableLine(label, i == m.projectIdx, m.focus == 0)
	}
	projectsPane := paneState("Projects", strings.Join(projectLines, "   "), contentWidth, projectHeight, m.focus == 0, projectHasRunning(m.runtime, *m.currentProject()))

	project := m.currentProject()
	worktreeLines, selectedLine := m.worktreeLines(*project)
	serviceLines := m.serviceLines(ctx)

	// Header, projects, help, and status occupy fixed rows. The dashboard gets
	// every remaining row and switches to one focused column on narrow screens.
	dashboardHeight := maxInt(3, layoutHeight-(1+projectHeight+1+1))
	fullDashboard := layoutHeight >= 20 && dashboardHeight >= 12
	topHeight := dashboardHeight
	bottomHeight := 0
	if fullDashboard {
		topHeight = dashboardHeight / 2
		bottomHeight = dashboardHeight - topHeight
	}

	wide := contentWidth >= 72
	var top string
	if wide {
		worktreeWidth := contentWidth * 52 / 100
		servicesWidth := contentWidth - worktreeWidth - 1
		worktreesPane := paneState("Worktrees · active first", visibleLines(worktreeLines, selectedLine, topHeight-3), worktreeWidth, topHeight, m.focus == 1, worktreeRunning(m.runtime, *ctx))
		servicesPane := paneState("Services & actions", visibleLines(serviceLines, m.itemIdx, topHeight-3), servicesWidth, topHeight, m.focus == 2, worktreeRunning(m.runtime, *ctx))
		top = lipgloss.JoinHorizontal(lipgloss.Top, worktreesPane, " ", servicesPane)
	} else if m.focus == 2 {
		top = paneState("Services & actions", visibleLines(serviceLines, m.itemIdx, topHeight-3), contentWidth, topHeight, true, worktreeRunning(m.runtime, *ctx))
	} else {
		top = paneState("Worktrees · active first", visibleLines(worktreeLines, selectedLine, topHeight-3), contentWidth, topHeight, m.focus == 1, worktreeRunning(m.runtime, *ctx))
	}

	parts := []string{
		clipLines(titleStyle.Render("dev-runner")+"  "+selectedStyle.Render(ctx.Project+" / "+ctx.Worktree)+"  "+dimStyle.Render(ctx.Path), viewportWidth, 1),
		projectsPane,
		top,
	}
	if fullDashboard {
		logLines := strings.Split(strings.TrimRight(m.logs, "\n"), "\n")
		if wide {
			runtimeWidth := contentWidth * 56 / 100
			logsWidth := contentWidth - runtimeWidth - 1
			runtimePane := paneState("Runtime · safe environment", m.runtimeView(*ctx, bottomHeight-3), runtimeWidth, bottomHeight, false, worktreeRunning(m.runtime, *ctx))
			logsPane := paneState("Logs · "+selectedItemName(m.items, m.itemIdx), tailLines(logLines, bottomHeight-3), logsWidth, bottomHeight, false, worktreeRunning(m.runtime, *ctx))
			parts = append(parts, lipgloss.JoinHorizontal(lipgloss.Top, runtimePane, " ", logsPane))
		} else if m.focus == 2 {
			parts = append(parts, paneState("Logs · "+selectedItemName(m.items, m.itemIdx), tailLines(logLines, bottomHeight-3), contentWidth, bottomHeight, false, worktreeRunning(m.runtime, *ctx)))
		} else {
			parts = append(parts, paneState("Runtime · safe environment", m.runtimeView(*ctx, bottomHeight-3), contentWidth, bottomHeight, false, worktreeRunning(m.runtime, *ctx)))
		}
	}
	parts = append(parts,
		clipLines(dimStyle.Render("h/l pane  j/k move  enter/s start/run  x stop  r restart  S/X all  i inspect  : shell  n plugin  R rescan  q quit"), viewportWidth, 1),
		clipLines(statusStyle.Render(m.status), viewportWidth, 1),
	)
	return fitFrame(strings.Join(parts, "\n"), viewportWidth, viewportHeight)
}

func (m model) worktreeLines(project projectGroup) ([]string, int) {
	runningCount := 0
	for _, worktree := range project.Worktrees {
		if worktreeRunning(m.runtime, worktree) {
			runningCount++
		}
	}
	lines := make([]string, 0, len(project.Worktrees)+1)
	selectedLine := m.worktreeIdx
	for i, worktree := range project.Worktrees {
		if i == runningCount && runningCount > 0 && runningCount < len(project.Worktrees) {
			lines = append(lines, dividerStyle.Render("── inactive · "+itoa(len(project.Worktrees)-runningCount)+" ──"))
			if m.worktreeIdx >= i {
				selectedLine++
			}
		}
		running, total := serviceCounts(m.runtime, worktree)
		indicator := dimStyle.Render("○")
		if running > 0 {
			indicator = activeStyle.Render("●")
		}
		label := indicator + " " + worktree.Worktree + dimStyle.Render("  "+itoa(running)+"/"+itoa(total))
		lines = append(lines, selectableLine(label, i == m.worktreeIdx, m.focus == 1))
	}
	return lines, selectedLine
}

func (m model) serviceLines(ctx *Context) []string {
	if ctx == nil {
		return nil
	}
	if ctx.ConfigErr != nil {
		return []string{errorStyle.Render(ctx.ConfigErr.Error())}
	}
	lines := make([]string, 0, len(m.items))
	for i, candidate := range m.items {
		detail := candidate.Kind
		if candidate.Service != nil {
			detail = m.runtime.Status(*ctx, *candidate.Service)
		} else if candidate.Action != nil && candidate.Action.Key != "" {
			detail = "action [" + candidate.Action.Key + "]"
		}
		lines = append(lines, selectableLine(candidate.Name+"  "+dimStyle.Render(detail), i == m.itemIdx, m.focus == 2))
	}
	return lines
}

func (m model) runtimeView(ctx Context, height int) string {
	lines := []string{
		labelStyle.Render("BRANCH") + " " + ctx.Worktree,
	}
	env := m.runtime.Environment(ctx)
	for _, key := range environmentKeys(env, false) {
		value := env[key]
		if sensitiveName(key) {
			value = "••••••"
		}
		lines = append(lines, labelStyle.Render(key)+"="+value)
	}
	return visibleLines(lines, 0, height)
}

func (m model) inspectorView() string {
	ctx := m.currentContext()
	if ctx == nil {
		return "No selected worktree"
	}
	lines := []string{
		titleStyle.Render("WORKTREE IDENTITY"),
		labelStyle.Render("PROJECT") + " " + ctx.Project,
		labelStyle.Render("BRANCH") + " " + ctx.Worktree,
		labelStyle.Render("ROOT") + " " + ctx.RootPath,
		labelStyle.Render("PATH") + " " + ctx.Path,
		labelStyle.Render("RUNTIME ID") + " " + ctx.ID,
		labelStyle.Render("PLUGIN") + " " + ctx.ConfigPath,
		"",
		titleStyle.Render("SAFE RUNTIME ENVIRONMENT"),
	}
	env := m.runtime.Environment(*ctx)
	for _, key := range environmentKeys(env, true) {
		value := env[key]
		if sensitiveName(key) {
			value = "••••••"
		}
		lines = append(lines, labelStyle.Render(key)+"="+value)
	}
	lines = append(lines, "", titleStyle.Render("SERVICES"))
	for _, svc := range ctx.Config.Services {
		line := labelStyle.Render(svc.Name) + " " + m.runtime.Status(*ctx, svc)
		if len(svc.DependsOn) > 0 {
			line += dimStyle.Render("  depends: " + strings.Join(svc.DependsOn, ", "))
		}
		lines = append(lines, line, dimStyle.Render("  "+svc.Command))
	}
	lines = append(lines, "", titleStyle.Render("SELECTED LOG"))
	lines = append(lines, strings.Split(strings.TrimRight(m.logs, "\n"), "\n")...)

	available := maxInt(4, m.height-8)
	maxStart := maxInt(0, len(lines)-available)
	start := clamp(m.inspectScroll, 0, maxStart)
	body := strings.Join(lines[start:minInt(len(lines), start+available)], "\n")
	footer := dimStyle.Render("j/k scroll  g/G ends  i/Esc close")
	box := lipgloss.NewStyle().Width(maxInt(40, m.width-6)).Height(maxInt(10, m.height-4)).Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("#38bdf8")).Padding(0, 1).Render(titleStyle.Render("Runtime inspector") + "\n" + body + "\n" + footer)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func environmentKeys(env map[string]string, includeInternal bool) []string {
	priority := []string{"DEV_RUNNER_URL", "DEV_RUNNER_WDS_URL", "PORT", "WDS_PORT", "FLOSHIP_DJANGO_PORT", "FLOSHIP_WDS_PORT", "POSTGRES_HOST", "POSTGRES_PORT", "POSTGRES_DATABASE", "REDIS_HOST", "REDIS_PORT", "RABBITMQ_HOST", "RABBITMQ_PORT", "FLOSHIP_COMPOSE_PROJECT", "DEV_RUNNER_DATA_DIR"}
	seen := make(map[string]bool)
	keys := make([]string, 0, len(env))
	for _, key := range priority {
		if _, ok := env[key]; ok {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	var rest []string
	for key := range env {
		if seen[key] || (!includeInternal && strings.HasPrefix(key, "DEV_RUNNER_")) {
			continue
		}
		rest = append(rest, key)
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

func sensitiveName(key string) bool {
	upper := strings.ToUpper(key)
	for _, fragment := range []string{"PASSWORD", "SECRET", "TOKEN", "API_KEY", "PRIVATE_KEY"} {
		if strings.Contains(upper, fragment) {
			return true
		}
	}
	return false
}

func sanitizeTerminalText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(value))
}

func worktreeRunning(runtime *runtimeManager, ctx Context) bool {
	running, _ := serviceCounts(runtime, ctx)
	return running > 0
}

func serviceCounts(runtime *runtimeManager, ctx Context) (int, int) {
	running := 0
	for _, svc := range ctx.Config.Services {
		if status := runtime.Status(ctx, svc); status == "running" || status == "starting" {
			running++
		}
	}
	return running, len(ctx.Config.Services)
}

func projectHasRunning(runtime *runtimeManager, project projectGroup) bool {
	for _, worktree := range project.Worktrees {
		if worktreeRunning(runtime, worktree) {
			return true
		}
	}
	return false
}

func selectedItemName(items []item, selected int) string {
	if selected < 0 || selected >= len(items) {
		return "idle"
	}
	return items[selected].Name
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

func pane(title, body string, width, height int, focused bool) string {
	return paneState(title, body, width, height, focused, false)
}

func paneState(title, body string, width, height int, focused, active bool) string {
	border := lipgloss.Color("#334155")
	if active {
		border = lipgloss.Color("#22c55e")
	}
	if focused {
		border = lipgloss.Color("#38bdf8")
	}
	// Width and height are outer dimensions. Clip each ANSI-styled line before
	// rendering: Lip Gloss's Width wraps long logs, while Height is a minimum,
	// so an unbounded line can otherwise grow a panel past the terminal.
	innerWidth := maxInt(1, width-6)
	innerHeight := maxInt(1, height-2)
	content := clipLines(titleStyle.Render(title), innerWidth, 1)
	if innerHeight > 1 && body != "" {
		content += "\n" + clipLines(body, innerWidth, innerHeight-1)
	}
	style := lipgloss.NewStyle().
		Width(maxInt(1, width-4)).
		Height(innerHeight).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1)
	return style.Render(content)
}

func clipLines(value string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(value, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	clipper := lipgloss.NewStyle().Inline(true).MaxWidth(width)
	for i := range lines {
		lines[i] = clipper.Render(lines[i])
	}
	return strings.Join(lines, "\n")
}

func fitFrame(frame string, width, height int) string {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	renderHeight := maxInt(1, height-3)
	if len(lines) > renderHeight {
		lines = lines[:renderHeight]
	}
	renderWidth := maxInt(1, width-2)
	for i := range lines {
		lines[i] = clipLines(lines[i], renderWidth, 1)
	}
	return strings.Join(lines, "\n")
}

func selectableLine(value string, selected, focused bool) string {
	prefix := "  "
	if selected {
		prefix = "▸ "
		if focused {
			return selectedStyle.Render(prefix + value)
		}
	}
	return prefix + value
}

func visibleLines(lines []string, selected, height int) string {
	if height <= 0 || len(lines) == 0 {
		return ""
	}
	if len(lines) <= height {
		return strings.Join(lines, "\n")
	}
	start := selected - height/2
	start = clamp(start, 0, len(lines)-height)
	return strings.Join(lines[start:start+height], "\n")
}

func tailLines(lines []string, height int) string {
	if height <= 0 {
		return ""
	}
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	return strings.Join(lines, "\n")
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#f9a8d4"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#64748b"))
	statusStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#86efac"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7dd3fc"))
	labelStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#c4b5fd"))
	activeStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4ade80"))
	dividerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8"))
)
