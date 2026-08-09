package rating

import (
	"fmt"
	"strings"
	"time"

	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/dates"
)

// The jury has six days to rule on controversials and sixteen to rule on
// appeals, both counted from the end of the tournament.
const (
	controversialsDeadline = 6
	appealsDeadline        = 16
)

// Reminders maps a subscription kind ("i" for controversials, "a" for appeals)
// to the text a subscriber of that kind should get today.
type Reminders map[string]string

func name(info Info) string { return fmt.Sprintf("<b>%d %s</b>", info.ID, info.Name) }

func initMessage(info Info, end time.Time) string {
	return fmt.Sprintf(
		"Крайний срок рассмотрения спорных на турнире %s — %s, апелляций — %s",
		name(info),
		dates.HR(end.Add(dates.Days(controversialsDeadline))),
		dates.HR(end.Add(dates.Days(appealsDeadline))),
	)
}

func dueMessage(info Info, what, page string) string {
	return fmt.Sprintf(
		`%s на турнире %s должны быть <a href="https://rating.chgk.info/tournament/%d/%s">рассмотрены</a> до конца следующего дня.`,
		what, name(info), info.ID, page)
}

func countMessage(info Info, count int, what, page string) string {
	return fmt.Sprintf(
		`На турнире %s %d %s. <a href="https://rating.chgk.info/tournament/%d/%s">Рассмотреть</a>`,
		name(info), count, what, info.ID, page)
}

// canCountExactly reports whether the questions are public yet. Until they are,
// the bot can only say a deadline is near, not how much is left to rule on.
func canCountExactly(info Info, now time.Time) bool {
	if info.HideQuestionsTo == "" {
		return false
	}
	hideTo, err := dates.Parse(info.HideQuestionsTo)
	if err != nil {
		return false
	}
	return hideTo.Before(now)
}

func (c *Client) newControversials(info Info) (int, error) {
	results, err := c.Results(info.ID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, result := range results {
		for _, controversial := range result.Controversials {
			if controversial.Status == "N" {
				count++
			}
		}
	}
	return count, nil
}

func (c *Client) newAppeals(info Info) (int, error) {
	appeals, err := c.Appeals(info.ID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, appeal := range appeals {
		if appeal.Status == "N" {
			count++
		}
	}
	return count, nil
}

// RemindersFor decides what today's reminders are for one tournament.
func (c *Client) RemindersFor(info Info, now time.Time) (Reminders, error) {
	end, err := dates.Parse(info.DateEnd)
	if err != nil {
		return nil, err
	}
	perKind := map[string][]string{}
	elapsed := dates.DaysBetween(end, now)

	if elapsed == 1 {
		opening := initMessage(info, end)
		perKind["i"] = append(perKind["i"], opening)
		perKind["a"] = append(perKind["a"], opening)
	}

	if canCountExactly(info, now) {
		if elapsed >= controversialsDeadline-1 {
			count, err := c.newControversials(info)
			if err != nil {
				return nil, err
			}
			if count > 0 {
				perKind["i"] = append(perKind["i"],
					countMessage(info, count, "нерассмотренных спорных", "controversials"))
			}
		}
		if elapsed >= appealsDeadline-1 {
			count, err := c.newAppeals(info)
			if err != nil {
				return nil, err
			}
			if count > 0 {
				perKind["a"] = append(perKind["a"],
					countMessage(info, count, "нерассмотренных апелляций", "appeals"))
			}
		}
	} else if elapsed == controversialsDeadline-1 {
		perKind["i"] = append(perKind["i"], dueMessage(info, "Спорные", "controversials"))
	} else if elapsed == appealsDeadline-1 {
		perKind["a"] = append(perKind["a"], dueMessage(info, "Апелляции", "appeals"))
	}

	reminders := Reminders{}
	for kind, messages := range perKind {
		reminders[kind] = strings.Join(messages, "\n\n")
	}
	return reminders, nil
}
