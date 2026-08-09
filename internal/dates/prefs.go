package dates

import (
	"fmt"
	"strings"
)

// Prefs is a chat's house style for the date grid: when a sync starts, and how
// long after it ends each deadline falls. An unset field reads as the default.
type Prefs struct {
	Time            *[2]int `json:"time"`
	ResultsRecapsTo *int    `json:"results_recaps_to"`
	AppealsTo       *int    `json:"appeals_to"`
	ResultsFixesTo  *int    `json:"results_fixes_to"`
}

const (
	defaultHour            = 19
	defaultMinute          = 0
	defaultResultsRecapsTo = 3
	defaultAppealsTo       = 8
	defaultResultsFixesTo  = 16
)

func (p Prefs) StartTime() (int, int) {
	if p.Time == nil {
		return defaultHour, defaultMinute
	}
	return p.Time[0], p.Time[1]
}

// A stored zero means "unset" here, exactly as it did in the Python bot: a
// deadline on the day the tournament ends is not something anyone configures.
func orDefault(v *int, fallback int) int {
	if v == nil || *v == 0 {
		return fallback
	}
	return *v
}

func (p Prefs) RecapsDays() int  { return orDefault(p.ResultsRecapsTo, defaultResultsRecapsTo) }
func (p Prefs) AppealsDays() int { return orDefault(p.AppealsTo, defaultAppealsTo) }
func (p Prefs) FixesDays() int   { return orDefault(p.ResultsFixesTo, defaultResultsFixesTo) }

func (p Prefs) HR() string {
	hour, minute := p.StartTime()
	return strings.Join([]string{
		fmt.Sprintf("время: %02d:%02d", hour, minute),
		fmt.Sprintf("составы/результаты: %d дней от конца турнира", p.RecapsDays()),
		fmt.Sprintf("апелляции: %d дней от конца турнира", p.AppealsDays()),
		fmt.Sprintf("корректировка: %d дней от конца турнира", p.FixesDays()),
	}, "\n")
}

func parseTime(s string) *[2]int {
	if !strings.Contains(s, ":") {
		if hour, ok := TryInt(s); ok {
			return &[2]int{hour, 0}
		}
		return nil
	}
	hourText, minuteText, _ := strings.Cut(s, ":")
	hour, okHour := TryInt(hourText)
	minute, okMinute := TryInt(minuteText)
	if !okHour || !okMinute {
		return nil
	}
	return &[2]int{hour, minute}
}

// UpdateFromString reads "время: 19:30, апелляции: 8" and keeps whatever the
// organiser did not mention.
func (p *Prefs) UpdateFromString(s string) {
	for _, part := range strings.Split(strings.TrimSpace(s), ",") {
		key, value, found := strings.Cut(part, ":")
		if !found {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.ToLower(strings.TrimSpace(value))
		switch key {
		case "время":
			p.Time = parseTime(value)
		case "составы", "результаты":
			p.ResultsRecapsTo = intOrNil(value)
		case "апелляции":
			p.AppealsTo = intOrNil(value)
		case "корректировка":
			p.ResultsFixesTo = intOrNil(value)
		}
	}
}

func intOrNil(s string) *int {
	n, ok := TryInt(s)
	if !ok {
		return nil
	}
	return &n
}
