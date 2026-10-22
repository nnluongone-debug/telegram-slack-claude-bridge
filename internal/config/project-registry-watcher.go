package config

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// ProjectRegistry holds the channel→project map and reloads it when
// projects.json changes, so adding a project needs no bridge restart.
type ProjectRegistry struct {
	path string

	mu       sync.RWMutex
	projects map[string]Project
	problems []string
	modTime  time.Time
}

// NewProjectRegistry loads path once; the file must be valid JSON at startup.
func NewProjectRegistry(path string) (*ProjectRegistry, error) {
	r := &ProjectRegistry{path: path}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	projects, problems, err := LoadProjects(path)
	if err != nil {
		return nil, err
	}
	r.projects, r.problems, r.modTime = projects, problems, info.ModTime()
	return r, nil
}

// Path is the projects file being watched.
func (r *ProjectRegistry) Path() string { return r.path }

// Problems lists projects that were left out (e.g. "web-app: folder not found (…)").
func (r *ProjectRegistry) Problems() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.problems)
}

// LogProblems reports skipped projects.
func (r *ProjectRegistry) LogProblems(log *slog.Logger) {
	for _, p := range r.Problems() {
		log.Warn("project skipped", "problem", p)
	}
}

// Lookup returns the project mapped to a Slack channel.
func (r *ProjectRegistry) Lookup(channelID string) (Project, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.projects[channelID]
	return p, ok
}

// Count returns how many projects (not multi-project chats) are defined.
func (r *ProjectRegistry) Count() int {
	return len(r.All())
}

// All returns every project (not multi-project chats), sorted by name.
func (r *ProjectRegistry) All() []Project {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Project
	seen := map[string]bool{}
	for _, p := range r.projects {
		if key := strings.ToLower(p.Name); !p.IsMulti() && !seen[key] { // identical copies count once
			seen[key] = true
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b Project) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// ByName finds a project by its name (case-insensitive).
func (r *ProjectRegistry) ByName(name string) (Project, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return findByName(r.projects, name)
}

// Members resolves a multi-project chat's project list ("*" = all projects).
func (r *ProjectRegistry) Members(multi Project) []Project {
	if slices.Contains(multi.Projects, "*") {
		return r.All()
	}
	var out []Project
	for _, name := range multi.Projects {
		if p, ok := r.ByName(name); ok {
			out = append(out, p)
		}
	}
	return out
}

// Watch polls the file until ctx ends. An invalid edit is logged and the
// previous projects stay active, so a typo never takes the bridge down.
func (r *ProjectRegistry) Watch(ctx context.Context, interval time.Duration, log *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reloadIfChanged(log)
		}
	}
}

func (r *ProjectRegistry) reloadIfChanged(log *slog.Logger) {
	info, err := os.Stat(r.path)
	if err != nil {
		return
	}
	r.mu.RLock()
	unchanged := info.ModTime().Equal(r.modTime)
	r.mu.RUnlock()
	if unchanged {
		return
	}

	projects, problems, err := LoadProjects(r.path)
	r.mu.Lock()
	r.modTime = info.ModTime() // don't re-log the same broken edit every tick
	if err == nil {
		r.projects, r.problems = projects, problems
	}
	r.mu.Unlock()
	if err != nil {
		log.Error("projects file changed but is invalid; keeping previous projects", "err", err)
		return
	}
	log.Info("projects reloaded", "projects", len(projects), "skipped", len(problems))
	r.LogProblems(log)
}
