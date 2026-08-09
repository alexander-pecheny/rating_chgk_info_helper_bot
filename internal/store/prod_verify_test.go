package store

import (
	"os"
	"testing"
)

// TestReadsARealDatabase runs only when pointed at a copy of a live bot.db,
// which is how the Go port was checked against production data before deploy.
func TestReadsARealDatabase(t *testing.T) {
	path := os.Getenv("REAL_BOT_DB")
	if path == "" {
		t.Skip("set REAL_BOT_DB to a copy of a live bot.db")
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tournaments, err := db.Tournaments()
	if err != nil {
		t.Fatalf("reading tournaments: %v", err)
	}
	chats := map[int64]bool{}
	applications := 0
	for _, tournament := range tournaments {
		applications += len(tournament.Applications)
		for chatID := range tournament.Subscribers {
			chats[chatID] = true
			if chatID == 0 {
				t.Errorf("tournament %d has a zero chat id", tournament.ID)
			}
		}
	}
	t.Logf("%d tournaments, %d subscriber chats, %d applications",
		len(tournaments), len(chats), applications)
	if len(tournaments) == 0 {
		t.Fatal("expected the live database to hold tournaments")
	}
	for _, tournament := range tournaments[:3] {
		t.Logf("  %d %s -> %v", tournament.ID, tournament.Name, tournament.Subscribers)
	}
}
