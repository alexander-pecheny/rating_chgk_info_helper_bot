package dates

import (
	"fmt"
	"strings"
	"time"
)

// grid is the set of dates the rating site asks an organiser to fill in when
// they create a tournament.
type grid struct {
	Start           time.Time
	End             time.Time
	RequestsAllowed time.Time
	DownloadFrom    time.Time
	RecapsTo        time.Time
	HideResultsTo   time.Time
	HideQuestionsTo time.Time
	AppealsTo       time.Time
	FixesTo         time.Time
}

var gridLabels = []struct {
	label string
	pick  func(grid) time.Time
}{
	{"Начало", func(g grid) time.Time { return g.Start }},
	{"Конец", func(g grid) time.Time { return g.End }},
	{"Приём заявок до", func(g grid) time.Time { return g.RequestsAllowed }},
	{"Скачивание пакетов ведущими с", func(g grid) time.Time { return g.DownloadFrom }},
	{"Приём составов команд и результатов до", func(g grid) time.Time { return g.RecapsTo }},
	{"Результаты скрыты до", func(g grid) time.Time { return g.HideResultsTo }},
	{"Срок незасветки пакета до", func(g grid) time.Time { return g.HideQuestionsTo }},
	{"Приём апелляций до", func(g grid) time.Time { return g.AppealsTo }},
	{"Корректировка результатов представителями до", func(g grid) time.Time { return g.FixesTo }},
}

func (g grid) lines() []string {
	out := make([]string, 0, len(gridLabels))
	for _, field := range gridLabels {
		out = append(out, fmt.Sprintf("%s: <pre>%s</pre>", field.label, Plain(field.pick(g))))
	}
	return out
}

// ParseStart reads the start of a sync, filling in the organiser's usual hour
// when they gave only a date.
func ParseStart(s string, prefs Prefs) (time.Time, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, ":") {
		return time.ParseInLocation("2006-01-02 15:04", s, Zone)
	}
	day, err := time.ParseInLocation("2006-01-02", s, Zone)
	if err != nil {
		return time.Time{}, err
	}
	hour, minute := prefs.StartTime()
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, Zone), nil
}

// Generate lays out both halves of a tournament from its start and lengths.
func Generate(start time.Time, syncDays, asyncDays int, prefs Prefs) string {
	sync := grid{Start: start}
	sync.End = sync.Start.Add(Days(syncDays))
	sync.RequestsAllowed = sync.End
	sync.DownloadFrom = sync.Start.Add(-Days(1))
	sync.RecapsTo = sync.End.Add(Days(prefs.RecapsDays()))
	sync.AppealsTo = sync.End.Add(Days(prefs.AppealsDays()))
	sync.FixesTo = sync.End.Add(Days(prefs.FixesDays()))

	if asyncDays == 0 {
		sync.HideResultsTo = sync.End
		sync.HideQuestionsTo = sync.End
		return strings.Join(append([]string{"<b>Синхрон:</b>"}, sync.lines()...), "\n")
	}

	async := grid{Start: sync.End.Add(3 * time.Hour)}
	async.End = async.Start.Add(Days(asyncDays))
	async.RequestsAllowed = async.End
	async.DownloadFrom = async.Start.Add(-Days(1))
	async.RecapsTo = async.End.Add(Days(30))
	async.AppealsTo = async.End.Add(Days(7))
	async.FixesTo = async.End.Add(Days(30))
	async.HideResultsTo = async.End
	async.HideQuestionsTo = async.End

	if asyncDays < 14 { // deadline imposed by https://www.maii.li/p/aegis-rating
		sync.HideResultsTo = async.End
	} else {
		sync.HideResultsTo = sync.End.Add(Days(14))
	}
	sync.HideQuestionsTo = async.End

	out := append([]string{"<b>Синхрон:</b>"}, sync.lines()...)
	out = append(out,
		"В синхроне не забудьте поставить галочку «Сразу показывать спорные, апелляции и расплюсовку всем причастным».",
		"",
		"<b>Асинхрон:</b>",
	)
	return strings.Join(append(out, async.lines()...), "\n")
}
