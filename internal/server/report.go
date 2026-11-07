package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/talkkonnect/xmlholiday/internal/report"
	"github.com/talkkonnect/xmlholiday/internal/store"
)

// ReportData collects every company, its assignments and the saved holidays
// of each variable for year (SAVED data only).
func (s *Server) ReportData(ctx context.Context, year int) (report.Data, error) {
	d := report.Data{Year: year, Generated: time.Now(), Calendars: map[string][]report.Day{}}
	reg, err := s.Store.Registry(ctx)
	if err != nil {
		return d, err
	}
	infos, err := s.Store.Assignments(ctx, "")
	if err != nil {
		return d, err
	}
	byCompany := map[int64][]store.AssignmentInfo{}
	for _, a := range infos {
		byCompany[a.CompanyID] = append(byCompany[a.CompanyID], a)
	}
	for _, c := range reg.Companies {
		rc := report.Company{Name: c.Name, Active: c.Active}
		for _, a := range byCompany[c.ID] {
			rc.Assignments = append(rc.Assignments, report.Assignment{Branch: a.BranchName, Variable: a.VariableName, Active: a.Active})
			if _, done := d.Calendars[a.VariableName]; done {
				continue
			}
			cal, err := s.Store.GetCalendar(ctx, year, a.VariableName)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				return d, err
			}
			days := []report.Day{}
			for _, x := range cal.Dates {
				if x.Selected {
					days = append(days, report.Day{Date: x.Date, Label: x.Label})
				}
			}
			d.Calendars[a.VariableName] = days
		}
		d.Companies = append(d.Companies, rc)
	}
	return d, nil
}

// ReportOptions returns rendering options; XMLH_REPORT_FONT (and optionally
// XMLH_REPORT_FONT_BOLD) point to TrueType fonts that replace the embedded
// DejaVu Sans, e.g. a Thai-capable font when names or labels contain Thai.
func ReportOptions() (report.Options, error) {
	opt := report.Options{}
	if p := os.Getenv("XMLH_REPORT_FONT"); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			return opt, fmt.Errorf("XMLH_REPORT_FONT: %w", err)
		}
		opt.FontRegular = b
	}
	if p := os.Getenv("XMLH_REPORT_FONT_BOLD"); p != "" && opt.FontRegular != nil {
		b, err := os.ReadFile(p)
		if err != nil {
			return opt, fmt.Errorf("XMLH_REPORT_FONT_BOLD: %w", err)
		}
		opt.FontBold = b
	}
	return opt, nil
}

// BuildReport renders the PDF calendar report for year.
func (s *Server) BuildReport(ctx context.Context, year int) ([]byte, error) {
	d, err := s.ReportData(ctx, year)
	if err != nil {
		return nil, err
	}
	opt, err := ReportOptions()
	if err != nil {
		return nil, err
	}
	return report.Render(d, opt)
}

// GET /api/report/pdf?year=2027[&download=1]
func (s *Server) reportPDF(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	year, err := parseYear(q.Get("year"))
	if err != nil {
		writeErr(w, err)
		return
	}
	pdf, err := s.BuildReport(r.Context(), year)
	if err != nil {
		writeErr(w, err)
		return
	}
	disp := "inline"
	if q.Get("download") == "1" {
		disp = "attachment"
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disp, report.Filename(year)))
	w.Header().Set("Cache-Control", "no-store")
	w.Write(pdf)
}
