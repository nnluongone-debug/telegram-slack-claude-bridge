package telegrambridge

import (
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
)

func albumItem(chat int64, group string, id int, caption string) *models.Message {
	return &models.Message{ID: id, Chat: models.Chat{ID: chat}, MediaGroupID: group, Caption: caption,
		Photo: []models.PhotoSize{{FileID: "f" + caption, FileUniqueID: "u"}}}
}

func TestAlbumCollectorGroupsItemsOfOneAlbum(t *testing.T) {
	c := newAlbumCollector(50 * time.Millisecond)
	var mu sync.Mutex
	flushed := map[string][][]*models.Message{}
	record := func(key string) func([]*models.Message) {
		return func(msgs []*models.Message) {
			mu.Lock()
			defer mu.Unlock()
			flushed[key] = append(flushed[key], msgs)
		}
	}
	c.add(albumItem(1, "A", 1, "look"), record("A"))
	c.add(albumItem(1, "A", 2, ""), record("A"))
	c.add(albumItem(2, "A", 3, ""), record("other-chat")) // same group ID in another chat is a different album
	c.add(albumItem(1, "B", 4, ""), record("B"))

	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(flushed["A"]) != 1 || len(flushed["A"][0]) != 2 {
		t.Fatalf("album A: want one flush with 2 items, got %v", flushed["A"])
	}
	if len(flushed["other-chat"]) != 1 || len(flushed["B"]) != 1 {
		t.Fatalf("other albums must flush on their own: %v", flushed)
	}
	if got := messageText(flushed["A"][0]...); got != "look" {
		t.Fatalf("caption from any item should be the text, got %q", got)
	}
	if got := attachments(flushed["A"][0]...); len(got) != 2 {
		t.Fatalf("want 2 photos, got %v", got)
	}
}

func TestMessageTextPrefersTextOverCaption(t *testing.T) {
	if got := messageText(&models.Message{Caption: "cap"}, &models.Message{Text: "txt"}); got != "txt" {
		t.Fatalf("got %q", got)
	}
	if got := messageText(&models.Message{}); got != "" {
		t.Fatalf("got %q", got)
	}
}
