package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

type wizardStage int

const (
	wizardTarget wizardStage = iota
	wizardRoot
	wizardName
	wizardMenu
	wizardForm
)

type wizardFormKind int

const (
	formCommand wizardFormKind = iota
	formCompose
	formAction
)

type wizard struct {
	stage      wizardStage
	targets    []Context
	targetIdx  int
	config     Config
	nameInput  textinput.Model
	rootInput  textinput.Model
	menuIdx    int
	formKind   wizardFormKind
	formInputs []textinput.Model
	formIdx    int
	error      string
}

type wizardSavedMsg struct {
	path string
	err  error
}

type wizardAddRootMsg struct{ path string }

func newWizard(contexts []Context) *wizard {
	input := textinput.New()
	input.Prompt = "Plugin name: "
	input.CharLimit = 80
	rootInput := textinput.New()
	rootInput.Prompt = "Directory or Git repository: "
	rootInput.Placeholder = "~/Code"
	rootInput.CharLimit = 500
	return &wizard{stage: wizardTarget, targets: unconfiguredContexts(contexts), nameInput: input, rootInput: rootInput}
}

func (w *wizard) update(msg tea.Msg) (tea.Cmd, bool) {
	key, isKey := msg.(tea.KeyMsg)
	if isKey && key.String() == "esc" {
		if w.stage == wizardForm {
			w.stage = wizardMenu
			w.error = ""
			return nil, false
		}
		if w.stage == wizardRoot {
			w.stage = wizardTarget
			w.error = ""
			return nil, false
		}
		return nil, true
	}

	switch w.stage {
	case wizardTarget:
		if !isKey {
			return nil, false
		}
		if key.String() == "a" {
			w.stage = wizardRoot
			w.rootInput.SetValue("")
			w.rootInput.Focus()
			w.error = ""
			return textinput.Blink, false
		}
		if len(w.targets) == 0 {
			return nil, false
		}
		switch key.String() {
		case "j", "down":
			w.targetIdx = clamp(w.targetIdx+1, 0, len(w.targets)-1)
		case "k", "up":
			w.targetIdx = clamp(w.targetIdx-1, 0, len(w.targets)-1)
		case "g", "home":
			w.targetIdx = 0
		case "G", "end":
			w.targetIdx = len(w.targets) - 1
		case "enter":
			w.stage = wizardName
			w.nameInput.SetValue(w.targets[w.targetIdx].Project)
			w.nameInput.CursorEnd()
			w.nameInput.Focus()
			return textinput.Blink, false
		}
	case wizardRoot:
		if isKey && key.String() == "enter" {
			path := strings.TrimSpace(w.rootInput.Value())
			if path == "" {
				w.error = "Directory is required"
				return nil, false
			}
			return func() tea.Msg { return wizardAddRootMsg{path: path} }, false
		}
		var cmd tea.Cmd
		w.rootInput, cmd = w.rootInput.Update(msg)
		return cmd, false
	case wizardName:
		if isKey && key.String() == "enter" {
			name := strings.TrimSpace(w.nameInput.Value())
			if name == "" {
				w.error = "Plugin name is required"
				return nil, false
			}
			w.config.Name = name
			w.stage = wizardMenu
			w.error = ""
			return nil, false
		}
		var cmd tea.Cmd
		w.nameInput, cmd = w.nameInput.Update(msg)
		return cmd, false
	case wizardMenu:
		if !isKey {
			return nil, false
		}
		switch key.String() {
		case "j", "down":
			w.menuIdx = clamp(w.menuIdx+1, 0, 3)
		case "k", "up":
			w.menuIdx = clamp(w.menuIdx-1, 0, 3)
		case "enter":
			switch w.menuIdx {
			case 0:
				w.beginForm(formCommand)
			case 1:
				w.beginForm(formCompose)
			case 2:
				w.beginForm(formAction)
			case 3:
				if len(w.config.Services) == 0 && len(w.config.Actions) == 0 {
					w.error = "Add at least one service or action"
					return nil, false
				}
				target := w.targets[w.targetIdx]
				cfg := w.config
				return saveWizardConfig(target, cfg), false
			}
		}
	case wizardForm:
		if isKey && key.String() == "enter" {
			if w.formIdx < len(w.formInputs)-1 {
				w.formInputs[w.formIdx].Blur()
				w.formIdx++
				w.formInputs[w.formIdx].Focus()
				return textinput.Blink, false
			}
			if err := w.commitForm(); err != nil {
				w.error = err.Error()
				return nil, false
			}
			w.stage = wizardMenu
			w.error = ""
			return nil, false
		}
		var cmd tea.Cmd
		w.formInputs[w.formIdx], cmd = w.formInputs[w.formIdx].Update(msg)
		return cmd, false
	}
	return nil, false
}

