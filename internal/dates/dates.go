// Package dates handles the tournament calendar: parsing the rating site's
// timestamps and generating the date grid an organiser fills into a new sync.
package dates

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Every date this bot says out loud is Moscow time; the rating site has no
// other timezone and none of these deadlines observe DST.
var Zone = time.FixedZone("UTC+3", 3*60*60)

func Now() time.Time { return time.Now().In(Zone) }

var months = [...]string{
	"января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря",
}

var layouts = []string{
	"2006-01-02",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05-0700",
	"2006-01-02 15:04",
}

// Parse reads any timestamp shape the rating API or an organiser produces, and
// returns it in Moscow time.
func Parse(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, Zone); err == nil {
			return t.In(Zone), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse %q as a date", s)
}

// HR renders a date the way it is read aloud: "13 ноября".
func HR(t time.Time) string {
	t = t.In(Zone)
	return fmt.Sprintf("%d %s", t.Day(), months[t.Month()-1])
}

// Plain renders a date the way the rating site's admin form wants it typed.
func Plain(t time.Time) string { return t.In(Zone).Format("2006-01-02 15:04:05") }

func Days(n int) time.Duration { return time.Duration(n) * 24 * time.Hour }

// DaysBetween counts whole calendar days from a to b, ignoring the time of day.
func DaysBetween(a, b time.Time) int {
	da := a.In(Zone).Truncate(0)
	db := b.In(Zone).Truncate(0)
	ya, ma, dda := da.Date()
	yb, mb, ddb := db.Date()
	midA := time.Date(ya, ma, dda, 0, 0, 0, 0, Zone)
	midB := time.Date(yb, mb, ddb, 0, 0, 0, 0, Zone)
	return int(midB.Sub(midA).Hours() / 24)
}

func TryInt(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return n, true
}
