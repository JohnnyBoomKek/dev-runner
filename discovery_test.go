package main

import "testing"

func TestSlugAndContextIDAreStable(t *testing.T) {
	if got := slug("Lyrics2Learn / Feature Login"); got != "lyrics2learn-feature-login" {
		t.Fatalf("unexpected slug %q", got)
	}
	first := contextID("Lyrics2Learn", "/tmp/a")
	second := contextID("Lyrics2Learn", "/tmp/a")
	other := contextID("Lyrics2Learn", "/tmp/b")
	if first != second || first == other {
		t.Fatalf("context IDs are not stable and unique: %q %q %q", first, second, other)
	}
}

func TestConfiguredContextsHideUnconfiguredProjects(t *testing.T) {
	contexts := []Context{
		{Project: "configured", RootPath: "/tmp/configured", ConfigPath: "/tmp/config.yaml"},
		{Project: "hidden", RootPath: "/tmp/hidden"},
		{Project: "broken", RootPath: "/tmp/broken", ConfigErr: errForTest("bad plugin")},
	}
	visible := configuredContexts(contexts)
	if len(visible) != 2 || visible[0].Project != "configured" || visible[1].Project != "broken" {
		t.Fatalf("unexpected visible contexts: %#v", visible)
	}
	hidden := unconfiguredContexts(contexts)
	if len(hidden) != 1 || hidden[0].Project != "hidden" {
		t.Fatalf("unexpected hidden contexts: %#v", hidden)
	}
}

func TestGroupProjectsKeepsWorktreesUnderOneProject(t *testing.T) {
	contexts := []Context{
		{Project: "demo", RootPath: "/repo", Path: "/repo", Worktree: "main"},
		{Project: "demo", RootPath: "/repo", Path: "/tmp/feature", Worktree: "feature"},
	}
	groups := groupProjects(contexts)
	if len(groups) != 1 || len(groups[0].Worktrees) != 2 {
		t.Fatalf("unexpected groups: %#v", groups)
	}
}

type errForTest string

func (e errForTest) Error() string { return string(e) }
