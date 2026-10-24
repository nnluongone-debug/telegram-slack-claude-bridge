package transcript

import (
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var nonAlnumRe = regexp.MustCompile(`[^a-zA-Z0-9]`)

// projectDirName is Claude Code's transcript folder name for a working
// directory: every non-alphanumeric character becomes "-".
func projectDirName(workDir string) string {
	if resolved, err := filepath.EvalSymlinks(workDir); err == nil {
		workDir = resolved
	}
	return nonAlnumRe.ReplaceAllString(workDir, "-")
}

// worktreeDirMarker joins a project folder and a Claude worktree name in a
// transcript folder name (".claude/worktrees/" encoded).
const worktreeDirMarker = "--claude-worktrees-"

// Exists reports whether a transcript for sessionID is on disk.
func Exists(sessionID string) bool {
	_, ok := locate(sessionID)
	return ok
}

// SessionBelongsTo reports whether sessionID ran in workDir or in one of its
// Claude worktrees (<workDir>/.claude/worktrees/<name>).
func SessionBelongsTo(sessionID, workDir string) bool {
	path, ok := locate(sessionID)
	if !ok {
		return false
	}
	dir, project := filepath.Base(filepath.Dir(path)), projectDirName(workDir)
	return sameFolder(dir, project) || hasFolderPrefix(dir, project+worktreeDirMarker)
}

// sameFolder compares transcript folder names. macOS and Windows file systems
// ignore case, so a folder first created from a session in ".../BI" also
// receives the transcripts of sessions started in ".../bi".
func sameFolder(a, b string) bool {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// hasFolderPrefix is sameFolder for the start of dir. Folder names are
// ASCII (see nonAlnumRe), so byte slicing is safe.
func hasFolderPrefix(dir, prefix string) bool {
	return len(dir) >= len(prefix) && sameFolder(dir[:len(prefix)], prefix)
}

// WorktreeName returns the Claude worktree sessionID ran in under workDir, or "".
func WorktreeName(sessionID, workDir string) string {
	path, ok := locate(sessionID)
	if !ok {
		return ""
	}
	dir, prefix := filepath.Base(filepath.Dir(path)), projectDirName(workDir)+worktreeDirMarker
	if !hasFolderPrefix(dir, prefix) {
		return ""
	}
	return dir[len(prefix):]
}
