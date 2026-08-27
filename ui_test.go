package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestSortWorktreesPutsRunningFirstAndAddsInactiveDivider(t *testing.T) {
	runtime, err := newRuntimeManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := Service{Name: "web", Command: "true"}
	inactive := Context{ID: "inactive", Project: "demo", Worktree: "aaa", Config: Config{Services: []Service{service}}}
	active := Context{ID: "active", Project: "demo", Worktree: "zzz", Config: Config{Services: []Service{service}}}
	runtime.setSession(sessionName(active, service.Name), true)
	if err := os.WriteFile(runtime.statusFile(active, service.Name), []byte("running"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := model{runtime: runtime, projects: []projectGroup{{Name: "demo", Worktrees: []Context{inactive, active}}}}
	m.sortWorktrees()
	if got := m.projects[0].Worktrees[0].ID; got != active.ID {
		t.Fatalf("first worktree = %q, want running worktree %q", got, active.ID)
	}
	lines, selected := m.worktreeLines(m.projects[0])
	rendered := strings.Join(lines, "\n")
	if activeAt, dividerAt, inactiveAt := strings.Index(rendered, "zzz"), strings.Index(rendered, "inactive · 1"), strings.Index(rendered, "aaa"); !(activeAt < dividerAt && dividerAt < inactiveAt) {
		t.Fatalf("unexpected worktree sections:\n%s", rendered)
	}
	if selected != 2 {
		t.Fatalf("selected display line = %d, want 2 after the inserted divider", selected)
	}
	if got := m.currentContext().ID; got != inactive.ID {
		t.Fatalf("selection moved to %q during sort, want %q", got, inactive.ID)
	}
}

func TestClipLinesBoundsStyledContent(t *testing.T) {
	got := clipLines(selectedStyle.Render(strings.Repeat("x", 40))+"\nsecond\nthird", 12, 2)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("line count = %d, want 2", len(lines))
	}
	for _, line := range lines {
		if width := lipgloss.Width(line); width > 12 {
			t.Fatalf("line width = %d, want <= 12", width)
		}
	}
}

func TestSanitizeTerminalTextRemovesCursorControl(t *testing.T) {
	input := "before\x1b[2J\x1b[1;1Hafter\rreturn\b\a\nnext"
	got := sanitizeTerminalText(input)
	if got != "beforeafterreturn\nnext" {
		t.Fatalf("sanitized text = %q", got)
	}
	if strings.ContainsRune(got, '\x1b') {
		t.Fatal("escape byte remained in sanitized text")
	}
}

func TestViewNeverExceedsTerminal(t *testing.T) {
	baseDir := t.TempDir()
	contextPath := t.TempDir()
	runtime := &runtimeManager{
		baseDir:   baseDir,
		statePath: filepath.Join(baseDir, "state.json"),
		state:     runtimeState{Ports: map[string]map[string]int{}},
		sessions:  map[string]bool{},
	}
	service := Service{Name: "development-server-with-a-long-name", Command: "true", Ports: map[string]int{"PORT": 8000}}
	ctx := Context{
		ID:       "viewport",
		Project:  "project-with-a-long-name",
		Worktree: "feature/a-very-long-worktree-name-that-must-never-break-the-layout",
		Path:     contextPath,
		Config:   Config{Services: []Service{service}},
	}
	m := model{
		runtime:  runtime,
		projects: []projectGroup{{Name: ctx.Project, Worktrees: []Context{ctx}}},
		status:   strings.Repeat("status ", 30),
		logs:     strings.Repeat("a very long log line ", 30),
	}
	m.rebuildItems()

	for _, size := range []struct{ width, height int }{{40, 12}, {60, 16}, {80, 20}, {90, 24}, {120, 32}, {200, 50}} {
		m.width, m.height = size.width, size.height
		frame := strings.TrimRight(m.View(), "\n")
		lines := strings.Split(frame, "\n")
		if len(lines) > maxInt(1, size.height-3) {
			t.Errorf("%dx%d frame has %d lines; renderer gutter exceeded", size.width, size.height, len(lines))
		}
		for lineNumber, line := range lines {
			if width := lipgloss.Width(line); width > maxInt(1, size.width-2) {
				t.Errorf("%dx%d line %d has width %d; renderer gutter exceeded", size.width, size.height, lineNumber+1, width)
			}
		}
	}
}
