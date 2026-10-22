package config

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// jsonPath puts dir inside a JSON string: forward slashes need no escaping,
// and Windows accepts them.
func jsonPath(dir string) string { return filepath.ToSlash(dir) }

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// .env values must be readable by the bridge but never exported to the process
// environment, otherwise every agent subprocess would inherit the Slack tokens.
func TestLoadReadsDotEnvWithoutExportingIt(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "projects.json"), `{"C1": {"workdir": "`+jsonPath(dir)+`"}}`)
	writeFile(t, filepath.Join(dir, ".env"), "SLACK_BOT_TOKEN=xoxb-test\nSLACK_APP_TOKEN=xapp-test\nALLOWED_USER_IDS=U1, U2\n")
	for _, k := range []string{"SLACK_BOT_TOKEN", "SLACK_APP_TOKEN", "ALLOWED_USER_IDS", "PROJECTS_FILE", "FLUSH_INTERVAL", "PTY_COLS"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SlackBotToken != "xoxb-test" || !cfg.AllowedUserIDs["U2"] || func() bool { p, _ := cfg.Projects.Lookup("C1"); return p.Name != filepath.Base(dir) }() {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if _, leaked := os.LookupEnv("SLACK_BOT_TOKEN"); leaked {
		t.Fatal("SLACK_BOT_TOKEN was exported to the process environment")
	}
}

func TestLoadRejectsUnsafeValues(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "projects.json"), `{"C1": {"workdir": "`+jsonPath(dir)+`"}}`)
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-x")
	t.Setenv("SLACK_APP_TOKEN", "xapp-x")

	cases := map[string][2]string{
		"empty allowlist": {"ALLOWED_USER_IDS", ""},
		"zero flush":      {"FLUSH_INTERVAL", "0s"},
		"huge pty":        {"PTY_COLS", "70000"},
	}
	for name, kv := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("ALLOWED_USER_IDS", "U1")
			t.Setenv(kv[0], kv[1])
			if _, err := Load(); err == nil {
				t.Fatalf("expected error for %s=%q", kv[0], kv[1])
			}
		})
	}
}

func TestLoadProjectsArgs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	writeFile(t, path, `{"C1": {"workdir": "`+jsonPath(dir)+`", "args": ["--model", "sonnet"]}}`)
	p, _, err := LoadProjects(path)
	if err != nil || len(p["C1"].Args) != 2 {
		t.Fatalf("args not loaded: %+v %v", p, err)
	}
	writeFile(t, path, `{"C1": {"name": "bad-args", "workdir": "`+jsonPath(dir)+`", "args": ["--resume=abc"]},
		"C2": {"name": "typo", "workdir": "`+jsonPath(dir)+`/some/missing"},
		"C3": {"name": "good", "workdir": "`+jsonPath(dir)+`"}}`)
	p, problems, err := LoadProjects(path)
	if err != nil || len(p) != 1 || p["C3"].Name != "good" || len(problems) != 2 ||
		!strings.HasPrefix(problems[0], "bad-args: flag --resume is not allowed") || !strings.HasPrefix(problems[1], "typo: folder not found") {
		t.Fatalf("invalid projects must be skipped and reported: %+v %q %v", p, problems, err)
	}
}

func TestProjectRegistryHotReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	writeFile(t, path, `{"C1": {"workdir": "`+jsonPath(dir)+`"}}`)
	r, err := NewProjectRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	writeFile(t, path, `{"C1": {"workdir": "`+jsonPath(dir)+`"}, "C2": {"name": "new", "workdir": "`+jsonPath(dir)+`"}}`)
	bump(t, path, 1)
	r.reloadIfChanged(log)
	if p, ok := r.Lookup("C2"); !ok || p.Name != "new" {
		t.Fatal("added project not picked up")
	}

	writeFile(t, path, `{broken`)
	bump(t, path, 2)
	r.reloadIfChanged(log)
	if _, ok := r.Lookup("C2"); !ok || r.Count() != 2 {
		t.Fatal("an invalid edit must keep the previous projects")
	}
}

// bump moves the mtime forward so the change is visible even on coarse clocks.
func bump(t *testing.T, path string, seconds int) {
	t.Helper()
	at := time.Now().Add(time.Duration(seconds) * time.Second)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestTelegramOnlyConfig(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "projects.json"), `{"-1001234567890": {"workdir": "`+jsonPath(dir)+`"}}`)
	for _, k := range []string{"SLACK_BOT_TOKEN", "SLACK_APP_TOKEN", "ALLOWED_USER_IDS"} {
		t.Setenv(k, "")
	}
	t.Setenv("TELEGRAM_BRIDGE_BOT_TOKEN", "123456:ABC-def")
	t.Setenv("TELEGRAM_BRIDGE_ALLOWED_USER_IDS", "111, 222")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SlackEnabled() || !cfg.TelegramEnabled() || !cfg.TelegramAllowedUserIDs["222"] {
		t.Fatalf("unexpected platforms: %+v", cfg)
	}
	if _, ok := cfg.Projects.Lookup("-1001234567890"); !ok {
		t.Fatal("Telegram chat ID must work as a project key")
	}

	t.Setenv("TELEGRAM_BRIDGE_BOT_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("no platform configured must be an error")
	}
}

