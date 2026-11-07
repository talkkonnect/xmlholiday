package holidays

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DefaultICSURL is Google's public "Holidays in Thailand" calendar. It is NOT
// an official government source; results are marked unverified.
const DefaultICSURL = "https://calendar.google.com/calendar/ical/en.th%23holiday%40group.v.calendar.google.com/public/basic.ics"

// ICSProvider reads an iCalendar feed. Only events whose DESCRIPTION marks
// them as "Public holiday" are returned unless IncludeObservances is set.
type ICSProvider struct {
	URL                string
	SourceName         string
	Status             string // default unverified
	IncludeObservances bool
	Client             *http.Client
	Now                func() time.Time
}

func (p *ICSProvider) Name() string {
	if p.SourceName != "" {
		return p.SourceName
	}
	return "Google Calendar – Holidays in Thailand (unofficial)"
}

func (p *ICSProvider) Fetch(ctx context.Context, year int) ([]Suggestion, error) {
	u := p.URL
	if u == "" {
		u = DefaultICSURL
	}
	body, err := httpGet(ctx, p.Client, u)
	if err != nil {
		return nil, err
	}
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	st := p.Status
	if st == "" {
		st = StatusUnverified
	}
	s, err := ParseICS(body, year, p.Name(), u, st, p.IncludeObservances, now().UTC())
	if err != nil {
		return nil, err
	}
	if len(s) == 0 {
		return nil, fmt.Errorf("%s lists no public holidays for %d", p.Name(), year)
	}
	return s, nil
}

// ParseICS extracts all-day events for year from an iCalendar document.
func ParseICS(body []byte, year int, sourceName, sourceURL, status string, includeObservances bool, retrieved time.Time) ([]Suggestion, error) {
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n ", "") // unfold
	text = strings.ReplaceAll(text, "\n\t", "")
	if !strings.Contains(text, "BEGIN:VCALENDAR") {
		return nil, fmt.Errorf("response from %s is not an iCalendar feed: %q", sourceURL, snippet(body))
	}
	var out []Suggestion
	for _, block := range strings.Split(text, "BEGIN:VEVENT")[1:] {
		var start, end, summary, desc string
		for _, line := range strings.Split(block, "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			name := strings.SplitN(k, ";", 2)[0]
			switch name {
			case "DTSTART":
				start = v
			case "DTEND":
				end = v
			case "SUMMARY":
				summary = icsUnescape(v)
			case "DESCRIPTION":
				desc = icsUnescape(v)
			}
		}
		if !includeObservances && !strings.Contains(strings.ToLower(desc), "public holiday") {
			continue
		}
		s, err := time.Parse("20060102", strings.TrimSpace(start))
		if err != nil {
			continue // not an all-day event
		}
		e := s.AddDate(0, 0, 1)
		if t, err := time.Parse("20060102", strings.TrimSpace(end)); err == nil && t.After(s) {
			e = t
		}
		for d := s; d.Before(e); d = d.AddDate(0, 0, 1) {
			if d.Year() != year {
				continue
			}
			label := cleanLabel(summary)
			out = append(out, Suggestion{
				Date: d.Format("2006-01-02"), Label: label, Category: Categorize(label),
				SourceName: sourceName, SourceURL: sourceURL, RetrievedAt: retrieved, VerificationStatus: status,
			})
		}
	}
	return Merge(out), nil
}

func icsUnescape(s string) string {
	r := strings.NewReplacer(`\n`, " ", `\N`, " ", `\,`, ",", `\;`, ";", `\\`, `\`)
	return strings.TrimSpace(r.Replace(s))
}
