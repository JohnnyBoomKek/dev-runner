package main

import (
	"path/filepath"
	"testing"
)

func TestWizardPortRequiresPair(t *testing.T) {
	if _, err := wizardPort("PORT", ""); err == nil {
		t.Fatal("expected incomplete port declaration to fail")
	}
	ports, err := wizardPort("PORT", "8000")
	if err != nil || ports["PORT"] != 8000 {
		t.Fatalf("unexpected port result: %#v, %v", ports, err)
	}
}

func TestWizardEnvironmentParsesVariableReferences(t *testing.T) {
	env, err := wizardEnvironment("REDIS_URL=redis://127.0.0.1:${REDIS_PORT}/0; DEBUG=true")
	if err != nil || env["DEBUG"] != "true" || env["REDIS_URL"] != "redis://127.0.0.1:${REDIS_PORT}/0" {
		t.Fatalf("unexpected environment: %#v, %v", env, err)
	}
	if _, err := wizardEnvironment("not-an-assignment"); err == nil {
		t.Fatal("expected invalid environment to fail")
	}
}

func TestSaveWizardConfigCreatesLoadablePlugin(t *testing.T) {
	repo := t.TempDir()
	target := Context{Project: "demo", RootPath: repo, Worktree: "main", Path: repo}
	cfg := Config{Name: "Demo", Services: []Service{{Name: "web", Command: "echo ready"}}}
	msg := saveWizardConfig(target, cfg)()
	result, ok := msg.(wizardSavedMsg)
	if !ok || result.err != nil {
		t.Fatalf("unexpected save result: %#v", msg)
	}
	if result.path != filepath.Join(repo, ".runner", "config.yaml") {
		t.Fatalf("unexpected save path: %s", result.path)
	}
	loaded, _, err := loadConfig(repo, "")
	if err != nil || loaded.Name != "Demo" || len(loaded.Services) != 1 {
		t.Fatalf("generated plugin did not load: %#v, %v", loaded, err)
	}
}
