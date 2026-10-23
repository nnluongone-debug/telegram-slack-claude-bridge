package telegrambridge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/chatcore"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/config"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/uploads"
)

// attachment is a document or photo sent with a message.
type attachment struct {
	fileID, name string
	size         int64
}

// attachments lists what the messages carry: documents and the largest size
// of each photo. Video, audio and voice are not handled.
func attachments(msgs ...*models.Message) []attachment {
	var out []attachment
	for _, m := range msgs {
		if d := m.Document; d != nil {
			out = append(out, attachment{fileID: d.FileID, name: d.FileName, size: d.FileSize})
		}
		if n := len(m.Photo); n > 0 {
			p := m.Photo[n-1]
			out = append(out, attachment{fileID: p.FileID, name: "photo-" + p.FileUniqueID + ".jpg", size: int64(p.FileSize)})
		}
	}
	return out
}

// saveAttachments downloads files into the uploads store and returns their
// paths. Each failure is reported in the chat; the rest continue.
func (b *Bridge) saveAttachments(ctx context.Context, conv chatcore.Conversation, project config.Project, files []attachment) []string {
	var paths []string
	for _, a := range files {
		path, err := b.download(ctx, project.Name, a)
		if err != nil {
			b.log.Warn("attachment download failed", "file", a.name, "err", err)
			conv.Reply("❌ Could not download " + a.name + ": " + err.Error())
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func (b *Bridge) download(ctx context.Context, projectName string, a attachment) (string, error) {
	if a.size > b.uploads.MaxBytes {
		return "", fmt.Errorf("file is larger than %d MB", b.uploads.MaxBytes>>20)
	}
	ctx, cancel := context.WithTimeout(ctx, uploads.DownloadTimeout)
	defer cancel()
	file, err := b.bot.GetFile(ctx, &bot.GetFileParams{FileID: a.fileID})
	if err != nil {
		return "", redactURL(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.bot.FileDownloadLink(file), nil)
	if err != nil {
		return "", redactURL(err)
	}
	resp, err := b.http.Do(req)
	if err != nil {
		return "", redactURL(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return b.uploads.Save(ctx, projectName, a.name, resp.Body)
}

// redactURL strips the request URL from HTTP errors: Telegram's download
// link contains the bot token.
func redactURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("%s: %w", urlErr.Op, urlErr.Err)
	}
	return err
}
