package main

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEnvironmentAllocatesAroundBusyPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	busy := listener.Addr().(*net.TCPAddr).Port

	repo := t.TempDir()
	manager, err := newRuntimeManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := Context{ID: "demo-1234", Project: "demo", RootPath: repo, Path: repo, Config: Config{
		RuntimeDir: "local/demo",
		Services:   []Service{{Name: "web", Ports: map[string]int{"PORT": busy}, Command: "sleep 1"}},
	}}
	env, err := manager.ensureEnvironment(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if env["PORT"] == strconv.Itoa(busy) {
		t.Fatalf("busy port %d was reused", busy)
	}
	if env["DEV_RUNNER_DATA_DIR"] != filepath.Join(repo, "local/demo/data") {
		t.Fatalf("unexpected data dir %q", env["DEV_RUNNER_DATA_DIR"])
	}
	if env["DEV_RUNNER_ROOT"] != repo {
		t.Fatalf("unexpected root %q", env["DEV_RUNNER_ROOT"])
	}
}

func TestTmuxServicePersistsLogsAndStops(t *testing.T) {
	manager, err := newRuntimeManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := Context{ID: "test-" + strconv.FormatInt(time.Now().UnixNano(), 36), Project: "test", Path: t.TempDir()}
	svc := Service{Name: "worker", Command: "printf 'ready\\n'; sleep 30"}
	t.Cleanup(func() { _, _ = manager.Stop(ctx, svc) })

	if _, err := manager.Start(ctx, svc); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(manager.Logs(ctx, svc.Name), "ready") && manager.Status(ctx, svc) == "running" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !strings.Contains(manager.Logs(ctx, svc.Name), "ready") {
		t.Fatalf("service log was not persisted: %q", manager.Logs(ctx, svc.Name))
	}
	if _, err := manager.Stop(ctx, svc); err != nil {
		t.Fatal(err)
	}
	if got := manager.Status(ctx, svc); got != "stopped" {
		t.Fatalf("unexpected status after stop: %s", got)
	}
}

func TestExpandUsesAllocatedEnvironment(t *testing.T) {
	os.Setenv("DEV_RUNNER_TEST_FALLBACK", "fallback")
	t.Cleanup(func() { _ = os.Unsetenv("DEV_RUNNER_TEST_FALLBACK") })
	got := expand("redis://127.0.0.1:${REDIS_PORT}/$DEV_RUNNER_TEST_FALLBACK", map[string]string{"REDIS_PORT": "6381"})
	if got != "redis://127.0.0.1:6381/fallback" {
		t.Fatalf("unexpected expansion %q", got)
	}
}

func TestServiceStartsDependenciesAndWaitsForReadiness(t *testing.T) {
	manager, err := newRuntimeManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	ctx := Context{ID: "deps-" + strconv.FormatInt(time.Now().UnixNano(), 36), Project: "demo", Path: repo}
	dependency := Service{Name: "support", Command: "sleep 0.1; touch ready; sleep 30", ReadyCommand: "test -f ready"}
	web := Service{Name: "web", Command: "printf web-ready; sleep 30", DependsOn: []string{"support"}}
	ctx.Config.Services = []Service{dependency, web}
	t.Cleanup(func() {
		_, _ = manager.Stop(ctx, web)
		_, _ = manager.Stop(ctx, dependency)
	})

	if _, err := manager.Start(ctx, web); err != nil {
		t.Fatal(err)
	}
	if manager.Status(ctx, dependency) != "running" || manager.Status(ctx, web) != "running" {
		t.Fatalf("dependencies did not reach running: support=%s web=%s", manager.Status(ctx, dependency), manager.Status(ctx, web))
	}
}

func TestStatusUsesSessionSnapshotWithoutSpawningTmux(t *testing.T) {
	manager, err := newRuntimeManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := Context{ID: "cached-status", Project: "demo", Path: t.TempDir()}
	svc := Service{Name: "web", Command: "sleep 30"}
	manager.setSession(sessionName(ctx, svc.Name), true)
	if err := os.WriteFile(manager.statusFile(ctx, svc.Name), []byte("running"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager.tmuxPath = "/definitely/not/a/tmux/binary"
	for i := 0; i < 100; i++ {
		if got := manager.Status(ctx, svc); got != "running" {
			t.Fatalf("cached status changed on iteration %d: %s", i, got)
		}
	}
}
