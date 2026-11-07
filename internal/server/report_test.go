package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/talkkonnect/xmlholiday/internal/store"
)

func TestReportPDF(t *testing.T) {
	e := setup(t)
	e.registerSample()
	e.must("POST", "/api/templates/2027", map[string]any{"dates": []store.CalendarDate{
		{Date: "2027-01-01", Label: "New Year’s Day", Selected: true},
		{Date: "2027-04-13", Label: "Songkran", Selected: true},
	}}, nil)
	// mespace-bangkok drops Songkran (manual exclusion) and adds a company day.
	c := e.getCal(2027, "mespace-bangkok-working-time")
	e.putCal(2027, "mespace-bangkok-working-time", c.Calendar.Version,
		store.CalendarDate{Date: "2027-01-01", Label: "New Year’s Day", Origin: "template", Selected: true},
		store.CalendarDate{Date: "2027-04-13", Label: "Songkran", Origin: "template", Selected: false},
		store.CalendarDate{Date: "2027-06-01", Label: "Company Day", Origin: "manual", Selected: true})

	d, err := e.s.ReportData(context.Background(), 2027)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Companies) != 3 {
		t.Fatalf("data = %+v", d)
	}
	days := d.Calendars["mespace-bangkok-working-time"]
	if len(days) != 2 || days[0].Date != "2027-01-01" || days[1].Date != "2027-06-01" { // excluded date not listed
		t.Fatalf("days = %+v", days)
	}
	for _, co := range d.Companies {
		if co.Name == "MeSpace" && (len(co.Assignments) != 2 || co.Assignments[0].Branch != "Bangkok") {
			t.Errorf("MeSpace assignments = %+v", co.Assignments)
		}
	}

	resp, err := http.Get(e.srv.URL + "/api/report/pdf?year=2027&download=1")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("status %d %s: %.200s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	if !strings.HasPrefix(string(body), "%PDF-") {
		t.Fatal("not a PDF")
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, `attachment; filename="holiday_calendar_report_2027.pdf"`) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if code := e.do("GET", "/api/report/pdf?year=abc", nil, nil); code != 400 {
		t.Errorf("bad year -> %d, want 400", code)
	}
}
