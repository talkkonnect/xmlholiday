// Package holidays retrieves suggested Thai holidays through swappable
// provider adapters. Providers never invent dates: they only return what the
// configured source publishes, tagged with source, URL, retrieval time and a
// verification status. Failures are returned as errors, never papered over.
package holidays

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Verification statuses.
const (
	StatusOfficial   = "official"
	StatusEstimated  = "estimated"
	StatusUnverified = "unverified"
)

// Suggestion is a single suggested holiday from a source.
type Suggestion struct {
	Date               string    `json:"date"` // YYYY-MM-DD
	Label              string    `json:"label"`
	Category           string    `json:"category"` // fixed|buddhist-lunar|substitute|special|public
	SourceName         string    `json:"source_name"`
	SourceURL          string    `json:"source_url"`
	RetrievedAt        time.Time `json:"retrieved_at"`
	VerificationStatus string    `json:"verification_status"`
	AlsoListedBy       []string  `json:"also_listed_by,omitempty"`
}

// Provider is a holiday source adapter.
type Provider interface {
	Name() string
	Fetch(ctx context.Context, year int) ([]Suggestion, error)
}

// ProviderError reports a failed provider.
type ProviderError struct {
	Provider string `json:"provider"`
	Error    string `json:"error"`
}

// FetchAll queries all providers, merging results per date. A date published
// by an official source takes that source's label and status; unofficial
// sources that list the same date are recorded in AlsoListedBy. Provider
// failures are returned alongside whatever succeeded.
func FetchAll(ctx context.Context, year int, providers []Provider) ([]Suggestion, []ProviderError) {
	var all []Suggestion
	var errs []ProviderError
	for _, p := range providers {
		s, err := p.Fetch(ctx, year)
		if err != nil {
			errs = append(errs, ProviderError{Provider: p.Name(), Error: err.Error()})
			continue
		}
		all = append(all, s...)
	}
	return Merge(all), errs
}

var statusRank = map[string]int{StatusOfficial: 0, StatusEstimated: 1, StatusUnverified: 2}

// Merge collapses suggestions to one per date, preferring the most
// authoritative status (stable on input order for ties).
func Merge(in []Suggestion) []Suggestion {
	byDate := map[string]*Suggestion{}
	var order []string
	for _, s := range in {
		cur, ok := byDate[s.Date]
		if !ok {
			c := s
			byDate[s.Date] = &c
			order = append(order, s.Date)
			continue
		}
		if statusRank[s.VerificationStatus] < statusRank[cur.VerificationStatus] {
			also := append(cur.AlsoListedBy, cur.SourceName)
			c := s
			c.AlsoListedBy = append(also, s.AlsoListedBy...)
			byDate[s.Date] = &c
		} else if s.SourceName != cur.SourceName {
			cur.AlsoListedBy = append(cur.AlsoListedBy, s.SourceName)
		} else if !strings.Contains(cur.Label, s.Label) {
			cur.Label += " / " + s.Label
		}
	}
	out := make([]Suggestion, 0, len(order))
	for _, d := range order {
		out = append(out, *byDate[d])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// Categorize derives a coarse category from a holiday label.
func Categorize(label string) string {
	l := strings.ToLower(label)
	switch {
	case strings.Contains(l, "substitut") || strings.Contains(l, "day off") || strings.Contains(l, "observed") || strings.Contains(l, "ชดเชย"):
		return "substitute"
	case strings.Contains(l, "special") || strings.Contains(l, "พิเศษ"):
		return "special"
	case strings.Contains(l, "bucha") || strings.Contains(l, "phansa") || strings.Contains(l, "บูชา") || strings.Contains(l, "พรรษา"):
		return "buddhist-lunar"
	default:
		return "fixed"
	}
}

func cleanLabel(s string) string { return strings.Join(strings.Fields(s), " ") }

func httpGet(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "xmlholiday/1.0 (holiday calendar tool)")
	req.Header.Set("Accept", "application/json, text/calendar, */*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP %d", url, resp.StatusCode)
	}
	return body, nil
}

func snippet(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}
