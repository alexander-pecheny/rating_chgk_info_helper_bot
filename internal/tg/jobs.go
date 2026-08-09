package tg

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"

	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/dates"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/rating"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/store"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/text"
)

// forgetAfter is how long a finished tournament stays watched. Past it there is
// nothing left to rule on and nobody left to remind.
const forgetAfter = 30 * 24 * time.Hour

// news is one tournament's fresh applications, already written out for one
// chat. watched is a tournament that chat follows but heard nothing about.
type newsItem struct {
	tournamentID int
	body         string
}

type watchedItem struct {
	tournamentID int
	name         string
}

// CheckApplications tells every subscriber about applications they have not
// reviewed yet. It runs every two hours.
func (b *Bot) CheckApplications(ctx context.Context) {
	b.checkApplications(ctx, nil)
}

// checkApplicationsForAdmins is the same sweep sent only to the admins, which
// is how a change gets tried against live data without messaging anyone.
func (b *Bot) checkApplicationsForAdmins(ctx context.Context) {
	b.checkApplications(ctx, b.config.Admins)
}

func (b *Bot) checkApplications(ctx context.Context, only map[int64]bool) {
	slog.Debug("running the applications job")
	dryRun := only != nil

	tournaments, err := b.db.Tournaments()
	if err != nil {
		slog.Error("reading tournaments", "err", err)
		return
	}

	var stale []int
	news := map[int64][]newsItem{}
	watching := map[int64][]watchedItem{}
	current := map[int]map[string]store.Application{}

	for _, tournament := range tournaments {
		for chatID, subscription := range tournament.Subscribers {
			if subscription["r"] != 0 {
				watching[chatID] = append(watching[chatID],
					watchedItem{tournament.ID, tournament.Name})
			}
		}
		info, err := b.rating.Info(tournament.ID)
		if err != nil {
			slog.Error("processing tournament", "tournament", tournament.ID, "err", err)
			continue
		}
		if !dryRun && info.IsBad() {
			slog.Debug("tournament is gone from the site", "tournament", tournament.ID)
			stale = append(stale, tournament.ID)
			continue
		}
		end, err := dates.Parse(info.DateEnd)
		if err != nil {
			slog.Error("reading the end date", "tournament", tournament.ID, "err", err)
			continue
		}
		if !end.After(dates.Now()) {
			continue
		}
		b.collectNewApplications(tournament, only, news, current)
	}

	slog.Debug("chats to message", "count", len(news))
	for chatID := range news {
		if dryRun && !only[chatID] {
			continue
		}
		body := strings.Join(b.messagesFor(chatID, news, watching, current), "\n\n")
		if body == "" {
			continue
		}
		slog.Debug("sending applications digest", "chat_id", chatID)
		b.send(ctx, chatID, body, true)
	}

	if dryRun {
		return
	}
	for _, tournamentID := range stale {
		if err := b.db.DeleteTournament(tournamentID); err != nil {
			slog.Error("forgetting tournament", "tournament", tournamentID, "err", err)
		}
	}
}

func (b *Bot) collectNewApplications(
	tournament store.Tournament,
	only map[int64]bool,
	news map[int64][]newsItem,
	current map[int]map[string]store.Application,
) {
	fresh, err := b.rating.Applications(tournament.ID)
	if err != nil {
		slog.Error("could not get applications, skipping", "tournament", tournament.ID, "err", err)
		return
	}
	stored := toStoredApplications(fresh)
	current[tournament.ID] = stored

	if only == nil && differs(tournament.Applications, stored) {
		slog.Debug("application list changed", "tournament", tournament.ID,
			"was", len(tournament.Applications), "became", len(stored))
		if err := b.db.SetApplications(tournament.ID, stored); err != nil {
			slog.Error("saving applications", "tournament", tournament.ID, "err", err)
		}
	}
	if !hasNew(tournament.Applications, stored) {
		return
	}

	for chatID, subscription := range tournament.Subscribers {
		if (only != nil && !only[chatID]) || subscription["r"] == 0 {
			continue
		}
		host := b.db.HostOf(chatID, text.DefaultHost)
		body := fmt.Sprintf("Для турнира <b>%d %s</b> есть %d %s. %s\n\n%s",
			tournament.ID, tournament.Name, len(stored), text.ApplicationForm(len(stored)),
			text.ReviewLink(host, tournament.ID), formatApplications(stored, host))
		news[chatID] = append(news[chatID], newsItem{tournament.ID, body})
	}
}

