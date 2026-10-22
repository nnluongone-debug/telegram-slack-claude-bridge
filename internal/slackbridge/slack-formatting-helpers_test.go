package slackbridge

import (
	"strings"
	"testing"

	"github.com/slack-go/slack"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/chatcore"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/terminal"
)

func TestTailTextKeepsNewestLines(t *testing.T) {
	text := "old line\nmiddle line\nnewest é line"
	got := tailText(text, 20)
	if got != "…\nnewest é line" {
		t.Fatalf("got %q", got)
	}
	if tailText("short", 20) != "short" {
		t.Fatal("short text must be unchanged")
	}
}

func TestCodeBlockEscapes(t *testing.T) {
	got := codeBlock("a < b && ```x```")
	want := "```\na &lt; b &amp;&amp; ʼʼʼxʼʼʼ\n```"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestApprovalBlocksAndValueParsing(t *testing.T) {
	blocks := approvalBlocks("sess-1", 7, terminal.ApprovalPrompt{Kind: terminal.PromptMenu, Context: "Do you want to proceed?"})
	actions, ok := blocks[len(blocks)-1].(*slack.ActionBlock)
	if !ok || len(actions.Elements.ElementSet) != 2 {
		t.Fatalf("expected action block with 2 buttons")
	}
	btn := actions.Elements.ElementSet[0].(*slack.ButtonBlockElement)
	id, seq, err := chatcore.ParseApprovalValue(btn.Value)
	if err != nil || id != "sess-1" || seq != 7 {
		t.Fatalf("parse %q -> %q %d %v", btn.Value, id, seq, err)
	}
	if _, _, err := chatcore.ParseApprovalValue("garbage"); err == nil {
		t.Fatal("expected error for malformed value")
	}

	resolved := resolvedBlocks(blocks, "done")
	for _, b := range resolved {
		if b.BlockType() == slack.MBTAction {
			t.Fatal("resolved blocks must not contain buttons")
		}
	}
}

func TestFitCodeBlockKeepsBottomAndWholeEntities(t *testing.T) {
	ctx := strings.Repeat("noise && noise\n", 400) + "rm -rf build && make\nDo you want to proceed?"
	got := fitCodeBlock(ctx, maxSectionChars)
	if len(got) > maxSectionChars || !strings.HasSuffix(got, "Do you want to proceed?\n```") {
		t.Fatalf("len=%d tail=%q", len(got), got[max(0, len(got)-60):])
	}
	if !strings.Contains(got, "rm -rf build &amp;&amp; make") {
		t.Fatal("approved command must stay visible and correctly escaped")
	}
}

func TestNormalizeSlackText(t *testing.T) {
	in := "check <https://example.com/a?b=1&amp;c=2|example.com> &lt;tag&gt; <mailto:a@b.co|a@b.co>"
	want := "check https://example.com/a?b=1&c=2 <tag> mailto:a@b.co"
	if got := normalizeSlackText(in); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
