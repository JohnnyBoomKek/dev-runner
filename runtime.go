package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type runtimeState struct {
	Ports map[string]map[string]int `json:"ports"`
}

type runtimeManager struct {
	baseDir   string
	statePath string
	tmuxPath  string
	mu        sync.Mutex
	state     runtimeState
	sessionMu sync.RWMutex
	sessions  map[string]bool
}

func newRuntimeManager(baseDir string) (*runtimeManager, error) {
	if baseDir == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		baseDir = filepath.Join(configDir, "dev-runner")
	}
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return nil, errors.New("dev-runner requires tmux (brew install tmux)")
	}
	for _, dir := range []string{baseDir, filepath.Join(baseDir, "logs"), filepath.Join(baseDir, "status")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	m := &runtimeManager{
		baseDir:   baseDir,
		statePath: filepath.Join(baseDir, "state.json"),
		tmuxPath:  tmuxPath,
		state:     runtimeState{Ports: make(map[string]map[string]int)},
		sessions:  make(map[string]bool),
	}
	if data, err := os.ReadFile(m.statePath); err == nil {
		_ = json.Unmarshal(data, &m.state)
	}
	if m.state.Ports == nil {
		m.state.Ports = make(map[string]map[string]int)
	}
	m.refreshSessions()
	return m, nil
}

func (m *runtimeManager) ensureEnvironment(ctx Context, verifyPorts bool) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	assigned := m.state.Ports[ctx.ID]
	if assigned == nil {
		assigned = make(map[string]int)
		m.state.Ports[ctx.ID] = assigned
	}
	reserved := make(map[int]bool)
	for contextID, ports := range m.state.Ports {
		if contextID == ctx.ID {
			continue
		}
		for _, port := range ports {
			reserved[port] = true
		}
	}

	changed := false
	for _, svc := range ctx.Config.Services {
		ownerActive := m.Status(ctx, svc) == "running" || m.Status(ctx, svc) == "starting"
		for _, name := range sortedPortNames(svc.Ports) {
			preferred := svc.Ports[name]
			current := assigned[name]
			if current == 0 || reserved[current] || (verifyPorts && !ownerActive && !portAvailable(current)) {
				port, err := allocatePort(preferred, reserved)
				if err != nil {
					return nil, err
				}
				assigned[name] = port
				current = port
				changed = true
			}
			reserved[current] = true
		}
	}
	if changed {
		if err := m.saveState(); err != nil {
			return nil, err
		}
	}

	runtimeDir := ctx.Config.RuntimeDir
	if runtimeDir == "" {
		runtimeDir = ".runner"
	}
	dataDir := filepath.Join(ctx.Path, runtimeDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	env := map[string]string{
		"DEV_RUNNER_CONTEXT_ID": ctx.ID,
		"DEV_RUNNER_PROJECT":    ctx.Project,
		"DEV_RUNNER_WORKTREE":   ctx.Path,
		"DEV_RUNNER_ROOT":       ctx.RootPath,
		"DEV_RUNNER_DATA_DIR":   dataDir,
	}
	for name, port := range assigned {
		env[name] = strconv.Itoa(port)
	}
	return env, nil
}

func (m *runtimeManager) Start(ctx Context, svc Service) (string, error) {
	m.refreshSessions()
	return m.start(ctx, svc, make(map[string]bool))
}

func (m *runtimeManager) start(ctx Context, svc Service, stack map[string]bool) (string, error) {
	status := m.Status(ctx, svc)
	if status == "running" || status == "starting" {
		return svc.Name + " is already " + status, nil
	}
	if stack[svc.Name] {
		return "", fmt.Errorf("dependency cycle at %s", svc.Name)
	}
	stack[svc.Name] = true
	defer delete(stack, svc.Name)
	for _, dependencyName := range svc.DependsOn {
		dependency, ok := serviceByName(ctx.Config, dependencyName)
		if !ok {
			return "", fmt.Errorf("service %s dependency %s not found", svc.Name, dependencyName)
		}
		if _, err := m.start(ctx, dependency, stack); err != nil {
			return "", err
		}
	}
	if m.hasSession(sessionName(ctx, svc.Name)) {
		_ = exec.Command(m.tmuxPath, "kill-session", "-t", sessionName(ctx, svc.Name)).Run()
	}
	env, err := m.ensureEnvironment(ctx, true)
	if err != nil {
		return "", err
	}
	for key, value := range svc.Env {
		env[key] = expand(value, env)
	}
	command := svc.Command
	if svc.Compose != nil {
		command = composeCommand(ctx, *svc.Compose, "up")
	}
	command = expand(command, env)

	logPath := m.logPath(ctx, svc.Name)
	statusPath := m.statusFile(ctx, svc.Name)
	_ = os.Remove(statusPath)
	script := buildWrapper(command, env, logPath, statusPath)
	cmd := exec.Command(m.tmuxPath, "new-session", "-d", "-s", sessionName(ctx, svc.Name), "-c", ctx.Path, script)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("start %s: %w: %s", svc.Name, err, strings.TrimSpace(string(output)))
	}
	m.setSession(sessionName(ctx, svc.Name), true)
	handshakeDeadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(statusPath); err == nil {
			break
		}
		if time.Now().After(handshakeDeadline) || !m.sessionExistsLive(sessionName(ctx, svc.Name)) {
			return "", fmt.Errorf("%s supervisor did not initialize", svc.Name)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if svc.ReadyCommand != "" {
		deadline := time.Now().Add(90 * time.Second)
		for {
			probe := exec.Command("sh", "-lc", expand(svc.ReadyCommand, env))
			probe.Dir = ctx.Path
			probe.Env = mergedEnvironment(env)
			if probe.Run() == nil {
				break
			}
			if time.Now().After(deadline) || !m.sessionExistsLive(sessionName(ctx, svc.Name)) {
				_ = exec.Command(m.tmuxPath, "kill-session", "-t", sessionName(ctx, svc.Name)).Run()
				m.setSession(sessionName(ctx, svc.Name), false)
				_ = os.WriteFile(statusPath, []byte("failed: readiness timeout"), 0o644)
				return "", fmt.Errorf("%s did not become ready", svc.Name)
			}
			time.Sleep(300 * time.Millisecond)
		}
	}
	_ = os.WriteFile(statusPath, []byte("running"), 0o644)
	return fmt.Sprintf("started %s%s", svc.Name, formatPorts(svc.Ports, env)), nil
}

