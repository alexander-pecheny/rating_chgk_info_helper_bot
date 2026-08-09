package tg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-telegram/bot/models"

	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/config"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/rating"
	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/store"
)

const (
	chatID            int64 = 608909090
	adminID           int64 = 663024
	announceChannelID int64 = -1001568906741
)

type call struct {
	Method string
	Values map[string]string
}

// fakeTelegram records what the bot sent and answers plausibly, so the tests
// exercise the real request-building path rather than a stubbed client.
type fakeTelegram struct {
	server *httptest.Server

	mu     sync.Mutex
	calls  []call
	refuse map[string]bool
	nextID int
}

func newFakeTelegram(t *testing.T) *fakeTelegram {
	t.Helper()
	fake := &fakeTelegram{refuse: map[string]bool{}, nextID: 1000}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeTelegram) serve(w http.ResponseWriter, r *http.Request) {
	method := path.Base(r.URL.Path)
	values := map[string]string{}
	if err := r.ParseMultipartForm(8 << 20); err == nil && r.MultipartForm != nil {
		for key, entries := range r.MultipartForm.Value {
			if len(entries) > 0 {
				values[key] = entries[0]
			}
		}
	}

	f.mu.Lock()
	if method != "getUpdates" {
		f.calls = append(f.calls, call{Method: method, Values: values})
	}
	refused := f.refuse[method]
	f.nextID++
	id := f.nextID
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if refused {
		fmt.Fprintf(w, `{"ok":false,"error_code":400,"description":"%s refused"}`, method)
		return
	}

	switch method {
	case "getUpdates":
		fmt.Fprint(w, `{"ok":true,"result":[]}`)
	case "copyMessage":
		fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d}}`, id)
	case "copyMessages", "forwardMessages":
		fmt.Fprintf(w, `{"ok":true,"result":[{"message_id":%d}]}`, id)
	default:
		chat := values["chat_id"]
		if chat == "" {
			chat = "1"
		}
		fmt.Fprintf(w,
			`{"ok":true,"result":{"message_id":%d,"date":1786266432,"chat":{"id":%s,"type":"private"},"text":%s}}`,
			id, chat, jsonOf(values["text"]))
	}
}

func (f *fakeTelegram) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.Method)
	}
	return out
}

func (f *fakeTelegram) countOf(method string) int {
	count := 0
	for _, name := range f.methods() {
		if name == method {
			count++
		}
	}
	return count
}

func (f *fakeTelegram) called(method string) bool { return f.countOf(method) > 0 }

func (f *fakeTelegram) sentTexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if c.Method == "sendMessage" && c.Values["text"] != "" {
			out = append(out, c.Values["text"])
		}
	}
	return out
}

// said reports whether any message the bot sent contains the fragment.
func (f *fakeTelegram) said(fragment string) bool {
	for _, sent := range f.sentTexts() {
		if strings.Contains(sent, fragment) {
			return true
		}
	}
	return false
}

func (f *fakeTelegram) timesSaid(fragment string) int {
	count := 0
	for _, sent := range f.sentTexts() {
		if strings.Contains(sent, fragment) {
			count++
		}
	}
	return count
}

func (f *fakeTelegram) lastCallOf(method string) (call, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].Method == method {
			return f.calls[i], true
		}
	}
	return call{}, false
}

// fakeRating answers the rating API from a table of canned JSON bodies.
type fakeRating struct {
	server *httptest.Server

	mu        sync.Mutex
	responses map[string]string
	status    map[string]int
}

func newFakeRating(t *testing.T) *fakeRating {
	t.Helper()
	fake := &fakeRating{responses: map[string]string{}, status: map[string]int{}}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		body, known := fake.responses[r.URL.Path]
		code := fake.status[r.URL.Path]
		fake.mu.Unlock()
		if code != 0 {
			w.WriteHeader(code)
		}
		if !known {
			body = `{"detail":"Not found"}`
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeRating) answer(path, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[path] = body
}

func (f *fakeRating) fail(path string, code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status[path] = code
	f.responses[path] = `{"detail":"boom"}`
}

// harness is a bot wired exactly as production, talking to both fakes.
type harness struct {
	bot       *Bot
	telegram  *fakeTelegram
	api       *fakeRating
	db        *store.DB
	logs      *store.LogStore
	scheduler *Scheduler

	nextUpdateID  int64
	nextMessageID int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()

	db, err := store.Open(filepath.Join(dir, "bot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	logs, err := store.OpenLogStore(filepath.Join(dir, "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logs.Close() })

	telegram := newFakeTelegram(t)
	api := newFakeRating(t)
	scheduler := NewScheduler()

	bot, err := New(Options{
		Config: config.Config{
			DataDir:           dir,
			Token:             "123:fake",
			Admins:            map[int64]bool{adminID: true},
			AnnounceChannelID: announceChannelID,
			Debug:             true,
		},
		DB:        db,
		Logs:      logs,
		Rating:    rating.NewAt(api.server.URL, 0),
		Scheduler: scheduler,
		ServerURL: telegram.server.URL,
		BotID:     1,
	})
	if err != nil {
		t.Fatal(err)
	}

	return &harness{
		bot: bot, telegram: telegram, api: api,
		db: db, logs: logs, scheduler: scheduler,
	}
}

// feed pushes one message through the same middleware and routing an update
// from Telegram would take.
func (h *harness) feed(t *testing.T, fields map[string]any) {
	t.Helper()
	h.nextUpdateID++
	h.nextMessageID++

	message := map[string]any{
		"message_id": h.nextMessageID,
		"date":       1786266432,
		"chat":       map[string]any{"id": chatID, "type": "private"},
		"from":       map[string]any{"id": chatID, "is_bot": false, "first_name": "Тимофей"},
	}
	for key, value := range fields {
		message[key] = value
	}

	raw, err := json.Marshal(map[string]any{
		"update_id": h.nextUpdateID,
		"message":   message,
	})
	if err != nil {
		t.Fatal(err)
	}
	var update models.Update
	if err := json.Unmarshal(raw, &update); err != nil {
		t.Fatalf("building the update: %v", err)
	}
	h.bot.tap.remember(update.ID, raw)
	h.bot.logIncoming(h.bot.dispatch)(context.Background(), h.bot.api, &update)
}

func (h *harness) send(t *testing.T, message string) {
	t.Helper()
	h.feed(t, map[string]any{"text": message})
}
