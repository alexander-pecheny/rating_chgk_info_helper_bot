package tg

import (
	"strings"
	"testing"
)

func TestSubscribeWithoutIDsAsksAndKeepsAsking(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/subscribe")
	if !h.telegram.said("укажите id турниров") {
		t.Fatal("expected the bot to ask for tournament ids")
	}
	h.send(t, "не число")
	if got := h.telegram.timesSaid("укажите id турниров"); got != 2 {
		t.Fatalf("expected the bot to ask twice, asked %d times", got)
	}
}

func TestCancelEndsTheFlow(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/subscribe")
	h.send(t, "/cancel")
	if !h.telegram.said("Команда отменена.") {
		t.Fatal("expected the cancellation to be acknowledged")
	}
	h.send(t, "не число")
	if got := h.telegram.timesSaid("укажите id турниров"); got != 1 {
		t.Fatalf("the flow kept running after /cancel: asked %d times", got)
	}
}

func TestBannedUserGetsNothingElse(t *testing.T) {
	h := newHarness(t)
	if _, err := h.db.Ban(chatID); err != nil {
		t.Fatal(err)
	}
	h.send(t, "/subscribe")
	sent := h.telegram.sentTexts()
	if len(sent) != 1 || sent[0] != "Вы забанены." {
		t.Fatalf("a banned user should only be told so, got %q", sent)
	}
}

func TestNonAdminIsToldSo(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/ban 123")
	if !h.telegram.said("Вы не админ бота.") {
		t.Fatal("expected the admin gate to refuse")
	}
}

func TestAdminCanBan(t *testing.T) {
	h := newHarness(t)
	h.bot.config.Admins = map[int64]bool{chatID: true}
	h.send(t, "/ban 4242")
	if !h.db.IsBanned(4242) {
		t.Fatal("4242 should be banned")
	}
	if !h.telegram.said("Пользователь 4242 забанен") {
		t.Fatal("expected the ban to be confirmed")
	}
}

func TestUnknownCommandIsIgnored(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/nonsense")
	if got := h.telegram.sentTexts(); len(got) != 0 {
		t.Fatalf("expected silence, got %q", got)
	}
}

func TestDatesFlowWalksThreeSteps(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/get_dates")
	h.send(t, "2026-11-13")
	if !h.telegram.said("Сколько дней от 1 до 7") {
		t.Fatal("expected the sync-length question")
	}
	h.send(t, "3")
	if !h.telegram.said("длиться асинхрон") {
		t.Fatal("expected the async-length question")
	}
	h.send(t, "0")

	sent := h.telegram.sentTexts()
	last := sent[len(sent)-1]
	for _, expected := range []string{"2026-11-13 19:00:00", "2026-11-16 19:00:00", "Синхрон"} {
		if !strings.Contains(last, expected) {
			t.Fatalf("date grid is missing %q:\n%s", expected, last)
		}
	}
}

func TestDatesFlowRejectsABadDate(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/get_dates")
	h.send(t, "не дата")
	if !h.telegram.said("Неверный формат") {
		t.Fatal("expected the bad date to be rejected")
	}
}

func TestDatesFlowHonoursTheStoredStartTime(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/set_dates_prefs время: 11:30")
	h.send(t, "/get_dates")
	h.send(t, "2026-11-13")
	h.send(t, "1")
	h.send(t, "0")

	sent := h.telegram.sentTexts()
	if last := sent[len(sent)-1]; !strings.Contains(last, "2026-11-13 11:30:00") {
		t.Fatalf("the stored start time was ignored:\n%s", last)
	}
}

func TestTrafficIsRecordedBothWays(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/start")

	rows, err := h.logs.Traffic()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Direction != "in" || rows[1].Direction != "out" {
		t.Fatalf("expected one in and one out, got %+v", rows)
	}
	if rows[0].Text != "/start" {
		t.Fatalf("incoming text was %q", rows[0].Text)
	}
	if !strings.Contains(rows[1].Text, "Привет!") {
		t.Fatalf("outgoing text was %q", rows[1].Text)
	}
}

func TestSubscribeIzhIsNotSwallowedBySubscribe(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/subscribe_izh")
	if !h.telegram.said("укажите id турниров") {
		t.Fatal("expected /subscribe_izh to start its own flow")
	}
	h.send(t, "/get_dates_prefs")
	if !h.telegram.said("настройки дат") {
		t.Fatal("a command should escape the flow")
	}
}

func TestSubscribeReportsAnAPIFailureInsteadOfSubscribing(t *testing.T) {
	h := newHarness(t)
	h.api.answer("/tournaments/9002.json", `{"id":9002,"name":"Кубок"}`)
	h.api.fail("/tournaments/9002/requests.json", 500)

	h.send(t, "/subscribe 9002")

	if !h.telegram.said("Не удалось получить заявки") {
		t.Fatalf("expected a failure report, got %q", h.telegram.sentTexts())
	}
	tournament, err := h.db.Tournament(9002)
	if err != nil {
		t.Fatal(err)
	}
	if tournament != nil {
		t.Fatal("a tournament must not be stored when its applications could not be read")
	}
}

func TestSubscribeStoresTheTournamentAndItsApplications(t *testing.T) {
	h := newHarness(t)
	h.api.answer("/tournaments/9002.json", `{"id":9002,"name":"Кубок"}`)
	h.api.answer("/tournaments/9002/requests.json", `[
		{"id":7,"status":"N","representative":{"id":123,"name":"Иван","surname":"Иванов"},
		 "venue":{"town":{"name":"Москва"}}},
		{"id":8,"status":"A","representative":{"id":124,"name":"Пётр","surname":"Петров"},
		 "venue":{"town":{"name":"Тверь"}}}]`)

	h.send(t, "/subscribe 9002")

	if !h.telegram.said("Вы теперь подписаны на турнир <b>9002 Кубок</b>") {
		t.Fatalf("got %q", h.telegram.sentTexts())
	}
	if !h.telegram.said("Там 1 нерассмотренная заявка") {
		t.Fatalf("only the unreviewed application should be counted, got %q", h.telegram.sentTexts())
	}
	tournament, err := h.db.Tournament(9002)
	if err != nil || tournament == nil {
		t.Fatalf("tournament not stored: %v", err)
	}
	if len(tournament.Applications) != 1 {
		t.Fatalf("expected one stored application, got %d", len(tournament.Applications))
	}
	if _, subscribed := tournament.Subscribers[chatID]; !subscribed {
		t.Fatal("the chat should be a subscriber")
	}

	h.send(t, "/subscribe 9002")
	if !h.telegram.said("Вы уже подписаны") {
		t.Fatal("a second subscribe should say so")
	}

	h.send(t, "/unsubscribe 9002")
	if !h.telegram.said("Вы теперь отписаны") {
		t.Fatal("expected an unsubscribe confirmation")
	}
	if tournament, _ := h.db.Tournament(9002); tournament != nil {
		t.Fatal("the last subscriber leaving should drop the tournament")
	}
}

func TestUnknownTournamentIsReportedNotStored(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/subscribe 1")
	if !h.telegram.said("Турнир 1 не найден.") {
		t.Fatalf("got %q", h.telegram.sentTexts())
	}
}

func TestSetHostChangesTheLinks(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/set_host https://rating.pecheny.me/")
	if !h.telegram.said("Ваш хост теперь <pre>rating.pecheny.me</pre>") {
		t.Fatalf("got %q", h.telegram.sentTexts())
	}
	if got := h.db.HostOf(chatID, "fallback"); got != "rating.pecheny.me" {
		t.Fatalf("stored host is %q", got)
	}
}
