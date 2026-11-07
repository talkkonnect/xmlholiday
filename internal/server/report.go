package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/talkkonnect/xmlholiday/internal/report"
	"github.com/talkkonnect/xmlholiday/internal/store"
)

// ReportData collects the companies, their assignments and the saved holidays
// of each variable for year (SAVED data only). companyIDs limits the report to
// those companies; nil or empty means every company. An unknown id is a
// validation error.
func (s *Server) ReportData(ctx context.Context, year int, companyIDs []int64) (report.Data, error) {
	d := report.Data{Year: year, Generated: time.Now(), Calendars: map[string][]report.Day{}}
	reg, err := s.Store.Registry(ctx)
	if err != nil {
		return d, err
	}
	d.TotalCompanies = len(reg.Companies)
	var want map[int64]bool
	if len(companyIDs) > 0 {
		want = map[int64]bool{}
		known := map[int64]bool{}
		for _, c := range reg.Companies {
			known[c.ID] = true
		}
		for _, id := range companyIDs {
			if !known[id] {
				return d, &store.ValidationError{Msg: fmt.Sprintf("unknown company id %d", id)}
			}
			want[id] = true
		}
		d.Filtered = true
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
		if want != nil && !want[c.ID] {
			continue
		}
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

// CompanyIDsByName resolves company names (case-insensitive) to ids.
func (s *Server) CompanyIDsByName(ctx context.Context, names []string) ([]int64, error) {
	reg, err := s.Store.Registry(ctx)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		found := false
		for _, c := range reg.Companies {
			if strings.EqualFold(c.Name, n) {
				ids, found = append(ids, c.ID), true
				break
			}
		}
		if !found {
			return nil, &store.ValidationError{Msg: fmt.Sprintf("unknown company %q", n)}
		}
	}
	return ids, nil
}

// parseIDs parses a comma-separated list of positive ids ("" = none).
func parseIDs(v string) ([]int64, error) {
	var ids []int64
	for _, f := range strings.Split(v, ",") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		id, err := strconv.ParseInt(f, 10, 64)
		if err != nil || id <= 0 {
			return nil, &store.ValidationError{Msg: fmt.Sprintf("invalid company id %q", f)}
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// BuildReport renders the PDF calendar report for year, limited to
// companyIDs when non-empty (all companies otherwise).
func (s *Server) BuildReport(ctx context.Context, year int, companyIDs []int64) ([]byte, error) {
	d, err := s.ReportData(ctx, year, companyIDs)
	if err != nil {
		return nil, err
	}
	opt, err := ReportOptions()
	if err != nil {
		return nil, err
	}
	return report.Render(d, opt)
}

// GET /api/report/pdf?year=2027[&companies=1,5,7][&download=1]
// companies limits the report to those company ids; omitted = all companies.
func (s *Server) reportPDF(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	year, err := parseYear(q.Get("year"))
	if err != nil {
		writeErr(w, err)
		return
	}
	ids, err := parseIDs(q.Get("companies"))
	if err != nil {
		writeErr(w, err)
		return
	}
	pdf, err := s.BuildReport(r.Context(), year, ids)
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
