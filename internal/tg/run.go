package tg

import (
	"context"
	"time"

	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/dates"
)

// ScheduleJobs puts the three periodic jobs on the clock: applications every
// two hours on the even hour, reminders once a day at half past six Moscow
// time, and the log prune once a day.
func (b *Bot) ScheduleJobs() {
	now := time.Now().UTC()
	aheadHours := 2
	if now.Hour()%2 == 1 {
		aheadHours = 1
	}
	firstCheck := now.Add(time.Duration(aheadHours) * time.Hour).Truncate(time.Hour)

	b.scheduler.Add(&Job{
		Name:  "check_applications",
		Next:  firstCheck,
		Every: 2 * time.Hour,
		Run:   b.CheckApplications,
	})
	b.scheduler.Add(&Job{
		Name:  "make_reminders",
		Next:  nextReminderTime(dates.Now()),
		Every: 24 * time.Hour,
		Run:   b.MakeReminders,
	})
	// Restarts are frequent enough that a plain 24h interval, which restarts
	// its countdown on every boot, could keep the cap from ever being applied.
	b.scheduler.Add(&Job{
		Name:  "prune_logs",
		Next:  time.Now().Add(5 * time.Minute),
		Every: 24 * time.Hour,
		Run:   func(context.Context) { b.logs.Prune() },
	})
}

// nextReminderTime is the next 15:30 Moscow time, which is when organisers are
// most likely to act on a deadline warning.
func nextReminderTime(now time.Time) time.Time {
	target := time.Date(now.Year(), now.Month(), now.Day(), 15, 30, 0, 0, dates.Zone)
	if target.Before(now) {
		target = target.Add(24 * time.Hour)
	}
	return target
}
