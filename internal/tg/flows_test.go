package tg

import (
	"strings"
	"testing"

	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/store"
)

func TestUnsubscribeWithoutIDsAsksAndKeepsAsking(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/unsubscribe")
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
	h.send(t, "/unsubscribe")
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

func TestSubscribeAnswersWithTheSunsetNotice(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/subscribe")
	h.send(t, "/subscribe_izh")
	h.send(t, "/subscribe 9002")

	if got := h.telegram.countOf("sendPhoto"); got != 3 {
		t.Fatalf("expected three sunset photos, got %d", got)
	}
	photo, _ := h.telegram.lastCallOf("sendPhoto")
	for _, expected := range []string{"https://t.me/tznatoki/188", "/unsubscribe_all"} {
		if !strings.Contains(photo.Values["caption"], expected) {
			t.Fatalf("the caption is missing %q:\n%s", expected, photo.Values["caption"])
		}
	}
	if tournament, _ := h.db.Tournament(9002); tournament != nil {
		t.Fatal("a subscribe attempt must not store anything")
	}
	if got := h.telegram.sentTexts(); len(got) != 0 {
		t.Fatalf("expected no text replies, got %q", got)
	}
}

func TestUnsubscribeDropsTheSubscription(t *testing.T) {
	h := newHarness(t)
	watchTournament(t, h)

	h.send(t, "/unsubscribe 9002")
	if !h.telegram.said("Вы теперь отписаны от турнира <b>9002 Кубок</b>") {
		t.Fatalf("got %q", h.telegram.sentTexts())
	}
	if tournament, _ := h.db.Tournament(9002); tournament != nil {
		t.Fatal("the last subscriber leaving should drop the tournament")
	}

	h.send(t, "/unsubscribe 9002")
	if !h.telegram.said("Вы и так не подписаны на турнир 9002.") {
		t.Fatal("a second unsubscribe should say so")
	}
}

func TestUnsubscribeAllDropsEverySubscription(t *testing.T) {
	h := newHarness(t)
	watchTournament(t, h)
	err := h.db.AddTournament(store.Tournament{
		ID:           9003,
		Name:         "Чаша",
		Applications: map[string]store.Application{},
		Subscribers: map[int64]store.Subscription{
			chatID: store.DefaultSubscription(),
			424242: store.DefaultSubscription(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	h.send(t, "/unsubscribe_all")

	for _, expected := range []string{"<b>9002 Кубок</b>", "<b>9003 Чаша</b>"} {
		if !h.telegram.said("Вы теперь отписаны от турнира " + expected) {
			t.Fatalf("missing the goodbye for %s, got %q", expected, h.telegram.sentTexts())
		}
	}
	if tournament, _ := h.db.Tournament(9002); tournament != nil {
		t.Fatal("the last subscriber leaving should drop the tournament")
	}
	tournament, err := h.db.Tournament(9003)
	if err != nil || tournament == nil {
		t.Fatalf("a tournament with other subscribers must stay: %v", err)
	}
	if _, subscribed := tournament.Subscribers[chatID]; subscribed {
		t.Fatal("the chat should be gone from the subscribers")
	}

	h.send(t, "/unsubscribe_all")
	if !h.telegram.said("Сейчас вы не подписаны ни на один турнир.") {
		t.Fatal("a repeat unsubscribe_all should say there is nothing left")
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
