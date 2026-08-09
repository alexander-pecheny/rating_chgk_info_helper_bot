package dates

import (
	"encoding/json"
	"testing"
	"time"
)

// The two grids below are the exact output of the Python bot this one replaces,
// captured before it was retired. Organisers paste these into the rating site
// by hand, so a stray minute is a real bug.
const gridDefaults = `<b>Синхрон:</b>
Начало: <pre>2026-11-13 19:00:00</pre>
Конец: <pre>2026-11-16 19:00:00</pre>
Приём заявок до: <pre>2026-11-16 19:00:00</pre>
Скачивание пакетов ведущими с: <pre>2026-11-12 19:00:00</pre>
Приём составов команд и результатов до: <pre>2026-11-19 19:00:00</pre>
Результаты скрыты до: <pre>2026-11-16 19:00:00</pre>
Срок незасветки пакета до: <pre>2026-11-16 19:00:00</pre>
Приём апелляций до: <pre>2026-11-24 19:00:00</pre>
Корректировка результатов представителями до: <pre>2026-12-02 19:00:00</pre>`

const gridWithAsync = `<b>Синхрон:</b>
Начало: <pre>2026-11-13 11:00:00</pre>
Конец: <pre>2026-11-14 11:00:00</pre>
Приём заявок до: <pre>2026-11-14 11:00:00</pre>
Скачивание пакетов ведущими с: <pre>2026-11-12 11:00:00</pre>
Приём составов команд и результатов до: <pre>2026-11-19 11:00:00</pre>
Результаты скрыты до: <pre>2026-11-28 11:00:00</pre>
Срок незасветки пакета до: <pre>2026-12-04 14:00:00</pre>
Приём апелляций до: <pre>2026-11-24 11:00:00</pre>
Корректировка результатов представителями до: <pre>2026-11-30 11:00:00</pre>
В синхроне не забудьте поставить галочку «Сразу показывать спорные, апелляции и расплюсовку всем причастным».

<b>Асинхрон:</b>
Начало: <pre>2026-11-14 14:00:00</pre>
Конец: <pre>2026-12-04 14:00:00</pre>
Приём заявок до: <pre>2026-12-04 14:00:00</pre>
Скачивание пакетов ведущими с: <pre>2026-11-13 14:00:00</pre>
Приём составов команд и результатов до: <pre>2027-01-03 14:00:00</pre>
Результаты скрыты до: <pre>2026-12-04 14:00:00</pre>
Срок незасветки пакета до: <pre>2026-12-04 14:00:00</pre>
Приём апелляций до: <pre>2026-12-11 14:00:00</pre>
Корректировка результатов представителями до: <pre>2027-01-03 14:00:00</pre>`

func TestGridMatchesTheRetiredBot(t *testing.T) {
	cases := []struct {
		name      string
		start     string
		syncDays  int
		asyncDays int
		prefsFrom string
		want      string
	}{
		{"defaults, no async", "2026-11-13", 3, 0, "", gridDefaults},
		{"explicit time and a long async", "2026-11-13 11:00", 1, 20,
			"апелляции: 10, составы: 5", gridWithAsync},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var prefs Prefs
			if tc.prefsFrom != "" {
				prefs.UpdateFromString(tc.prefsFrom)
			}
			start, err := ParseStart(tc.start, prefs)
			if err != nil {
				t.Fatal(err)
			}
			if got := Generate(start, tc.syncDays, tc.asyncDays, prefs); got != tc.want {
				t.Fatalf("grid differs:\n--- got ---\n%s\n--- want ---\n%s", got, tc.want)
			}
		})
	}
}

func TestParseAcceptsEveryShapeTheAPIUses(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"2026-11-13", "2026-11-13 00:00:00"},
		{"2026-11-13T09:30:00", "2026-11-13 09:30:00"},
		{"2026-11-13T09:30:00+03:00", "2026-11-13 09:30:00"},
		{"2026-11-13T06:30:00+00:00", "2026-11-13 09:30:00"},
		{"2026-11-13 09:30", "2026-11-13 09:30:00"},
	}
	for _, tc := range cases {
		got, err := Parse(tc.input)
		if err != nil {
			t.Errorf("%q: %v", tc.input, err)
			continue
		}
		if Plain(got) != tc.want {
			t.Errorf("%q: got %s, want %s", tc.input, Plain(got), tc.want)
		}
	}
	if _, err := Parse("не дата"); err == nil {
		t.Error("nonsense should not parse")
	}
}

func TestHRReadsTheDateAloud(t *testing.T) {
	when := time.Date(2026, 11, 13, 0, 0, 0, 0, Zone)
	if got := HR(when); got != "13 ноября" {
		t.Fatalf("got %q", got)
	}
}

func TestDaysBetweenCountsCalendarDays(t *testing.T) {
	end := time.Date(2026, 11, 13, 23, 0, 0, 0, Zone)
	next := time.Date(2026, 11, 14, 1, 0, 0, 0, Zone)
	if got := DaysBetween(end, next); got != 1 {
		t.Fatalf("two hours across midnight is %d days, want 1", got)
	}
	if got := DaysBetween(end, end.Add(30*time.Minute)); got != 0 {
		t.Fatalf("same day is %d days, want 0", got)
	}
}

func TestPrefsFallBackToDefaults(t *testing.T) {
	var prefs Prefs
	hour, minute := prefs.StartTime()
	if hour != 19 || minute != 0 {
		t.Errorf("default start time is %02d:%02d", hour, minute)
	}
	if prefs.RecapsDays() != 3 || prefs.AppealsDays() != 8 || prefs.FixesDays() != 16 {
		t.Errorf("default deadlines are %d/%d/%d",
			prefs.RecapsDays(), prefs.AppealsDays(), prefs.FixesDays())
	}

	prefs.UpdateFromString("время: 11:30, апелляции: 10")
	hour, minute = prefs.StartTime()
	if hour != 11 || minute != 30 {
		t.Errorf("start time is %02d:%02d", hour, minute)
	}
	if prefs.AppealsDays() != 10 {
		t.Errorf("appeals deadline is %d", prefs.AppealsDays())
	}
	if prefs.RecapsDays() != 3 {
		t.Errorf("an unmentioned deadline should keep its default, got %d", prefs.RecapsDays())
	}
}

// The stored shape is the Python bot's, so a chat that set its preferences
// before the rewrite keeps them.
func TestPrefsReadTheStoredJSON(t *testing.T) {
	const stored = `{"time": [11, 30], "results_recaps_to": 5, "appeals_to": null, "results_fixes_to": 20}`
	var prefs Prefs
	if err := json.Unmarshal([]byte(stored), &prefs); err != nil {
		t.Fatal(err)
	}
	hour, minute := prefs.StartTime()
	if hour != 11 || minute != 30 {
		t.Errorf("start time is %02d:%02d", hour, minute)
	}
	if prefs.RecapsDays() != 5 || prefs.FixesDays() != 20 {
		t.Errorf("stored deadlines read as %d/%d", prefs.RecapsDays(), prefs.FixesDays())
	}
	if prefs.AppealsDays() != 8 {
		t.Errorf("a null should read as the default, got %d", prefs.AppealsDays())
	}
}

func TestNonsenseTimeLeavesTheDefault(t *testing.T) {
	var prefs Prefs
	prefs.UpdateFromString("время: не время")
	if hour, minute := prefs.StartTime(); hour != 19 || minute != 0 {
		t.Fatalf("got %02d:%02d, want the default", hour, minute)
	}
}
