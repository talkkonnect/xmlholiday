package holidays

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultBOTURL is the Bank of Thailand "Financial Institutions Holiday"
// calendar data endpoint. {year} is replaced by the Gregorian year.
const DefaultBOTURL = "https://www.bot.or.th/content/bot/en/financial-institutions-holiday/jcr:content/root/container/holidaycalendar.model.{year}.json"

// BOTProvider reads the official Bank of Thailand financial-institution
// holiday list (includes substitute and additional special holidays; it lists
// weekday bank holidays only, so weekend-falling holidays appear via their
// substitution day).
type BOTProvider struct {
	URLTemplate string
	Client      *http.Client
	Now         func() time.Time
}

func (p *BOTProvider) Name() string { return "Bank of Thailand – Financial Institutions Holidays" }

type botResponse struct {
	List []struct {
		Description string `json:"holidayDescription"`
		Date        string `json:"date"`  // e.g. "Friday 1"
		Month       string `json:"month"` // e.g. "January"
		Year        string `json:"year"`
	} `json:"holidayCalendarLists"`
	Disclaimer string `json:"calendarDisclaimer"`
}

func (p *BOTProvider) url(year int) string {
	t := p.URLTemplate
	if t == "" {
		t = DefaultBOTURL
	}
	return strings.ReplaceAll(t, "{year}", strconv.Itoa(year))
}

func (p *BOTProvider) Fetch(ctx context.Context, year int) ([]Suggestion, error) {
	u := p.url(year)
	body, err := httpGet(ctx, p.Client, u)
	if err != nil {
		return nil, err
	}
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	return ParseBOT(body, year, u, now().UTC())
}

// ParseBOT parses a BOT holidaycalendar.model JSON document.
func ParseBOT(body []byte, year int, sourceURL string, retrieved time.Time) ([]Suggestion, error) {
	var r botResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("Bank of Thailand response is not the expected JSON (site may be under maintenance): %q", snippet(body))
	}
	if len(r.List) == 0 {
		return nil, fmt.Errorf("Bank of Thailand has not published holidays for %d (empty list)", year)
	}
	var out []Suggestion
	for _, h := range r.List {
		f := strings.Fields(h.Date)
		if len(f) == 0 {
			return nil, fmt.Errorf("unparseable BOT date %q", h.Date)
		}
		day, err := strconv.Atoi(f[len(f)-1])
		if err != nil {
			return nil, fmt.Errorf("unparseable BOT date %q", h.Date)
		}
		mt, err := time.Parse("January", strings.TrimSpace(h.Month))
		if err != nil {
			return nil, fmt.Errorf("unparseable BOT month %q", h.Month)
		}
		y, err := strconv.Atoi(strings.TrimSpace(h.Year))
		if err != nil || y != year {
			return nil, fmt.Errorf("BOT entry year %q does not match requested %d", h.Year, year)
		}
		t := time.Date(y, mt.Month(), day, 0, 0, 0, 0, time.UTC)
		if t.Day() != day || t.Month() != mt.Month() {
			return nil, fmt.Errorf("BOT entry has invalid date %s %s %s", h.Date, h.Month, h.Year)
		}
		label := cleanLabel(h.Description)
		out = append(out, Suggestion{
			Date:               t.Format("2006-01-02"),
			Label:              label,
			Category:           Categorize(label),
			SourceName:         "Bank of Thailand – Financial Institutions Holidays",
			SourceURL:          sourceURL,
			RetrievedAt:        retrieved,
			VerificationStatus: StatusOfficial,
		})
	}
	return Merge(out), nil
}
