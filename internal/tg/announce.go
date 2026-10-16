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

	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/store"
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

	// Past this an author can no longer take a post back; Telegram itself
	// stops bots deleting channel posts at 48 hours.
	announceRevisable = 24 * time.Hour

	editPrompt     = "Пришлите новую версию анонса, она заменит предыдущую:"
	noRecentPost   = "За последние сутки вы не отправляли анонсов через бота."
	announceGone   = "Анонс удалён."
	deleteFailed   = "Не получилось удалить анонс :( Напишите разработчику бота"
	announceEdited = "Анонс заменён!"
	oldNotDeleted  = "Новая версия отправлена, но старую удалить не получилось :( Напишите разработчику бота"
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

// lastAnnounce answers the author itself when there is nothing to revise.
func (b *Bot) lastAnnounce(ctx context.Context, message *models.Message) *store.Announce {
	last, err := b.db.LastAnnounce(b.key(message).UserID, time.Now().Add(-announceRevisable))
	if err != nil {
		slog.Error("looking up last announce", "err", err)
	}
	if last == nil {
		b.reply(ctx, message, noRecentPost)
	}
	return last
}

func (b *Bot) deleteEntry(ctx context.Context, message *models.Message) {
	last := b.lastAnnounce(ctx, message)
	if last == nil {
		return
	}
	if b.removeAnnounce(ctx, last) {
		b.reply(ctx, message, announceGone)
	} else {
		b.reply(ctx, message, deleteFailed)
	}
}

func (b *Bot) editEntry(ctx context.Context, message *models.Message) {
	last := b.lastAnnounce(ctx, message)
	if last == nil {
		return
	}
	err := b.fsm.Set(b.key(message), store.State{
		Name: stateAnnounce, Data: map[string]any{"replaces": last.ID},
	})
	if err != nil {
		slog.Error("saving conversation state", "err", err)
	}
	b.reply(ctx, message, editPrompt)
}

func (b *Bot) removeAnnounce(ctx context.Context, announce *store.Announce) bool {
	target := b.config.AnnounceChannelID
	params := &bot.DeleteMessagesParams{ChatID: target, MessageIDs: announce.MessageIDs}
	b.logs.RecordTraffic("out", "deleteMessages", target, "", jsonOf(params))
	if _, err := b.api.DeleteMessages(ctx, params); err != nil {
		slog.Error("deleting announce", "message_ids", announce.MessageIDs, "err", err)
		return false
	}
	if err := b.db.DeleteAnnounce(announce.ID); err != nil {
		slog.Error("forgetting deleted announce", "id", announce.ID, "err", err)
	}
	return true
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

	key := b.key(message)
	replaces := int64(b.fsm.Get(key).Int("replaces"))
	if err := b.fsm.Clear(key); err != nil {
		slog.Error("clearing conversation state", "err", err)
	}

	chatID := message.Chat.ID
	isForward := group[0].ForwardOrigin != nil
	posted, sent := b.relay(ctx, chatID, messageIDs, isForward)
	if !sent && !isForward {
		slog.Info("copy refused, forwarding instead", "message_ids", messageIDs)
		posted, sent = b.relay(ctx, chatID, messageIDs, true)
	}
	if !sent {
		b.reply(ctx, message, announceFailed)
		slog.Error("could not send announce", "chat_id", chatID)
		return
	}
	slog.Info("announce sent", "chat_id", chatID, "text", body)
	if err := b.db.RecordAnnounce(key.UserID, posted, time.Now()); err != nil {
		slog.Error("recording announce", "err", err)
	}

	switch {
	case replaces == 0:
		b.reply(ctx, message, announceSent)
	case b.replaceAnnounce(ctx, key.UserID, replaces):
		b.reply(ctx, message, announceEdited)
	default:
		b.reply(ctx, message, oldNotDeleted)
	}
}

// replaceAnnounce deletes the post an /edit started from, provided it is
// still the author's to delete.
func (b *Bot) replaceAnnounce(ctx context.Context, authorID, id int64) bool {
	old, err := b.db.Announce(authorID, id)
	if err != nil || old == nil {
		slog.Error("finding the announce being replaced", "id", id, "err", err)
		return false
	}
	return b.removeAnnounce(ctx, old)
}

// relay hands the message back to Telegram by id, copying it when it is the
// organiser's own and forwarding it when it is not. It returns the ids the
// post got in the channel.
func (b *Bot) relay(ctx context.Context, fromChatID int64, messageIDs []int, forward bool) ([]int, bool) {
	target := b.config.AnnounceChannelID
	method, posted, err := b.callRelay(ctx, target, fromChatID, messageIDs, forward)
	b.logs.RecordTraffic("out", method, target, "", jsonOf(map[string]any{
		"chat_id": target, "from_chat_id": fromChatID, "message_ids": messageIDs,
	}))
	if err != nil {
		slog.Error("relaying announce", "method", method, "message_ids", messageIDs,
			"target", target, "err", err)
		return nil, false
	}
	return posted, true
}

func (b *Bot) callRelay(
	ctx context.Context, target, fromChatID int64, messageIDs []int, forward bool,
) (string, []int, error) {
	if len(messageIDs) > 1 {
		if forward {
			ids, err := b.api.ForwardMessages(ctx, &bot.ForwardMessagesParams{
				ChatID: target, FromChatID: fromChatID, MessageIDs: messageIDs})
			return "forwardMessages", idsOf(ids), err
		}
		ids, err := b.api.CopyMessages(ctx, &bot.CopyMessagesParams{
			ChatID: target, FromChatID: fromChatID, MessageIDs: messageIDs})
		return "copyMessages", idsOf(ids), err
	}
	if forward {
		posted, err := b.api.ForwardMessage(ctx, &bot.ForwardMessageParams{
			ChatID: target, FromChatID: fromChatID, MessageID: messageIDs[0]})
		if err != nil {
			return "forwardMessage", nil, err
		}
		return "forwardMessage", []int{posted.ID}, nil
	}
	posted, err := b.api.CopyMessage(ctx, &bot.CopyMessageParams{
		ChatID: target, FromChatID: fromChatID, MessageID: messageIDs[0]})
	if err != nil {
		return "copyMessage", nil, err
	}
	return "copyMessage", []int{posted.ID}, nil
}

func idsOf(messages []models.MessageID) []int {
	ids := make([]int, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
	}
	return ids
}
