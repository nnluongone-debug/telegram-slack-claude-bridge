// Package telegrambridge connects Telegram (Bot API long polling) to agent
// sessions: a forum group = project, a topic = session.
package telegrambridge

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/mdblocks"
)

// maxMessageChars stays under Telegram's 4096-character message limit.
const maxMessageChars = 3800

var (
	headingRe = regexp.MustCompile(`^#{1,6}\s+(.+)$`)
	bulletRe  = regexp.MustCompile(`^(\s*)[-*+]\s+`)
	ruleRe    = regexp.MustCompile(`^\s*(-{3,}|\*{3,}|_{3,})\s*$`)
	boldRe    = regexp.MustCompile(`\*\*([^*]+?)\*\*|__([^_]+?)__`)
	italicRe  = regexp.MustCompile(`(^|[^\w*])\*([^*\s][^*]*?)\*`)
	strikeRe  = regexp.MustCompile(`~~(.+?)~~`)
	linkRe    = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)\s]+)\)`)
)

// markdownToTelegramHTML renders Markdown as Telegram-HTML messages, each
// under maxMessageChars, splitting only between lines.
func markdownToTelegramHTML(md string) []string {
	var parts []string
	for _, b := range mdblocks.Split(md) {
		switch b.Kind {
		case mdblocks.Code:
			parts = append(parts, preChunks(b.Text)...)
		case mdblocks.Table:
			parts = append(parts, preChunks(alignTable(b.Header, b.Rows))...)
		default:
			parts = append(parts, proseHTML(b.Text)...)
		}
	}
	return pack(parts)
}

// proseHTML converts Markdown prose line by line; returned pieces never split a tag.
func proseHTML(text string) []string {
	var out []string
	var quote []string
	flushQuote := func() {
		if len(quote) > 0 {
			out = append(out, "<blockquote>"+strings.Join(quote, "\n")+"</blockquote>")
			quote = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimLeft(line, " "), ">"); ok {
			quote = append(quote, inlineHTML(strings.TrimPrefix(rest, " ")))
			continue
		}
		flushQuote()
		switch {
		case ruleRe.MatchString(line):
			out = append(out, "──────────")
		case headingRe.MatchString(line):
			out = append(out, "<b>"+inlineHTML(headingRe.FindStringSubmatch(line)[1])+"</b>")
		default:
			out = append(out, inlineHTML(bulletRe.ReplaceAllString(line, "$1• ")))
		}
	}
	flushQuote()
	return out
}

// inlineHTML escapes a line and applies inline Markdown, never touching code spans.
func inlineHTML(line string) string {
	segments := strings.Split(line, "`")
	if len(segments)%2 == 0 { // unbalanced backtick: treat it literally
		segments = []string{line}
	}
	var sb strings.Builder
	for i, seg := range segments {
		if i%2 == 1 {
			sb.WriteString("<code>" + html.EscapeString(seg) + "</code>")
			continue
		}
		s := html.EscapeString(seg)
		s = linkRe.ReplaceAllString(s, `<a href="$2">$1</a>`)
		s = boldRe.ReplaceAllStringFunc(s, func(m string) string {
			g := boldRe.FindStringSubmatch(m)
			return "<b>" + g[1] + g[2] + "</b>"
		})
		s = italicRe.ReplaceAllString(s, "$1<i>$2</i>")
		s = strikeRe.ReplaceAllString(s, "<s>$1</s>")
		sb.WriteString(s)
	}
	return sb.String()
}

// preChunks wraps text in <pre>, splitting long text into several blocks.
func preChunks(text string) []string {
	var out []string
	var cur []string
	size := 0
	for _, line := range strings.Split(text, "\n") {
		escaped := html.EscapeString(line)
		if size+len(escaped) > maxMessageChars-20 && len(cur) > 0 {
			out = append(out, "<pre>"+strings.Join(cur, "\n")+"</pre>")
			cur, size = nil, 0
		}
		cur = append(cur, escaped)
		size += len(escaped) + 1
	}
	return append(out, "<pre>"+strings.Join(cur, "\n")+"</pre>")
}

// alignTable lays a table out in monospace columns (Telegram has no tables).
func alignTable(header []string, rows [][]string) string {
	all := append([][]string{header}, rows...)
	widths := make([]int, len(header))
	for _, r := range all {
		for i, c := range r {
			widths[i] = max(widths[i], utf8.RuneCountInString(plainCell(c)))
		}
	}
	var lines []string
	for n, r := range all {
		cells := make([]string, len(r))
		for i, c := range r {
			c = plainCell(c)
			cells[i] = c + strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c))
		}
		lines = append(lines, strings.TrimRight(strings.Join(cells, " │ "), " "))
		if n == 0 {
			seps := make([]string, len(widths))
			for i, w := range widths {
				seps[i] = strings.Repeat("─", w)
			}
			lines = append(lines, strings.Join(seps, "─┼─"))
		}
	}
	return strings.Join(lines, "\n")
}

func plainCell(c string) string { return strings.NewReplacer("**", "", "__", "", "`", "").Replace(c) }

// pack joins rendered pieces into messages under maxMessageChars.
func pack(parts []string) []string {
	var msgs []string
	var cur strings.Builder
	for _, p := range parts {
		if cur.Len() > 0 && cur.Len()+len(p)+1 > maxMessageChars {
			msgs = append(msgs, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte('\n')
		}
		cur.WriteString(p)
	}
	if strings.TrimSpace(cur.String()) != "" {
		msgs = append(msgs, cur.String())
	}
	return msgs
}

// tagRe strips tags for the plain-text fallback when Telegram rejects our HTML.
var tagRe = regexp.MustCompile(`<[^>]+>`)

func htmlToPlain(s string) string { return html.UnescapeString(tagRe.ReplaceAllString(s, "")) }
