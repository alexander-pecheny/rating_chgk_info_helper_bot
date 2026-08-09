package tg

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/go-telegram/bot/models"

	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/dates"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/store"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/text"
)

// The state names are aiogram's, so a conversation begun before the switch to
// this binary is still understood after it.
const (
	stateSubscribe     = "Flows:subscribe"
	stateSubscribeIzh  = "Flows:subscribe_izh"
	stateUnsubscribe   = "Flows:unsubscribe"
	stateTop3          = "Flows:top3"
	stateHost          = "Flows:host"
	stateDatesPrefs    = "Flows:dates_prefs"
	stateAnnounce      = "Flows:announce"
	stateDatesStart    = "Flows:dates_start"
	stateDatesSyncDays = "Flows:dates_sync_days"
	stateDatesAsync    = "Flows:dates_async_days"
)

const (
	datePrompt = "Введите дату либо дату и время (UTC+3) начала синхрона в формате:" +
		" <pre>2023-01-31</pre> либо <pre>2023-01-31 11:00</pre>"
	hostPrompt = "Введите новый хост (например, <pre>rating.chgk.info</pre>" +
		" или <pre>rating.pecheny.me</pre>)"
	syncDaysPrompt  = "Сколько дней от 1 до 7 будет длиться синхрон?"
	asyncDaysPrompt = "Сколько дней будет длиться асинхрон? Если не хотите асинхрон, напишите 0"
	daysError       = "Неверный формат. Введите количество дней от 1 до 7"
)

// Step reports what to say and whether that finished the conversation.
type Step func(ctx context.Context, message *models.Message, input string) (string, bool)

// registerFlow wires a command that either completes from its arguments or
// asks for one more message and waits for it.
func (b *Bot) registerFlow(command, state string, step Step) {
	advance := func(ctx context.Context, message *models.Message, input string) {
		reply, finished := step(ctx, message, input)
		// Settle the state before the round-trip; updates are handled
		// concurrently, so a fast follow-up can arrive while the reply is in
		// flight.
		var err error
		if finished {
			err = b.fsm.Clear(b.key(message))
		} else {
			err = b.fsm.SetName(b.key(message), state)
		}
		if err != nil {
			slog.Error("saving conversation state", "err", err)
		}
		if reply != "" {
			b.reply(ctx, message, reply)
		}
	}
	b.commands[command] = func(ctx context.Context, message *models.Message) {
		advance(ctx, message, commandArgument(message, command))
	}
	b.states[state] = func(ctx context.Context, message *models.Message) {
		advance(ctx, message, message.Text)
	}
}

func (b *Bot) registerHandlers() {
	b.commands = map[string]Handler{}
	b.states = map[string]Handler{}

	b.commands["start"] = func(ctx context.Context, message *models.Message) {
		b.reply(ctx, message, text.Start)
	}
	b.commands["cancel"] = func(ctx context.Context, message *models.Message) {
		b.replyPlain(ctx, message, "Команда отменена.")
		slog.Info("conversation cancelled", "chat_id", message.Chat.ID)
	}

	b.registerFlow("subscribe", stateSubscribe, b.subscribeStep(false))
	b.registerFlow("subscribe_izh", stateSubscribeIzh, b.subscribeStep(true))
	b.registerFlow("unsubscribe", stateUnsubscribe, b.unsubscribeStep)
	b.registerFlow("get_top3", stateTop3, b.top3Step)
	b.registerFlow("set_host", stateHost, b.hostStep)
	b.registerFlow("set_dates_prefs", stateDatesPrefs, b.datesPrefsStep)

	b.commands["my_subscriptions"] = b.mySubscriptions
	b.commands["get_dates_prefs"] = b.getDatesPrefs
	b.commands["get_dates"] = b.getDates
	b.states[stateDatesStart] = b.datesStart
	b.states[stateDatesSyncDays] = b.datesSyncDays
	b.states[stateDatesAsync] = b.datesAsyncDays

	b.commands["announce"] = b.announceEntry
	b.states[stateAnnounce] = b.announce

	b.registerAdminHandlers()
}

func (b *Bot) subscribeStep(juryOnly bool) Step {
	return func(_ context.Context, message *models.Message, input string) (string, bool) {
		ids := text.ListOfInts(input)
		if len(ids) == 0 {
			return text.IDTournsPrompt, false
		}
		replies := make([]string, 0, len(ids))
		for _, id := range ids {
			replies = append(replies, b.Subscribe(id, message.Chat.ID, juryOnly))
		}
		return strings.Join(replies, "\n"), true
	}
}

func (b *Bot) unsubscribeStep(_ context.Context, message *models.Message, input string) (string, bool) {
	ids := text.ListOfInts(input)
	if len(ids) == 0 {
		return text.IDTournsPrompt, false
	}
	replies := make([]string, 0, len(ids))
	for _, id := range ids {
		replies = append(replies, b.Unsubscribe(id, message.Chat.ID))
	}
	return strings.Join(replies, "\n"), true
}

func (b *Bot) top3Step(_ context.Context, _ *models.Message, input string) (string, bool) {
	ids := text.ListOfInts(input)
	if len(ids) != 1 {
		return text.IDTournPrompt, false
	}
	top3, err := b.rating.Top3(ids[0])
	if err != nil {
		slog.Error("getting top 3", "tournament", ids[0], "err", err)
		return "Не удалось получить результаты турнира, попробуйте позже.", true
	}
	return top3, true
}

