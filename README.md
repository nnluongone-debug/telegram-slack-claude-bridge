<p align="center">
  <img src="assets/icon-1024.png" width="128" alt="Claude Bridge icon">
</p>

<h1 align="center">Claude Bridge</h1>

<p align="center">
  Drive the <a href="https://docs.claude.com/en/docs/claude-code/overview">Claude Code</a> agents on your own computer from <b>Slack</b> or <b>Telegram</b>, including from your phone.
</p>

<p align="center">
  macOS (menu bar app) · Linux · Windows &nbsp;|&nbsp; MIT licensed
</p>

---

You start a Claude Code session from a chat message, follow its answers in a thread, and approve its actions with buttons. Everything runs on **your** machine with **your** Claude login. The bridge only relays text and key presses between the chat and a local `claude` process.

> **Unofficial project.** Claude Bridge is a community tool and is not affiliated with or endorsed by Anthropic. "Claude" and "Claude Code" are trademarks of Anthropic.

## Contents

- [Features](#features)
- [How it works](#how-it-works)
- [Security model: read this first](#security-model-read-this-first)
- [Requirements](#requirements)
- [Installation](#installation)
- [Set up Slack](#set-up-slack)
- [Set up Telegram](#set-up-telegram)
- [Configuration](#configuration)
- [Running the bridge](#running-the-bridge)
- [Usage](#usage)
- [Where files live](#where-files-live)
- [Troubleshooting](#troubleshooting)
- [Development](#development)
- [Limitations](#limitations)
- [License](#license)

## Features

- **Channel = project.** Each Slack channel or Telegram group maps to a working directory. One chat can also host several projects and ask *Which project?* with buttons.
- **Thread = session.** A top-level message starts a `claude` process in a pseudo-terminal. Replies in the thread (Slack) or topic (Telegram) are typed into it.
- **Readable answers.** Claude's replies are read from its session transcript and posted as formatted messages. Tables become native Slack tables, and tool calls show as compact lines (`🔧 Bash git status`).
- **Send files.** A photo, screenshot, PDF or any document sent with a message is downloaded and handed to Claude, which reads it with its own tools.
- **Approve from your phone.** Claude's permission prompts become **Approve / Reject** buttons, and multiple-choice questions become one button per option.
- **Resume anywhere.** The session ID in the chat is Claude's own session ID. `!resume` continues it after a restart, and `claude --resume <id>` continues it on your laptop.
- **Parallel sessions without conflicts.** When a second session starts in a git repository that already has a live session, it gets its own git worktree automatically.
- **No public URL.** Slack uses Socket Mode and Telegram uses long polling. Nothing listens on a port.
- **macOS menu bar app** that shows status, edits the config, opens at login and stops sessions cleanly.

## How it works

```
 Slack / Telegram                    your computer
┌───────────────────┐   events   ┌────────────────────────────────────────────┐
│ #web-app channel  │──────────▶ │ claude-bridge                              │
│  └ thread = session│◀────────── │  ├─ session 1: claude (PTY) in ~/code/web  │
│ Telegram group    │  replies,  │  ├─ session 2: claude (PTY) in a worktree  │
│  └ topic = session│  buttons   │  └─ reads ~/.claude/projects/*/<id>.jsonl  │
└───────────────────┘            └────────────────────────────────────────────┘
```

1. A message arrives from an allowed user in a mapped chat.
2. A top-level message starts `claude --session-id <uuid> "<message>"` in the project folder, inside a pseudo-terminal (a Unix PTY, or ConPTY on Windows).
3. The bridge tails Claude's transcript file and posts each answer and tool call to the thread.
4. It also renders the terminal screen to detect permission prompts and questions, and turns them into buttons. A click sends the matching key press.
5. A reply in the thread is typed into Claude's input box and submitted.

## Security model: read this first

The bridge gives remote control of a coding agent that runs **as your OS user**. Treat a chat account on the allowlist like an SSH key to your machine.

What the bridge enforces:

| Protection | Details |
|---|---|
| Allowlist | Only user IDs in `ALLOWED_USER_IDS` (Slack) or `TELEGRAM_BRIDGE_ALLOWED_USER_IDS` (Telegram) can send input or click buttons. A platform without an allowlist does not start. |
| Tokens stay private | `.env` is read into memory and never exported. Bot tokens are removed from the agent's environment. |
| Agent cannot rewrite its own powers | Every session starts with Claude permission rules that deny editing the bridge's config files and reading its `.env`. The rules also deny editing Claude Code's own configuration: `.claude/settings*.json`, `.claude/agents`, `.claude/commands`, `.claude/hooks` and `.mcp.json` in the project, and the same files under `~/.claude`. |
| Safe launch flags | `args` in `projects.json` and `CLAUDE_ARGS` accept only an allowlist of flags. Flags that widen the agent's power (`--dangerously-skip-permissions`, `--permission-mode bypassPermissions`, `--allowedTools`, `--add-dir`, `--mcp-config`, `--settings`, `--agent`, `--chrome`…) are rejected. |
| Narrow working folders | `workdir` may not be `/`, a drive root, your home folder or one of its parents. Only `$HOME` and `~` are expanded, so `projects.json` cannot pull other environment variables into paths. |
| No link previews | Messages are posted with link unfurling off, so a URL the agent prints is never fetched by Slack or Telegram. |
| No inbound ports | Socket Mode and long polling only. |
| Files are saved, never run | Attachments are downloaded only from allowlisted users, capped at 20 MB, saved without the execute bit (quarantined on macOS) in a folder the agent can read but not edit. Claude reads them as untrusted content, under the usual permission prompts. |

What **you** must do:

- **Turn on two-factor authentication** for the Slack and Telegram accounts on the allowlist. For Telegram, set a cloud password: SMS codes alone are a common way to lose an account.
- **Use private channels or groups that contain only you** (or people you fully trust). The allowlist controls who can *send commands*, not who can *read*. Everyone in the chat sees Claude's answers, which can include file contents and secrets.
- **Keep Claude's permission prompts on.** Approve actions one by one, or use `acceptEdits`. Avoid broad auto-approval for shell commands: the deny rules above cover Claude's file tools, not what a shell command does.
- Be careful with what the agent reads. Web pages, issues and files from other people can contain prompt injection. That includes files you forward into the chat: a PDF or image can carry text that tells Claude to run something. The permission prompts are your guard.
- In @BotFather, set `/setjoingroups` to *Disable* after adding your bot to its groups, so nobody else can add it to theirs.

To report a vulnerability, please open a private security advisory on GitHub rather than a public issue.

## Requirements

- [Claude Code](https://docs.claude.com/en/docs/claude-code/setup) installed and logged in (`claude` works in a terminal) on the machine that runs the bridge.
  - **Windows:** use the native installer, which provides `claude.exe`. The npm package installs a `claude.cmd` script, which the bridge refuses to run because `cmd.exe` would re-parse prompts as commands.
- A Slack workspace where you can create apps, and/or a Telegram account.
- To build from source: Go 1.26 or newer. The macOS menu bar app also needs Xcode Command Line Tools (`xcode-select --install`).

## Installation

### Download a release

Download the archive for your system from the GitHub **Releases** page:

| System | File |
|---|---|
| macOS, Apple silicon (M1–M4), menu bar app | `Claude-Bridge_<version>_macos_arm64.zip` |
| macOS, Apple silicon, command line | `claude-bridge_<version>_darwin_arm64.tar.gz` |
| Linux x86-64 / ARM64 | `claude-bridge_<version>_linux_amd64.tar.gz` / `_linux_arm64.tar.gz` |
| Windows x86-64 / ARM64 | `claude-bridge_<version>_windows_amd64.zip` / `_windows_arm64.zip` |

Check the download against `SHA256SUMS`: `shasum -a 256 -c SHA256SUMS --ignore-missing`.

**macOS app:** unzip it and move **Claude Bridge.app** to `/Applications`. The app is ad-hoc signed, not notarized, so the first time you open it, right-click the app → **Open** → **Open**. On recent macOS versions you may instead need *System Settings → Privacy & Security → Open Anyway*.

**Command line (all systems):** extract the archive and put `claude-bridge` (`claude-bridge.exe` on Windows) somewhere on your `PATH`, or run it from its folder.

### Build from source

```bash
git clone https://github.com/nnluongone-debug/telegram-slack-claude-bridge.git
cd telegram-slack-claude-bridge
make build          # → bin/claude-bridge
make install-app    # macOS only: builds the menu bar app and installs it in /Applications
make release        # macOS only: all release archives in dist/release
```

## Set up Slack

1. **Create the app.** Go to <https://api.slack.com/apps> → **Create New App** → **From an app manifest**, choose your workspace, and paste the contents of [`slack-app-manifest.yaml`](slack-app-manifest.yaml). It requests:
   - Bot scopes `app_mentions:read`, `channels:history`, `groups:history`, `chat:write` and `files:read` (to download files you send).
   - Socket Mode and interactivity (for the buttons).
2. **Create the app-level token.** Go to *Basic Information* → *App-Level Tokens* → **Generate Token and Scopes**, and add the scope `connections:write`. The token (`xapp-…`) is your `SLACK_APP_TOKEN`.
3. **Install the app.** Go to *Install App* → **Install to Workspace**. The *Bot User OAuth Token* (`xoxb-…`) is your `SLACK_BOT_TOKEN`.
4. **Find your user ID.** Click your profile picture → **Profile** → **⋮** → **Copy member ID** (`U…`). Put it in `ALLOWED_USER_IDS`.
5. **Create one private channel per project** and invite the bot: `/invite @claude-bridge`.
6. **Find each channel ID.** Click the channel name → *About* → the ID (`C…`) is at the bottom. Add it to `projects.json` (see [Configuration](#configuration)).

## Set up Telegram

Telegram works the same way and can run alongside Slack. One **forum group** (a group with Topics turned on) is one project chat, and each session gets its own topic.

1. **Create a bot.** Message [@BotFather](https://t.me/BotFather), send `/newbot`, and follow the steps. Put the token in `.env` as `TELEGRAM_BRIDGE_BOT_TOKEN`.
2. **Get your user ID.** Telegram stays off until its allowlist has an entry, so first set a placeholder (`TELEGRAM_BRIDGE_ALLOWED_USER_IDS=0`) and start the bridge. Send your bot any private message: it replies with your numeric user ID. Replace the placeholder with that ID and restart the bridge.
3. **Create a group** and add the bot. Turn on **Topics** (group → Edit → Topics). Make the bot an **admin** with the *Manage Topics* and *Delete Messages* rights.
4. **Link the group.** Send any message in the group. The bot replies with the group's chat ID and the exact line to add to `projects.json`, for example:
   ```json
   "-1001234567890": { "name": "web-app", "workdir": "$HOME/code/web-app" }
   ```
5. **Start a session** by writing in the **General** topic. The bot opens a new topic named after your prompt. Write inside that topic to talk to Claude. Commands work with `!` or `/` (`/model sonnet`, `/resume`, `/help`).
6. In @BotFather, set `/setjoingroups` → **Disable** so nobody else can add your bot to a group.

On Telegram, tables are shown as aligned monospace text, long replies are split into several messages, and the live status refreshes about every 3 seconds because of Telegram's rate limits.

## Configuration

The bridge reads two files: `.env` (settings and tokens) and `projects.json` (which chat maps to which folder). Start from the examples:

```bash
cp .env.example .env
cp projects.example.json projects.json
chmod 600 .env
```

The CLI reads both files from its **working directory**. The macOS app reads them from its config folder (see [Where files live](#where-files-live)).

### `.env`

| Variable | Default | Description |
|---|---|---|
| `SLACK_BOT_TOKEN` | — | Bot token (`xoxb-…`). Slack is enabled when this is set. |
| `SLACK_APP_TOKEN` | — | App-level token with `connections:write` (`xapp-…`). |
| `ALLOWED_USER_IDS` | — | **Required for Slack.** Comma-separated Slack user IDs. |
| `TELEGRAM_BRIDGE_BOT_TOKEN` | — | Token from @BotFather. Telegram is enabled when this is set. |
| `TELEGRAM_BRIDGE_ALLOWED_USER_IDS` | — | **Required for Telegram.** Comma-separated Telegram user IDs. |
| `PROJECTS_FILE` | `projects.json` | Path to the project map. |
| `CLAUDE_BIN` | `claude` | The Claude Code executable. It is looked up on `PATH`, then in `~/.local/bin`, `~/.claude/local`, `~/.npm-global/bin`, `/opt/homebrew/bin` and `/usr/local/bin`. |
| `CLAUDE_ARGS` | empty | Extra flags for every session, from the [allowed flags](#allowed-claude-flags). Example: `--model opus`. |
| `MAX_SESSIONS` | `10` | Maximum number of concurrent agents (1–100). |
| `OUTPUT_MODE` | `markdown` | `markdown` posts formatted replies from the transcript. `screen` mirrors the raw terminal instead, which is useful for debugging. |
| `PTY_COLS` × `PTY_ROWS` | `100` × `50` | Size of the virtual terminal. A taller screen keeps more history in each update. |
| `FLUSH_INTERVAL` | `2s` | How often the live status is refreshed. Keep it at 1s or more because of rate limits. |
| `DEBUG` | `false` | Verbose Slack client logs. |

Slack and Telegram can be enabled together. If one platform's settings are incomplete, it is turned off with a warning and the other one keeps running.

### `projects.json`

Each key is a chat ID: a Slack channel ID (`C…`) or a Telegram group ID (`-100…`). The value is either **one project** or a **multi-project chat**:

```json
{
  "C0123ABCDEF": { "name": "web-app", "workdir": "$HOME/code/web-app" },

  "C0456GHIJKL": {
    "name": "Team",
    "projects": [
      { "name": "api",    "workdir": "$HOME/code/api", "args": ["--model", "sonnet"] },
      { "name": "mobile", "workdir": "$HOME/code/mobile", "worktree": "never" }
    ]
  },

  "-1001234567890": { "name": "Telegram (all projects)", "projects": ["*"] }
}
```

| Field | Description |
|---|---|
| `name` | Project name shown in the chat. It defaults to the folder name. Names must be unique. |
| `workdir` | The folder Claude works in. `$HOME`, `${HOME}` and a leading `~` are expanded; other variables are not. It may not be `/`, a drive root, your home folder or one of its parents. |
| `args` | Optional extra Claude flags for this project, from the [allowed flags](#allowed-claude-flags). |
| `worktree` | `auto` (default): a session gets its own git worktree when another live session already works in the folder. `always`: every session gets a worktree. `never`: sessions share the folder. |
| `projects` | Turns the entry into a multi-project chat. Items can be project objects defined right there, the names of projects defined elsewhere, or `"*"` for every project. |

Notes:

- **The file is reloaded automatically** within a few seconds of saving. No restart is needed.
- A project with a missing folder or a forbidden flag is **skipped and reported** in the log and the menu bar. The other projects keep working.
- **In a multi-project chat**, a new message asks *Which project?* with buttons. Prefix the message to skip the question, for example `api: fix the failing tests`. A thread stays tied to its project, so `!resume`, `!model` and the other commands just work.
- Worktrees are created by Claude Code under `<workdir>/.claude/worktrees/bridge-<id>` on the branch `worktree-bridge-<id>`. Merge the branch when you are happy with the result.

### Allowed Claude flags

`args` and `CLAUDE_ARGS` may only use these flags:

`--model`, `--fallback-model`, `--permission-mode` (any mode except `bypassPermissions`), `--effort`, `--append-system-prompt`, `--name`/`-n`, `--disallowedTools`, `--verbose`

Anything else is rejected, because `projects.json` is reloaded while the bridge runs and an agent could try to edit it. For other options, such as allowed tools, MCP servers or extra directories, edit Claude Code's own [settings files](https://docs.claude.com/en/docs/claude-code/settings) yourself. Sessions started by the bridge cannot edit them.

`CLAUDE_ARGS` is split on spaces, so a value that contains spaces (such as an `--append-system-prompt` text) only works in `projects.json` `args`.

## Running the bridge

### macOS: menu bar app

Open **Claude Bridge** from Applications or Spotlight. It runs in the menu bar (a chat bubble icon) with no window and no Dock icon. On first launch it creates `.env` and `projects.json` from the examples in its config folder. Fill them in with **Edit settings (.env)…** and **Edit projects…**, then choose **Restart bridge**.

The menu has:

- Connection status, the number of sessions and projects, and any skipped projects (⚠️).
- **Edit projects…**: changes apply automatically within a few seconds.
- **Edit settings (.env)…**, then **Restart bridge** to apply them. Running sessions stop; continue them with `!resume`.
- **Open config folder**, **Open log**.
- **Open at login**.
- **Quit**: stops agent sessions cleanly.

Notes:

- The app loads your login shell's environment (`PATH` and so on), so `claude` and the tools it runs are found the same way as in Terminal.
- The first time an agent works in a protected folder (Documents, Desktop…), macOS asks whether **Claude Bridge** may access it. Allow it once.
- Only one bridge (app or CLI) can run per computer. A second one exits with "already running".

### macOS and Linux: command line

```bash
cd ~/claude-bridge-config    # the folder that holds .env and projects.json
claude-bridge
```

To keep it running on Linux, use a systemd **user** service. Save this as `~/.config/systemd/user/claude-bridge.service`:

```ini
[Unit]
Description=Claude Bridge
After=network-online.target

[Service]
WorkingDirectory=%h/claude-bridge-config
ExecStart=%h/.local/bin/claude-bridge
# claude and the tools it runs must be on this PATH
Environment=PATH=%h/.local/bin:/usr/local/bin:/usr/bin:/bin
Restart=on-failure

[Install]
WantedBy=default.target
```

Then enable it:

```bash
systemctl --user daemon-reload
systemctl --user enable --now claude-bridge
journalctl --user -u claude-bridge -f      # logs
loginctl enable-linger "$USER"             # keep it running when you are logged out
```

### Windows

Open PowerShell in the folder that holds `.env` and `projects.json`, and run:

```powershell
.\claude-bridge.exe
```

To start it at logon, create a task in **Task Scheduler**: trigger *At log on*, action *Start a program* with `claude-bridge.exe`, and set *Start in* to the config folder.

Windows support uses ConPTY and needs Windows 10 1809 or newer. Each agent runs in a job object, so `!stop` also ends every process it started. Windows support is newer than macOS and Linux support, so please report any problems.

## Usage

### Messages

| Where | Message | Effect |
|---|---|---|
| Project chat (top level) | `fix the failing auth tests` | Starts a new session in a thread or topic, with this message as the first prompt. |
| Multi-project chat | `api: fix the failing tests` | Starts the session in project `api` without asking. |
| Session thread or topic | any text | Typed into that session and submitted. Multi-line text is sent as a paste. |
| Anywhere a message works | a photo or file, with or without a caption | The file is downloaded and its path is added to the message, so Claude can read it. |

On Slack you can also @mention the bot.

### Sending files

- Files are saved under the bridge's support folder (see [Where files live](#where-files-live)), outside your project, in `uploads/<project>/`. Claude is allowed to read that folder; it cannot change it.
- One file may be up to **20 MB** (Telegram's limit for bots). Larger files are reported with ❌ and skipped.
- The file is never run or opened, only saved. Files are saved without the execute bit and, on macOS, with the same quarantine flag as a browser download. They are **not deleted automatically**: clean up `uploads/` when you like.
- Telegram: photos and documents are supported; video, audio and voice messages are not. An album (several photos sent at once) is collected for about two seconds and handled as one message with all its files and the caption.
- Slack: files sent together with @mention also work. The app must have the `files:read` scope.
- A `!command` with a file attached runs the command; the file is ignored.

### Commands

| Command | Where | Effect |
|---|---|---|
| `!help` | anywhere | Show help. |
| `!sessions` | anywhere | List live sessions in this chat. |
| `!status` | thread | Session ID, project, folder and uptime. |
| `!stop` | thread | Stop the session. |
| `!restart [prompt]` | thread | Start a fresh session in the same thread. |
| `!resume` | thread | Reopen this thread's last session with its context. This also works after a bridge restart. |
| `!resume <session-id>` | top level | Reopen any session of this chat's projects, including one started on your laptop. |
| `!model <name>` | thread | Switch the model (`opus`, `sonnet`, or a full model ID). The conversation is kept and your default model is not changed. |
| `!mode [plan\|auto\|accept\|manual]` | thread | Show or switch the permission mode (like Shift+Tab). |
| `!screen` | thread | Snapshot of the session's terminal, for example to see a dialog. |
| `!interrupt` / `!esc` / `!enter` | thread | Send Ctrl-C / Esc / Enter. |

On Telegram, every command also works with `/` (`/status`, `/model sonnet`), and the bot registers them in Telegram's command menu.

### Approvals and questions

- **Permission prompts** ("Do you want to proceed? 1. Yes … Esc to cancel"): **Approve** sends `1`, **Reject** sends Esc.
- **Classic `(y/n)` prompts:** **Approve** sends `y`, **Reject** sends `n`.
- **Multiple-choice questions** from Claude get one button per option.
- A button stops working once its prompt is gone, for example if the prompt was already answered by typing.

## Where files live

| | macOS app | CLI (macOS / Linux / Windows) |
|---|---|---|
| `.env`, `projects.json` | `~/Library/Application Support/ClaudeBridge/` | The working directory |
| Thread → session records, lock file | `~/Library/Application Support/ClaudeBridge/` | macOS: same as the app · Linux: `~/.config/ClaudeBridge/` · Windows: `%AppData%\ClaudeBridge\` |
| Files sent in chats | `~/Library/Application Support/ClaudeBridge/uploads/<project>/` | same folder as the records, `uploads/<project>/` |
| Log | `~/Library/Logs/ClaudeBridge/bridge.log` | Standard output |
| Claude transcripts | `~/.claude/projects/<folder>/<session-id>.jsonl` (or `$CLAUDE_CONFIG_DIR`) | same |

## Troubleshooting

| Symptom | Fix |
|---|---|
| `another Claude bridge is already running` | Quit the other copy (menu bar app or CLI). Only one can run per computer. |
| `claude not found on PATH…` | Set `CLAUDE_BIN` in `.env` to the full path printed by `which claude` (`where claude` on Windows). |
| The bot ignores my messages | Check that your user ID is in the allowlist, the channel ID is in `projects.json`, and the bot is in the channel. The log shows `ignoring message from unauthorised user` with the ID it saw. |
| `This channel is not mapped to a project` | Add the channel ID to `projects.json`. |
| A project is missing from the menu | Look for ⚠️ in the menu bar or `skipped` in the log: usually a wrong folder, a folder that is too broad (home or `/`), or a forbidden flag in `args`. |
| The agent says a `.claude/settings` file is denied | Sessions started by the bridge cannot edit Claude Code's configuration. Edit it yourself on the computer. |
| No buttons for a permission prompt | Claude Code's prompt text may have changed. Run `make smoke` and open an issue. Meanwhile, use `!screen` and answer by typing `1`, or use `!esc`. |
| Telegram: "Turn on Topics" | Enable Topics in the group settings. Every session needs its own topic. |
| Windows: `… is a .cmd script` | Install the native `claude.exe` and point `CLAUDE_BIN` at it. |
| macOS: the app can't read Documents | Allow **Claude Bridge** under *System Settings → Privacy & Security → Files and Folders*. |

## Development

```bash
make test     # unit tests and a real PTY round trip (race detector on)
make lint     # go vet + gofmt
make smoke    # live test with your installed claude: detects a real permission prompt and approves it
make app      # macOS menu bar app in dist/ (ARCH=arm64|amd64)
make release  # every release archive in dist/release (run on a Mac)
make icons    # redraw assets/ from scripts/generate-icons.py (needs Pillow)
```

`make smoke` uses your Claude account. Run it after upgrading Claude Code to confirm the prompt format still matches.

### Project layout

```
cmd/claude-bridge/           CLI entry point (all platforms)
cmd/claude-bridge-menubar/   macOS menu bar app
internal/bridgeapp/          wiring, single-instance lock, claude lookup, login items
internal/chatcore/           platform-neutral message routing and ! commands
internal/config/             .env and projects.json loading, flag allowlist
internal/session/            claude processes in a PTY/ConPTY, input, approvals
internal/terminal/           virtual screen, prompt and question detection
internal/transcript/         reading Claude's session transcripts
internal/slackbridge/        Slack Socket Mode adapter
internal/telegrambridge/     Telegram adapter
internal/mdblocks/           Markdown splitting shared by both adapters
assets/                      icons
scripts/                     build, install and icon scripts
```

Contributions are welcome. Please run `make lint test` before opening a pull request.

## Limitations

- In `markdown` mode, slash-command output and dialogs are visible only through `!screen`. In `screen` mode, output that scrolls off between refreshes is not shown.
- Don't type `/model` into a session: Claude Code saves it as your default model for **all** sessions. Use `!model` instead.
- Prompt detection relies on Claude Code's current interface text ("1. Yes", "Esc to cancel"). Re-run `make smoke` after upgrades.
- Restarting the bridge stops running sessions. Continue them with `!resume` in their threads.
- Telegram video, audio and voice messages are not passed to Claude. Photos and documents are (see [Sending files](#sending-files)).

## License

[MIT](LICENSE)
