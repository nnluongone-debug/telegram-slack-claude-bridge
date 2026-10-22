package chatcore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/config"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/transcript"
)

const pendingTTL = 15 * time.Minute

// projectPrefixRe matches the shortcut "web-app: fix the tests".
var projectPrefixRe = regexp.MustCompile(`(?s)^\s*([\w.-]+)\s*:\s*(\S.*)$`)

// AskProject posts a project choice for a waiting prompt; each choice must
// call Core.PickProject with pendingID and the project's index in members.
type AskProject func(pendingID, prompt string, members []config.Project)

// HandleMultiMessage routes a message in a chat that hosts several projects:
// a top-level message starts a session in a project chosen by "name: prompt"
// or by asking; a thread belongs to the project its session ran in.
func (c *Core) HandleMultiMessage(ctx context.Context, conv Conversation, group config.Project, members []config.Project, text string, ask AskProject) {
	text = strings.TrimSpace(text)
	if conv.ThreadID() != "" {
		project, ok := c.threadProject(ctx, conv, members)
		if !ok {
			conv.Reply("This thread isn't linked to a project. Start a new session with a new top-level message.")
			return
		}
		c.HandleMessage(ctx, conv, project, text)
		return
	}

	if strings.HasPrefix(text, "!") {
		if cmd, id, _ := strings.Cut(text, " "); cmd == "!resume" && strings.TrimSpace(id) != "" {
			parsed, err := uuid.Parse(strings.TrimSpace(id))
			if err != nil {
				conv.Reply("❌ Not a valid session ID.")
				return
			}
			project, ok := projectOfSession(parsed.String(), members) // transcripts are named in lower case
			if !ok {
				conv.Reply("❌ That session doesn't belong to any project of this chat.")
				return
			}
			c.HandleMessage(ctx, conv, project, "!resume "+parsed.String())
			return
		}
		c.HandleMessage(ctx, conv, group, text) // !help, !sessions…
		return
	}
	if m := projectPrefixRe.FindStringSubmatch(text); m != nil {
		for _, p := range members {
			if strings.EqualFold(p.Name, m[1]) {
				c.StartSession(ctx, conv, p, m[2])
				return
			}
		}
	}
	if len(members) == 0 {
		conv.Reply("⚠️ This chat has no valid projects. Check **projects.json**.")
		return
	}
	ask(c.pending.add(pendingStart{conv: conv, prompt: text, projects: members, at: time.Now()}), text, members)
}

// PickProject starts a waiting prompt in the chosen project. picked runs
// first (e.g. to replace the buttons) so a slow start doesn't invite a
// second click. ok is false if the choice expired or was already used.
func (c *Core) PickProject(ctx context.Context, pendingID string, index int, picked func(project config.Project, prompt string)) bool {
	start, ok := c.pending.take(pendingID)
	if !ok || index < 0 || index >= len(start.projects) {
		return false
	}
	project := start.projects[index]
	picked(project, start.prompt)
	c.StartSession(ctx, start.conv, project, start.prompt)
	return true
}

// threadProject finds a thread's project: its live session, else the record,
// else (threads older than the record, Slack) the session announced in it.
func (c *Core) threadProject(ctx context.Context, conv Conversation, members []config.Project) (config.Project, bool) {
	if s, ok := c.sessions.ByThread(conv.ChatID(), conv.ThreadID()); ok {
		return s.Project, true
	}
	if name := c.directory.ProjectOf(conv.ChatID(), conv.ThreadID()); name != "" {
		for _, p := range members {
			if strings.EqualFold(p.Name, name) {
				return p, true
			}
		}
	}
	if id, err := conv.LastSessionID(ctx); err == nil && id != "" {
		return projectOfSession(id, members)
	}
	return config.Project{}, false
}

func projectOfSession(sessionID string, members []config.Project) (config.Project, bool) {
	for _, p := range members {
		if transcript.SessionBelongsTo(sessionID, p.WorkDir) {
			return p, true
		}
	}
	return config.Project{}, false
}

// pendingStart is a prompt waiting for the user to pick its project.
type pendingStart struct {
	conv     Conversation
	prompt   string
	projects []config.Project
	at       time.Time
}

type pendingStarts struct {
	mu      sync.Mutex
	entries map[string]pendingStart
}

func (p *pendingStarts) add(s pendingStart) string {
	raw := make([]byte, 4)
	_, _ = rand.Read(raw)
	id := hex.EncodeToString(raw)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.entries == nil {
		p.entries = map[string]pendingStart{}
	}
	for k, v := range p.entries {
		if time.Since(v.at) > pendingTTL {
			delete(p.entries, k)
		}
	}
	p.entries[id] = s
	return id
}

// take removes and returns a pending start, so each prompt starts only once.
func (p *pendingStarts) take(id string) (pendingStart, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.entries[id]
	delete(p.entries, id)
	return s, ok && time.Since(s.at) <= pendingTTL
}
