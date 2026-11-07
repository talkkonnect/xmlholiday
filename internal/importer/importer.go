// Package importer reads existing FreeSWITCH holiday "tod" XML files (such as
// 01-tod-group.xml / 01-tod-mespace.xml) and extracts variable -> date sets.
// Only <condition> elements with numeric mday/mon/year attributes and
// <action application="set" data="VAR=false"> children are imported.
package importer

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Date is one imported date with an optional label taken from the XML comment
// immediately preceding the condition.
type Date struct {
	Date  string
	Label string
}

// Result maps exact variable names to their dates.
type Result struct {
	Variables map[string][]Date
	Breaks    map[string]int // break attribute values seen -> count
	Skipped   []string       // human readable notes about ignored elements
}

// Parse parses a FreeSWITCH include file.
func Parse(data []byte) (*Result, error) {
	res := &Result{Variables: map[string][]Date{}, Breaks: map[string]int{}}
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = true
	var lastComment string
	var cur []Date // dates of the condition being read
	inCond := false
	seen := map[string]map[string]bool{}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("XML parse error: %w", err)
		}
		switch t := tok.(type) {
		case xml.Comment:
			lastComment = strings.Join(strings.Fields(string(t)), " ")
		case xml.StartElement:
			switch t.Name.Local {
			case "condition":
				a := attrs(t)
				if a["mday"] == "" || a["mon"] == "" || a["year"] == "" {
					res.Skipped = append(res.Skipped, fmt.Sprintf("condition without mday/mon/year: %v", a))
					inCond = false
					lastComment = ""
					continue
				}
				dates, err := expand(a["mday"], a["mon"], a["year"])
				if err != nil {
					return nil, err
				}
				res.Breaks[a["break"]]++
				cur, inCond = dates, true
				for i := range cur {
					cur[i].Label = lastComment
				}
				lastComment = ""
			case "action":
				if !inCond {
					continue
				}
				a := attrs(t)
				v, val, ok := strings.Cut(a["data"], "=")
				if a["application"] != "set" || !ok || strings.TrimSpace(val) != "false" || v == "" {
					res.Skipped = append(res.Skipped, fmt.Sprintf("action ignored: application=%q data=%q", a["application"], a["data"]))
					continue
				}
				if seen[v] == nil {
					seen[v] = map[string]bool{}
				}
				for _, dt := range cur {
					if !seen[v][dt.Date] {
						seen[v][dt.Date] = true
						res.Variables[v] = append(res.Variables[v], dt)
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "condition" {
				inCond = false
			}
		}
	}
	for v := range res.Variables {
		sort.Slice(res.Variables[v], func(i, j int) bool { return res.Variables[v][i].Date < res.Variables[v][j].Date })
	}
	return res, nil
}

func attrs(t xml.StartElement) map[string]string {
	m := map[string]string{}
	for _, a := range t.Attr {
		m[a.Name.Local] = a.Value
	}
	return m
}

// expand handles FreeSWITCH numeric lists/ranges such as "13-15" or "1,3".
func expand(mday, mon, year string) ([]Date, error) {
	days, err := numList(mday, 1, 31)
	if err != nil {
		return nil, fmt.Errorf("mday %q: %w", mday, err)
	}
	mons, err := numList(mon, 1, 12)
	if err != nil {
		return nil, fmt.Errorf("mon %q: %w", mon, err)
	}
	years, err := numList(year, 1900, 9999)
	if err != nil {
		return nil, fmt.Errorf("year %q: %w", year, err)
	}
	var out []Date
	for _, y := range years {
		for _, m := range mons {
			for _, dd := range days {
				t := time.Date(y, time.Month(m), dd, 0, 0, 0, 0, time.UTC)
				if t.Day() != dd {
					return nil, fmt.Errorf("invalid date %04d-%02d-%02d", y, m, dd)
				}
				out = append(out, Date{Date: t.Format("2006-01-02")})
			}
		}
	}
	return out, nil
}

func numList(s string, lo, hi int) ([]int, error) {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		a, b, isRange := strings.Cut(part, "-")
		x, err := strconv.Atoi(strings.TrimSpace(a))
		if err != nil {
			return nil, fmt.Errorf("not numeric")
		}
		y := x
		if isRange {
			if y, err = strconv.Atoi(strings.TrimSpace(b)); err != nil {
				return nil, fmt.Errorf("not numeric")
			}
		}
		if x < lo || y > hi || y < x {
			return nil, fmt.Errorf("out of range")
		}
		for i := x; i <= y; i++ {
			out = append(out, i)
		}
	}
	return out, nil
}
