package store

import (
	"encoding/hex"
	"path/filepath"
	"reflect"
	"testing"
)

// These are the exact bytes Python's msgpack.dumps produced for the chat_ids
// column, so a database written by the old bot must still read here.
func TestParseChatIDsFromPython(t *testing.T) {
	cases := []struct {
		name string
		hex  string
		want map[int64]Subscription
	}{
		{"empty", "80", map[int64]Subscription{}},
		{"one subscriber", "81ce244b372283a17201a16901a16101", map[int64]Subscription{
			608909090: {"r": 1, "i": 1, "a": 1},
		}},
		{"a channel among them", "82ce244b372283a17201a16901a16101d3ffffff16cdd7520b83a17200a16901a16100",
			map[int64]Subscription{
				608909090:      {"r": 1, "i": 1, "a": 1},
				-1001568906741: {"r": 0, "i": 1, "a": 0},
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := hex.DecodeString(tc.hex)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ParseChatIDs(raw)
			if err != nil {
				t.Fatalf("parsing: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChatIDsRoundTrip(t *testing.T) {
	want := map[int64]Subscription{
		608909090:      {"r": 1, "i": 1, "a": 1},
		-1001568906741: {"r": 0, "i": 1, "a": 0},
	}
	encoded, err := SerializeChatIDs(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseChatIDs(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func testDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestTournamentRoundTrip(t *testing.T) {
	db := testDB(t)
	want := Tournament{
		ID:           9002,
		Name:         "Кубок",
		Applications: map[string]Application{"77": {Status: "N", Rep: "12 Тимофей Иванов (Тарту)"}},
		Subscribers:  map[int64]Subscription{608909090: DefaultSubscription()},
	}
	if err := db.AddTournament(want); err != nil {
		t.Fatal(err)
	}
	got, err := db.Tournament(9002)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !reflect.DeepEqual(*got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	missing, err := db.Tournament(1)
	if err != nil || missing != nil {
		t.Fatalf("expected no tournament 1, got %+v (%v)", missing, err)
	}
}

func TestBanIsIdempotent(t *testing.T) {
	db := testDB(t)
	if banned, _ := db.Ban(4242); !banned {
		t.Fatal("first ban should report a change")
	}
	if banned, _ := db.Ban(4242); banned {
		t.Fatal("second ban should report no change")
	}
	if !db.IsBanned(4242) {
		t.Fatal("4242 should be banned")
	}
	if unbanned, _ := db.Unban(4242); !unbanned {
		t.Fatal("unban should report a change")
	}
	if unbanned, _ := db.Unban(4242); unbanned {
		t.Fatal("second unban should report no change")
	}
}

func TestPrefsSurviveARoundTrip(t *testing.T) {
	db := testDB(t)
	if got := db.HostOf(1, "fallback.example"); got != "fallback.example" {
		t.Fatalf("unset host should fall back, got %q", got)
	}
	if err := db.SetPrefs(1, Prefs{Host: "rating.pecheny.me"}); err != nil {
		t.Fatal(err)
	}
	if got := db.HostOf(1, "fallback.example"); got != "rating.pecheny.me" {
		t.Fatalf("got %q", got)
	}
}
