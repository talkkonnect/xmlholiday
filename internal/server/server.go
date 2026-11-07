// Package server exposes the JSON API and serves the embedded web UI. It never
// contacts, deploys to, or reloads FreeSWITCH.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/talkkonnect/xmlholiday/internal/holidays"
	"github.com/talkkonnect/xmlholiday/internal/store"
	"github.com/talkkonnect/xmlholiday/internal/xmlgen"
)

// Setting keys and defaults.
var DefaultSettings = map[string]string{
	"break":        xmlgen.DefaultBreak,
	"providers":    "bot,ics",
	"bot_url":      holidays.DefaultBOTURL,
	"ics_url":      holidays.DefaultICSURL,
	"default_year": "",
	// export_filename overrides the XML download filename; empty means
	// xmlgen.Filename(year). "{year}" is replaced with the export year.
	"export_filename": "",
}

var exportNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// normalizeExportFilename validates a user-supplied export filename template
// and appends ".xml" when missing. Empty is allowed (use the default).
func normalizeExportFilename(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if !strings.HasSuffix(strings.ToLower(v), ".xml") {
		v += ".xml"
	}
	if len(v) > 128 || !exportNameRe.MatchString(strings.ReplaceAll(v, "{year}", "2000")) || strings.HasPrefix(v, ".") {
		return "", fmt.Errorf("export filename may only contain letters, digits, '.', '_', '-' and the {year} placeholder (max 128 chars), e.g. 01-tod-holidays.xml")
	}
	return v, nil
}

// exportFilename resolves the download filename for year from settings.
func exportFilename(cfg map[string]string, year int) string {
	if t := cfg["export_filename"]; t != "" {
		return strings.ReplaceAll(t, "{year}", strconv.Itoa(year))
	}
	return xmlgen.Filename(year)
}

type Server struct {
	Store *store.Store
	// ProviderFactory builds providers from settings; overridable in tests.
	ProviderFactory func(settings map[string]string) []holidays.Provider
	Static          fs.FS
	HTTPClient      *http.Client
}

func New(st *store.Store, static fs.FS) *Server {
	s := &Server{Store: st, Static: static, HTTPClient: &http.Client{Timeout: 20 * time.Second}}
	s.ProviderFactory = s.defaultProviders
	return s
}

func (s *Server) defaultProviders(cfg map[string]string) []holidays.Provider {
	var ps []holidays.Provider
	for _, name := range strings.Split(cfg["providers"], ",") {
		switch strings.TrimSpace(name) {
		case "bot":
			ps = append(ps, &holidays.BOTProvider{URLTemplate: cfg["bot_url"], Client: s.HTTPClient})
		case "ics":
			ps = append(ps, &holidays.ICSProvider{URL: cfg["ics_url"], Client: s.HTTPClient})
		}
	}
	return ps
}

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/registry", s.getRegistry)
	m.HandleFunc("POST /api/registry", s.postRegistry)
	m.HandleFunc("GET /api/companies", s.listPart("companies"))
	m.HandleFunc("POST /api/companies", s.postOne("companies"))
	m.HandleFunc("GET /api/branches", s.listPart("branches"))
	m.HandleFunc("POST /api/branches", s.postOne("branches"))
	m.HandleFunc("GET /api/assignments", s.getAssignments)
	m.HandleFunc("POST /api/assignments", s.postOne("assignments"))
	m.HandleFunc("GET /api/variables", s.getVariables)
	m.HandleFunc("GET /api/calendars", s.getCalendarIndex)
	m.HandleFunc("GET /api/calendars/{year}/{variable}", s.getCalendar)
	m.HandleFunc("PUT /api/calendars/{year}/{variable}", s.putCalendar)
	m.HandleFunc("GET /api/holiday-suggestions", s.getSuggestions)
	m.HandleFunc("GET /api/templates/{year}", s.getTemplate)
	m.HandleFunc("POST /api/templates/{year}", s.postTemplate)
	m.HandleFunc("POST /api/bulk-apply", s.postBulkApply)
	m.HandleFunc("GET /api/xml/preview", s.xmlPreview)
	m.HandleFunc("GET /api/xml/download", s.xmlDownload)
	m.HandleFunc("GET /api/report/pdf", s.reportPDF)
	m.HandleFunc("GET /api/documents", s.getDocuments)
	m.HandleFunc("POST /api/companies/{id}/documents", s.postDocument)
	m.HandleFunc("GET /api/documents/{id}", s.getDocument)
	m.HandleFunc("DELETE /api/documents/{id}", s.deleteDocument)
	m.HandleFunc("GET /api/settings", s.getSettings)
	m.HandleFunc("PUT /api/settings", s.putSettings)
	if s.Static != nil {
		m.Handle("GET /", http.FileServerFS(s.Static))
	}
	return logRequests(m)
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet {
			log.Printf("%s %s", r.Method, r.URL.Path)
		}
		h.ServeHTTP(w, r)
	})
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	var ve *store.ValidationError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": ve.Msg})
	case errors.Is(err, store.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, store.ErrNotRegistered):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	default:
		log.Printf("internal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

func badRequest(w http.ResponseWriter, f string, a ...any) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf(f, a...)})
}

