package tg

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-telegram/bot/models"
)

// richMessageJSON is trimmed from the real update the old bot refused on
// 2026-08-09: a post-editor message whose whole body lives in
// rich_message.blocks, with no text and no caption.
const richMessageJSON = `{
  "message_id": 53686,
  "date": 1786266432,
  "chat": {"id": 608909090, "type": "private", "first_name": "Тимофей"},
  "rich_message": {
    "blocks": [
      {"type": "heading", "size": 1,
       "text": [{"type":"custom_emoji","custom_emoji_id":"5199623533231101837","alternative_text":"🍁"},
                " АНОНС НОВОГО СЕЗОНА"]},
      {"type": "paragraph", "text": ""},
      {"type": "paragraph",
       "text": [{"type":"custom_emoji","custom_emoji_id":"1","alternative_text":"🗓"},
                " Фестиваль пройдет ",
                {"type":"bold","text":"13-15 ноября"},
                " 2026 года в отеле «Москва» в Санкт-Петербурге."]},
      {"type": "photo",
       "photo": [{"file_id":"AgACAgIAAxUAAWp4Q0","file_unique_id":"AQADiBZrG1rvwUt4",
                  "file_size":2027,"width":90,"height":83}]},
      {"type": "paragraph",
       "text": "В этом году мы расширяемся и готовы принять 80 команд. Ждём и студентов, и молодёжь!"},
      {"type": "paragraph", "text": "Мы приглашаем команды для участия в двух призовых зачётах:"},
      {"type": "list", "items": [
        {"label":"•","blocks":[{"type":"paragraph",
          "text":"Студенческий (все игроки родились не ранее 01.09.2003)"}]},
        {"label":"•","blocks":[{"type":"paragraph",
          "text":"Молодежный (все игроки родились не ранее 01.09.1996)"}]}]}
    ]
  }
}`

func richMessage(t *testing.T) *models.Message {
	t.Helper()
	var message models.Message
	if err := json.Unmarshal([]byte(richMessageJSON), &message); err != nil {
		t.Fatalf("parsing the rich message: %v", err)
	}
	return &message
}

func TestReadsTextTheOldCodeCouldNotSee(t *testing.T) {
	message := richMessage(t)
	if message.Text != "" || message.Caption != "" {
		t.Fatal("this fixture is only interesting while it has no text and no caption")
	}
	if !strings.Contains(MessageText(message), "АНОНС НОВОГО СЕЗОНА") {
		t.Fatalf("got %q", MessageText(message))
	}
}

func TestKeepsStyledRunsAndEmojiButNotStructure(t *testing.T) {
	body := MessageText(richMessage(t))
	for _, wanted := range []string{"13-15 ноября", "🍁"} {
		if !strings.Contains(body, wanted) {
			t.Errorf("expected %q in the flattened text", wanted)
		}
	}
	for _, structural := range []string{"paragraph", "heading", "custom_emoji", "AgACAgIAAxUAAWp4Q0"} {
		if strings.Contains(body, structural) {
			t.Errorf("structure leaked into the text: %q", structural)
		}
	}
}

func TestBlocksAreSeparateLinesAndEmptiesDropped(t *testing.T) {
	lines := strings.Split(MessageText(richMessage(t)), "\n")
	if lines[0] != "🍁 АНОНС НОВОГО СЕЗОНА" {
		t.Fatalf("first line is %q", lines[0])
	}
	for _, line := range lines {
		if line == "" {
			t.Fatal("an empty block should not produce a line")
		}
	}
}

func TestLongEnoughToPassTheAnnounceMinimum(t *testing.T) {
	if got := utf8.RuneCountInString(MessageText(richMessage(t))); got < minAnnounceLength {
		t.Fatalf("flattened text is %d characters, need %d", got, minAnnounceLength)
	}
}

func TestPlainMessagesAreUntouched(t *testing.T) {
	var message models.Message
	if err := json.Unmarshal([]byte(
		`{"message_id":1,"date":1,"chat":{"id":1,"type":"private"},"text":"обычный текст"}`,
	), &message); err != nil {
		t.Fatal(err)
	}
	if got := MessageText(&message); got != "обычный текст" {
		t.Fatalf("got %q", got)
	}
}

// A block type Telegram has not invented yet must not be able to stop the bot.
// Without the tap, one of these fails the whole getUpdates batch, and because
// the offset only advances on success the bot refetches it forever.
func TestUnknownBlockTypeIsDegradedNotFatal(t *testing.T) {
	const future = `{"ok":true,"result":[{"update_id":7,"message":{
		"message_id":1,"date":1,"chat":{"id":608909090,"type":"private"},
		"rich_message":{"blocks":[
			{"type":"crystal_ball","text":"нечто из будущего"},
			{"type":"paragraph","text":"и обычный абзац"}]}}}]}`

	tap := newUpdateTap(nil)
	sifted := tap.sift([]byte(future))

	var envelope struct {
		Result []json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(sifted, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Result) != 1 {
		t.Fatalf("the update should survive, got %d updates", len(envelope.Result))
	}
	var update models.Update
	if err := json.Unmarshal(envelope.Result[0], &update); err != nil {
		t.Fatalf("the degraded update must parse: %v", err)
	}
	body := MessageText(update.Message)
	for _, wanted := range []string{"нечто из будущего", "и обычный абзац"} {
		if !strings.Contains(body, wanted) {
			t.Errorf("degraded text lost %q, got %q", wanted, body)
		}
	}
}

func TestParseableUpdatesPassThroughUntouched(t *testing.T) {
	const plain = `{"ok":true,"result":[{"update_id":9,"message":{
		"message_id":1,"date":1,"chat":{"id":1,"type":"private"},"text":"привет"}}]}`

	tap := newUpdateTap(nil)
	if got := string(tap.sift([]byte(plain))); got != plain {
		t.Fatalf("body was rewritten:\n%s", got)
	}
	if raw := tap.claim(9); raw == "" {
		t.Fatal("the raw update should have been remembered for the traffic log")
	}
}
