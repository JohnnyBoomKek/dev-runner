package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Name       string    `yaml:"name"`
	RuntimeDir string    `yaml:"runtime_dir,omitempty"`
	Services   []Service `yaml:"services"`
	Actions    []Action  `yaml:"actions"`
}

type Service struct {
	Name         string            `yaml:"name"`
	Command      string            `yaml:"command"`
	StopCommand  string            `yaml:"stop_command,omitempty"`
	ReadyCommand string            `yaml:"ready_command,omitempty"`
	DependsOn    []string          `yaml:"depends_on,omitempty"`
	Env          map[string]string `yaml:"env,omitempty"`
	Ports        map[string]int    `yaml:"ports,omitempty"`
	Compose      *Compose          `yaml:"compose,omitempty"`
}

type Compose struct {
	File     string   `yaml:"file"`
	Project  string   `yaml:"project,omitempty"`
	Services []string `yaml:"services,omitempty"`
}

type Action struct {
	Name    string            `yaml:"name"`
	Command string            `yaml:"command"`
	Key     string            `yaml:"key,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
}

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func loadConfig(worktreePath, fallbackPath string) (Config, string, error) {
	candidates := configCandidates(worktreePath)
	if fallbackPath != "" && fallbackPath != worktreePath {
		candidates = append(candidates, configCandidates(fallbackPath)...)
	}

	seen := make(map[string]bool)
	for _, path := range candidates {
		if seen[path] {
			continue
		}
		seen[path] = true
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return Config{}, path, err
		}
		var cfg Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, path, fmt.Errorf("parse %s: %w", path, err)
		}
		if cfg.Name == "" {
			cfg.Name = filepath.Base(worktreePath)
		}
		if err := validateConfig(worktreePath, cfg); err != nil {
			return Config{}, path, err
		}
		return cfg, path, nil
	}

	return Config{Name: filepath.Base(worktreePath)}, "", nil
}

func configCandidates(dir string) []string {
	localMatches, _ := filepath.Glob(filepath.Join(dir, "local", "*", "runner.yaml"))
	sort.Strings(localMatches)
	return append(localMatches,
		filepath.Join(dir, ".runner", "config.yaml"),
		filepath.Join(dir, ".runner.yaml"),
	)
}

func validateConfig(worktreePath string, cfg Config) error {
	seen := make(map[string]bool)
	for _, svc := range cfg.Services {
		if strings.TrimSpace(svc.Name) == "" {
			return fmt.Errorf("service name is required")
		}
		if seen[svc.Name] {
			return fmt.Errorf("duplicate item name %q", svc.Name)
		}
		seen[svc.Name] = true
		if (strings.TrimSpace(svc.Command) == "") == (svc.Compose == nil) {
			return fmt.Errorf("service %q must define exactly one of command or compose", svc.Name)
		}
		for name, preferred := range svc.Ports {
			if !envNamePattern.MatchString(name) {
				return fmt.Errorf("service %q has invalid port variable %q", svc.Name, name)
			}
			if preferred < 1 || preferred > 65535 {
				return fmt.Errorf("service %q port %s must be between 1 and 65535", svc.Name, name)
			}
		}
		if svc.Compose != nil {
			if strings.TrimSpace(svc.Compose.File) == "" {
				return fmt.Errorf("service %q compose.file is required", svc.Name)
			}
			if _, err := os.Stat(filepath.Join(worktreePath, svc.Compose.File)); err != nil {
				return fmt.Errorf("service %q compose file: %w", svc.Name, err)
			}
		}
	}
	for _, svc := range cfg.Services {
		for _, dependency := range svc.DependsOn {
			if dependency == svc.Name || !seen[dependency] {
				return fmt.Errorf("service %q has invalid dependency %q", svc.Name, dependency)
			}
		}
	}

	reserved := map[string]bool{"q": true, "j": true, "k": true, "h": true, "l": true, "g": true, "G": true, "n": true, "s": true, "x": true, "r": true, "S": true, "X": true, "R": true, ":": true, "enter": true}
	for _, action := range cfg.Actions {
		if strings.TrimSpace(action.Name) == "" || strings.TrimSpace(action.Command) == "" {
			return fmt.Errorf("actions require name and command")
		}
		if seen[action.Name] {
			return fmt.Errorf("duplicate item name %q", action.Name)
		}
		seen[action.Name] = true
		if action.Key != "" && reserved[action.Key] {
			return fmt.Errorf("action %q uses reserved key %q", action.Name, action.Key)
		}
	}
	return nil
}