func decode(r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return &store.ValidationError{Msg: "invalid JSON body: " + err.Error()}
	}
	return nil
}

func parseYear(s string) (int, error) {
	y, err := strconv.Atoi(s)
	if err != nil || y < 1900 || y > 9999 {
		return 0, &store.ValidationError{Msg: fmt.Sprintf("invalid year %q", s)}
	}
	return y, nil
}

// ---------- registry ----------

func (s *Server) getRegistry(w http.ResponseWriter, r *http.Request) {
	reg, err := s.Store.Registry(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, reg)
}

func (s *Server) postRegistry(w http.ResponseWriter, r *http.Request) {
	var in store.Registry
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	ids, err := s.Store.SaveRegistry(r.Context(), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	reg, err := s.Store.Registry(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	m := map[string]int64{}
	for k, v := range ids {
		m[strconv.FormatInt(k, 10)] = v
	}
	writeJSON(w, 200, map[string]any{"registry": reg, "id_map": m})
}

func (s *Server) listPart(part string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reg, err := s.Store.Registry(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		switch part {
		case "companies":
			writeJSON(w, 200, reg.Companies)
		case "branches":
			writeJSON(w, 200, reg.Branches)
		}
	}
}

// postOne upserts a single company/branch/assignment (id<=0 creates).
func (s *Server) postOne(part string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in store.Registry
		var err error
		switch part {
		case "companies":
			var c store.Company
			err = decode(r, &c)
			in.Companies = []store.Company{c}
		case "branches":
			var b store.Branch
			err = decode(r, &b)
			in.Branches = []store.Branch{b}
		case "assignments":
			var a store.Assignment
			err = decode(r, &a)
			in.Assignments = []store.Assignment{a}
		}
		if err != nil {
			writeErr(w, err)
			return
		}
		ids, err := s.Store.SaveRegistry(r.Context(), in)
		if err != nil {
			writeErr(w, err)
			return
		}
		var id int64
		for _, v := range ids {
			id = v
		}
		writeJSON(w, 200, map[string]any{"ok": true, "created_id": id})
	}
}

func (s *Server) getAssignments(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.Assignments(r.Context(), r.URL.Query().Get("variable"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, a)
}

type variableInfo struct {
	VariableName string                 `json:"variable_name"`
	Active       bool                   `json:"active"`
	Shared       bool                   `json:"shared"`
	Assignments  []store.AssignmentInfo `json:"assignments"`
}

func (s *Server) variableInfos(ctx context.Context) ([]variableInfo, error) {
	all, err := s.Store.Assignments(ctx, "")
	if err != nil {
		return nil, err
	}
	by := map[string]*variableInfo{}
	for _, a := range all {
		vi := by[a.VariableName]
		if vi == nil {
			vi = &variableInfo{VariableName: a.VariableName}
			by[a.VariableName] = vi
		}
		vi.Assignments = append(vi.Assignments, a)
		vi.Active = vi.Active || a.Active
	}
	out := make([]variableInfo, 0, len(by))
	for _, v := range by {
		v.Shared = len(v.Assignments) > 1
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].VariableName < out[j].VariableName })
	return out, nil
}

func (s *Server) getVariables(w http.ResponseWriter, r *http.Request) {
	v, err := s.variableInfos(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, v)
}

// ---------- calendars ----------

func (s *Server) getCalendarIndex(w http.ResponseWriter, r *http.Request) {
	idx, err := s.Store.CalendarIndex(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, idx)
}

func (s *Server) getCalendar(w http.ResponseWriter, r *http.Request) {
	year, err := parseYear(r.PathValue("year"))
	if err != nil {
		writeErr(w, err)
		return
	}
	v := r.PathValue("variable")
	assign, err := s.Store.Assignments(r.Context(), v)
	if err != nil {
		writeErr(w, err)
		return
	}
	tpl, err := s.Store.GetTemplate(r.Context(), year)
	if err != nil {
		writeErr(w, err)
		return
	}
	cal, err := s.Store.GetCalendar(r.Context(), year, v)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"year": year, "variable_name": v,
		"exists":          cal != nil,
		"calendar":        cal,
		"registered":      len(assign) > 0,
		"shared":          len(assign) > 1,
		"assignments":     assign,
		"template_exists": tpl.Exists,
		"template_dates":  tpl.Dates,
	})
}

