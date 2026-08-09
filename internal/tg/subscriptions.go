package tg

import (
	"fmt"
	"log/slog"

	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/rating"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/store"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/text"
)

func subscribedMessage(tournament store.Tournament, suffix, host string) string {
	count := len(tournament.Applications)
	message := fmt.Sprintf(
		"Вы теперь подписаны на турнир <b>%d %s</b>%s. Там %d %s.",
		tournament.ID, tournament.Name, suffix, count, text.ApplicationForm(count))
	if count > 0 {
		message += " " + text.ReviewLink(host, tournament.ID)
	}
	return message
}

// Subscribe adds a chat to a tournament, fetching the tournament first if this
// is the first chat to care about it.
func (b *Bot) Subscribe(tournamentID int, chatID int64, juryOnly bool) string {
	subscription := store.DefaultSubscription()
	suffix := ""
	if juryOnly {
		subscription = store.JuryOnlySubscription()
		suffix = " в качестве ИЖ"
	}
	host := b.db.HostOf(chatID, text.DefaultHost)

	tournament, err := b.db.Tournament(tournamentID)
	if err != nil {
		slog.Error("reading tournament", "tournament", tournamentID, "err", err)
		return fmt.Sprintf("Не удалось прочитать турнир %d, попробуйте позже.", tournamentID)
	}

	if tournament == nil {
		return b.subscribeToNew(tournamentID, chatID, subscription, suffix, host)
	}
	if _, already := tournament.Subscribers[chatID]; already {
		return fmt.Sprintf("Вы уже подписаны на турнир <b>%d %s</b>.", tournamentID, tournament.Name)
	}
	tournament.Subscribers[chatID] = subscription
	slog.Debug("adding chat to subscribers", "chat_id", chatID, "tournament", tournamentID)
	if err := b.db.SetSubscribers(tournamentID, tournament.Subscribers); err != nil {
		slog.Error("saving subscribers", "tournament", tournamentID, "err", err)
	}
	return subscribedMessage(*tournament, suffix, host)
}

func (b *Bot) subscribeToNew(
	tournamentID int, chatID int64, subscription store.Subscription, suffix, host string,
) string {
	info, err := b.rating.Info(tournamentID)
	if err != nil {
		slog.Error("getting tournament info", "tournament", tournamentID, "err", err)
		return fmt.Sprintf("Не удалось получить информацию о турнире %d, попробуйте позже.", tournamentID)
	}
	if info.IsBad() || info.Name == "" {
		return fmt.Sprintf("Турнир %d не найден.", tournamentID)
	}
	applications, err := b.rating.Applications(tournamentID)
	if err != nil {
		// Storing an empty baseline would report zero applications now and
		// then announce every existing one as new on the next check.
		slog.Error("getting applications", "tournament", tournamentID, "err", err)
		return fmt.Sprintf("Не удалось получить заявки турнира %d, попробуйте позже.", tournamentID)
	}
	tournament := store.Tournament{
		ID:           tournamentID,
		Name:         info.Name,
		Applications: toStoredApplications(applications),
		Subscribers:  map[int64]store.Subscription{chatID: subscription},
	}
	slog.Debug("adding tournament with its first subscriber",
		"tournament", tournamentID, "chat_id", chatID)
	if err := b.db.AddTournament(tournament); err != nil {
		slog.Error("saving tournament", "tournament", tournamentID, "err", err)
		return fmt.Sprintf("Не удалось сохранить турнир %d, попробуйте позже.", tournamentID)
	}
	return subscribedMessage(tournament, suffix, host)
}

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

func toStoredApplications(applications map[string]rating.Application) map[string]store.Application {
	out := make(map[string]store.Application, len(applications))
	for id, application := range applications {
		out[id] = store.Application{Status: application.Status, Rep: application.Rep}
	}
	return out
}
