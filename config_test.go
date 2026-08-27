package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigFromLocalPlugin(t *testing.T) {
	repo := t.TempDir()
	localDir := filepath.Join(repo, "local", "demo")
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plugin := `name: Demo
runtime_dir: local/demo
services:
  - name: redis
    compose:
      file: local/demo/compose.yaml
      services: [redis]
    ports:
      REDIS_PORT: 6380
actions:
  - name: migrate
    key: m
    command: echo migrate
`
	if err := os.WriteFile(filepath.Join(localDir, "runner.yaml"), []byte(plugin), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, path, err := loadConfig(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "Demo" || cfg.RuntimeDir != "local/demo" || len(cfg.Services) != 1 || len(cfg.Actions) != 1 {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if !strings.HasSuffix(path, "local/demo/runner.yaml") {
		t.Fatalf("unexpected config path: %s", path)
	}
}

func TestConfigRejectsReservedActionKey(t *testing.T) {
	err := validateConfig(t.TempDir(), Config{Actions: []Action{{Name: "quit", Key: "q", Command: "echo no"}}})
	if err == nil || !strings.Contains(err.Error(), "reserved key") {
		t.Fatalf("expected reserved-key error, got %v", err)
	}
}
