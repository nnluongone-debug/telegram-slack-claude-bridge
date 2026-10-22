package chatcore

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/config"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/session"
)

// HelpText lists the commands; adapters may append platform notes.
const HelpText = "**Claude bridge commands**\n" +
	"- New message in the project chat (not in a thread/topic) → starts an agent session in its own thread/topic; the message is the first prompt\n" +
	"- Message inside the thread/topic → typed into that agent\n" +
	"- `!stop` stop the agent · `!restart [prompt]` start a fresh one here\n" +
	"- `!resume` reopen this thread's last session · `!resume <session-id>` reopen any session of this project\n" +
	"- `!interrupt` Ctrl-C · `!esc` Esc · `!enter` Enter\n" +
	"- `!model <name>` switch model (context kept) · `!mode [plan|auto|accept|manual]` show/switch permission mode\n" +
	"- `!screen` terminal snapshot · `!status` this session · `!sessions` all sessions in this chat"

const maxScreenChars = 3000

// modelNameRe accepts aliases and full IDs (opus, sonnet, claude-opus-5-5, opus[1m])
// and never something that could be read as a flag.
var modelNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\[\]-]*$`)

func (c *Core) runCommand(ctx context.Context, conv Conversation, project config.Project, text string) {
	cmd, arg, _ := strings.Cut(text, " ")
	arg = strings.TrimSpace(arg)
	inThread := conv.ThreadID() != ""

	switch cmd {
	case "!help":
		conv.Reply(HelpText)
		return
	case "!sessions":
		conv.Reply(c.describeSessions(conv))
		return
	case "!resume":
		c.resume(ctx, conv, project, arg)
		return
	case "!restart":
		if !inThread {
			conv.Reply("`!restart` must be used inside a session thread/topic.")
			return
		}
		if s, ok := c.sessions.ByThread(conv.ChatID(), conv.ThreadID()); ok {
			s.Stop()
			<-s.Done()
		}
		c.launch(conv, project, "started", func(sink session.Sink) (*session.Session, error) {
			return c.sessions.Start(conv.ChatID(), conv.ThreadID(), project, arg, sink)
		})
		return
	}

	s, ok := c.sessions.ByThread(conv.ChatID(), conv.ThreadID())
	if !inThread || !ok {
		conv.Reply("💤 No live session here. Type `!help` for usage.")
		return
	}
	var err error
	switch cmd {
	case "!stop":
		s.Stop()
		conv.Reply("🛑 Stopping session…")
	case "!interrupt":
		err = s.SendKeys([]byte{0x03})
	case "!esc":
		err = s.SendKeys([]byte{0x1b})
	case "!enter":
		err = s.SendKeys([]byte{'\r'})
	case "!status":
		conv.Reply(fmt.Sprintf("Session `%s` · project **%s** · `%s` · up %s",
			s.ID, s.Project.Name, s.Project.WorkDir, time.Since(s.StartedAt).Round(time.Second)))
	case "!model":
		c.switchModel(conv, project, s, arg)
	case "!mode":
		conv.Reply(modeReply(s, arg))
	case "!screen":
		conv.Reply("```\n" + tailLines(s.ScreenText(), maxScreenChars) + "\n```")
	default:
		conv.Reply("Unknown command `" + cmd + "`.\n\n" + HelpText)
	}
	if err != nil {
		conv.Reply("❌ " + err.Error())
	}
}

// launch starts or resumes an agent, announces it with the full session ID
// and records it for `!resume`.
func (c *Core) launch(conv Conversation, project config.Project, verb string, start func(session.Sink) (*session.Session, error)) {
	s, err := start(conv.NewSink())
	if err != nil {
		c.log.Error("session "+verb+" failed", "chat", conv.ChatID(), "err", err)
		conv.Reply("❌ Could not start agent: " + err.Error())
		return
	}
	c.log.Info("session "+verb, "session", s.ID, "project", project.Name, "workdir", project.WorkDir)
	if err := c.directory.Remember(conv.ChatID(), conv.ThreadID(), s.ID, project.Name); err != nil {
		c.log.Warn("could not save session directory", "err", err)
	}
	conv.Reply(fmt.Sprintf("🚀 Session `%s` %s for **%s** in `%s`.%s\nWrite here to talk to Claude; `!help` for commands.",
		s.ID, verb, project.Name, project.WorkDir, c.folderNote(s)))
}

// folderNote explains where a session works when others use the same project.
func (c *Core) folderNote(s *session.Session) string {
	switch {
	case s.Worktree != "":
		return fmt.Sprintf("\n🌿 Works in its own git worktree `.claude/worktrees/%s` (branch `worktree-%s`), so it can't clash with other sessions of this project. Merge that branch when you're happy with the result.",
			s.Worktree, s.Worktree)
	case len(c.sessions.SharingFolder(s)) > 0:
		return "\n⚠️ Another session is working in the same folder (not a git repo, or `\"worktree\": \"never\"`): avoid editing the same files at the same time."
	}
	return ""
}

