// Package mdblocks splits Claude's Markdown replies into prose, fenced code
// and GFM tables, so each chat adapter can render them natively.
package mdblocks

import (
	"regexp"
	"strings"
)

// Kind is the type of a Block.
type Kind int

const (
	Prose Kind = iota + 1 // ordinary Markdown text
	Code                  // fenced code block
	Table                 // GFM table
)

// Block is one top-level piece of a Markdown document.
type Block struct {
	Kind   Kind
	Text   string     // Prose: the Markdown; Code: the code without fences
	Lang   string     // Code: the info string after ```
	Header []string   // Table
	Rows   [][]string // Table: each row padded/truncated to len(Header)
}

var (
	tableDelimRe  = regexp.MustCompile(`^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)*\|?\s*$`)
	escapedPipeRe = strings.NewReplacer(`\|`, "\x00")
)

// Split parses md into blocks in document order. Consecutive prose lines form
// one block; an unterminated fence runs to the end of the document.
func Split(md string) []Block {
	lines := strings.Split(md, "\n")
	var blocks []Block
	var prose []string
	flushProse := func() {
		if text := strings.Trim(strings.Join(prose, "\n"), "\n"); strings.TrimSpace(text) != "" {
			blocks = append(blocks, Block{Kind: Prose, Text: text})
		}
		prose = nil
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if fence, ok := strings.CutPrefix(strings.TrimSpace(line), "```"); ok {
			flushProse()
			end := i + 1
			for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[end]), "```") {
				end++
			}
			blocks = append(blocks, Block{Kind: Code, Lang: strings.TrimSpace(fence), Text: strings.Join(lines[i+1:min(end, len(lines))], "\n")})
			i = end
			continue
		}
		if isTableRow(line) && i+1 < len(lines) && tableDelimRe.MatchString(lines[i+1]) {
			flushProse()
			header := splitRow(line, 0)
			end := i + 2
			var rows [][]string
			for end < len(lines) && isTableRow(lines[end]) {
				rows = append(rows, splitRow(lines[end], len(header)))
				end++
			}
			blocks = append(blocks, Block{Kind: Table, Header: header, Rows: rows})
			i = end - 1
			continue
		}
		prose = append(prose, line)
	}
	flushProse()
	return blocks
}

// Markdown renders a block back to Markdown (tables as GFM).
func (b Block) Markdown() string {
	switch b.Kind {
	case Code:
		return "```" + b.Lang + "\n" + b.Text + "\n```"
	case Table:
		var sb strings.Builder
		sb.WriteString("| " + strings.Join(b.Header, " | ") + " |\n|")
		sb.WriteString(strings.Repeat(" --- |", len(b.Header)))
		for _, r := range b.Rows {
			sb.WriteString("\n| " + strings.Join(r, " | ") + " |")
		}
		return sb.String()
	default:
		return b.Text
	}
}

func isTableRow(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "|") }

// splitRow splits "| a | b |" into cells; width > 0 pads/truncates to match the header.
func splitRow(line string, width int) []string {
	line = strings.TrimSpace(escapedPipeRe.Replace(line))
	line = strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")
	cells := strings.Split(line, "|")
	for i, c := range cells {
		cells[i] = strings.TrimSpace(strings.ReplaceAll(c, "\x00", "|"))
	}
	if width > 0 {
		for len(cells) < width {
			cells = append(cells, "")
		}
		cells = cells[:width]
	}
	return cells
}
