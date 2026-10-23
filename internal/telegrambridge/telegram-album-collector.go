package telegrambridge

import (
	"fmt"
	"sync"
	"time"

	"github.com/go-telegram/bot/models"
)

// albumWait is how long an album (several photos sent at once) is collected
// before it is handled as one message. Telegram delivers the items as separate
// updates with the same media_group_id, usually within a second.
const albumWait = 2 * time.Second

// albumCollector groups the messages of one album so a multi-photo message
// starts one session instead of one per photo.
type albumCollector struct {
	wait    time.Duration
	mu      sync.Mutex
	pending map[string]*album
}

type album struct {
	msgs  []*models.Message
	flush func([]*models.Message)
}

func newAlbumCollector(wait time.Duration) *albumCollector {
	return &albumCollector{wait: wait, pending: map[string]*album{}}
}

// add queues m with its album; flush runs once with every item collected
// within wait of the first.
func (c *albumCollector) add(m *models.Message, flush func([]*models.Message)) {
	key := fmt.Sprintf("%d:%s", m.Chat.ID, m.MediaGroupID)
	c.mu.Lock()
	defer c.mu.Unlock()
	if a, ok := c.pending[key]; ok {
		a.msgs = append(a.msgs, m)
		return
	}
	c.pending[key] = &album{msgs: []*models.Message{m}, flush: flush}
	time.AfterFunc(c.wait, func() { c.take(key) })
}

func (c *albumCollector) take(key string) {
	c.mu.Lock()
	a := c.pending[key]
	delete(c.pending, key)
	c.mu.Unlock()
	if a != nil {
		a.flush(a.msgs)
	}
}

// messageText is a message's text, or the caption under its photo or file.
// For an album Telegram attaches the caption to one of the items.
func messageText(msgs ...*models.Message) string {
	for _, m := range msgs {
		if m.Text != "" {
			return m.Text
		}
	}
	for _, m := range msgs {
		if m.Caption != "" {
			return m.Caption
		}
	}
	return ""
}
