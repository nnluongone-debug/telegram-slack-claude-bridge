package slackbridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/slack-go/slack"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/chatcore"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/config"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/uploads"
)

// saveAttachments downloads the message's files into the uploads store and
// returns their paths. Each failure is reported in the thread; the rest continue.
func (b *Bridge) saveAttachments(ctx context.Context, conv chatcore.Conversation, project config.Project, files []slack.File) []string {
	var paths []string
	for _, f := range files {
		name := fileName(f)
		path, err := b.download(ctx, project.Name, name, f)
		if err != nil {
			b.log.Warn("attachment download failed", "file", name, "err", err)
			conv.Reply("❌ Could not download " + name + ": " + err.Error())
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// fileName picks a name Claude can recognise: Slack's name or title, else the
// file ID, with the file type as extension when the name has none.
func fileName(f slack.File) string {
	name := f.Name
	if name == "" {
		name = f.Title
	}
	if name == "" {
		name = f.ID
	}
	if filepath.Ext(name) == "" && f.Filetype != "" {
		name += "." + f.Filetype
	}
	return name
}

func (b *Bridge) download(ctx context.Context, projectName, name string, f slack.File) (string, error) {
	if int64(f.Size) > b.uploads.MaxBytes {
		return "", fmt.Errorf("file is larger than %d MB", b.uploads.MaxBytes>>20)
	}
	if f.URLPrivateDownload == "" {
		return "", errors.New("no download URL (external or deleted file)")
	}
	ctx, cancel := context.WithTimeout(ctx, uploads.DownloadTimeout)
	defer cancel()
	// Stream Slack's response straight into the store; the writer side ends the pipe.
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(b.api.GetFileContext(ctx, f.URLPrivateDownload, pw))
	}()
	path, err := b.uploads.Save(ctx, projectName, name, pr)
	pr.Close() // unblocks the download if the store stopped reading early
	return path, err
}
