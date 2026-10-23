package chatcore

import "testing"

func TestWithAttachments(t *testing.T) {
	if got := WithAttachments("fix it", nil); got != "fix it" {
		t.Fatalf("no paths should leave text alone, got %q", got)
	}
	want := "fix it\n\nAttached files (read them with the Read tool):\n- /u/a.png\n- /u/b.pdf"
	if got := WithAttachments("fix it ", []string{"/u/a.png", "/u/b.pdf"}); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	want = "Look at the attached file(s).\n\nAttached files (read them with the Read tool):\n- /u/a.png"
	if got := WithAttachments("", []string{"/u/a.png"}); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	// The "name: prompt" project shortcut must still match on the first line.
	if m := projectPrefixRe.FindStringSubmatch(WithAttachments("api: fix", []string{"/u/a.png"})); m == nil || m[1] != "api" {
		t.Fatalf("project prefix lost: %v", m)
	}
}