func (b *Bot) hostStep(_ context.Context, message *models.Message, input string) (string, bool) {
	host := text.StripHost(input)
	if host == "" {
		return hostPrompt, false
	}
	if !text.ValidHost(host) {
		return "Не удалось распарсить хост, попробуйте ещё раз", false
	}
	prefs, err := b.db.Prefs(message.Chat.ID)
	if err != nil {
		slog.Error("reading prefs", "chat_id", message.Chat.ID, "err", err)
	}
	prefs.Host = host
	if err := b.db.SetPrefs(message.Chat.ID, prefs); err != nil {
		slog.Error("saving prefs", "chat_id", message.Chat.ID, "err", err)
	}
	return fmt.Sprintf("Ваш хост теперь <pre>%s</pre>.", host), true
}

// datesPrefs reads a chat's stored date settings, falling back to the defaults
// for anything it never set.
func (b *Bot) datesPrefs(chatID int64) (store.Prefs, dates.Prefs) {
	stored, err := b.db.Prefs(chatID)
	if err != nil {
		slog.Error("reading prefs", "chat_id", chatID, "err", err)
	}
	var prefs dates.Prefs
	if len(stored.Dates) > 0 {
		if err := json.Unmarshal(stored.Dates, &prefs); err != nil {
			slog.Error("reading date prefs", "chat_id", chatID, "err", err)
		}
	}
	return stored, prefs
}

func (b *Bot) datesPrefsStep(_ context.Context, message *models.Message, input string) (string, bool) {
	stored, prefs := b.datesPrefs(message.Chat.ID)
	if strings.TrimSpace(input) == "" {
		return fmt.Sprintf("Ваши настройки дат: %s.\n\nВведите новые настройки:", prefs.HR()), false
	}
	prefs.UpdateFromString(input)
	encoded, err := json.Marshal(prefs)
	if err != nil {
		slog.Error("encoding date prefs", "err", err)
		return "Не удалось сохранить настройки.", true
	}
	stored.Dates = encoded
	if err := b.db.SetPrefs(message.Chat.ID, stored); err != nil {
		slog.Error("saving prefs", "chat_id", message.Chat.ID, "err", err)
	}
	return fmt.Sprintf("Ваши настройки дат теперь: %s.", prefs.HR()), true
}

func (b *Bot) getDatesPrefs(ctx context.Context, message *models.Message) {
	_, prefs := b.datesPrefs(message.Chat.ID)
	b.reply(ctx, message, fmt.Sprintf("Ваши настройки дат: %s.", prefs.HR()))
}

func (b *Bot) getDates(ctx context.Context, message *models.Message) {
	if err := b.fsm.SetName(b.key(message), stateDatesStart); err != nil {
		slog.Error("saving conversation state", "err", err)
	}
	b.reply(ctx, message, datePrompt)
}

func (b *Bot) datesStart(ctx context.Context, message *models.Message) {
	input := strings.TrimSpace(message.Text)
	if _, err := dates.ParseStart(input, dates.Prefs{}); err != nil {
		b.reply(ctx, message, "Неверный формат. "+datePrompt)
		return
	}
	if err := b.fsm.Update(b.key(message), map[string]any{"sync_start": input}); err != nil {
		slog.Error("saving conversation state", "err", err)
	}
	if err := b.fsm.SetName(b.key(message), stateDatesSyncDays); err != nil {
		slog.Error("saving conversation state", "err", err)
	}
	b.reply(ctx, message, syncDaysPrompt)
}

func (b *Bot) datesSyncDays(ctx context.Context, message *models.Message) {
	days, ok := dates.TryInt(message.Text)
	if !ok || days == 0 {
		b.reply(ctx, message, daysError)
		return
	}
	if err := b.fsm.Update(b.key(message), map[string]any{"sync_days": days}); err != nil {
		slog.Error("saving conversation state", "err", err)
	}
	if err := b.fsm.SetName(b.key(message), stateDatesAsync); err != nil {
		slog.Error("saving conversation state", "err", err)
	}
	b.reply(ctx, message, asyncDaysPrompt)
}

func (b *Bot) datesAsyncDays(ctx context.Context, message *models.Message) {
	asyncDays, _ := dates.TryInt(message.Text)
	state := b.fsm.Get(b.key(message))
	_, prefs := b.datesPrefs(message.Chat.ID)
	start, parseErr := dates.ParseStart(state.String("sync_start"), prefs)
	if err := b.fsm.Clear(b.key(message)); err != nil {
		slog.Error("clearing conversation state", "err", err)
	}
	if parseErr != nil {
		b.reply(ctx, message, "Неверный формат. "+datePrompt)
		return
	}
	b.reply(ctx, message, dates.Generate(start, state.Int("sync_days"), asyncDays, prefs))
}

func (b *Bot) mySubscriptions(ctx context.Context, message *models.Message) {
	chatID := message.Chat.ID
	tournaments, err := b.db.Tournaments()
	if err != nil {
		slog.Error("reading tournaments", "err", err)
	}
	var names []string
	for _, tournament := range tournaments {
		if _, subscribed := tournament.Subscribers[chatID]; subscribed {
			names = append(names, fmt.Sprintf("%d %s", tournament.ID, tournament.Name))
		}
	}
	if len(names) == 0 {
		b.replyPlain(ctx, message, "Сейчас вы не подписаны ни на один турнир.")
		return
	}
	host := b.db.HostOf(chatID, text.DefaultHost)
	links := make([]string, 0, len(names))
	for _, name := range names {
		links = append(links, text.TournamentLink(name, host))
	}
	b.reply(ctx, message, "Турниры, на которые вы подписаны:\n"+strings.Join(links, "\n"))
}

// sortedIDs keeps admin listings stable between runs.
func sortedIDs(ids map[int64][]string) []int64 {
	out := make([]int64, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
