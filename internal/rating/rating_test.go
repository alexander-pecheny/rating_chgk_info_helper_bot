package rating

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/dates"
)

// resultsJSON is a played tournament with a three-way tie for second, which is
// the case the podium logic exists for.
const resultsJSON = `[
 {"questionsTotal":30,"current":{"name":"Альфа","town":{"name":"Москва"}},
  "flags":[{"shortName":"Ст"}],
  "teamMembers":[{"player":{"name":"Иван","surname":"Петров"}},
                 {"player":{"name":"Анна","surname":"Иванова"}}]},
 {"questionsTotal":25,"current":{"name":"Бета","town":{"name":"Тверь"}},
  "flags":[{"shortName":"Ст"}],"teamMembers":[]},
 {"questionsTotal":25,"current":{"name":"Гамма","town":{"name":"Тула"}},
  "flags":[],"teamMembers":[]},
 {"questionsTotal":25,"current":{"name":"Дельта","town":{"name":"Омск"}},
  "flags":[{"shortName":"Ст"}],"teamMembers":[]},
 {"questionsTotal":10,"current":{"name":"Эпсилон","town":{"name":"Пермь"}},
  "flags":[],"teamMembers":[]}]`

// This is what the Python bot printed for the same results.
const wantTop3 = `<b>Топ-3 в общем зачёте</b>
1. Альфа (Москва) (Анна Иванова, Иван Петров) — 30
2–4. Бета (Тверь) () — 25
2–4. Гамма (Тула) () — 25
2–4. Дельта (Омск) () — 25
<b>Топ-3 по флагу Ст</b>
1. Альфа (Москва) (Анна Иванова, Иван Петров) — 30
2–3. Бета (Тверь) () — 25
2–3. Дельта (Омск) () — 25`

// serve answers the given paths and 404s everything else.
func serve(t *testing.T, bodies map[string]string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, known := bodies[r.URL.Path]
		if !known {
			w.WriteHeader(http.StatusNotFound)
			body = `{"detail":"Not found"}`
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return NewAt(server.URL, 0)
}

func TestTop3MatchesTheRetiredBot(t *testing.T) {
	client := serve(t, map[string]string{"/tournaments/9002/results.json": resultsJSON})
	got, err := client.Top3(9002)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantTop3 {
		t.Fatalf("podium differs:\n--- got ---\n%s\n--- want ---\n%s", got, wantTop3)
	}
}

func TestTop3SaysSoWhenResultsAreIncomplete(t *testing.T) {
	client := serve(t, map[string]string{
		"/tournaments/9002/results.json": `[{"questionsTotal":null,"current":{"name":"Альфа","town":{"name":"Москва"}}}]`,
	})
	got, err := client.Top3(9002)
	if err != nil {
		t.Fatal(err)
	}
	if got != ResultsUnavailable {
		t.Fatalf("got %q", got)
	}
}

func TestApplicationsKeepOnlyTheUnreviewed(t *testing.T) {
	client := serve(t, map[string]string{
		"/tournaments/9002/requests.json": `[
		 {"id":7,"status":"N","representative":{"id":123,"name":"Иван","surname":"Иванов"},
		  "venue":{"town":{"name":"Москва"}}},
		 {"id":8,"status":"A","representative":{"id":124,"name":"Пётр","surname":"Петров"},
		  "venue":{"town":{"name":"Тверь"}}}]`,
	})
	applications, err := client.Applications(9002)
	if err != nil {
		t.Fatal(err)
	}
	if len(applications) != 1 {
		t.Fatalf("expected one unreviewed application, got %d", len(applications))
	}
	if got := applications["7"].Rep; got != "123 Иван Иванов (Москва)" {
		t.Fatalf("representative reads %q", got)
	}
}

func TestMissingTournamentIsReportedAsBad(t *testing.T) {
	// A 404 body still decodes, which is how the site says "no such tournament".
	var info Info
	if err := json.Unmarshal([]byte(`{"detail":"Not found"}`), &info); err != nil {
		t.Fatal(err)
	}
	if !info.IsBad() {
		t.Fatal(`"Not found" should read as a missing tournament`)
	}
	if (Info{Name: "Кубок"}).IsBad() {
		t.Fatal("a real tournament should not read as missing")
	}
}

func infoEndingDaysAgo(days int, hideQuestions bool) Info {
	end := dates.Now().Add(-dates.Days(days))
	info := Info{ID: 9002, Name: "Кубок", DateEnd: end.Format(time.RFC3339)}
	if hideQuestions {
		info.HideQuestionsTo = end.Add(-dates.Days(1)).Format(time.RFC3339)
	}
	return info
}

func TestRemindersFollowTheDeadlineCalendar(t *testing.T) {
	client := serve(t, nil)
	cases := []struct {
		name     string
		days     int
		wantKind string
		wantText string
	}{
		{"the day after it ends, both juries are told the deadlines", 1, "i", "Крайний срок рассмотрения спорных"},
		{"five days later, controversials are due tomorrow", 5, "i", "Спорные на турнире"},
		{"fifteen days later, appeals are due tomorrow", 15, "a", "Апелляции на турнире"},
		{"in between, nothing is said", 7, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reminders, err := client.RemindersFor(infoEndingDaysAgo(tc.days, false), dates.Now())
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantKind == "" {
				if len(reminders) != 0 {
					t.Fatalf("expected no reminders, got %v", reminders)
				}
				return
			}
			if !strings.Contains(reminders[tc.wantKind], tc.wantText) {
				t.Fatalf("reminder %q is %q", tc.wantKind, reminders[tc.wantKind])
			}
		})
	}
}

func TestRemindersCountWhatIsLeftOnceQuestionsArePublic(t *testing.T) {
	client := serve(t, map[string]string{
		"/tournaments/9002/results.json": `[{"questionsTotal":30,"controversials":[
			{"status":"N"},{"status":"A"},{"status":"N"}]}]`,
	})
	reminders, err := client.RemindersFor(infoEndingDaysAgo(5, true), dates.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reminders["i"], "2 нерассмотренных спорных") {
		t.Fatalf("expected an exact count, got %q", reminders["i"])
	}
}

func TestNothingIsSaidWhenEverythingIsRuledOn(t *testing.T) {
	client := serve(t, map[string]string{
		"/tournaments/9002/results.json": `[{"questionsTotal":30,"controversials":[{"status":"A"}]}]`,
	})
	reminders, err := client.RemindersFor(infoEndingDaysAgo(5, true), dates.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(reminders) != 0 {
		t.Fatalf("expected silence, got %v", reminders)
	}
}
