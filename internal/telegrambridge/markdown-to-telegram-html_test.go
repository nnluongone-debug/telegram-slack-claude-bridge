package telegrambridge

import (
	"regexp"
	"strings"
	"testing"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/chatcore"
)

func TestMarkdownToTelegramHTML(t *testing.T) {
	md := "## Tiến độ\n\n- **backend !257**: 3 bảng `silver_<x>` & *mart*\n> note\n\n" +
		"| Mốc | Nội dung |\n|---|---|\n| 15/10 | voucher |\n\n```go\nif a < b {}\n```\n[docs](https://x.y/a?b=1&c=2) ~~old~~"
	msgs := markdownToTelegramHTML(md)
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	got := msgs[0]
	for _, want := range []string{
		"<b>Tiến độ</b>",
		"• <b>backend !257</b>: 3 bảng <code>silver_&lt;x&gt;</code> &amp; <i>mart</i>",
		"<blockquote>note</blockquote>",
		"<pre>Mốc   │ Nội dung\n──────┼─────────\n15/10 │ voucher</pre>",
		"<pre>if a &lt; b {}</pre>",
		`<a href="https://x.y/a?b=1&amp;c=2">docs</a> <s>old</s>`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestLongRepliesAreSplitOutsideTags(t *testing.T) {
	code := "```\n" + strings.Repeat("line of code\n", 600) + "```"
	msgs := markdownToTelegramHTML(strings.Repeat("paragraph text\n", 400) + code)
	if len(msgs) < 3 {
		t.Fatalf("expected several messages, got %d", len(msgs))
	}
	for _, m := range msgs {
		if len(m) > maxMessageChars || strings.Count(m, "<pre>") != strings.Count(m, "</pre>") {
			t.Fatalf("bad chunk len=%d", len(m))
		}
	}
}

func TestSnakeCaseIsNotItalic(t *testing.T) {
	if got := inlineHTML("use mart_voucher_invoices and 2*3*4"); strings.Contains(got, "<i>") {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeCommand(t *testing.T) {
	cases := map[string]string{
		"/model@my_claude_bot sonnet": "!model sonnet",
		"/start":                      "!help",
		"/resume":                     "!resume",
		"!mode plan":                  "!mode plan",
		"fix the build":               "fix the build",
	}
	for in, want := range cases {
		if got := normalizeCommand(in, "my_claude_bot"); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestTopicNameAndLink(t *testing.T) {
	if got := topicName("[web-app]   kiểm tra voucher\nchi tiết"); got != "🤖 [web-app]   kiểm tra voucher" {
		t.Fatalf("got %q", got)
	}
	if got := []rune(topicName(strings.Repeat("x", 200))); len(got) > 70 {
		t.Fatalf("topic name too long: %d", len(got))
	}
}

// Every suggested command must be valid for Telegram and handled by the core.
func TestCommandMenuMatchesCoreCommands(t *testing.T) {
	nameRe := regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
	for _, c := range commandMenu {
		if !nameRe.MatchString(c.Command) || c.Description == "" || len(c.Description) > 256 {
			t.Fatalf("invalid command %+v", c)
		}
		if !strings.Contains(chatcore.HelpText, "`!"+c.Command) && c.Command != "help" {
			t.Fatalf("/%s is not a core command (missing from HelpText)", c.Command)
		}
	}
}