// resume handles `!resume [session-id]`. Without an ID it reopens this
// thread's last session; at top level with an ID it opens a new thread.
func (c *Core) resume(ctx context.Context, conv Conversation, project config.Project, id string) {
	if id == "" {
		if conv.ThreadID() == "" {
			conv.Reply("Usage: `!resume <session-id>` here, or `!resume` inside a session thread/topic.")
			return
		}
		if id = c.directory.Last(conv.ChatID(), conv.ThreadID()); id == "" {
			found, err := conv.LastSessionID(ctx)
			if err != nil {
				conv.Reply("❌ Could not read thread history: " + err.Error())
				return
			}
			id = found
		}
		if id == "" {
			conv.Reply("No previous session found here. Use `!resume <session-id>`.")
			return
		}
	}
	if conv.ThreadID() == "" {
		thread, err := conv.OpenThread(ctx, threadTitle(project, "resume "+id))
		if err != nil {
			conv.Reply("❌ Could not open a thread for the session: " + err.Error())
			return
		}
		conv = thread
	}
	if s, ok := c.sessions.ByThread(conv.ChatID(), conv.ThreadID()); ok {
		conv.Reply(fmt.Sprintf("Session `%s` is still running here. `!stop` it first.", s.ID))
		return
	}
	c.launch(conv, project, "resumed", func(sink session.Sink) (*session.Session, error) {
		return c.sessions.Resume(conv.ChatID(), conv.ThreadID(), project, id, nil, sink)
	})
}

// switchModel restarts the session via `claude --resume <id> --model <name>`.
// Unlike typing /model in the TUI, this keeps the conversation and never
// changes the user's saved default model.
func (c *Core) switchModel(conv Conversation, project config.Project, s *session.Session, name string) {
	if name == "" {
		conv.Reply("Usage: `!model <name>`, e.g. `opus`, `sonnet` or a full model ID. Applies to this session only.")
		return
	}
	if !modelNameRe.MatchString(name) {
		conv.Reply("❌ Not a valid model name.")
		return
	}
	conv.Reply(fmt.Sprintf("🔄 Switching this session to `%s` (conversation is kept)…", name))
	s.Stop()
	<-s.Done()
	c.launch(conv, project, "resumed", func(sink session.Sink) (*session.Session, error) {
		return c.sessions.Resume(conv.ChatID(), conv.ThreadID(), project, s.ID, []string{"--model", name}, sink)
	})
}

// modeReply shows or switches the permission mode by pressing Shift+Tab.
func modeReply(s *session.Session, want string) string {
	if want == "" {
		if current := s.PermissionMode(); current != "" {
			return fmt.Sprintf("Current mode: **%s**. Switch with `!mode plan|auto|accept|manual`.", current)
		}
		return "The mode isn't visible on Claude's screen right now (e.g. a dialog is open). Try again in a moment, or `!screen`."
	}
	got, err := s.SwitchPermissionMode(want)
	if err != nil {
		return "❌ " + err.Error()
	}
	return fmt.Sprintf("✅ Mode: **%s**", got)
}

func (c *Core) describeSessions(conv Conversation) string {
	list := c.sessions.List(conv.ChatID())
	if len(list) == 0 {
		return "No live sessions in this chat."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "**%d live session(s):**\n", len(list))
	for _, s := range list {
		fmt.Fprintf(&sb, "- `%s` · **%s** · up %s %s\n", s.ID[:min(8, len(s.ID))], s.Project.Name, time.Since(s.StartedAt).Round(time.Second), conv.ThreadLink(s))
	}
	return sb.String()
}

// tailLines keeps the last limit bytes of text on a line boundary.
func tailLines(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := text[len(text)-limit:]
	if i := strings.IndexByte(cut, '\n'); i >= 0 {
		cut = cut[i+1:]
	}
	return strings.ToValidUTF8(cut, "")
}