func (m *runtimeManager) Stop(ctx Context, svc Service) (string, error) {
	env, err := m.ensureEnvironment(ctx, false)
	if err != nil {
		return "", err
	}
	for key, value := range svc.Env {
		env[key] = expand(value, env)
	}
	if svc.StopCommand != "" {
		cmd := exec.Command("sh", "-lc", expand(svc.StopCommand, env))
		cmd.Dir = ctx.Path
		cmd.Env = mergedEnvironment(env)
		if output, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("stop %s: %w: %s", svc.Name, err, strings.TrimSpace(string(output)))
		}
	}
	if svc.Compose != nil {
		cmd := exec.Command("sh", "-lc", composeCommand(ctx, *svc.Compose, "stop"))
		cmd.Dir = ctx.Path
		cmd.Env = mergedEnvironment(env)
		if output, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("stop %s: %w: %s", svc.Name, err, strings.TrimSpace(string(output)))
		}
	}
	_ = exec.Command(m.tmuxPath, "kill-session", "-t", sessionName(ctx, svc.Name)).Run()
	m.setSession(sessionName(ctx, svc.Name), false)
	_ = os.WriteFile(m.statusFile(ctx, svc.Name), []byte("stopped"), 0o644)
	return "stopped " + svc.Name, nil
}

func (m *runtimeManager) Restart(ctx Context, svc Service) (string, error) {
	if _, err := m.Stop(ctx, svc); err != nil {
		return "", err
	}
	return m.Start(ctx, svc)
}

func (m *runtimeManager) RunAction(ctx Context, action Action) (string, string, error) {
	env, err := m.ensureEnvironment(ctx, true)
	if err != nil {
		return "", "", err
	}
	for key, value := range action.Env {
		env[key] = expand(value, env)
	}
	cmd := exec.Command("sh", "-lc", expand(action.Command, env))
	cmd.Dir = ctx.Path
	cmd.Env = mergedEnvironment(env)
	output, runErr := cmd.CombinedOutput()
	log := string(output)
	_ = os.WriteFile(m.logPath(ctx, "action-"+action.Name), output, 0o644)
	if runErr != nil {
		return log, fmt.Sprintf("%s failed", action.Name), runErr
	}
	return log, action.Name + " completed", nil
}

func (m *runtimeManager) Shell(ctx Context) (*exec.Cmd, error) {
	env, err := m.ensureEnvironment(ctx, false)
	if err != nil {
		return nil, err
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	cmd := exec.Command(shell, "-l")
	cmd.Dir = ctx.Path
	cmd.Env = mergedEnvironment(env)
	return cmd, nil
}

func (m *runtimeManager) Status(ctx Context, svc Service) string {
	if !m.hasSession(sessionName(ctx, svc.Name)) {
		return "stopped"
	}
	data, err := os.ReadFile(m.statusFile(ctx, svc.Name))
	if err != nil {
		return "starting"
	}
	status := strings.TrimSpace(string(data))
	if status == "" {
		return "starting"
	}
	return status
}

func (m *runtimeManager) Logs(ctx Context, name string) string {
	data, err := os.ReadFile(m.logPath(ctx, name))
	if err != nil {
		return ""
	}
	const maxLog = 256 * 1024
	if len(data) > maxLog {
		data = data[len(data)-maxLog:]
	}
	return string(data)
}

func (m *runtimeManager) Environment(ctx Context) map[string]string {
	env, err := m.ensureEnvironment(ctx, false)
	if err != nil {
		return map[string]string{"DEV_RUNNER_ERROR": err.Error()}
	}
	for _, svc := range ctx.Config.Services {
		for key, value := range svc.Env {
			env[key] = expand(value, env)
		}
	}
	if port := env["PORT"]; port != "" {
		env["DEV_RUNNER_URL"] = "http://127.0.0.1:" + port
	}
	if port := env["WDS_PORT"]; port != "" {
		env["DEV_RUNNER_WDS_URL"] = "http://127.0.0.1:" + port
	}
	data, err := os.ReadFile(filepath.Join(ctx.Path, ".env.worktree"))
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if key, value, ok := strings.Cut(line, "="); ok && envNamePattern.MatchString(key) {
				env[key] = strings.Trim(value, "\"'")
			}
		}
	}
	return env
}

