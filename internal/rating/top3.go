package rating

import (
	"fmt"
	"sort"
	"strings"
)

const ResultsUnavailable = "Результаты пока недоступны или доступны не полностью."

// Top3 renders the podium overall and once per team flag, extending past three
// places whenever teams are tied.
func (c *Client) Top3(tournamentID int) (string, error) {
	results, err := c.Results(tournamentID)
	if err != nil {
		return "", err
	}
	for _, result := range results {
		if result.QuestionsTotal == nil {
			return ResultsUnavailable, nil
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		return *results[i].QuestionsTotal > *results[j].QuestionsTotal
	})

	lines := []string{"<b>Топ-3 в общем зачёте</b>", topByFlag(results, "")}
	for _, flag := range flagsOf(results) {
		lines = append(lines, fmt.Sprintf("<b>Топ-3 по флагу %s</b>", flag), topByFlag(results, flag))
	}
	return strings.Join(lines, "\n"), nil
}

func flagsOf(results []Result) []string {
	seen := map[string]bool{}
	for _, result := range results {
		for _, flag := range result.Flags {
			seen[flag.ShortName] = true
		}
	}
	flags := make([]string, 0, len(seen))
	for flag := range seen {
		flags = append(flags, flag)
	}
	sort.Strings(flags)
	return flags
}

func hasFlag(result Result, flag string) bool {
	for _, f := range result.Flags {
		if f.ShortName == flag {
			return true
		}
	}
	return false
}

func topByFlag(results []Result, flag string) string {
	if flag != "" {
		filtered := make([]Result, 0, len(results))
		for _, result := range results {
			if hasFlag(result, flag) {
				filtered = append(filtered, result)
			}
		}
		results = filtered
	}

	var teams []Result
	for i, result := range results {
		teams = append(teams, result)
		// Stop at three, unless the next team is tied with this one — a shared
		// third place is still third place.
		if len(teams) >= 3 && i+1 < len(results) &&
			*results[i+1].QuestionsTotal < *result.QuestionsTotal {
			break
		}
	}

	lines := make([]string, 0, len(teams))
	for _, team := range teams {
		lines = append(lines, fmt.Sprintf("%s. %s (%s) (%s) — %d",
			place(teams, *team.QuestionsTotal),
			team.Current.Name, team.Current.Town.Name, recaps(team), *team.QuestionsTotal))
	}
	return strings.Join(lines, "\n")
}

// place reports "2" for a clear position and "2–4" for a tie, counting how many
// teams scored the same.
func place(teams []Result, score int) string {
	first, last := 0, 0
	for i, team := range teams {
		if *team.QuestionsTotal == score {
			if first == 0 {
				first = i + 1
			}
			last = i + 1
		}
	}
	if first == last {
		return fmt.Sprintf("%d", first)
	}
	return fmt.Sprintf("%d–%d", first, last)
}

// recaps lists the team as it played, by surname, the way the site does.
func recaps(team Result) string {
	players := make([]string, 0, len(team.TeamMembers))
	sorted := append([]TeamMember(nil), team.TeamMembers...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Player.Surname != sorted[j].Player.Surname {
			return sorted[i].Player.Surname < sorted[j].Player.Surname
		}
		return sorted[i].Player.Name < sorted[j].Player.Name
	})
	for _, member := range sorted {
		players = append(players, member.Player.Name+" "+member.Player.Surname)
	}
	return strings.Join(players, ", ")
}
