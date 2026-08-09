// Package text holds the wording and the formatting rules: how a message is
// split to fit Telegram's limit, how a host is cleaned up, and how Russian
// counts are pluralised.
package text

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	Start = `Привет! Это бот-помощник для турнирного сайта.

Он умеет оповещать о новых заявках на турниры.

Чтобы подписаться на обновления, напиши <code>/subscribe</code> и id турниров через запятую, вот так:

<code>/subscribe 7000, 9002, 9015</code>

Чтобы отписаться, то же самое, но с командой <code>/unsubscribe</code>.
`
	IDTournsPrompt = "Пожалуйста, укажите id турниров через запятую или /cancel для отмены."
	IDTournPrompt  = "Пожалуйста, укажите id турнира или /cancel для отмены."
	DefaultHost    = "rating.chgk.info"
)

var reHost = regexp.MustCompile(`[a-z][a-z.]+[a-z]`)

// ApplicationForm picks the Russian plural that goes with a count.
func ApplicationForm(n int) string {
	s := strconv.Itoa(n)
	if strings.HasSuffix(s, "11") || strings.HasSuffix(s, "12") ||
		strings.HasSuffix(s, "13") || strings.HasSuffix(s, "14") {
		return "нерассмотренных заявок"
	}
	if strings.HasSuffix(s, "1") {
		return "нерассмотренная заявка"
	}
	if strings.HasSuffix(s, "2") || strings.HasSuffix(s, "3") || strings.HasSuffix(s, "4") {
		return "нерассмотренные заявки"
	}
	return "нерассмотренных заявок"
}

// ListOfInts reads tournament ids out of whatever separators someone typed.
func ListOfInts(s string) []int {
	var out []int
	for _, field := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == '\r'
	}) {
		if n, err := strconv.Atoi(field); err == nil && n != 0 {
			out = append(out, n)
		}
	}
	return out
}

func StripHost(host string) string {
	host = strings.TrimSpace(host)
	for _, prefix := range []string{"http://", "https://", "www."} {
		host = strings.TrimPrefix(host, prefix)
	}
	parsed, err := url.Parse("https://" + host)
	if err != nil {
		return ""
	}
	return parsed.Host
}

func ValidHost(host string) bool {
	return host != "" && reHost.MatchString(host) && strings.Contains(host, ".")
}

func PlayerLink(entry, host string) string {
	id, _, _ := strings.Cut(entry, " ")
	return fmt.Sprintf(`<a href="https://%s/player/%s">%s</a>`, host, id, entry)
}

func TournamentLink(entry, host string) string {
	id, _, _ := strings.Cut(entry, " ")
	return fmt.Sprintf(`<a href="https://%s/tournament/%s">%s</a>`, host, id, entry)
}

func ReviewLink(host string, tournamentID int) string {
	return fmt.Sprintf(`<a href="https://%s/tournament/%d/requests">Рассмотреть</a>`, host, tournamentID)
}

// FormatApplications lists the representatives in the order the bot has always
// used: by given name, then surname, then id.
func FormatApplications(reps []string, host string) string {
	sorted := append([]string(nil), reps...)
	sort.Slice(sorted, func(i, j int) bool {
		return sortKey(sorted[i]) < sortKey(sorted[j])
	})
	lines := make([]string, 0, len(sorted))
	for _, rep := range sorted {
		lines = append(lines, PlayerLink(rep, host))
	}
	return strings.Join(lines, "\n")
}

// A representative reads "<id> <name> <surname> (<town>)".
func sortKey(entry string) string {
	parts := strings.Fields(entry)
	if len(parts) < 3 {
		return entry
	}
	return parts[1] + "\x00" + parts[2] + "\x00" + parts[0]
}
