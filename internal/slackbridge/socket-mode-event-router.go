// Package slackbridge connects Slack (Socket Mode) to agent sessions:
// channel = project, thread = session.
package slackbridge

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/chatcore"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/config"
	"github.com/nnluongone-debug/telegram-slack-claude-bridge/internal/uploads"
)

// Bridge wires Slack events to the session manager.
type Bridge struct {
	api       *slack.Client
	socket    *socketmode.Client
	cfg       *config.Config
	core      *chatcore.Core
	poster    *messagePoster
	uploads   *uploads.Store
	log       *slog.Logger
	botUserID string

	seen      *chatcore.RecentKeys // drops events Slack redelivers after reconnects
	connected atomic.Bool
	lastErr   atomic.Pointer[string]
}

// Connected reports whether the Socket Mode connection is currently up.
func (b *Bridge) Connected() bool { return b.connected.Load() }

// Name identifies the platform in status displays.
func (b *Bridge) Name() string { return "Slack" }

// Err returns the error that stopped the bridge, if any.
func (b *Bridge) Err() string {
	if p := b.lastErr.Load(); p != nil {
		return *p
	}
	return ""
}

// New builds a Bridge; call Run to connect. store receives files sent in chats.
func New(cfg *config.Config, core *chatcore.Core, store *uploads.Store, log *slog.Logger) *Bridge {
	api := slack.New(cfg.SlackBotToken, slack.OptionAppLevelToken(cfg.SlackAppToken), slack.OptionDebug(cfg.Debug))
	return &Bridge{
		api:     api,
		socket:  socketmode.New(api, socketmode.OptionDebug(cfg.Debug)),
		cfg:     cfg,
		core:    core,
		poster:  &messagePoster{api: api, log: log},
		uploads: store,
		log:     log,
		seen:    chatcore.NewRecentKeys(chatcore.EventDedupeTTL),
	}
}

// Run authenticates, then processes Socket Mode events until ctx is cancelled.
func (b *Bridge) Run(ctx context.Context) error {
	auth, err := b.api.AuthTestContext(ctx)
	if err != nil {
		msg := "slack: " + err.Error()
		b.lastErr.Store(&msg)
		return fmt.Errorf("slack auth test: %w", err)
	}
	b.botUserID = auth.UserID
	b.log.Info("authenticated with Slack", "team", auth.Team, "bot_user", auth.UserID)

	go b.consumeEvents(ctx)
	return b.socket.RunContext(ctx)
}

func (b *Bridge) consumeEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-b.socket.Events:
			if !ok {
				return
			}
			b.dispatch(ctx, evt)
		}
	}
}

func (b *Bridge) dispatch(ctx context.Context, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeConnecting:
		b.connected.Store(false)
		b.log.Info("connecting to Slack Socket Mode")
	case socketmode.EventTypeConnected:
		b.connected.Store(true)
		b.log.Info("connected to Slack Socket Mode")
	case socketmode.EventTypeConnectionError, socketmode.EventTypeDisconnect:
		b.connected.Store(false)
		b.log.Warn("socket mode connection lost", "type", evt.Type, "data", evt.Data)
	case socketmode.EventTypeEventsAPI:
		b.ack(evt)
		if apiEvt, ok := evt.Data.(slackevents.EventsAPIEvent); ok {
			go b.handleEventsAPI(ctx, apiEvt)
		}
	case socketmode.EventTypeInteractive:
		b.ack(evt)
		if cb, ok := evt.Data.(slack.InteractionCallback); ok {
			go b.handleInteraction(ctx, cb)
		}
	}
}

// ack must happen within 3s or Slack redelivers the event, so it precedes any work.
func (b *Bridge) ack(evt socketmode.Event) {
	if evt.Request == nil {
		return
	}
	if err := b.socket.Ack(*evt.Request); err != nil {
		b.log.Warn("ack failed", "err", err)
	}
}

func (b *Bridge) handleEventsAPI(ctx context.Context, e slackevents.EventsAPIEvent) {
	defer b.recoverPanic("events_api")

	switch ev := e.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		// Skip bot posts, edits/joins/etc. (file uploads excepted), and mentions
		// (those arrive again as app_mention). A mention with files is kept
		// here in case app_mention omits them; the channel:ts dedupe keeps it single.
		isUpload := ev.SubType == "file_share"
		if ev.BotID != "" || (ev.SubType != "" && !isUpload) || ev.User == "" || ev.User == b.botUserID ||
			(!isUpload && strings.Contains(ev.Text, b.mentionTag())) {
			return
		}
		var files []slack.File
		if ev.Message != nil { // the unmarshaller puts a file_share's files here
			files = ev.Message.Files
		}
		b.handleUserMessage(ctx, incomingMessage{
			channel: ev.Channel, user: ev.User, text: ev.Text, ts: ev.TimeStamp, threadTS: ev.ThreadTimeStamp, files: files,
		})
	case *slackevents.AppMentionEvent:
		if ev.BotID != "" {
			return
		}
		b.handleUserMessage(ctx, incomingMessage{
			channel: ev.Channel, user: ev.User, text: ev.Text, ts: ev.TimeStamp, threadTS: ev.ThreadTimeStamp, mentioned: true, files: ev.Files,
		})
	}
}

func (b *Bridge) mentionTag() string { return "<@" + b.botUserID + ">" }

func (b *Bridge) recoverPanic(where string) {
	if r := recover(); r != nil {
		b.log.Error("recovered from panic", "where", where, "panic", r)
	}
}
