package report

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func sample() Data {
	days := []Day{
		{Date: "2027-01-01", Label: "New Year’s Day"},
		{Date: "2027-04-13", Label: "Songkran Festival"},
		{Date: "2027-05-01", Label: "Labour Day"},
	}
	return Data{
		Year:      2027,
		Generated: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC),
		Companies: []Company{
			{Name: "JWD", Active: true, Assignments: []Assignment{{Variable: "jwd-working-time", Active: true}}},
			{Name: "MeSpace", Active: true, Assignments: []Assignment{
				{Branch: "Patong", Variable: "mespace-patong-working-time", Active: true},
				{Branch: "Siam", Variable: "mespace-siam-working-time", Active: true},
				{Branch: "Ladprao", Variable: "mespace-ladprao-working-time", Active: true},
				{Branch: "Bangsue", Variable: "mespace-bangsue-working-time", Active: true},
			}},
			{Name: "Empty Co", Active: true},
			{Name: "Old Co", Active: false, Assignments: []Assignment{{Variable: "old-working-time"}}},
		},
		Calendars: map[string][]Day{
			"jwd-working-time":             days,
			"mespace-patong-working-time":  days,
			"mespace-siam-working-time":    days,
			"mespace-ladprao-working-time": days[:2],
			"old-working-time":             {},
		},
	}
}

func TestGroups(t *testing.T) {
	d := sample()
	gs := groups(d, d.Companies[1])
	if len(gs) != 3 {
		t.Fatalf("groups = %+v", gs)
	}
	if len(gs[0].Variables) != 2 || len(gs[0].Days) != 3 { // patong + siam identical
		t.Errorf("group 0 = %+v", gs[0])
	}
	if len(gs[1].Days) != 2 || gs[2].HasCal { // ladprao differs; bangsue has no calendar
		t.Errorf("groups = %+v", gs)
	}
}

func TestIssues(t *testing.T) {
	is := issues(sample())
	want := []string{"Empty Co", "MeSpace"} // MeSpace: bangsue has no calendar; Old Co is inactive
	if len(is) != 2 || is[0][0] != "MeSpace" || is[1][0] != "Empty Co" {
		t.Fatalf("issues = %v, want companies %v", is, want)
	}
}

func TestRender(t *testing.T) {
	pdf, err := Render(sample(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) || !bytes.Contains(pdf, []byte("%%EOF")) {
		t.Fatal("output is not a PDF")
	}
	// JWD + MeSpace + summary; Empty Co (no variable) and Old Co (inactive) only on the summary
	if n := bytes.Count(pdf, []byte("/Type /Page\n")); n != 3 {
		t.Errorf("%d pages, want 2 company pages + summary", n)
	}
	if out := os.Getenv("REPORT_TEST_OUT"); out != "" {
		os.WriteFile(out, pdf, 0o644)
	}
	if _, err := Render(Data{Year: 0}, Options{}); err == nil {
		t.Error("year 0 accepted")
	}
	if _, err := Render(Data{Year: 2027}, Options{}); err != nil {
		t.Errorf("empty data: %v", err)
	}
}

func TestThai(t *testing.T) {
	cases := map[string]string{
		"New Year’s Day": "วันขึ้นปีใหม่\nNew Year’s Day",
		"Substitution for Makha Bucha Day (Sunday 21st February 2027)": "ชดเชยวันมาฆบูชา\nSubstitution for Makha Bucha Day (Sunday 21st February 2027)",
		"Company Outing": "Company Outing",
		"วันหยุดบริษัท": "วันหยุดบริษัท",
	}
	for in, want := range cases {
		if got := bilingualLabel(in); got != want {
			t.Errorf("bilingualLabel(%q) = %q, want %q", in, got, want)
		}
	}
	if got := bilingualDate("2027-01-01"); got != "1 ม.ค. 2570\n01 Jan 2027" {
		t.Errorf("date = %q", got)
	}
	if got := bilingualWeekday("2027-01-01"); got != "วันศุกร์\nFriday" {
		t.Errorf("weekday = %q", got)
	}
}
