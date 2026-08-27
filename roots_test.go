package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimePersistsAdditionalRoot(t *testing.T) {
	manager, err := newRuntimeManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	roots, err := manager.addRoot(nil, root)
	if err != nil || len(roots) != 1 || roots[0] != root {
		t.Fatalf("unexpected roots: %#v, %v", roots, err)
	}
	loaded := manager.roots(nil)
	found := false
	for _, value := range loaded {
		if value == root {
			found = true
		}
	}
	if !found {
		t.Fatalf("persisted root missing from %#v", loaded)
	}
}

func TestRepositoryCandidatesAcceptContainerAndDirectRepo(t *testing.T) {
	container := t.TempDir()
	repo := filepath.Join(container, "demo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	fromContainer := repositoryCandidates([]string{container})
	fromRepo := repositoryCandidates([]string{repo})
	if len(fromContainer) != 1 || fromContainer[0] != repo || len(fromRepo) != 1 || fromRepo[0] != repo {
		t.Fatalf("unexpected candidates: %#v %#v", fromContainer, fromRepo)
	}
}