func (w *wizard) beginForm(kind wizardFormKind) {
	w.stage = wizardForm
	w.formKind = kind
	w.formIdx = 0
	w.error = ""
	var fields []struct {
		prompt      string
		placeholder string
	}
	switch kind {
	case formCommand:
		fields = []struct{ prompt, placeholder string }{
			{"Service name: ", "web"},
			{"Command: ", "npm run dev"},
			{"Environment (optional, KEY=value;...): ", "API_URL=http://127.0.0.1:${API_PORT}"},
			{"Port variable (optional): ", "PORT"},
			{"Preferred port (optional): ", "8000"},
		}
	case formCompose:
		fields = []struct{ prompt, placeholder string }{
			{"Service name: ", "redis"},
			{"Compose file: ", "compose.yaml"},
			{"Compose services (comma-separated): ", "redis"},
			{"Port variable (optional): ", "REDIS_PORT"},
			{"Preferred port (optional): ", "6379"},
		}
	case formAction:
		fields = []struct{ prompt, placeholder string }{
			{"Action name: ", "migrate"},
			{"Command: ", "python manage.py migrate"},
			{"Environment (optional, KEY=value;...): ", "DATABASE_URL=sqlite:///local.db"},
			{"Shortcut (optional): ", "m"},
		}
	}
	w.formInputs = make([]textinput.Model, len(fields))
	for i, field := range fields {
		input := textinput.New()
		input.Prompt = field.prompt
		input.Placeholder = field.placeholder
		input.CharLimit = 500
		w.formInputs[i] = input
	}
	w.formInputs[0].Focus()
}

func (w *wizard) commitForm() error {
	values := make([]string, len(w.formInputs))
	for i := range w.formInputs {
		values[i] = strings.TrimSpace(w.formInputs[i].Value())
	}
	switch w.formKind {
	case formCommand:
		if values[0] == "" || values[1] == "" {
			return fmt.Errorf("service name and command are required")
		}
		env, err := wizardEnvironment(values[2])
		if err != nil {
			return err
		}
		ports, err := wizardPort(values[3], values[4])
		if err != nil {
			return err
		}
		w.config.Services = append(w.config.Services, Service{Name: values[0], Command: values[1], Env: env, Ports: ports})
	case formCompose:
		if values[0] == "" || values[1] == "" {
			return fmt.Errorf("service name and Compose file are required")
		}
		ports, err := wizardPort(values[3], values[4])
		if err != nil {
			return err
		}
		var services []string
		for _, value := range strings.Split(values[2], ",") {
			if value = strings.TrimSpace(value); value != "" {
				services = append(services, value)
			}
		}
		w.config.Services = append(w.config.Services, Service{Name: values[0], Compose: &Compose{File: values[1], Services: services}, Ports: ports})
	case formAction:
		if values[0] == "" || values[1] == "" {
			return fmt.Errorf("action name and command are required")
		}
		env, err := wizardEnvironment(values[2])
		if err != nil {
			return err
		}
		if len(values[3]) > 1 {
			return fmt.Errorf("shortcut must be one character")
		}
		w.config.Actions = append(w.config.Actions, Action{Name: values[0], Command: values[1], Env: env, Key: values[3]})
	}
	target := w.targets[w.targetIdx]
	return validateConfig(target.Path, w.config)
}

