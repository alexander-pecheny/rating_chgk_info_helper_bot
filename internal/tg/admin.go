package tg

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/dates"
)

// adminOnly refuses everyone but the bot's admins, and says so.
func (b *Bot) adminOnly(handler Handler) Handler {
	return func(ctx context.Context, message *models.Message) {
		if !b.config.IsAdmin(message.Chat.ID) {
			b.replyPlain(ctx, message, "Вы не админ бота.")
			return
		}
		handler(ctx, message)
	}
}

func (b *Bot) registerAdminHandlers() {
	admin := map[string]Handler{
		"debug_info":      b.debugInfo,
		"ban":             b.ban,
		"unban":           b.unban,
		"get_subscribers": b.getSubscribers,
		"run_check_requests": b.runJob("check_applications",
			"regular job scheduled", b.CheckApplications),
		"run_make_reminders": b.runJob("make_reminders",
			"regular job scheduled", b.MakeReminders),
		"run_check_requests_debug": b.runJob("check_applications_debug",
			"regular job scheduled in debug mode", b.checkApplicationsForAdmins),
		"run_test_job": b.runJob("test_job", "test job scheduled", b.testJob),
		"echo_md":      b.echo("echo_md", models.ParseModeMarkdown),
		"echo_html":    b.echo("echo_html", models.ParseModeHTML),
	}
	for command, handler := range admin {
		b.commands[command] = b.adminOnly(handler)
	}
}

func (b *Bot) debugInfo(ctx context.Context, message *models.Message) {
	times := b.scheduler.NextRuns()
	if len(times) == 0 {
		b.replyPlain(ctx, message, "no jobs scheduled")
		return
	}
	b.replyPlain(ctx, message, "next regular job will be run at "+times[0].String())
}

func (b *Bot) ban(ctx context.Context, message *models.Message) {
	chatID, ok := parseChatID(message, "ban")
	if !ok {
		b.replyPlain(ctx, message, "ID чата не распознан")
		return
	}
	banned, err := b.db.Ban(chatID)
	if err != nil {
		slog.Error("banning", "chat_id", chatID, "err", err)
	}
	if banned {
		b.replyPlain(ctx, message, fmt.Sprintf("Пользователь %d забанен", chatID))
		return
	}
	b.replyPlain(ctx, message, fmt.Sprintf("Пользователь %d уже забанен", chatID))
}

func (b *Bot) unban(ctx context.Context, message *models.Message) {
	chatID, ok := parseChatID(message, "unban")
	if !ok {
		b.replyPlain(ctx, message, "ID чата не распознан")
		return
	}
	unbanned, err := b.db.Unban(chatID)
	if err != nil {
		slog.Error("unbanning", "chat_id", chatID, "err", err)
	}
	if unbanned {
		b.replyPlain(ctx, message, fmt.Sprintf("Пользователь %d разбанен", chatID))
		return
	}
	b.replyPlain(ctx, message, fmt.Sprintf("Пользователь %d и так не забанен", chatID))
}

func parseChatID(message *models.Message, command string) (int64, bool) {
	value, ok := dates.TryInt(commandArgument(message, command))
	return int64(value), ok && value != 0
}

func (b *Bot) getSubscribers(ctx context.Context, message *models.Message) {
	tournaments, err := b.db.Tournaments()
	if err != nil {
		slog.Error("reading tournaments", "err", err)
	}
	perChat := map[int64][]string{}
	for _, tournament := range tournaments {
		for chatID := range tournament.Subscribers {
			perChat[chatID] = append(perChat[chatID],
				fmt.Sprintf("%d %s", tournament.ID, tournament.Name))
		}
	}
	var lines []string
	for i, chatID := range sortedIDs(perChat) {
		lines = append(lines, fmt.Sprintf("%d. %d - %d tournaments (%s)",
			i+1, chatID, len(perChat[chatID]), strings.Join(perChat[chatID], ", ")))
	}
	lines = append(lines, fmt.Sprintf("%d chats subscribed to %d unique tournaments",
		len(lines), len(tournaments)))
	b.replyPlain(ctx, message, strings.Join(lines, "\n"))
}

func (b *Bot) runJob(name, reply string, job func(context.Context)) Handler {
	return func(ctx context.Context, message *models.Message) {
		b.scheduler.RunSoon(name, job)
		b.replyPlain(ctx, message, reply)
	}
}

func (b *Bot) echo(command string, mode models.ParseMode) Handler {
	return func(ctx context.Context, message *models.Message) {
		answer := commandArgument(message, command)
		if answer == "" {
			return
		}
		params := &bot.SendMessageParams{
			ChatID: message.Chat.ID, Text: answer, ParseMode: mode,
		}
		b.logs.RecordTraffic("out", "sendMessage", message.Chat.ID, answer, jsonOf(params))
		if _, err := b.api.SendMessage(ctx, params); err != nil {
			slog.Error("echoing", "err", err)
		}
	}
}

func (b *Bot) testJob(ctx context.Context) {
	for chatID := range b.config.Admins {
		b.send(ctx, chatID, "<b>test</b>", true)
	}
}
