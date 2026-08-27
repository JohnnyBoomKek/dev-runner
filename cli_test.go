package main

import "testing"

func TestSelectContextRequiresUniqueProject(t *testing.T) {
	contexts := []Context{
		{ID: "demo-a", Project: "demo", Worktree: "main"},
		{ID: "demo-b", Project: "demo", Worktree: "feature"},
	}
	if _, err := selectContext(contexts, "demo"); err == nil {
		t.Fatal("expected ambiguous project selector")
	}
	ctx, err := selectContext(contexts, "demo/feature")
	if err != nil || ctx.ID != "demo-b" {
		t.Fatalf("unexpected selection: %#v, %v", ctx, err)
	}
}
