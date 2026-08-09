package tg

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/config"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/rating"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/store"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/text"
)

// Handler answers one message. Everything it needs beyond the message hangs
// off the Bot it is a method of.
type Handler func(ctx context.Context, message *models.Message)

type Bot struct {
	api       *bot.Bot
	db        *store.DB
	fsm       *store.FSM
	logs      *store.LogStore
	rating    *rating.Client
	config    config.Config
	scheduler *Scheduler
	tap       *updateTap
	botID     int64

	commands map[string]Handler
	states   map[string]Handler
	albums   *albumBuffer
}

type Options struct {
	Config    config.Config
	DB        *store.DB
	Logs      *store.LogStore
	Rating    *rating.Client
	Scheduler *Scheduler
	// ServerURL points the bot at a stand-in Telegram, which is how the tests
	// drive it.
	ServerURL string
	// BotID identifies the conversation rows in fsm_state. Left unset, it is
	// read off the token.
	BotID int64
}

func New(options Options) (*Bot, error) {
	tap := newUpdateTap(&http.Client{Timeout: 90 * time.Second})
	b := &Bot{
		db:        options.DB,
		fsm:       store.NewFSM(options.DB, store.IdleTTL),
		logs:      options.Logs,
		rating:    options.Rating,
		config:    options.Config,
		scheduler: options.Scheduler,
		tap:       tap,
		botID:     options.BotID,
		albums:    newAlbumBuffer(),
	}
	if b.botID == 0 {
		b.botID = botIDFromToken(options.Config.Token)
	}

	botOptions := []bot.Option{
		bot.WithDefaultHandler(b.dispatch),
		bot.WithMiddlewares(b.logIncoming),
		bot.WithHTTPClient(30*time.Second, tap),
		bot.WithSkipGetMe(),
		bot.WithErrorsHandler(func(err error) { slog.Error("telegram", "err", err) }),
	}
	if options.ServerURL != "" {
		botOptions = append(botOptions, bot.WithServerURL(options.ServerURL))
	}

	api, err := bot.New(options.Config.Token, botOptions...)
	if err != nil {
		return nil, err
	}
	b.api = api
	b.registerHandlers()
	return b, nil
}

// botIDFromToken reads the numeric part every bot token starts with, which is
// the bot's own user id.
func botIDFromToken(token string) int64 {
	id, _, _ := strings.Cut(token, ":")
	var n int64
	for _, digit := range id {
		if digit < '0' || digit > '9' {
			return 0
		}
		n = n*10 + int64(digit-'0')
	}
	return n
}

func (b *Bot) Start(ctx context.Context) { b.api.Start(ctx) }

func (b *Bot) key(message *models.Message) store.Key {
	userID := message.Chat.ID
	if message.From != nil {
		userID = message.From.ID
	}
	return store.Key{
		BotID:   b.botID,
		ChatID:  message.Chat.ID,
		UserID:  userID,
		Destiny: "default",
	}
}

// commandName is the bare command a message begins with, or "" when it is not
// a command at all.
func commandName(message *models.Message) string {
	if !strings.HasPrefix(message.Text, "/") {
		return ""
	}
	word := strings.Fields(message.Text)[0][1:]
	name, _, _ := strings.Cut(word, "@")
	return name
}

// commandArgument is everything the organiser typed after the command itself.
func commandArgument(message *models.Message, command string) string {
	if len(message.Text) <= len(command)+1 {
		return ""
	}
	return message.Text[len(command)+1:]
}

// dispatch is the whole routing rule: a command the bot really has always wins
// and ends whatever conversation was in progress, and anything else continues
// the conversation the chat is already in.
func (b *Bot) dispatch(ctx context.Context, _ *bot.Bot, update *models.Update) {
	message := update.Message
	if message == nil {
		return
	}

	command := commandName(message)
	handler, isCommand := b.commands[command]
	if isCommand {
		// An announcement that happens to start with a slash is conversation
		// input, not an escape; only a command the bot answers clears state.
		if err := b.fsm.Clear(b.key(message)); err != nil {
			slog.Error("clearing state", "err", err)
		}
	} else {
		var known bool
		handler, known = b.states[b.fsm.Get(b.key(message)).Name]
		if !known {
			return
		}
	}

	if b.db.IsBanned(message.Chat.ID) {
		b.replyPlain(ctx, message, "Вы забанены.")
		return
	}
	handler(ctx, message)
}

// logIncoming records every update, whether or not a handler wants it.
func (b *Bot) logIncoming(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, api *bot.Bot, update *models.Update) {
		chatID, summary := incomingSummary(update)
		payload := b.tap.claim(update.ID)
		if payload == "" {
			if encoded, err := json.Marshal(update); err == nil {
				payload = string(encoded)
			}
		}
		b.logs.RecordTraffic("in", "", chatID, summary, payload)
		next(ctx, api, update)
	}
}

func incomingSummary(update *models.Update) (int64, string) {
	for _, message := range []*models.Message{
		update.Message, update.EditedMessage, update.ChannelPost, update.EditedChannelPost,
	} {
		if message != nil {
			return message.Chat.ID, MessageText(message)
		}
	}
	if update.CallbackQuery != nil {
		var chatID int64
		if update.CallbackQuery.Message.Message != nil {
			chatID = update.CallbackQuery.Message.Message.Chat.ID
		}
		return chatID, update.CallbackQuery.Data
	}
	return 0, ""
}

// send delivers a message in as many batches as its length needs, recording
// each one on the way out.
func (b *Bot) send(ctx context.Context, chatID int64, message string, html bool) {
	for _, batch := range text.Batches(message) {
		params := &bot.SendMessageParams{ChatID: chatID, Text: batch}
		if html {
			params.ParseMode = models.ParseModeHTML
		}
		b.logs.RecordTraffic("out", "sendMessage", chatID, batch, jsonOf(params))
		if _, err := b.api.SendMessage(ctx, params); err != nil {
			slog.Error("messaging chat", "chat_id", chatID, "err", err)
		}
	}
}

func (b *Bot) reply(ctx context.Context, message *models.Message, answer string) {
	b.send(ctx, message.Chat.ID, answer, true)
}

func (b *Bot) replyPlain(ctx context.Context, message *models.Message, answer string) {
	b.send(ctx, message.Chat.ID, answer, false)
}

func jsonOf(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}
