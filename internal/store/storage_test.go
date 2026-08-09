package store

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testKey = Key{BotID: 1, ChatID: 2, UserID: 2, Destiny: "default"}

func TestStateAndDataSurviveANewFSM(t *testing.T) {
	db := testDB(t)
	fsm := NewFSM(db, 100*time.Second)

	if err := fsm.SetName(testKey, "Flows:announce"); err != nil {
		t.Fatal(err)
	}
	if err := fsm.Update(testKey, map[string]any{"sync_days": 3}); err != nil {
		t.Fatal(err)
	}

	reopened := NewFSM(db, 100*time.Second)
	state := reopened.Get(testKey)
	if state.Name != "Flows:announce" {
		t.Fatalf("state name is %q", state.Name)
	}
	if got := state.Int("sync_days"); got != 3 {
		t.Fatalf("sync_days is %d", got)
	}
}

func TestStateOlderThanTheTTLReadsAsAbsent(t *testing.T) {
	db := testDB(t)
	fsm := NewFSM(db, -time.Second)
	if err := fsm.SetName(testKey, "Flows:announce"); err != nil {
		t.Fatal(err)
	}
	if got := fsm.Get(testKey).Name; got != "" {
		t.Fatalf("expired state came back as %q", got)
	}
}

func TestClearingStateWithoutDataRemovesTheRow(t *testing.T) {
	db := testDB(t)
	fsm := NewFSM(db, IdleTTL)
	if err := fsm.SetName(testKey, "Flows:announce"); err != nil {
		t.Fatal(err)
	}
	if err := fsm.SetName(testKey, ""); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := db.conn.QueryRow("select count(*) from fsm_state").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("expected the row to be gone, found %d", rows)
	}
}

func testLogStore(t *testing.T, maxBytes int64) *LogStore {
	t.Helper()
	logs, err := OpenLogStore(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	logs.MaxBytes = maxBytes
	t.Cleanup(func() { logs.Close() })
	return logs
}

func fill(logs *LogStore, rows int) {
	payload := strings.Repeat("x", 2000)
	for i := range rows {
		logs.RecordTraffic("in", "", int64(i), "t", payload)
	}
}

func TestPruneDropsOldestRowsUntilUnderCap(t *testing.T) {
	logs := testLogStore(t, 120*1024)
	fill(logs, 400)
	if logs.Size() <= logs.MaxBytes {
		t.Skip("the test data did not exceed the cap")
	}

	logs.Prune()

	if logs.Size() > logs.MaxBytes {
		t.Fatalf("still %d bytes, cap is %d", logs.Size(), logs.MaxBytes)
	}
	remaining := logs.CountRows("traffic")
	if remaining == 0 || remaining >= 400 {
		t.Fatalf("expected some but not all rows to survive, %d remain", remaining)
	}
	var oldest int64
	if err := logs.conn.QueryRow("select min(chat_id) from traffic").Scan(&oldest); err != nil {
		t.Fatal(err)
	}
	if oldest == 0 {
		t.Fatal("the oldest rows should have gone first")
	}
}

func TestPruneIsANoOpWhenSmall(t *testing.T) {
	logs := testLogStore(t, MaxLogBytes)
	logs.RecordTraffic("in", "", 1, "t", "{}")
	if removed := logs.Prune(); removed != 0 {
		t.Fatalf("pruned %d rows from a small file", removed)
	}
	if got := logs.CountRows("traffic"); got != 1 {
		t.Fatalf("expected the row to stay, found %d", got)
	}
}

func TestTrafficRecordsBothDirections(t *testing.T) {
	logs := testLogStore(t, MaxLogBytes)
	logs.RecordTraffic("in", "", 42, "/start", "{}")
	logs.RecordTraffic("out", "sendMessage", 42, "Привет", "{}")

	rows, err := logs.Traffic()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected two rows, got %d", len(rows))
	}
	if rows[0].Direction != "in" || rows[0].Text != "/start" || rows[0].ChatID != 42 {
		t.Fatalf("incoming row is %+v", rows[0])
	}
	if rows[1].Method != "sendMessage" {
		t.Fatalf("outgoing row is %+v", rows[1])
	}
}