// messagesFor is what one chat is told: the tournaments with new applications,
// then a nudge about the ones it still has not got to.
func (b *Bot) messagesFor(
	chatID int64,
	news map[int64][]newsItem,
	watching map[int64][]watchedItem,
	current map[int]map[string]store.Application,
) []string {
	var out []string
	told := map[int]bool{}
	for _, item := range news[chatID] {
		out = append(out, item.body)
		told[item.tournamentID] = true
	}

	host := b.db.HostOf(chatID, text.DefaultHost)
	others := make([]watchedItem, 0, len(watching[chatID]))
	for _, item := range watching[chatID] {
		if !told[item.tournamentID] {
			others = append(others, item)
		}
	}
	sort.Slice(others, func(i, j int) bool { return others[i].tournamentID < others[j].tournamentID })

	for _, item := range others {
		applications := current[item.tournamentID]
		if len(applications) == 0 {
			continue
		}
		out = append(out, fmt.Sprintf(
			"Ранее нерассмотренные заявки на турнир <b>%d %s</b>. %s\n\n%s",
			item.tournamentID, item.name, text.ReviewLink(host, item.tournamentID),
			formatApplications(applications, host)))
	}
	return out
}

// MakeReminders warns subscribers about the controversials and appeals
// deadlines. It runs once a day.
func (b *Bot) MakeReminders(ctx context.Context) {
	slog.Debug("running the reminders job")
	tournaments, err := b.db.Tournaments()
	if err != nil {
		slog.Error("reading tournaments", "err", err)
		return
	}

	var stale []int
	perChat := map[int64][]string{}

	for _, tournament := range tournaments {
		slog.Info("processing tournament", "tournament", tournament.ID)
		info, err := b.rating.Info(tournament.ID)
		if err != nil {
			slog.Error("getting tournament info", "tournament", tournament.ID, "err", err)
			continue
		}
		if info.IsBad() {
			stale = append(stale, tournament.ID)
			continue
		}
		end, err := dates.Parse(info.DateEnd)
		if err != nil {
			slog.Error("reading the end date", "tournament", tournament.ID, "err", err)
			continue
		}
		if dates.Now().Sub(end) >= forgetAfter {
			slog.Debug("tournament is long over", "tournament", tournament.ID)
			stale = append(stale, tournament.ID)
			continue
		}
		reminders, err := b.rating.RemindersFor(info, dates.Now())
		if err != nil {
			slog.Debug("could not build reminders", "tournament", tournament.ID, "err", err)
			continue
		}
		for chatID, subscription := range tournament.Subscribers {
			if wanted := wantedReminders(subscription, reminders); wanted != "" {
				perChat[chatID] = append(perChat[chatID], wanted)
			}
		}
	}

	slog.Info("chats to remind", "count", len(perChat))
	for chatID, messages := range perChat {
		body := strings.Join(messages, "\n\n")
		if host := b.db.HostOf(chatID, text.DefaultHost); host != text.DefaultHost {
			body = strings.ReplaceAll(body, text.DefaultHost, host)
		}
		b.send(ctx, chatID, body, true)
	}

	for _, tournamentID := range stale {
		if err := b.db.DeleteTournament(tournamentID); err != nil {
			slog.Error("forgetting tournament", "tournament", tournamentID, "err", err)
		}
	}
}

// wantedReminders picks the reminders this subscription asked for, without
// repeating one that covers two kinds at once.
func wantedReminders(subscription store.Subscription, reminders rating.Reminders) string {
	var chosen []string
	for _, kind := range []string{"i", "a"} {
		if subscription[kind] == 0 {
			continue
		}
		reminder, ok := reminders[kind]
		if !ok || reminder == "" {
			continue
		}
		if !slices.Contains(chosen, reminder) {
			chosen = append(chosen, reminder)
		}
	}
	return strings.Join(chosen, "\n\n")
}

func differs(before, after map[string]store.Application) bool {
	if len(before) != len(after) {
		return true
	}
	for id := range after {
		if _, known := before[id]; !known {
			return true
		}
	}
	return false
}

func hasNew(before, after map[string]store.Application) bool {
	for id := range after {
		if _, known := before[id]; !known {
			return true
		}
	}
	return false
}

func formatApplications(applications map[string]store.Application, host string) string {
	reps := make([]string, 0, len(applications))
	for _, application := range applications {
		reps = append(reps, application.Rep)
	}
	return text.FormatApplications(reps, host)
}
