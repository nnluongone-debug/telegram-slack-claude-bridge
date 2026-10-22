package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Project maps one chat (Slack channel, Telegram group) to a working directory
// (usually a git worktree). A multi-project chat has Projects instead of a
// WorkDir: the names of its projects, or "*" for all.
type Project struct {
	Name    string   `json:"name"`
	WorkDir string   `json:"workdir"`
	Args    []string `json:"args"` // extra claude flags for this project, e.g. ["--model", "sonnet"]
	// Worktree: "auto" (default) gives a session its own git worktree when
	// another session already works in the folder; "always" or "never".
	Worktree string   `json:"worktree"`
	Projects []string `json:"-"`
}

// projectEntry is one entry of projects.json. "projects" items are either
// project objects ({"name", "workdir", "args"}) defined right there, or the
// names of projects defined elsewhere, or "*".
type projectEntry struct {
	Name     string            `json:"name"`
	WorkDir  string            `json:"workdir"`
	Args     []string          `json:"args"`
	Worktree string            `json:"worktree"`
	Projects []json.RawMessage `json:"projects"`
}

// childKeySep joins a chat ID and a child project name into a registry key
// that can never collide with a real chat ID.
const childKeySep = "/"

// IsMulti reports whether this entry is a multi-project chat rather than a project.
func (p Project) IsMulti() bool { return p.Projects != nil }

// LoadProjects reads a JSON object of {"<channelID>": {"name": "...", "workdir": "..."}}.
// A project with a missing workdir or a bridge-managed arg is left out and
// reported in problems, so one typo never disables the other projects.
// Only an unreadable or malformed file is an error.
func LoadProjects(path string) (projects map[string]Project, problems []string, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read projects file %s: %w", path, err)
	}
	parsed := map[string]projectEntry{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, nil, fmt.Errorf("parse projects file %s: %w", path, err)
	}

	projects = map[string]Project{}
	add := func(key, label string, p Project) bool {
		if problem := validateProject(&p); problem != "" {
			problems = append(problems, label+": "+problem)
			return false
		}
		projects[key] = p
		return true
	}
	for chatID, e := range parsed {
		p := Project{Name: e.Name, WorkDir: e.WorkDir, Args: e.Args, Worktree: e.Worktree}
		if p.Name == "" && len(e.Projects) > 0 {
			p.Name = chatID // a single project defaults to its folder name instead
		}
		for _, item := range e.Projects {
			var ref string
			if json.Unmarshal(item, &ref) == nil {
				p.Projects = append(p.Projects, ref)
				continue
			}
			var child Project
			if err := json.Unmarshal(item, &child); err != nil || child.Name == "" {
				problems = append(problems, p.Name+": each item of \"projects\" needs a \"name\"")
				continue
			}
			if add(chatID+childKeySep+child.Name, p.Name+" › "+child.Name, child) {
				p.Projects = append(p.Projects, child.Name)
			}
		}
		if len(e.Projects) > 0 && len(p.Projects) == 0 {
			p.Projects = []string{} // keep it a (broken) multi-project chat, reported below
		}
		label := p.Name
		if label == "" {
			label = chatID
		}
		add(chatID, label, p)
	}
	problems = append(problems, unknownMembers(projects)...)
	problems = append(problems, duplicateNames(projects)...)
	slices.Sort(problems)
	return projects, problems, nil
}

// unknownMembers reports multi-project chats that name projects which don't exist.
func unknownMembers(projects map[string]Project) []string {
	var problems []string
	for _, p := range projects {
		for _, name := range p.Projects {
			if name == "*" {
				continue
			}
			if _, ok := findByName(projects, name); !ok {
				problems = append(problems, fmt.Sprintf("%s: unknown project %q", p.Name, name))
			}
		}
	}
	return problems
}

// duplicateNames reports a name defined twice with different settings:
// picking by name would be ambiguous. Identical copies (the same list pasted
// into a Slack channel and a Telegram group) are fine.
func duplicateNames(projects map[string]Project) []string {
	first := map[string]Project{}
	var problems []string
	for _, p := range projects {
		if p.IsMulti() {
			continue
		}
		key := strings.ToLower(p.Name)
		if prev, ok := first[key]; !ok {
			first[key] = p
		} else if !sameDefinition(prev, p) {
			problems = append(problems, fmt.Sprintf("project %q is defined twice with different settings", p.Name))
		}
	}
	return slices.Compact(slices.Sorted(slices.Values(problems)))
}

func sameDefinition(a, b Project) bool {
	return a.WorkDir == b.WorkDir && a.Worktree == b.Worktree && slices.Equal(a.Args, b.Args)
}

func findByName(projects map[string]Project, name string) (Project, bool) {
	for _, p := range projects {
		if !p.IsMulti() && strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return Project{}, false
}

// validateProject resolves the workdir in place and returns a problem or "".
func validateProject(p *Project) string {
	if p.IsMulti() {
		if p.WorkDir != "" {
			return `use either "workdir" or "projects", not both`
		}
		return ""
	}
	abs, err := filepath.Abs(expandHome(p.WorkDir))
	if err != nil {
		return err.Error()
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return fmt.Sprintf("folder not found (%s)", p.WorkDir)
	}
	if tooBroad(abs) {
		return fmt.Sprintf("folder %s is too broad: use a project folder, not / or your home folder", p.WorkDir)
	}
	if err := ValidateClaudeArgs(p.Args); err != nil {
		return err.Error()
	}
	switch p.Worktree {
	case "", "auto", "always", "never":
	default:
		return fmt.Sprintf(`"worktree" must be "auto", "always" or "never", not %q`, p.Worktree)
	}
	if p.Name == "" {
		p.Name = filepath.Base(abs)
	}
	p.WorkDir = abs
	return ""
}

// expandHome expands $HOME, ${HOME} and a leading ~. Other variables are left
// alone: expanding any variable would let projects.json pull secrets from the
// bridge's environment into paths and error messages.
func expandHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		path = home + path[1:]
	}
	return strings.NewReplacer("${HOME}", home, "$HOME", home).Replace(path)
}

// tooBroad reports a workdir that is the filesystem root, the home folder or
// one of its parents: an agent there could reach every project and the
// bridge's own files.
func tooBroad(abs string) bool {
	abs = filepath.Clean(abs)
	if abs == filepath.Dir(abs) { // "/" or "C:\"
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(abs, filepath.Clean(home))
	return err == nil && (rel == "." || !strings.HasPrefix(rel, ".."))
}
