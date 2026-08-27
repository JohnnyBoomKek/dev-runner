package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type rootFlags []string

func (r *rootFlags) String() string { return strings.Join(*r, ",") }
func (r *rootFlags) Set(value string) error {
	*r = append(*r, value)
	return nil
}

func defaultRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return existingRoots([]string{filepath.Join(home, "Projects"), filepath.Join(home, "Work")})
}

func existingRoots(values []string) []string {
	seen := make(map[string]bool)
	var roots []string
	for _, value := range values {
		path, err := normalizeRoot(value)
		if err != nil || seen[path] {
			continue
		}
		seen[path] = true
		roots = append(roots, path)
	}
	return roots
}

func normalizeRoot(value string) (string, error) {
	value = strings.TrimSpace(os.ExpandEnv(value))
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~/"))
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", path)
	}
	return filepath.Clean(path), nil
}

func (m *runtimeManager) roots(explicit []string) []string {
	if len(explicit) > 0 {
		return existingRoots(explicit)
	}
	roots := defaultRoots()
	data, err := os.ReadFile(filepath.Join(m.baseDir, "roots.json"))
	if err == nil {
		var persisted []string
		if json.Unmarshal(data, &persisted) == nil {
			roots = append(roots, persisted...)
		}
	}
	return existingRoots(roots)
}

func (m *runtimeManager) addRoot(current []string, value string) ([]string, error) {
	path, err := normalizeRoot(value)
	if err != nil {
		return current, err
	}
	roots := existingRoots(append(current, path))
	data, err := json.MarshalIndent(roots, "", "  ")
	if err != nil {
		return current, err
	}
	statePath := filepath.Join(m.baseDir, "roots.json")
	temp := statePath + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return current, err
	}
	if err := os.Rename(temp, statePath); err != nil {
		return current, err
	}
	return roots, nil
}