func (m *runtimeManager) Refresh() {
	m.refreshSessions()
}

func (m *runtimeManager) refreshSessions() {
	out, err := exec.Command(m.tmuxPath, "list-sessions", "-F", "#{session_name}").Output()
	next := make(map[string]bool)
	if err == nil {
		for _, name := range strings.Split(string(out), "\n") {
			if name = strings.TrimSpace(name); name != "" {
				next[name] = true
			}
		}
	}
	m.sessionMu.Lock()
	m.sessions = next
	m.sessionMu.Unlock()
}

func (m *runtimeManager) hasSession(name string) bool {
	m.sessionMu.RLock()
	defer m.sessionMu.RUnlock()
	return m.sessions[name]
}

func (m *runtimeManager) setSession(name string, running bool) {
	m.sessionMu.Lock()
	if running {
		m.sessions[name] = true
	} else {
		delete(m.sessions, name)
	}
	m.sessionMu.Unlock()
}

func (m *runtimeManager) sessionExistsLive(name string) bool {
	return exec.Command(m.tmuxPath, "has-session", "-t", name).Run() == nil
}

func (m *runtimeManager) logPath(ctx Context, name string) string {
	return filepath.Join(m.baseDir, "logs", ctx.ID+"--"+slug(name)+".log")
}

func (m *runtimeManager) statusFile(ctx Context, name string) string {
	return filepath.Join(m.baseDir, "status", ctx.ID+"--"+slug(name)+".status")
}

func (m *runtimeManager) saveState() error {
	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return err
	}
	temp := m.statePath + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temp, m.statePath)
}

func sessionName(ctx Context, serviceName string) string {
	name := "dr-" + ctx.ID + "-" + slug(serviceName)
	if len(name) > 80 {
		return name[:80]
	}
	return name
}

func composeProject(ctx Context, spec Compose) string {
	if spec.Project != "" {
		return slug(expand(spec.Project, map[string]string{"CONTEXT_ID": ctx.ID}))
	}
	return "dr-" + ctx.ID
}

func composeCommand(ctx Context, spec Compose, operation string) string {
	args := []string{"docker", "compose", "-p", composeProject(ctx, spec), "-f", spec.File, operation}
	if operation == "up" {
		args = append(args, "--remove-orphans")
	}
	args = append(args, spec.Services...)
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func buildWrapper(command string, env map[string]string, logPath, statusPath string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var exports strings.Builder
	for _, key := range keys {
		exports.WriteString("export " + key + "=" + shellQuote(env[key]) + "\n")
	}
	return fmt.Sprintf("%sprintf starting > %s\nprintf '\\n[dev-runner] started %s\\n' >> %s\n( %s ) >> %s 2>&1\ncode=$?\nif [ \"$code\" -eq 0 ]; then printf stopped > %s; else printf 'failed:%%s' \"$code\" > %s; fi\nexec sleep 31536000", exports.String(), shellQuote(statusPath), time.Now().Format(time.RFC3339), shellQuote(logPath), command, shellQuote(logPath), shellQuote(statusPath), shellQuote(statusPath))
}

func serviceByName(cfg Config, name string) (Service, bool) {
	for _, svc := range cfg.Services {
		if svc.Name == name {
			return svc, true
		}
	}
	return Service{}, false
}

func mergedEnvironment(overrides map[string]string) []string {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok {
			values[key] = value
		}
	}
	for key, value := range overrides {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func expand(value string, env map[string]string) string {
	return os.Expand(value, func(key string) string {
		if result, ok := env[key]; ok {
			return result
		}
		return os.Getenv(key)
	})
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func sortedPortNames(ports map[string]int) []string {
	names := make([]string, 0, len(ports))
	for name := range ports {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func allocatePort(preferred int, reserved map[int]bool) (int, error) {
	for port := preferred; port < preferred+1000 && port <= 65535; port++ {
		if reserved[port] || !portAvailable(port) {
			continue
		}
		return port, nil
	}
	return 0, fmt.Errorf("no free port found from %d", preferred)
}

func portAvailable(port int) bool {
	address := "127.0.0.1:" + strconv.Itoa(port)
	connection, err := net.DialTimeout("tcp", address, 75*time.Millisecond)
	if err == nil {
		_ = connection.Close()
		return false
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

func formatPorts(ports map[string]int, env map[string]string) string {
	if len(ports) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ports))
	for _, name := range sortedPortNames(ports) {
		parts = append(parts, name+"="+env[name])
	}
	return " (" + strings.Join(parts, ", ") + ")"
}
