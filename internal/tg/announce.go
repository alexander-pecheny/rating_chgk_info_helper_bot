package tg

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// An announcement is still just a message handed back to Telegram by id, so
// this stays agnostic about how its content is encoded: it needs enough text to
// judge the length, and the ids to send.
const (
	minAnnounceLength = 200
	// Telegram delivers an album as separate updates; they land within a few
	// milliseconds of each other, so a short wait collects the whole set.
	albumDebounce = time.Second

	announcePrompt = "Пришлите сообщение для отправки в канал с анонсами:"
	tooShort       = "Слишком короткое сообщение. Введите новую версию или нажмите /cancel"
	unparseable    = "Не получилось распарсить сообщение :( Введите новую версию или нажмите /cancel"
	announceSent   = "Анонс успешно отправлен!"
	announceFailed = "Что-то пошло не так :( Напишите разработчику бота"
)

type albumKey struct {
	chatID  int64
	groupID string
}

type albumBuffer struct {
	debounce time.Duration

	mu     sync.Mutex
	groups map[albumKey][]*models.Message
	timers map[albumKey]*time.Timer
	// Stragglers of an album already relayed must not start a second post.
	recent map[albumKey]bool
	order  []albumKey
}

func newAlbumBuffer() *albumBuffer {
	return &albumBuffer{
		debounce: albumDebounce,
		groups:   map[albumKey][]*models.Message{},
		timers:   map[albumKey]*time.Timer{},
		recent:   map[albumKey]bool{},
	}
}

func (a *albumBuffer) markSent(key albumKey) {
	a.recent[key] = true
	a.order = append(a.order, key)
	if len(a.order) > 64 {
		delete(a.recent, a.order[0])
		a.order = a.order[1:]
	}
}

func (b *Bot) announceEntry(ctx context.Context, message *models.Message) {
	// Set the state before the round-trip: updates are handled concurrently,
	// so a fast follow-up can arrive while the prompt is still in flight.
	if err := b.fsm.SetName(b.key(message), stateAnnounce); err != nil {
		slog.Error("saving conversation state", "err", err)
	}
	b.reply(ctx, message, announcePrompt)
}

func (b *Bot) announce(ctx context.Context, message *models.Message) {
	if message.MediaGroupID == "" {
		b.deliverAnnounce(ctx, message, []*models.Message{message})
		return
	}
	b.collectAlbum(message)
}

func (b *Bot) collectAlbum(message *models.Message) {
	key := albumKey{chatID: message.Chat.ID, groupID: message.MediaGroupID}
	a := b.albums
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.recent[key] {
		slog.Info("ignoring album item arriving after the album was relayed",
			"chat_id", key.chatID, "album", key.groupID)
		return
	}
	a.groups[key] = append(a.groups[key], message)
	if timer, waiting := a.timers[key]; waiting {
		timer.Stop()
	}
	a.timers[key] = time.AfterFunc(a.debounce, func() { b.flushAlbum(key) })
}

func (b *Bot) flushAlbum(key albumKey) {
	a := b.albums
	a.mu.Lock()
	group := a.groups[key]
	delete(a.groups, key)
	delete(a.timers, key)
	if len(group) == 0 {
		a.mu.Unlock()
		return
	}
	a.markSent(key)
	a.mu.Unlock()

	sort.Slice(group, func(i, j int) bool { return group[i].ID < group[j].ID })
	// The organiser may have cancelled while the debounce was pending.
	if b.fsm.Get(b.key(group[0])).Name != stateAnnounce {
		slog.Info("dropping album, conversation already left", "chat_id", key.chatID)
		return
	}
	b.deliverAnnounce(context.Background(), group[0], group)
}

func (b *Bot) deliverAnnounce(ctx context.Context, message *models.Message, group []*models.Message) {
	var parts []string
	messageIDs := make([]int, 0, len(group))
	for _, item := range group {
		if part := MessageText(item); part != "" {
			parts = append(parts, part)
		}
		messageIDs = append(messageIDs, item.ID)
	}
	body := strings.Join(parts, "\n")

	if body == "" {
		b.reply(ctx, message, unparseable)
		return
	}
	if utf8.RuneCountInString(body) < minAnnounceLength {
		b.reply(ctx, message, tooShort)
		return
	}

	chatID := message.Chat.ID
	isForward := group[0].ForwardOrigin != nil
	sent := b.relay(ctx, chatID, messageIDs, isForward)
	if !sent && !isForward {
		slog.Info("copy refused, forwarding instead", "message_ids", messageIDs)
		sent = b.relay(ctx, chatID, messageIDs, true)
	}

	if sent {
		b.reply(ctx, message, announceSent)
		slog.Info("announce sent", "chat_id", chatID, "text", body)
	} else {
		b.reply(ctx, message, announceFailed)
		slog.Error("could not send announce", "chat_id", chatID)
	}
	if err := b.fsm.Clear(b.key(message)); err != nil {
		slog.Error("clearing conversation state", "err", err)
	}
}

// relay hands the message back to Telegram by id, copying it when it is the
// organiser's own and forwarding it when it is not.
func (b *Bot) relay(ctx context.Context, fromChatID int64, messageIDs []int, forward bool) bool {
	target := b.config.AnnounceChannelID
	method, err := b.callRelay(ctx, target, fromChatID, messageIDs, forward)
	b.logs.RecordTraffic("out", method, target, "", jsonOf(map[string]any{
		"chat_id": target, "from_chat_id": fromChatID, "message_ids": messageIDs,
	}))
	if err != nil {
		slog.Error("relaying announce", "method", method, "message_ids", messageIDs,
			"target", target, "err", err)
		return false
	}
	return true
}

func (b *Bot) callRelay(
	ctx context.Context, target, fromChatID int64, messageIDs []int, forward bool,
) (string, error) {
	if len(messageIDs) > 1 {
		if forward {
			_, err := b.api.ForwardMessages(ctx, &bot.ForwardMessagesParams{
				ChatID: target, FromChatID: fromChatID, MessageIDs: messageIDs})
			return "forwardMessages", err
		}
		_, err := b.api.CopyMessages(ctx, &bot.CopyMessagesParams{
			ChatID: target, FromChatID: fromChatID, MessageIDs: messageIDs})
		return "copyMessages", err
	}
	if forward {
		_, err := b.api.ForwardMessage(ctx, &bot.ForwardMessageParams{
			ChatID: target, FromChatID: fromChatID, MessageID: messageIDs[0]})
		return "forwardMessage", err
	}
	_, err := b.api.CopyMessage(ctx, &bot.CopyMessageParams{
		ChatID: target, FromChatID: fromChatID, MessageID: messageIDs[0]})
	return "copyMessage", err
}
