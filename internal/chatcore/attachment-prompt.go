package chatcore

import "strings"

const (
	attachmentsLead   = "Look at the attached file(s)."
	attachmentsHeader = "Attached files (read them with the Read tool):"
)

// WithAttachments appends the saved attachment paths to a message so the
// agent knows where to find them. A message with no text gets a short lead
// line, which also becomes the thread title.
func WithAttachments(text string, paths []string) string {
	if len(paths) == 0 {
		return text
	}
	text = strings.TrimSpace(text)
	if text == "" {
		text = attachmentsLead
	}
	var b strings.Builder
	b.WriteString(text)
	b.WriteString("\n\n")
	b.WriteString(attachmentsHeader)
	for _, p := range paths {
		b.WriteString("\n- ")
		b.WriteString(p)
	}
	return b.String()
}