// The menu bar app inherits the user's shell environment, which may hold
// another tool's tokens; .env must win.
func TestDotEnvBeatsInheritedEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "projects.json"), `{"C1": {"workdir": "`+jsonPath(dir)+`"}}`)
	writeFile(t, filepath.Join(dir, ".env"), "SLACK_BOT_TOKEN=xoxb-from-file\nSLACK_APP_TOKEN=xapp-x\nALLOWED_USER_IDS=U1\n")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-from-shell")
	t.Setenv("TELEGRAM_BOT_TOKEN", "999:other-tools-bot") // unrelated name: must not enable Telegram
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SlackBotToken != "xoxb-from-file" || cfg.TelegramEnabled() {
		t.Fatalf("got slack=%q telegram=%v", cfg.SlackBotToken, cfg.TelegramEnabled())
	}
}

// A half-configured platform is switched off with a problem; the others keep running.
func TestIncompletePlatformIsTurnedOffNotFatal(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "projects.json"), `{"C1": {"workdir": "`+jsonPath(dir)+`"}}`)
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-x")
	t.Setenv("SLACK_APP_TOKEN", "xapp-x")
	t.Setenv("ALLOWED_USER_IDS", "U1")
	t.Setenv("TELEGRAM_BRIDGE_BOT_TOKEN", "123:abc")
	t.Setenv("TELEGRAM_BRIDGE_ALLOWED_USER_IDS", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SlackEnabled() || cfg.TelegramEnabled() || len(cfg.PlatformProblems) != 1 ||
		!strings.HasPrefix(cfg.PlatformProblems[0], "Telegram off: set TELEGRAM_BRIDGE_ALLOWED_USER_IDS") {
		t.Fatalf("slack=%v telegram=%v problems=%q", cfg.SlackEnabled(), cfg.TelegramEnabled(), cfg.PlatformProblems)
	}

	t.Setenv("SLACK_BOT_TOKEN", "bad")
	if _, err := Load(); err == nil {
		t.Fatal("no usable platform left must be an error")
	}
}

func TestMultiProjectChat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	writeFile(t, path, `{
		"C1": {"name": "web-app", "workdir": "`+jsonPath(dir)+`"},
		"C2": {"name": "api", "workdir": "`+jsonPath(dir)+`"},
		"-100all": {"name": "Team", "projects": ["*"]},
		"-100some": {"projects": ["API", "missing"]}
	}`)
	r, err := NewProjectRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if r.Count() != 2 {
		t.Fatalf("multi-project chats must not count as projects: %d", r.Count())
	}
	all, _ := r.Lookup("-100all")
	if names := projectNames(r.Members(all)); names != "api,web-app" {
		t.Fatalf("all members: %s", names)
	}
	some, _ := r.Lookup("-100some")
	if !some.IsMulti() || some.Name != "-100some" || projectNames(r.Members(some)) != "api" {
		t.Fatalf("named members: %+v %s", some, projectNames(r.Members(some)))
	}
	if p := r.Problems(); len(p) != 1 || !strings.Contains(p[0], `unknown project "missing"`) {
		t.Fatalf("problems: %q", p)
	}
}

func projectNames(ps []Project) string {
	var names []string
	for _, p := range ps {
		names = append(names, p.Name)
	}
	return strings.Join(names, ",")
}

func TestInlineChildProjects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	writeFile(t, path, `{
		"C0SLACK": {"name": "Team", "projects": [
			{"name": "web-app", "workdir": "`+jsonPath(dir)+`", "args": ["--model", "sonnet"]},
			{"name": "api", "workdir": "`+jsonPath(dir)+`/missing"},
			{"workdir": "`+jsonPath(dir)+`"}
		]},
		"-100tg": {"name": "TG", "projects": ["*"]}
	}`)
	r, err := NewProjectRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	slackChat, _ := r.Lookup("C0SLACK")
	if !slackChat.IsMulti() || projectNames(r.Members(slackChat)) != "web-app" {
		t.Fatalf("slack members: %s", projectNames(r.Members(slackChat)))
	}
	tg, _ := r.Lookup("-100tg")
	if projectNames(r.Members(tg)) != "web-app" {
		t.Fatalf("\"*\" must include inline children: %s", projectNames(r.Members(tg)))
	}
	if p, ok := r.ByName("web-app"); !ok || len(p.Args) != 2 || p.WorkDir != dir {
		t.Fatalf("child project: %+v", p)
	}
	if _, ok := r.Lookup("C0SLACK/web-app"); !ok {
		t.Fatal("children are stored under chat/name keys")
	}
	problems := strings.Join(r.Problems(), "\n")
	for _, want := range []string{"Team › api: folder not found", `needs a "name"`} {
		if !strings.Contains(problems, want) {
			t.Fatalf("missing problem %q in:\n%s", want, problems)
		}
	}
}
