// Package config loads bridge settings from the environment (.env) and the
// channel→project registry JSON file.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	minFlushInterval = 500 * time.Millisecond
	maxPTYSize       = 1000
)

// Config holds every runtime setting of the bridge.
type Config struct {
	SlackBotToken  string          // xoxb-...; Slack is enabled when set
	SlackAppToken  string          // xapp-... (Socket Mode)
	AllowedUserIDs map[string]bool // Slack user IDs

	TelegramBotToken       string // TELEGRAM_BRIDGE_BOT_TOKEN from @BotFather; Telegram is enabled when set
	TelegramAllowedUserIDs map[string]bool

	// PlatformProblems explains platforms left off because their settings are
	// incomplete; the other platforms still run.
	PlatformProblems []string
	slackOff         bool
	telegramOff      bool

	Projects *ProjectRegistry // keyed by Slack channel ID or Telegram chat ID, reloaded on change

	ClaudeBin     string
	ClaudeArgs    []string
	FlushInterval time.Duration
	MaxSessions   int
	PTYCols       uint16
	PTYRows       uint16
	MirrorScreen  bool // OUTPUT_MODE=screen: post the terminal screen instead of markdown replies
	Debug         bool
}

// Load reads settings from .env, falling back to the process environment, and
// validates them. .env wins because the menu bar app inherits the user's whole
// shell environment, which may hold unrelated tokens. .env is read into a
// private map rather than exported, so tokens never reach agent processes.
func Load() (*Config, error) {
	fileEnv, err := godotenv.Read()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read .env: %w", err)
	}
	getenv := func(key string) string {
		if v, ok := fileEnv[key]; ok {
			return v
		}
		return os.Getenv(key)
	}
	envOr := func(key, fallback string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return fallback
	}
	envInt := func(key string, fallback, maxVal int) (int, error) {
		raw := getenv(key)
		if raw == "" {
			return fallback, nil
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > maxVal {
			return 0, fmt.Errorf("%s must be an integer in 1..%d, got %q", key, maxVal, raw)
		}
		return n, nil
	}

	cfg := &Config{
		SlackBotToken:          strings.TrimSpace(getenv("SLACK_BOT_TOKEN")),
		SlackAppToken:          strings.TrimSpace(getenv("SLACK_APP_TOKEN")),
		AllowedUserIDs:         parseIDSet(getenv("ALLOWED_USER_IDS")),
		TelegramBotToken:       strings.TrimSpace(getenv("TELEGRAM_BRIDGE_BOT_TOKEN")),
		TelegramAllowedUserIDs: parseIDSet(getenv("TELEGRAM_BRIDGE_ALLOWED_USER_IDS")),
		ClaudeBin:              envOr("CLAUDE_BIN", "claude"),
		ClaudeArgs:             strings.Fields(getenv("CLAUDE_ARGS")),
		Debug:                  getenv("DEBUG") == "true",
	}
	if err := cfg.validatePlatforms(); err != nil {
		return nil, err
	}
	if err := ValidateClaudeArgs(cfg.ClaudeArgs); err != nil {
		return nil, fmt.Errorf("CLAUDE_ARGS: %w", err)
	}

	if cfg.FlushInterval, err = time.ParseDuration(envOr("FLUSH_INTERVAL", "2s")); err != nil || cfg.FlushInterval < minFlushInterval {
		return nil, fmt.Errorf("FLUSH_INTERVAL must be a duration >= %s", minFlushInterval)
	}
	if cfg.MaxSessions, err = envInt("MAX_SESSIONS", 10, 100); err != nil {
		return nil, err
	}
	cols, err := envInt("PTY_COLS", 100, maxPTYSize)
	if err != nil {
		return nil, err
	}
	rows, err := envInt("PTY_ROWS", 50, maxPTYSize)
	if err != nil {
		return nil, err
	}
	cfg.PTYCols, cfg.PTYRows = uint16(cols), uint16(rows)

	switch mode := envOr("OUTPUT_MODE", "markdown"); mode {
	case "markdown", "screen":
		cfg.MirrorScreen = mode == "screen"
	default:
		return nil, fmt.Errorf("OUTPUT_MODE must be markdown or screen, got %q", mode)
	}

	if cfg.Projects, err = NewProjectRegistry(envOr("PROJECTS_FILE", "projects.json")); err != nil {
		return nil, err
	}
	return cfg, nil
}

func parseIDSet(raw string) map[string]bool {
	set := map[string]bool{}
	for _, id := range strings.Split(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			set[id] = true
		}
	}
	return set
}

// SlackEnabled reports whether Slack is configured and valid.
func (c *Config) SlackEnabled() bool {
	return (c.SlackBotToken != "" || c.SlackAppToken != "") && !c.slackOff
}

// TelegramEnabled reports whether Telegram is configured and valid.
func (c *Config) TelegramEnabled() bool { return c.TelegramBotToken != "" && !c.telegramOff }

// validatePlatforms turns off a platform whose settings are incomplete (with a
// problem for the UI) and fails only if no platform is left. Each platform needs
// an allowlist: the bridge runs an agent with shell access.
func (c *Config) validatePlatforms() error {
	if c.SlackEnabled() {
		if problem := slackProblem(c); problem != "" {
			c.slackOff = true
			c.PlatformProblems = append(c.PlatformProblems, "Slack off: "+problem)
		}
	}
	if c.TelegramEnabled() {
		if problem := telegramProblem(c); problem != "" {
			c.telegramOff = true
			c.PlatformProblems = append(c.PlatformProblems, "Telegram off: "+problem)
		}
	}
	if !c.SlackEnabled() && !c.TelegramEnabled() {
		if len(c.PlatformProblems) > 0 {
			return errors.New(strings.Join(c.PlatformProblems, "; "))
		}
		return errors.New("configure SLACK_BOT_TOKEN/SLACK_APP_TOKEN and/or TELEGRAM_BRIDGE_BOT_TOKEN")
	}
	return nil
}

func slackProblem(c *Config) string {
	switch {
	case !strings.HasPrefix(c.SlackBotToken, "xoxb-"):
		return "SLACK_BOT_TOKEN must start with xoxb-"
	case !strings.HasPrefix(c.SlackAppToken, "xapp-"):
		return "SLACK_APP_TOKEN must start with xapp-"
	case len(c.AllowedUserIDs) == 0:
		return "ALLOWED_USER_IDS must list at least one Slack user ID"
	}
	return ""
}

func telegramProblem(c *Config) string {
	switch {
	case !strings.Contains(c.TelegramBotToken, ":"):
		return "TELEGRAM_BRIDGE_BOT_TOKEN looks invalid (expected 123456:ABC… from @BotFather)"
	case len(c.TelegramAllowedUserIDs) == 0:
		return "set TELEGRAM_BRIDGE_ALLOWED_USER_IDS (message your bot privately to get your ID)"
	}
	return ""
}
