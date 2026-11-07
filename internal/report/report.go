// Package report renders a printable PDF listing, one page per active
// company, the holidays saved for that company's FreeSWITCH variables in one
// year, followed by a summary page (overview, problems, inactive companies).
// It is meant for a human to check that every company's holidays are set
// correctly.
//
// The package is pure (no database access): the caller supplies Data.
package report

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"time"
)

//go:embed fonts/Sarabun-Regular.ttf
var fontRegular []byte

//go:embed fonts/Sarabun-Bold.ttf
var fontBold []byte

// Day is one holiday date.
type Day struct {
	Date  string // YYYY-MM-DD
	Label string
}

// Assignment links a company (optionally a branch) to a FreeSWITCH variable.
type Assignment struct {
	Branch   string // empty = company-wide
	Variable string
	Active   bool // assignment, company and branch all active
}

// Company is one registered company with its assignments.
type Company struct {
	Name        string
	Active      bool
	Assignments []Assignment
}

// Data is everything the report shows.
type Data struct {
	Year      int
	Generated time.Time
	Companies []Company
	// Calendars holds the selected holidays per variable; a missing key
	// means no calendar is saved for the year.
	Calendars map[string][]Day
	// Filtered is true when Companies is a user-chosen subset of the
	// TotalCompanies registered companies.
	Filtered       bool
	TotalCompanies int
}

// Options control rendering.
type Options struct {
	// FontRegular / FontBold override the embedded Sarabun (TrueType, Thai + Latin).
	// Use a Thai-capable TTF when labels or names contain Thai text.
	FontRegular, FontBold []byte
}

// Filename is the suggested download name.
func Filename(year int) string { return fmt.Sprintf("holiday_calendar_report_%d.pdf", year) }

// group is a set of a company's variables that have identical holidays.
type group struct {
	Variables []string // "variable (Branch)" descriptions
	HasCal    bool
	Days      []Day
}

// groups merges a company's variables whose saved holidays are identical, so
// a company with many branches sharing one holiday list prints it once.
func groups(d Data, c Company) []group {
	var out []group
	idx := map[string]int{}
	seen := map[string]int{} // variable -> index in out
	for _, a := range c.Assignments {
		desc := a.Variable
		if a.Branch != "" {
			desc += " (" + a.Branch + ")"
		}
		if !a.Active {
			desc += " [inactive]"
		}
		if i, ok := seen[a.Variable]; ok {
			out[i].Variables = append(out[i].Variables, desc)
			continue
		}
		days, has := d.Calendars[a.Variable]
		key := "none"
		if has {
			var b strings.Builder
			b.WriteString("cal:")
			for _, x := range days {
				b.WriteString(x.Date + "\x00" + x.Label + "\x01")
			}
			key = b.String()
		}
		i, ok := idx[key]
		if !ok {
			i = len(out)
			idx[key] = i
			out = append(out, group{HasCal: has, Days: days})
		}
		seen[a.Variable] = i
		out[i].Variables = append(out[i].Variables, desc)
	}
	return out
}

// ---------- date helpers ----------

func parseDate(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

func isWeekend(s string) bool {
	t, ok := parseDate(s)
	return ok && (t.Weekday() == time.Saturday || t.Weekday() == time.Sunday)
}

// Render produces the PDF: one page per active company, then the summary.
func Render(d Data, opt Options) ([]byte, error) {
	if d.Year < 1 || d.Year > 9999 {
		return nil, fmt.Errorf("invalid year %d", d.Year)
	}
	if d.Calendars == nil {
		d.Calendars = map[string][]Day{}
	}
	if d.Generated.IsZero() {
		d.Generated = time.Now()
	}
	r := newRenderer(d, opt)
	if err := r.build(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := r.pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