func wizardEnvironment(value string) (map[string]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	env := make(map[string]string)
	for _, entry := range strings.Split(value, ";") {
		key, itemValue, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if !ok || !envNamePattern.MatchString(strings.TrimSpace(key)) {
			return nil, fmt.Errorf("environment must use KEY=value entries separated by semicolons")
		}
		env[strings.TrimSpace(key)] = strings.TrimSpace(itemValue)
	}
	return env, nil
}

func wizardPort(name, preferred string) (map[string]int, error) {
	if name == "" && preferred == "" {
		return nil, nil
	}
	if name == "" || preferred == "" {
		return nil, fmt.Errorf("port variable and preferred port must be provided together")
	}
	if !envNamePattern.MatchString(name) {
		return nil, fmt.Errorf("invalid port variable %q", name)
	}
	port, err := strconv.Atoi(preferred)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("preferred port must be between 1 and 65535")
	}
	return map[string]int{name: port}, nil
}

func saveWizardConfig(target Context, cfg Config) tea.Cmd {
	return func() tea.Msg {
		path := filepath.Join(target.RootPath, ".runner", "config.yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return wizardSavedMsg{path: path, err: err}
		}
		data, err := yaml.Marshal(cfg)
		if err == nil {
			err = os.WriteFile(path, data, 0o644)
		}
		return wizardSavedMsg{path: path, err: err}
	}
}

func (w *wizard) view(width, height int) string {
	var body strings.Builder
	switch w.stage {
	case wizardTarget:
		if len(w.targets) == 0 {
			body.WriteString("Every discovered project/worktree already has a plugin.\n\n")
		} else {
			body.WriteString("Choose a project to configure once for all of its worktrees:\n\n")
		}
		lines := make([]string, len(w.targets))
		for i, target := range w.targets {
			lines[i] = selectableLine(wizardTargetLabel(target), i == w.targetIdx, true)
		}
		body.WriteString(visibleLines(lines, w.targetIdx, maxInt(4, height-10)))
		body.WriteString("\n\nEnter  select   a  add directory/repository   Esc  cancel")
	case wizardRoot:
		body.WriteString("Add another project directory or a Git repository. This location is persisted.\n\n")
		body.WriteString(w.rootInput.View())
		body.WriteString("\n\nEnter  add and scan   Esc  back")
	case wizardName:
		body.WriteString("Project: " + wizardTargetLabel(w.targets[w.targetIdx]) + "\n\n")
		body.WriteString(w.nameInput.View())
		body.WriteString("\n\nEnter  continue   Esc  cancel")
	case wizardMenu:
		body.WriteString("Project: " + wizardTargetLabel(w.targets[w.targetIdx]) + "\n")
		body.WriteString(fmt.Sprintf("Plugin: %s · %d services · %d actions\n\n", w.config.Name, len(w.config.Services), len(w.config.Actions)))
		options := []string{"Add command service", "Add Compose service", "Add action", "Save plugin"}
		for i, option := range options {
			body.WriteString(selectableLine(option, i == w.menuIdx, true) + "\n")
		}
		body.WriteString("\nj/k  move   Enter  select   Esc  cancel")
	case wizardForm:
		body.WriteString("Complete each field and press Enter. Placeholder text is only an example.\n\n")
		for i := range w.formInputs {
			line := w.formInputs[i].View()
			if i == w.formIdx {
				line = selectedStyle.Render("▸ ") + line
			} else {
				line = "  " + line
			}
			body.WriteString(line + "\n")
		}
		body.WriteString("\nEnter  next/save item   Esc  back")
	}
	if w.error != "" {
		body.WriteString("\n\n" + errorStyle.Render(w.error))
	}
	return wizardPane("Create runner plugin", body.String(), width, height)
}

func wizardPane(title, body string, width, height int) string {
	boxWidth := minInt(88, maxInt(50, width-8))
	boxHeight := minInt(28, maxInt(14, height-4))
	box := lipgloss.NewStyle().Width(boxWidth).Height(boxHeight).Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("#c084fc")).Padding(1, 2).Render(titleStyle.Render(title) + "\n\n" + body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func wizardTargetLabel(target Context) string {
	return target.Project + "  " + dimStyle.Render(target.RootPath)
}
