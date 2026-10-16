package store

import (
	"testing"
	"time"
)

func TestLastAnnounceIsTheAuthorsNewestWithinTheWindow(t *testing.T) {
	db := testDB(t)
	now := time.Now()
	for _, record := range []struct {
		author int64
		ids    []int
		at     time.Time
	}{
		{1, []int{10}, now.Add(-2 * time.Hour)},
		{1, []int{11, 12}, now.Add(-time.Hour)},
		{2, []int{13}, now},
	} {
		if err := db.RecordAnnounce(record.author, record.ids, record.at); err != nil {
			t.Fatal(err)
		}
	}

	last, err := db.LastAnnounce(1, now.Add(-3*time.Hour))
	if err != nil || last == nil {
		t.Fatalf("got %v, %v", last, err)
	}
	if len(last.MessageIDs) != 2 || last.MessageIDs[0] != 11 {
		t.Fatalf("got message ids %v", last.MessageIDs)
	}
	if other, _ := db.Announce(2, last.ID); other != nil {
		t.Fatal("another author must not see this post")
	}

	if err := db.DeleteAnnounce(last.ID); err != nil {
		t.Fatal(err)
	}
	if last, _ := db.LastAnnounce(1, now.Add(-90*time.Minute)); last != nil {
		t.Fatalf("a post older than the window came back: %v", last.MessageIDs)
	}
}
