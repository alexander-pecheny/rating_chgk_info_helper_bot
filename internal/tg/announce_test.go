package tg

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

var forwardOrigin = map[string]any{
	"type":       "channel",
	"date":       1786266171,
	"message_id": 413,
	"chat":       map[string]any{"id": -1001533750283, "type": "channel", "title": "Полифест"},
}

var longText = strings.Repeat("Анонс турнира. ", 20)

var photo = []any{map[string]any{
	"file_id": "a", "file_unique_id": "b", "width": 90, "height": 90,
}}

// quickAlbums shortens the debounce so the album tests do not each cost a
// second of wall clock.
func quickAlbums(h *harness) time.Duration {
	h.bot.albums.debounce = 10 * time.Millisecond
	return 200 * time.Millisecond
}

func richMessageField(t *testing.T) any {
	t.Helper()
	var message map[string]any
	if err := json.Unmarshal([]byte(richMessageJSON), &message); err != nil {
		t.Fatal(err)
	}
	return message["rich_message"]
}

func TestRichMessageIsAnnounced(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/announce")
	h.feed(t, map[string]any{
		"rich_message":   richMessageField(t),
		"forward_origin": forwardOrigin,
	})

	forward, ok := h.telegram.lastCallOf("forwardMessage")
	if !ok {
		t.Fatalf("expected a forward, calls were %v", h.telegram.methods())
	}
	if forward.Values["chat_id"] != strconv.FormatInt(announceChannelID, 10) {
		t.Fatalf("forwarded to %q", forward.Values["chat_id"])
	}
	if forward.Values["from_chat_id"] != strconv.FormatInt(chatID, 10) {
		t.Fatalf("forwarded from %q", forward.Values["from_chat_id"])
	}
	if !h.telegram.said(announceSent) {
		t.Fatal("expected a success reply")
	}
}

func TestPlainTextAnnounceIsCopiedNotForwarded(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/announce")
	h.send(t, longText)

	if !h.telegram.called("copyMessage") {
		t.Fatalf("expected a copy, calls were %v", h.telegram.methods())
	}
	if h.telegram.called("forwardMessage") {
		t.Fatal("an organiser's own message should not be forwarded")
	}
	if !h.telegram.said(announceSent) {
		t.Fatal("expected a success reply")
	}
}

func TestFallsBackToForwardingWhenCopyIsRefused(t *testing.T) {
	h := newHarness(t)
	h.telegram.refuse["copyMessage"] = true
	h.send(t, "/announce")
	h.send(t, longText)

	if got := h.telegram.countOf("copyMessage"); got != 1 {
		t.Fatalf("expected one copy attempt, got %d", got)
	}
	if !h.telegram.called("forwardMessage") {
		t.Fatal("expected a fallback forward")
	}
	if !h.telegram.said(announceSent) {
		t.Fatal("expected a success reply")
	}
}

func TestShortAnnounceIsRejectedAndFlowContinues(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/announce")
	h.send(t, "коротко")

	if h.telegram.called("copyMessage") {
		t.Fatal("a short announcement must not be relayed")
	}
	if !h.telegram.said("Слишком короткое") {
		t.Fatal("expected the length complaint")
	}

	h.send(t, longText)
	if !h.telegram.called("copyMessage") {
		t.Fatal("the flow should still accept a longer version")
	}
}

func TestTextlessMessageIsRefusedNotIgnored(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/announce")
	h.feed(t, map[string]any{"sticker": map[string]any{
		"file_id": "x", "file_unique_id": "y", "width": 1, "height": 1,
		"type": "regular", "is_animated": false, "is_video": false,
	}})

	if h.telegram.called("copyMessage") {
		t.Fatal("a message with no text must not be relayed")
	}
	if !h.telegram.said("Не получилось распарсить") {
		t.Fatal("expected the bot to say it could not read the message")
	}
}

func TestAlbumIsAnnouncedWhole(t *testing.T) {
	h := newHarness(t)
	settle := quickAlbums(h)
	h.send(t, "/announce")
	h.feed(t, map[string]any{"media_group_id": "42", "photo": photo, "caption": longText})
	h.feed(t, map[string]any{"media_group_id": "42", "photo": photo})
	time.Sleep(settle)

	copied, ok := h.telegram.lastCallOf("copyMessages")
	if !ok {
		t.Fatalf("expected the album to be copied whole, calls were %v", h.telegram.methods())
	}
	var ids []int
	if err := json.Unmarshal([]byte(copied.Values["message_ids"]), &ids); err != nil {
		t.Fatalf("message_ids was %q: %v", copied.Values["message_ids"], err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected both items, got %v", ids)
	}
	if !h.telegram.said(announceSent) {
		t.Fatal("expected a success reply")
	}
}

func TestCommandEscapesTheAnnounceState(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/announce")
	h.send(t, "/start")

	if h.telegram.called("copyMessage") {
		t.Fatal("/start is a command, not an announcement")
	}
	if !h.telegram.said("Привет!") {
		t.Fatal("expected the start message")
	}
}

func TestALaterCommandAlsoEscapesTheState(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/announce")
	h.send(t, "/get_dates_prefs")

	if h.telegram.called("copyMessage") {
		t.Fatal("a command must not be relayed as an announcement")
	}
	if !h.telegram.said("настройки дат") {
		t.Fatal("expected the preferences reply")
	}
}

func TestSlashPrefixedAnnouncementIsRelayedNotDropped(t *testing.T) {
	h := newHarness(t)
	h.send(t, "/announce")
	h.send(t, "/12.03 — отбор. "+strings.Repeat("Подробности турнира. ", 15))

	if !h.telegram.called("copyMessage") {
		t.Fatalf("an announcement starting with a slash should still be relayed, calls were %v",
			h.telegram.methods())
	}
	if !h.telegram.said(announceSent) {
		t.Fatal("expected a success reply")
	}
}

func TestCancelStopsAPendingAlbum(t *testing.T) {
	h := newHarness(t)
	settle := quickAlbums(h)
	h.send(t, "/announce")
	h.feed(t, map[string]any{"media_group_id": "77", "photo": photo, "caption": longText})
	h.send(t, "/cancel")
	time.Sleep(settle)

	if h.telegram.called("copyMessages") || h.telegram.called("copyMessage") {
		t.Fatal("a cancelled album must not be relayed")
	}
	if !h.telegram.said("Команда отменена.") {
		t.Fatal("expected the cancellation reply")
	}
}

func TestLateAlbumItemDoesNotStartASecondPost(t *testing.T) {
	h := newHarness(t)
	settle := quickAlbums(h)
	h.send(t, "/announce")
	h.feed(t, map[string]any{"media_group_id": "88", "photo": photo, "caption": longText})
	time.Sleep(settle)
	if got := h.telegram.countOf("copyMessage"); got != 1 {
		t.Fatalf("expected one copy, got %d", got)
	}

	h.feed(t, map[string]any{"media_group_id": "88", "photo": photo})
	time.Sleep(settle)
	if got := h.telegram.countOf("copyMessage"); got != 1 {
		t.Fatalf("a straggler started a second post: %d copies", got)
	}
	if h.telegram.called("copyMessages") {
		t.Fatal("a straggler must not be relayed as its own album")
	}
}
