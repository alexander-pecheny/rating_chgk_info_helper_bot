package tg

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot/models"

	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/rating"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/store"
)

func (b *Bot) Unsubscribe(tournamentID int, chatID int64) string {
	tournament, err := b.db.Tournament(tournamentID)
	if err != nil {
		slog.Error("reading tournament", "tournament", tournamentID, "err", err)
	}
	if tournament == nil {
		return fmt.Sprintf("Вы и так не подписаны на турнир %d.", tournamentID)
	}
	if _, subscribed := tournament.Subscribers[chatID]; !subscribed {
		return fmt.Sprintf("Вы и так не подписаны на турнир %d.", tournamentID)
	}

	delete(tournament.Subscribers, chatID)
	if len(tournament.Subscribers) > 0 {
		slog.Debug("removing chat from subscribers", "chat_id", chatID, "tournament", tournamentID)
		err = b.db.SetSubscribers(tournamentID, tournament.Subscribers)
	} else {
		slog.Debug("removing tournament, nobody left watching", "tournament", tournamentID)
		err = b.db.DeleteTournament(tournamentID)
	}
	if err != nil {
		slog.Error("saving unsubscribe", "tournament", tournamentID, "err", err)
	}
	return fmt.Sprintf("Вы теперь отписаны от турнира <b>%d %s</b>.", tournamentID, tournament.Name)
}

func (b *Bot) unsubscribeAll(ctx context.Context, message *models.Message) {
	chatID := message.Chat.ID
	tournaments, err := b.db.Tournaments()
	if err != nil {
		slog.Error("reading tournaments", "err", err)
	}
	var replies []string
	for _, tournament := range tournaments {
		if _, subscribed := tournament.Subscribers[chatID]; subscribed {
			replies = append(replies, b.Unsubscribe(tournament.ID, chatID))
		}
	}
	if len(replies) == 0 {
		b.replyPlain(ctx, message, "Сейчас вы не подписаны ни на один турнир.")
		return
	}
	b.reply(ctx, message, strings.Join(replies, "\n"))
}

func toStoredApplications(applications map[string]rating.Application) map[string]store.Application {
	out := make(map[string]store.Application, len(applications))
	for id, application := range applications {
		out[id] = store.Application{Status: application.Status, Rep: application.Rep}
	}
	return out
}