func (s *Server) putCalendar(w http.ResponseWriter, r *http.Request) {
	year, err := parseYear(r.PathValue("year"))
	if err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Version int                  `json:"version"`
		Dates   []store.CalendarDate `json:"dates"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	cal, err := s.Store.SaveCalendar(r.Context(), year, r.PathValue("variable"), in.Version, in.Dates)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, cal)
}

// ---------- suggestions & templates ----------

func (s *Server) settings(ctx context.Context) (map[string]string, error) {
	cfg := map[string]string{}
	for k, v := range DefaultSettings {
		cfg[k] = v
	}
	saved, err := s.Store.Settings(ctx)
	if err != nil {
		return nil, err
	}
	for k, v := range saved {
		cfg[k] = v
	}
	return cfg, nil
}

func (s *Server) getSuggestions(w http.ResponseWriter, r *http.Request) {
	year, err := parseYear(r.URL.Query().Get("year"))
	if err != nil {
		writeErr(w, err)
		return
	}
	resp := map[string]any{"year": year}
	if r.URL.Query().Get("cached") != "1" {
		cfg, err := s.settings(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		providers := s.ProviderFactory(cfg)
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		sugg, perrs := holidays.FetchAll(ctx, year, providers)
		cancel()
		if perrs == nil {
			perrs = []holidays.ProviderError{}
		}
		if len(providers) == 0 {
			perrs = append(perrs, holidays.ProviderError{Provider: "configuration", Error: "no holiday providers are enabled in settings"})
		}
		resp["errors"] = perrs
		if len(sugg) > 0 {
			if err := s.Store.CacheSuggestions(r.Context(), year, sugg); err != nil {
				log.Printf("cache suggestions: %v", err)
			}
			resp["suggestions"] = sugg
			resp["from_cache"] = false
			writeJSON(w, 200, resp)
			return
		}
	} else {
		resp["errors"] = []holidays.ProviderError{}
	}
	cached, err := s.Store.CachedSuggestions(r.Context(), year)
	if err != nil {
		writeErr(w, err)
		return
	}
	resp["suggestions"] = cached
	resp["from_cache"] = true
	writeJSON(w, 200, resp)
}

func (s *Server) getTemplate(w http.ResponseWriter, r *http.Request) {
	year, err := parseYear(r.PathValue("year"))
	if err != nil {
		writeErr(w, err)
		return
	}
	t, err := s.Store.GetTemplate(r.Context(), year)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, t)
}

func (s *Server) postTemplate(w http.ResponseWriter, r *http.Request) {
	year, err := parseYear(r.PathValue("year"))
	if err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Dates []store.CalendarDate `json:"dates"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := s.Store.SaveTemplate(r.Context(), year, in.Dates)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) postBulkApply(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Year      int                  `json:"year"`
		Dates     []store.CalendarDate `json:"dates"`
		Variables []string             `json:"variables"`
		Commit    bool                 `json:"commit"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if len(in.Dates) == 0 || len(in.Variables) == 0 {
		badRequest(w, "dates and variables are required")
		return
	}
	res, err := s.Store.BulkApply(r.Context(), in.Year, in.Dates, in.Variables, in.Commit)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"committed": in.Commit, "items": res})
}

// ---------- XML ----------

type xmlSummary struct {
	Year                     int      `json:"year"`
	Break                    string   `json:"break"`
	ActiveVariables          int      `json:"active_variables"`
	UniqueVariables          int      `json:"unique_variables"` // variables with >=1 action
	SelectedDates            int      `json:"selected_dates"`   // distinct dates
	Conditions               int      `json:"xml_blocks"`
	Actions                  int      `json:"actions"`
	VariablesWithoutCalendar []string `json:"variables_without_calendar"`
	VariablesWithoutDates    []string `json:"variables_without_dates"`
	InactiveSkipped          []string `json:"inactive_variables_skipped"`
	Warnings                 []string `json:"warnings"`
	Filename                 string   `json:"filename"`
}

// BuildXML generates the consolidated XML for year from SAVED data only.
func (s *Server) BuildXML(ctx context.Context, year int) (xmlgen.Result, xmlSummary, error) {
	sum := xmlSummary{Year: year, VariablesWithoutCalendar: []string{}, VariablesWithoutDates: []string{}, InactiveSkipped: []string{}, Warnings: []string{}}
	cfg, err := s.settings(ctx)
	if err != nil {
		return xmlgen.Result{}, sum, err
	}
	sum.Break = cfg["break"]
	active, err := s.Store.ActiveVariables(ctx)
	if err != nil {
		return xmlgen.Result{}, sum, err
	}
	saved, err := s.Store.SelectedByVariable(ctx, year)
	if err != nil {
		return xmlgen.Result{}, sum, err
	}
	idx, err := s.Store.CalendarIndex(ctx)
	if err != nil {
		return xmlgen.Result{}, sum, err
	}
	hasCal := map[string]bool{}
	for _, c := range idx {
		if c.Year == year {
			hasCal[c.VariableName] = true
		}
	}
	activeSet := map[string]bool{}
	in := xmlgen.Input{Year: year, Break: cfg["break"], Calendars: map[string][]xmlgen.Entry{}}
	for _, v := range active {
		activeSet[v] = true
		switch {
		case !hasCal[v]:
			sum.VariablesWithoutCalendar = append(sum.VariablesWithoutCalendar, v)
		case len(saved[v]) == 0:
			sum.VariablesWithoutDates = append(sum.VariablesWithoutDates, v)
		default:
			in.Calendars[v] = saved[v]
		}
	}
	for v := range saved {
		if !activeSet[v] {
			sum.InactiveSkipped = append(sum.InactiveSkipped, v)
		}
	}
	sort.Strings(sum.InactiveSkipped)
	res, err := xmlgen.Generate(in)
	if err != nil {
		return res, sum, err
	}
	sum.ActiveVariables = len(active)
	sum.UniqueVariables = res.Variables
	sum.SelectedDates = res.Dates
	sum.Conditions = res.Conditions
	sum.Actions = res.Actions
	res.Filename = exportFilename(cfg, year)
	sum.Filename = res.Filename
	if len(active) == 0 {
		sum.Warnings = append(sum.Warnings, "No active variables are registered.")
	}
	if res.Actions == 0 {
		sum.Warnings = append(sum.Warnings, "No holidays are selected for any active variable; the XML would be an empty wrapper.")
	}
	if len(sum.VariablesWithoutCalendar) > 0 {
		sum.Warnings = append(sum.Warnings, fmt.Sprintf("%d active variable(s) have no saved calendar for %d: %s", len(sum.VariablesWithoutCalendar), year, strings.Join(sum.VariablesWithoutCalendar, ", ")))
	}
	if sum.Break != "never" {
		sum.Warnings = append(sum.Warnings, fmt.Sprintf("break=%q differs from the prototype default \"never\".", sum.Break))
	}
	sum.Warnings = append(sum.Warnings, "Break semantics are PROVISIONAL and not validated against FreeSWITCH; test with the deployed dialplan before production use.")
	return res, sum, nil
}

func (s *Server) xmlPreview(w http.ResponseWriter, r *http.Request) {
	year, err := parseYear(r.URL.Query().Get("year"))
	if err != nil {
		writeErr(w, err)
		return
	}
	res, sum, err := s.BuildXML(r.Context(), year)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"xml": string(res.XML), "summary": sum})
}

func (s *Server) xmlDownload(w http.ResponseWriter, r *http.Request) {
	year, err := parseYear(r.URL.Query().Get("year"))
	if err != nil {
		writeErr(w, err)
		return
	}
	res, sum, err := s.BuildXML(r.Context(), year)
	if err != nil {
		writeErr(w, err)
		return
	}
	if res.Actions == 0 && r.URL.Query().Get("confirm_empty") != "1" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "no active variables or no holidays; add confirm_empty=1 to download an empty wrapper", "summary": sum})
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, res.Filename))
	w.Header().Set("Cache-Control", "no-store")
	w.Write(res.XML)
}

// ---------- settings ----------

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.settings(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, cfg)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	for k, v := range in {
		if _, ok := DefaultSettings[k]; !ok {
			badRequest(w, "unknown setting %q", k)
			return
		}
		switch k {
		case "break":
			if _, err := xmlgen.Generate(xmlgen.Input{Year: 2000, Break: v}); err != nil {
				badRequest(w, "%s", err.Error())
				return
			}
		case "providers":
			for _, p := range strings.Split(v, ",") {
				if p = strings.TrimSpace(p); p != "" && p != "bot" && p != "ics" {
					badRequest(w, "unknown provider %q (allowed: bot, ics)", p)
					return
				}
			}
		case "bot_url", "ics_url":
			if v != "" && !strings.HasPrefix(v, "https://") && !strings.HasPrefix(v, "http://") {
				badRequest(w, "%s must be an http(s) URL", k)
				return
			}
		case "export_filename":
			nv, err := normalizeExportFilename(v)
			if err != nil {
				badRequest(w, "%s", err.Error())
				return
			}
			in[k] = nv
		case "default_year":
			if v != "" {
				if _, err := parseYear(v); err != nil {
					writeErr(w, err)
					return
				}
			}
		}
	}
	if err := s.Store.SetSettings(r.Context(), in); err != nil {
		writeErr(w, err)
		return
	}
	s.getSettings(w, r)
}
