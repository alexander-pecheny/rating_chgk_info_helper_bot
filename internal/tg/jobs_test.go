package tg

import (
	"context"
	"strings"
	"testing"

	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/dates"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/store"
)

const futureDate = "2030-12-31T00:00:00+03:00"

// watchTournament makes chatID a subscriber of 9002 with nothing seen yet.
func watchTournament(t *testing.T, h *harness) {
	t.Helper()
	err := h.db.AddTournament(store.Tournament{
		ID:           9002,
		Name:         "Кубок",
		Applications: map[string]store.Application{},
		Subscribers:  map[int64]store.Subscription{chatID: store.DefaultSubscription()},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func stubTournamentAPI(h *harness) {
	h.api.answer("/tournaments/9002.json",
		`{"id":9002,"name":"Кубок","dateEnd":"`+futureDate+`"}`)
	h.api.answer("/tournaments/9002/requests.json",
		`[{"id":7,"status":"N","representative":{"id":123,"name":"Иван","surname":"Иванов"},
		   "venue":{"town":{"name":"Москва"}}}]`)
}

func TestPlayerLinksUseTheRealHost(t *testing.T) {
	h := newHarness(t)
	watchTournament(t, h)
	stubTournamentAPI(h)

	h.bot.CheckApplications(context.Background())

	body := strings.Join(h.telegram.sentTexts(), "\n")
	if strings.Contains(body, "{host}") {
		t.Fatal("the host placeholder leaked into a message")
	}
	if !strings.Contains(body, "https://rating.chgk.info/player/123") {
		t.Fatalf("expected a player link, got:\n%s", body)
	}
}

func TestPlayerLinksHonourTheChatHostPreference(t *testing.T) {
	h := newHarness(t)
	watchTournament(t, h)
	stubTournamentAPI(h)
	if err := h.db.SetPrefs(chatID, store.Prefs{Host: "rating.pecheny.me"}); err != nil {
		t.Fatal(err)
	}

	h.bot.CheckApplications(context.Background())

	body := strings.Join(h.telegram.sentTexts(), "\n")
	if !strings.Contains(body, "https://rating.pecheny.me/player/123") {
		t.Fatalf("expected the preferred host, got:\n%s", body)
	}
	if strings.Contains(body, "rating.chgk.info") {
		t.Fatalf("the default host leaked through:\n%s", body)
	}
}

func TestSeenApplicationsAreNotReportedTwice(t *testing.T) {
	h := newHarness(t)
	watchTournament(t, h)
	stubTournamentAPI(h)

	h.bot.CheckApplications(context.Background())
	first := len(h.telegram.sentTexts())
	if first != 1 {
		t.Fatalf("expected exactly one message, got %d", first)
	}

	h.bot.CheckApplications(context.Background())
	if got := len(h.telegram.sentTexts()); got != first {
		t.Fatalf("an already-reported application was reported again: %d messages", got)
	}
}

func TestAFailedApplicationFetchReportsNothing(t *testing.T) {
	h := newHarness(t)
	watchTournament(t, h)
	h.api.answer("/tournaments/9002.json",
		`{"id":9002,"name":"Кубок","dateEnd":"`+futureDate+`"}`)
	h.api.fail("/tournaments/9002/requests.json", 500)

	h.bot.CheckApplications(context.Background())

	if got := h.telegram.sentTexts(); len(got) != 0 {
		t.Fatalf("expected silence, got %q", got)
	}
	tournament, err := h.db.Tournament(9002)
	if err != nil || tournament == nil {
		t.Fatal("the tournament should still be there")
	}
	if len(tournament.Applications) != 0 {
		t.Fatal("a failed fetch must not overwrite the stored applications")
	}
}

func TestATournamentGoneFromTheSiteIsForgotten(t *testing.T) {
	h := newHarness(t)
	watchTournament(t, h)
	// The fake answers "Not found" for anything it was not told about.

	h.bot.CheckApplications(context.Background())

	tournament, err := h.db.Tournament(9002)
	if err != nil {
		t.Fatal(err)
	}
	if tournament != nil {
		t.Fatal("a tournament the site no longer has should be dropped")
	}
}

func TestASubscriberWhoOptedOutOfApplicationsHearsNothing(t *testing.T) {
	h := newHarness(t)
	err := h.db.AddTournament(store.Tournament{
		ID:           9002,
		Name:         "Кубок",
		Applications: map[string]store.Application{},
		Subscribers:  map[int64]store.Subscription{chatID: store.JuryOnlySubscription()},
	})
	if err != nil {
		t.Fatal(err)
	}
	stubTournamentAPI(h)

	h.bot.CheckApplications(context.Background())

	if got := h.telegram.sentTexts(); len(got) != 0 {
		t.Fatalf("a jury-only subscriber should hear nothing about applications, got %q", got)
	}
}

func TestRemindersReachOnlyTheSubscribersWhoWantThem(t *testing.T) {
	h := newHarness(t)
	// A tournament that ended five days ago is one day from its
	// controversials deadline.
	ended := endedDaysAgo(5)
	err := h.db.AddTournament(store.Tournament{
		ID:           9002,
		Name:         "Кубок",
		Applications: map[string]store.Application{},
		Subscribers: map[int64]store.Subscription{
			chatID:  {"r": 1, "i": 1, "a": 0},
			1234567: {"r": 1, "i": 0, "a": 0},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.api.answer("/tournaments/9002.json", `{"id":9002,"name":"Кубок","dateEnd":"`+ended+`"}`)

	h.bot.MakeReminders(context.Background())

	texts := h.telegram.sentTexts()
	if len(texts) != 1 {
		t.Fatalf("expected exactly the one interested subscriber, got %q", texts)
	}
	if !strings.Contains(texts[0], "Спорные на турнире") {
		t.Fatalf("expected a controversials reminder, got %q", texts[0])
	}
}

// endedDaysAgo formats a tournament end date the given number of days back.
func endedDaysAgo(days int) string {
	return dates.Now().Add(-dates.Days(days)).Format("2006-01-02T15:04:05-07:00")
}
