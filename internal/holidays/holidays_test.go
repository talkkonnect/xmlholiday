package holidays

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

var fixed = time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)

func TestParseBOTFixture(t *testing.T) {
	b, err := os.ReadFile("testdata/bot_2027.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseBOT(b, 2027, "https://example/bot", fixed)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 18 {
		t.Fatalf("want 18 BOT 2027 holidays, got %d", len(s))
	}
	if s[0].Date != "2027-01-01" || s[0].VerificationStatus != StatusOfficial || s[0].SourceURL != "https://example/bot" || !s[0].RetrievedAt.Equal(fixed) {
		t.Fatalf("bad first %+v", s[0])
	}
	var sub, lunar bool
	for _, x := range s {
		if x.Date == "2027-02-22" && x.Category == "substitute" {
			sub = true
		}
		if x.Date == "2027-05-20" && x.Category == "buddhist-lunar" {
			lunar = true
		}
		if strings.Contains(x.Label, "\n") {
			t.Fatalf("label not cleaned: %q", x.Label)
		}
	}
	if !sub || !lunar {
		t.Fatal("categories not detected")
	}
}

func TestParseBOTFailures(t *testing.T) {
	if _, err := ParseBOT([]byte("<html>The website is under maintenance.</html>"), 2027, "u", fixed); err == nil {
		t.Fatal("HTML accepted")
	}
	if _, err := ParseBOT([]byte(`{"calendarDisclaimer":"x"}`), 2028, "u", fixed); err == nil || !strings.Contains(err.Error(), "not published") {
		t.Fatalf("empty list must fail clearly, got %v", err)
	}
	bad := `{"holidayCalendarLists":[{"holidayDescription":"x","date":"Monday 30","month":"February","year":"2027"}]}`
	if _, err := ParseBOT([]byte(bad), 2027, "u", fixed); err == nil {
		t.Fatal("invalid date accepted")
	}
}

func TestParseICSFixture(t *testing.T) {
	b, err := os.ReadFile("testdata/th_excerpt.ics")
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseICS(b, 2027, "G", "u", StatusUnverified, false, fixed)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, x := range s {
		got = append(got, x.Date)
		if x.VerificationStatus != StatusUnverified {
			t.Fatal("ICS must be unverified")
		}
	}
	// Observances (Chinese New Year) are excluded; 2026-12-31 is another year.
	want := "2027-01-01,2027-02-21,2027-02-22"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v want %s", got, want)
	}
}

func TestFetchAllMergeAndFailure(t *testing.T) {
	bot, _ := os.ReadFile("testdata/bot_2027.json")
	ics, _ := os.ReadFile("testdata/th_excerpt.ics")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "bot.2027"):
			w.Write(bot)
		case strings.Contains(r.URL.Path, "ics"):
			w.Write(ics)
		default:
			http.Error(w, "down", 503)
		}
	}))
	defer srv.Close()
	ps := []Provider{
		&BOTProvider{URLTemplate: srv.URL + "/bot.{year}.json", Now: func() time.Time { return fixed }},
		&ICSProvider{URL: srv.URL + "/th.ics", Now: func() time.Time { return fixed }},
		&BOTProvider{URLTemplate: srv.URL + "/broken.{year}"},
	}
	s, errs := FetchAll(context.Background(), 2027, ps)
	if len(errs) != 1 || !strings.Contains(errs[0].Error, "503") {
		t.Fatalf("want one clear failure, got %+v", errs)
	}
	byDate := map[string]Suggestion{}
	for _, x := range s {
		byDate[x.Date] = x
	}
	if x := byDate["2027-01-01"]; x.VerificationStatus != StatusOfficial || len(x.AlsoListedBy) != 1 {
		t.Fatalf("official should win and record the other source: %+v", x)
	}
	if x := byDate["2027-02-21"]; x.VerificationStatus != StatusUnverified {
		t.Fatalf("ICS-only date must stay unverified: %+v", x)
	}
	// Whole failure: nothing fabricated.
	s, errs = FetchAll(context.Background(), 2027, ps[2:])
	if len(s) != 0 || len(errs) != 1 {
		t.Fatal("failure must yield no suggestions")
	}
}
