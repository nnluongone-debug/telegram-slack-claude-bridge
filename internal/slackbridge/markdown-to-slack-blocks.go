package slackbridge

import (
	"regexp"
	"strings"

	"github.com/slack-go/slack"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/mdblocks"
)

const (
	maxMarkdownChars = 10000 // markdown blocks share a 12,000-char budget per message
	maxTableRows     = 100   // Slack table block limits (header row included)
	maxTableCols     = 20
)

var inlineMarkRe = regexp.MustCompile("\\*\\*|__|`")

// markdownMessages turns one markdown reply into Slack messages. Text goes in
// markdown blocks (Slack renders headings, lists, code, links). GFM tables
// become native table blocks; Slack shows a table below the rest of its
// message, so every table closes the message that holds the text before it.
func markdownMessages(md string) [][]slack.Block {
	var msgs [][]slack.Block
	var text []string
	for _, b := range mdblocks.Split(md) {
		if b.Kind != mdblocks.Table || len(b.Rows)+1 > maxTableRows || len(b.Header) > maxTableCols {
			text = append(text, b.Markdown()) // too-large tables stay as Markdown text
			continue
		}
		chunks := splitMarkdown(strings.Join(text, "\n\n"), maxMarkdownChars)
		text = nil
		msg := []slack.Block{}
		if n := len(chunks); n > 0 {
			for _, c := range chunks[:n-1] {
				msgs = append(msgs, []slack.Block{slack.NewMarkdownBlock("", c)})
			}
			msg = append(msg, slack.NewMarkdownBlock("", chunks[n-1]))
		}
		msgs = append(msgs, append(msg, tableBlock(b.Header, b.Rows)))
	}
	for _, c := range splitMarkdown(strings.Join(text, "\n\n"), maxMarkdownChars) {
		msgs = append(msgs, []slack.Block{slack.NewMarkdownBlock("", c)})
	}
	return msgs
}

func tableBlock(header []string, rows [][]string) *slack.TableBlock {
	table := slack.NewTableBlock("")
	settings := make([]slack.ColumnSetting, len(header))
	headCells := make([]slack.TableCell, len(header))
	for i, h := range header {
		settings[i] = slack.ColumnSetting{Align: slack.ColumnAlignmentLeft, IsWrapped: true}
		style := &slack.RichTextSectionTextStyle{Bold: true}
		headCells[i] = slack.NewTableRichTextCell(slack.NewRichTextSection(slack.NewRichTextSectionTextElement(cellText(h), style)))
	}
	table.WithColumnSettings(settings...).AddRow(headCells...)
	for _, row := range rows {
		cells := make([]slack.TableCell, len(row))
		for i, c := range row {
			cells[i] = slack.NewTableRawTextCell(cellText(c))
		}
		table.AddRow(cells...)
	}
	return table
}

// cellText drops inline markdown markers (table cells are plain text) and
// never returns an empty string.
func cellText(s string) string {
	if s = strings.TrimSpace(inlineMarkRe.ReplaceAllString(s, "")); s == "" {
		return "–"
	}
	return s
}

// splitMarkdown cuts text into chunks of at most limit bytes on paragraph,
// then line boundaries. Blank input yields no chunks.
func splitMarkdown(text string, limit int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(text) <= limit {
		return []string{text}
	}
	var chunks []string
	var cur strings.Builder
	for _, line := range strings.SplitAfter(text, "\n") {
		if cur.Len()+len(line) > limit && cur.Len() > 0 {
			chunks = append(chunks, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
		for len(line) > limit { // a single giant line: hard split on a rune boundary
			cut := limit
			for cut > 0 && !isRuneStart(line[cut]) {
				cut--
			}
			chunks = append(chunks, line[:cut])
			line = line[cut:]
		}
		cur.WriteString(line)
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		chunks = append(chunks, s)
	}
	return chunks
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
