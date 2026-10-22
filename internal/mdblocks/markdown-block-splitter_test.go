package mdblocks

import (
	"slices"
	"testing"
)

func TestSplit(t *testing.T) {
	md := "## Tiến độ\n\n| Mốc | Nội dung |\n|---|---|\n| 15/10 | a \\| b |\n| 19/10 |\n\nText with | pipe\n```go\n| not | table |\n|---|---|\n```\nend"
	blocks := Split(md)
	kinds := []Kind{}
	for _, b := range blocks {
		kinds = append(kinds, b.Kind)
	}
	if !slices.Equal(kinds, []Kind{Prose, Table, Prose, Code, Prose}) {
		t.Fatalf("kinds %v", kinds)
	}
	table := blocks[1]
	if !slices.Equal(table.Header, []string{"Mốc", "Nội dung"}) || !slices.Equal(table.Rows[0], []string{"15/10", "a | b"}) || !slices.Equal(table.Rows[1], []string{"19/10", ""}) {
		t.Fatalf("table %+v", table)
	}
	if blocks[3].Lang != "go" || blocks[3].Text != "| not | table |\n|---|---|" {
		t.Fatalf("code %+v", blocks[3])
	}
	if blocks[0].Text != "## Tiến độ" || blocks[4].Text != "end" {
		t.Fatalf("prose %q %q", blocks[0].Text, blocks[4].Text)
	}
}

func TestUnterminatedFence(t *testing.T) {
	blocks := Split("intro\n```\ncode line")
	if len(blocks) != 2 || blocks[1].Kind != Code || blocks[1].Text != "code line" {
		t.Fatalf("%+v", blocks)
	}
}
