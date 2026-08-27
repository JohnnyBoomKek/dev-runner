package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Context struct {
	ID         string
	Project    string
	RootPath   string
	Worktree   string
	Path       string
	Config     Config
	ConfigPath string
	ConfigErr  error
}

type worktreeRecord struct {
	Path   string
	Branch string
}

func discoverContexts(roots []string) ([]Context, error) {
	var contexts []Context
	seen := make(map[string]bool)
	for _, repoPath := range repositoryCandidates(roots) {
		projectName := filepath.Base(repoPath)
		projectConfig, configPath, configErr := loadConfig(repoPath, "")
		records := discoverWorktrees(repoPath)
		if len(records) == 0 {
			records = []worktreeRecord{{Path: repoPath, Branch: projectName}}
		}
		for _, record := range records {
			if info, err := os.Stat(record.Path); err != nil || !info.IsDir() {
				continue
			}
			canonical, err := filepath.EvalSymlinks(record.Path)
			if err != nil {
				canonical = filepath.Clean(record.Path)
			}
			if seen[canonical] {
				continue
			}
			seen[canonical] = true
			label := record.Branch
			if label == "" {
				label = filepath.Base(record.Path)
			} else if label == "detached" {
				label = "detached · " + filepath.Base(filepath.Dir(record.Path)) + "/" + filepath.Base(record.Path)
			}
			contexts = append(contexts, Context{
				ID:         contextID(projectName, record.Path),
				Project:    projectName,
				RootPath:   repoPath,
				Worktree:   label,
				Path:       record.Path,
				Config:     projectConfig,
				ConfigPath: configPath,
				ConfigErr:  configErr,
			})
		}
	}
	sort.Slice(contexts, func(i, j int) bool {
		iConfigured := contexts[i].ConfigPath != ""
		jConfigured := contexts[j].ConfigPath != ""
		if iConfigured != jConfigured {
			return iConfigured
		}
		if strings.EqualFold(contexts[i].Project, contexts[j].Project) {
			return strings.ToLower(contexts[i].Worktree) < strings.ToLower(contexts[j].Worktree)
		}
		return strings.ToLower(contexts[i].Project) < strings.ToLower(contexts[j].Project)
	})
	return contexts, nil
}

func repositoryCandidates(roots []string) []string {
	seenPaths := make(map[string]bool)
	var candidates []string
	for _, root := range roots {
		if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
			if !seenPaths[root] {
				candidates = append(candidates, root)
				seenPaths[root] = true
			}
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			path := filepath.Join(root, entry.Name())
			if _, err := os.Stat(filepath.Join(path, ".git")); err == nil && !seenPaths[path] {
				candidates = append(candidates, path)
				seenPaths[path] = true
			}
		}
	}

	byCommonDir := make(map[string][]string)
	for _, candidate := range candidates {
		commonDir := gitCommonDir(candidate)
		if commonDir == "" {
			commonDir = candidate
		}
		byCommonDir[commonDir] = append(byCommonDir[commonDir], candidate)
	}
	var repositories []string
	for commonDir, group := range byCommonDir {
		preferred := group[0]
		mainPath := filepath.Dir(commonDir)
		for _, candidate := range group {
			if filepath.Clean(candidate) == filepath.Clean(mainPath) {
				preferred = candidate
				break
			}
		}
		repositories = append(repositories, preferred)
	}
	sort.Strings(repositories)
	return repositories
}

func gitCommonDir(repo string) string {
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo, path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

func configuredContexts(contexts []Context) []Context {
	visible := make([]Context, 0, len(contexts))
	for _, ctx := range contexts {
		if ctx.ConfigPath != "" || ctx.ConfigErr != nil {
			visible = append(visible, ctx)
		}
	}
	return visible
}

func unconfiguredContexts(contexts []Context) []Context {
	hidden := make([]Context, 0, len(contexts))
	seen := make(map[string]bool)
	for _, ctx := range contexts {
		if ctx.ConfigPath == "" && ctx.ConfigErr == nil && !seen[ctx.RootPath] {
			ctx.Path = ctx.RootPath
			ctx.Worktree = "project root"
			hidden = append(hidden, ctx)
			seen[ctx.RootPath] = true
		}
	}
	return hidden
}

func discoverWorktrees(repo string) []worktreeRecord {
	out, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil
	}
	var records []worktreeRecord
	var current *worktreeRecord
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			records = append(records, worktreeRecord{Path: strings.TrimPrefix(line, "worktree ")})
			current = &records[len(records)-1]
		case current != nil && strings.HasPrefix(line, "branch refs/heads/"):
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		case current != nil && line == "detached":
			current.Branch = "detached"
		}
	}
	return records
}

func contextID(project, path string) string {
	sum := sha256.Sum256([]byte(path))
	return slug(project) + "-" + hex.EncodeToString(sum[:4])
}

func slug(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
