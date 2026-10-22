// Package assets embeds the app's icons. Regenerate them with
// scripts/generate-icons.py.
package assets

import _ "embed"

// MenubarTemplate is the menu bar icon: black on transparent, tinted by macOS.
//
//go:embed menubar-template@2x.png
var MenubarTemplate []byte
