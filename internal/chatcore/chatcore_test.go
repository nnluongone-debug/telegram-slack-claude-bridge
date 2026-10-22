package chatcore

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecentKeysAndKeyedMutex(t *testing.T) {
	r := NewRecentKeys(time.Minute)
	if !r.FirstSeen("C:1") || r.FirstSeen("C:1") || !r.FirstSeen("C:2") {
		t.Fatal("dedupe failed")
	}

	var k KeyedMutex
	unlock := k.Lock("t")
	acquired := make(chan struct{})
	go func() { k.Lock("t")(); close(acquired) }()
	select {
	case <-acquired:
		t.Fatal("second lock on the same key must wait")
	case <-time.After(50 * time.Millisecond):
	}
	k.Lock("other")() // other keys are independent
	unlock()
	<-acquired
	if len(k.locks) != 0 {
		t.Fatalf("idle locks not freed: %d", len(k.locks))
	}
}

func TestSessionDirectoryPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	d := OpenSessionDirectory(path)
	if err := d.Remember("-1001", "42", "sess-a", "web-app"); err != nil {
		t.Fatal(err)
	}
	reloaded := OpenSessionDirectory(path)
	if reloaded.Last("-1001", "42") != "sess-a" || reloaded.ProjectOf("-1001", "42") != "web-app" {
		t.Fatalf("reloaded directory: %q %q", reloaded.Last("-1001", "42"), reloaded.ProjectOf("-1001", "42"))
	}
	legacy := filepath.Join(t.TempDir(), "old.json")
	if err := os.WriteFile(legacy, []byte(`{"C1:t": "sess-old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if OpenSessionDirectory(legacy).Last("C1", "t") != "sess-old" {
		t.Fatal("first file format (plain session IDs) must still load")
	}
	if OpenSessionDirectory(path).Last("-1001", "43") != "" {
		t.Fatal("unknown thread must be empty")
	}
}

func TestApprovalValueRoundTrip(t *testing.T) {
	v := ApprovalValue("0b1c2d3e-4f50-4617-8293-a4b5c6d7e8f9", 12)
	if len("ap:"+v) > 64 {
		t.Fatalf("callback data too long for Telegram: %d bytes", len(v))
	}
	id, seq, err := ParseApprovalValue(v)
	if err != nil || id != "0b1c2d3e-4f50-4617-8293-a4b5c6d7e8f9" || seq != 12 {
		t.Fatalf("got %q %d %v", id, seq, err)
	}
}

func TestProjectPrefixShortcut(t *testing.T) {
	m := projectPrefixRe.FindStringSubmatch("web-app: fix the\nflaky tests")
	if m == nil || m[1] != "web-app" || m[2] != "fix the\nflaky tests" {
		t.Fatalf("got %q", m)
	}
	if projectPrefixRe.MatchString("why does this fail?") || projectPrefixRe.MatchString("note: ") {
		t.Fatal("plain prompts must not look like a project prefix")
	}
}

func TestPendingStartIsUsedOnce(t *testing.T) {
	var p pendingStarts
	id := p.add(pendingStart{prompt: "fix it", at: time.Now()})
	if s, ok := p.take(id); !ok || s.prompt != "fix it" {
		t.Fatal("pending start lost")
	}
	if _, ok := p.take(id); ok {
		t.Fatal("a choice must start only one session")
	}
	old := p.add(pendingStart{at: time.Now().Add(-pendingTTL - time.Minute)})
	if _, ok := p.take(old); ok {
		t.Fatal("expired choice must be rejected")
	}
}

func TestChoiceValueFitsTelegram(t *testing.T) {
	v := ChoiceValue("0b1c2d3e-4f50-4617-8293-a4b5c6d7e8f9", 123, "4")
	if len("ch:"+v) > 64 || v != "0b1c2d3e-4f50-4617-8293-a4b5c6d7e8f9|123|4" {
		t.Fatalf("%q (%d bytes)", v, len(v))
	}
}
