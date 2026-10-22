package slackbridge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slack-go/slack"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/transcript"
)

// Shaped like the reply in the user's screenshot: prose, a 2-column table, more prose.
const sampleReply = "Việc voucher/coupon đã xong.\n\n## Tiến độ\n\n" +
	"| Mốc | Nội dung |\n|------|---------|\n" +
	"| 15/10 | backend **!257**: 3 bảng silver |\n| 19/10 | sync \\| đối chiếu `100%` |\n\n" +
	"Còn lại: deploy."

func TestMarkdownMessagesUseNativeTables(t *testing.T) {
	msgs := markdownMessages(sampleReply)
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages (text+table, trailing text), got %d", len(msgs))
	}
	first := msgs[0]
	md, ok := first[0].(*slack.MarkdownBlock)
	if !ok || !strings.Contains(md.Text, "## Tiến độ") || strings.Contains(md.Text, "|") {
		t.Fatalf("first block should be the prose before the table: %#v", first[0])
	}
	table, ok := first[1].(*slack.TableBlock)
	if !ok || len(table.Rows) != 3 {
		t.Fatalf("want table block with header + 2 rows: %#v", first[1])
	}
	raw, _ := json.Marshal(table)
	for _, want := range []string{`"bold":true`, `"text":"backend !257: 3 bảng silver"`, `"text":"sync | đối chiếu 100%"`, `"is_wrapped":true`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("table JSON missing %s:\n%s", want, raw)
		}
	}
	if last := msgs[1][0].(*slack.MarkdownBlock); last.Text != "Còn lại: deploy." {
		t.Fatalf("trailing text: %q", last.Text)
	}
}

func TestMarkdownMessagesLeaveCodeFencesAlone(t *testing.T) {
	md := "```\n| not | a table |\n|---|---|\n```"
	msgs := markdownMessages(md)
	if len(msgs) != 1 || msgs[0][0].(*slack.MarkdownBlock).Text != md {
		t.Fatalf("fenced pipes must stay as code: %#v", msgs)
	}
}

func TestSplitMarkdownRespectsLimit(t *testing.T) {
	text := strings.Repeat("đoạn văn dài\n", 3000)
	for _, c := range splitMarkdown(text, maxMarkdownChars) {
		if len(c) > maxMarkdownChars || strings.ToValidUTF8(c, "?") != c {
			t.Fatalf("bad chunk len=%d", len(c))
		}
	}
	if splitMarkdown("  \n ", 10) != nil {
		t.Fatal("blank text must produce no chunks")
	}
}

func TestToolSummaryBlock(t *testing.T) {
	block := toolSummaryBlock([]transcript.Event{
		{Kind: transcript.EventTool, Text: "Bash", Detail: "git log --oneline | head -5"},
		{Kind: transcript.EventTool, Text: "Read", Detail: "backend/models/order.py"},
	})
	raw, _ := json.Marshal(block)
	for _, want := range []string{"*Bash* `git log --oneline | head -5`", "*Read* `backend/models/order.py`"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %q in %s", want, raw)
		}
	}
}

func TestStatusText(t *testing.T) {
	got := statusText("Flummoxing… (10s)", "⏺ Bash(git log)\n  ⎿  Running…", ":wrench: *Bash* `git log`")
	for _, want := range []string{":hourglass_flowing_sand: _Flummoxing… (10s)_", ":wrench: *Bash* `git log`", "```\n⏺ Bash(git log)\n  ⎿  Running…\n```"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
