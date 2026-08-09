package text

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestApplicationFormPluralises(t *testing.T) {
	cases := map[int]string{
		0:   "нерассмотренных заявок",
		1:   "нерассмотренная заявка",
		2:   "нерассмотренные заявки",
		4:   "нерассмотренные заявки",
		5:   "нерассмотренных заявок",
		11:  "нерассмотренных заявок",
		12:  "нерассмотренных заявок",
		21:  "нерассмотренная заявка",
		22:  "нерассмотренные заявки",
		111: "нерассмотренных заявок",
	}
	for count, want := range cases {
		if got := ApplicationForm(count); got != want {
			t.Errorf("%d: got %q, want %q", count, got, want)
		}
	}
}

func TestListOfIntsReadsWhateverWasTyped(t *testing.T) {
	cases := []struct {
		input string
		want  []int
	}{
		{"7000, 9002, 9015", []int{7000, 9002, 9015}},
		{"7000 9002", []int{7000, 9002}},
		{" 9002 ", []int{9002}},
		{"не число", nil},
		{"", nil},
		{"9002, не число", []int{9002}},
	}
	for _, tc := range cases {
		got := ListOfInts(tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("%q: got %v, want %v", tc.input, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%q: got %v, want %v", tc.input, got, tc.want)
				break
			}
		}
	}
}

func TestStripHostAndValidate(t *testing.T) {
	cases := map[string]string{
		"https://rating.chgk.info/":   "rating.chgk.info",
		"http://www.rating.chgk.info": "rating.chgk.info",
		"  rating.pecheny.me  ":       "rating.pecheny.me",
	}
	for input, want := range cases {
		got := StripHost(input)
		if got != want {
			t.Errorf("%q: got %q, want %q", input, got, want)
		}
		if !ValidHost(got) {
			t.Errorf("%q should be a valid host", got)
		}
	}
	if ValidHost("nodots") {
		t.Error("a host with no dot should be refused")
	}
}

func TestApplicationsAreSortedByGivenName(t *testing.T) {
	reps := []string{
		"3 Пётр Яковлев (Тверь)",
		"1 Иван Иванов (Москва)",
		"2 Анна Иванова (Москва)",
	}
	got := FormatApplications(reps, "rating.chgk.info")
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected three lines, got %d", len(lines))
	}
	for i, want := range []string{"Анна", "Иван ", "Пётр"} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d is %q, expected %q", i, lines[i], want)
		}
	}
	if !strings.Contains(lines[0], `href="https://rating.chgk.info/player/2"`) {
		t.Errorf("expected a player link, got %q", lines[0])
	}
}

func TestShortMessageIsOneBatch(t *testing.T) {
	batches := Batches("<b>коротко</b>")
	if len(batches) != 1 || batches[0] != "<b>коротко</b>" {
		t.Fatalf("got %q", batches)
	}
}

func TestEmptyMessageProducesNothing(t *testing.T) {
	if got := Batches(""); len(got) != 0 {
		t.Fatalf("expected no batches, got %q", got)
	}
}

func TestEveryBatchFitsTheLimit(t *testing.T) {
	// One long list of links, the shape the applications job actually sends.
	var builder strings.Builder
	for range 300 {
		builder.WriteString(PlayerLink("123 Иван Иванов (Москва)", "rating.chgk.info"))
		builder.WriteString("\n")
	}
	batches := Batches(builder.String())
	if len(batches) < 2 {
		t.Fatalf("expected the message to be split, got %d batch", len(batches))
	}
	for i, batch := range batches {
		if got := utf8.RuneCountInString(batch); got > MaxLength {
			t.Errorf("batch %d is %d characters, over the %d limit", i, got, MaxLength)
		}
	}
}

func TestSplitReopensTagsLeftOpen(t *testing.T) {
	// A single bold run far longer than one message: every batch must be
	// balanced on its own, or Telegram rejects it.
	long := "<b>" + strings.Repeat("очень длинный анонс ", 400) + "</b>"
	batches := Batches(long)
	if len(batches) < 2 {
		t.Fatalf("expected a split, got %d batch", len(batches))
	}
	for i, batch := range batches {
		if strings.Count(batch, "<b>") != strings.Count(batch, "</b>") {
			t.Errorf("batch %d has unbalanced bold tags: %q…%q",
				i, batch[:20], batch[len(batch)-20:])
		}
	}
	if !strings.HasPrefix(batches[1], "<b>") {
		t.Errorf("the second batch should reopen the bold run, starts %q", batches[1][:20])
	}
}

func TestSplitPrefersAParagraphBreak(t *testing.T) {
	first := strings.Repeat("а", 1500)
	second := strings.Repeat("б", 1500)
	batches := Batches(first + "\n\n" + second)
	if len(batches) != 2 {
		t.Fatalf("expected two batches, got %d", len(batches))
	}
	if batches[0] != first+"\n\n" {
		t.Error("the first batch should run up to and include the paragraph break")
	}
	if batches[1] != second {
		t.Error("the second batch should start exactly after the paragraph break")
	}
}
