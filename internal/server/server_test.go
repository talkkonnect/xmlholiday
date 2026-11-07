package server

// Integration tests against a real MariaDB. They use (and drop) the database
// named in XMLH_TEST_DSN (default root:bj4free@tcp(127.0.0.1:3306)/xmlholiday_test).
// Set XMLH_TEST_DSN=skip to skip.

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/talkkonnect/xmlholiday/internal/holidays"
	"github.com/talkkonnect/xmlholiday/internal/store"
)

func testDSN() string {
	if v := os.Getenv("XMLH_TEST_DSN"); v != "" {
		return v
	}
	return "root:bj4free@tcp(127.0.0.1:3306)/xmlholiday_test?charset=utf8mb4"
}

type env struct {
	t   *testing.T
	srv *httptest.Server
	st  *store.Store
	s   *Server
}

func setup(t *testing.T) *env {
	t.Helper()
	dsn := testDSN()
	if dsn == "skip" {
		t.Skip("XMLH_TEST_DSN=skip")
	}
	st, err := store.Open(dsn)
	if err != nil {
		t.Skipf("MariaDB not available: %v", err)
	}
	// clean slate
	for _, tbl := range []string{"calendar_dates", "calendars", "assignments", "branches", "company_documents", "companies", "template_dates", "templates", "holiday_suggestions", "settings"} {
		if _, err := st.DB.Exec("DELETE FROM " + tbl); err != nil {
			t.Fatal(err)
		}
	}
	s := New(st, nil)
	// Default: providers fail (no network needed for tests).
	s.ProviderFactory = func(map[string]string) []holidays.Provider {
		return []holidays.Provider{&holidays.BOTProvider{URLTemplate: "http://127.0.0.1:1/{year}", Client: &http.Client{Timeout: time.Second}}}
	}
	e := &env{t: t, srv: httptest.NewServer(s.Handler()), st: st, s: s}
	t.Cleanup(func() { e.srv.Close(); st.Close() })
	return e
}

func (e *env) do(method, path string, body any, out any) int {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if out != nil {
		if s, ok := out.(*string); ok {
			*s = string(b)
		} else if err := json.Unmarshal(b, out); err != nil {
			e.t.Fatalf("%s %s: decode %v: %s", method, path, err, b)
		}
	}
	return resp.StatusCode
}

func (e *env) must(method, path string, body any, out any) {
	e.t.Helper()
	var raw string
	code := e.do(method, path, body, &raw)
	if code != 200 {
		e.t.Fatalf("%s %s -> %d: %s", method, path, code, raw)
	}
	if out != nil {
		if err := json.Unmarshal([]byte(raw), out); err != nil {
			e.t.Fatal(err)
		}
	}
}

type regResp struct {
	Registry store.Registry   `json:"registry"`
	IDMap    map[string]int64 `json:"id_map"`
}

func ptr(i int64) *int64 { return &i }

// registerSample: JWD (no branches) + MeSpace with Bangkok/Phuket + DITS.
func (e *env) registerSample() regResp {
	var r regResp
	e.must("POST", "/api/registry", store.Registry{
		Companies: []store.Company{{ID: -1, Name: "JWD", Active: true}, {ID: -2, Name: "MeSpace", Active: true}, {ID: -3, Name: "DITS", Active: true}},
		Branches:  []store.Branch{{ID: -11, CompanyID: -2, Name: "Bangkok", Active: true}, {ID: -12, CompanyID: -2, Name: "Phuket", Active: true}},
		Assignments: []store.Assignment{
			{ID: -21, CompanyID: -1, VariableName: "jwd-working-time", Active: true},
			{ID: -22, CompanyID: -2, BranchID: ptr(-11), VariableName: "mespace-bangkok-working-time", Active: true},
			{ID: -23, CompanyID: -2, BranchID: ptr(-12), VariableName: "mespace-Phuket-working-time", Active: true},
			{ID: -24, CompanyID: -3, VariableName: "dits-working-time", Active: true},
		},
	}, &r)
	return r
}

type calResp struct {
	Exists      bool                   `json:"exists"`
	Shared      bool                   `json:"shared"`
	Calendar    *store.Calendar        `json:"calendar"`
	Assignments []store.AssignmentInfo `json:"assignments"`
}

func (e *env) getCal(year int, v string) calResp {
	var c calResp
	e.must("GET", fmt.Sprintf("/api/calendars/%d/%s", year, v), nil, &c)
	return c
}

func (e *env) putCal(year int, v string, version int, dates ...store.CalendarDate) store.Calendar {
	var c store.Calendar
	e.must("PUT", fmt.Sprintf("/api/calendars/%d/%s", year, v), map[string]any{"version": version, "dates": dates}, &c)
	return c
}

func sel(date, label string) store.CalendarDate {
	return store.CalendarDate{Date: date, Label: label, Origin: "manual", Selected: true}
}

func (e *env) preview(year int) (string, xmlSummary) {
	var p struct {
		XML     string     `json:"xml"`
		Summary xmlSummary `json:"summary"`
	}
	e.must("GET", fmt.Sprintf("/api/xml/preview?year=%d", year), nil, &p)
	return p.XML, p.Summary
}

// AT-01: company without branches + variable persists across reload.
func TestAT01_Persist(t *testing.T) {
	e := setup(t)
	e.registerSample()
	var reg store.Registry
	e.must("GET", "/api/registry", nil, &reg)
	found := false
	for _, a := range reg.Assignments {
		if a.VariableName == "jwd-working-time" && a.BranchID == nil {
			found = true
		}
	}
	if !found || len(reg.Companies) != 3 || len(reg.Branches) != 2 {
		t.Fatalf("registry not persisted: %+v", reg)
	}
	// exact case preserved
	for _, a := range reg.Assignments {
		if strings.EqualFold(a.VariableName, "mespace-phuket-working-time") && a.VariableName != "mespace-Phuket-working-time" {
			t.Fatal("case not preserved")
		}
	}
}

// AT-02 & AT-07: branches with distinct variables have independent calendars.
func TestAT02_AT07_IndependentCalendars(t *testing.T) {
	e := setup(t)
	e.registerSample()
	e.putCal(2027, "mespace-bangkok-working-time", 0, sel("2027-01-01", "New Year"))
	e.putCal(2027, "mespace-Phuket-working-time", 0, sel("2027-01-01", "New Year"), sel("2027-07-15", "Phuket custom"))
	b := e.getCal(2027, "mespace-bangkok-working-time")
	p := e.getCal(2027, "mespace-Phuket-working-time")
	if len(b.Calendar.Dates) != 1 || len(p.Calendar.Dates) != 2 {
		t.Fatalf("calendars not independent: %+v / %+v", b.Calendar.Dates, p.Calendar.Dates)
	}
	// case-sensitivity: a differently-cased name is a different (unregistered) variable
	if code := e.do("PUT", "/api/calendars/2027/mespace-phuket-working-time", map[string]any{"version": 0, "dates": []any{}}, nil); code != 400 {
		t.Fatalf("differently-cased variable must not alias: got %d", code)
	}
}

// AT-03 & AT-10: shared variable -> one calendar, one action per date.
func TestAT03_AT10_SharedCalendar(t *testing.T) {
	e := setup(t)
	r := e.registerSample()
	// DITS gets a branch that shares JWD's variable.
	e.must("POST", "/api/registry", store.Registry{
		Branches:    []store.Branch{{ID: -1, CompanyID: r.IDMap["-3"], Name: "HQ", Active: true}},
		Assignments: []store.Assignment{{ID: -2, CompanyID: r.IDMap["-3"], BranchID: ptr(-1), VariableName: "jwd-working-time", Active: true}},
	}, nil)
	c := e.getCal(2027, "jwd-working-time")
	if !c.Shared || len(c.Assignments) != 2 {
		t.Fatalf("shared calendar not reported: %+v", c)
	}
	e.putCal(2027, "jwd-working-time", 0, sel("2027-01-01", "New Year"))
	x, _ := e.preview(2027)
	if n := strings.Count(x, "jwd-working-time=false"); n != 1 {
		t.Fatalf("want exactly one action, got %d\n%s", n, x)
	}
	var vars []variableInfo
	e.must("GET", "/api/variables", nil, &vars)
	for _, v := range vars {
		if v.VariableName == "jwd-working-time" && !v.Shared {
			t.Fatal("variables endpoint should mark shared")
		}
	}
}

// AT-04 / AT-13: suggestions show source+status; failure is reported, manual work continues.
func TestAT04_AT13_Suggestions(t *testing.T) {
	e := setup(t)
	e.registerSample()
	var r struct {
		Suggestions []holidays.Suggestion    `json:"suggestions"`
		Errors      []holidays.ProviderError `json:"errors"`
		FromCache   bool                     `json:"from_cache"`
	}
	e.must("GET", "/api/holiday-suggestions?year=2027", nil, &r)
	if len(r.Errors) != 1 || len(r.Suggestions) != 0 || !r.FromCache {
		t.Fatalf("failure must be explicit and nothing fabricated: %+v", r)
	}
	// manual editing + saved-data generation still work
	e.putCal(2027, "jwd-working-time", 0, sel("2027-03-03", "Company day"))
	if x, _ := e.preview(2027); !strings.Contains(x, `mday="03" mon="03" year="2027"`) {
		t.Fatal("generation blocked by provider failure")
	}

	// Working provider (fixture via httptest) -> official data with source/status, then cached for offline use.
	fixture, _ := os.ReadFile("../holidays/testdata/bot_2027.json")
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(fixture) }))
	defer hs.Close()
	e.s.ProviderFactory = func(map[string]string) []holidays.Provider {
		return []holidays.Provider{&holidays.BOTProvider{URLTemplate: hs.URL + "/{year}.json"}}
	}
	e.must("GET", "/api/holiday-suggestions?year=2027", nil, &r)
	if len(r.Suggestions) != 18 || r.Suggestions[0].VerificationStatus != "official" || r.Suggestions[0].SourceURL == "" || r.Suggestions[0].RetrievedAt.IsZero() {
		t.Fatalf("bad suggestions %+v", r.Suggestions)
	}
	hs.Close()
	e.must("GET", "/api/holiday-suggestions?year=2027", nil, &r)
	if !r.FromCache || len(r.Suggestions) != 18 || len(r.Errors) != 1 {
		t.Fatalf("offline cache fallback failed: %+v", r)
	}
}

// AT-06: unsaved edits are not committed; save persists; stale version conflicts.
func TestAT06_SaveAndConflict(t *testing.T) {
	e := setup(t)
	e.registerSample()
	if c := e.getCal(2027, "dits-working-time"); c.Exists {
		t.Fatal("calendar must not exist before save")
	}
	c := e.putCal(2027, "dits-working-time", 0, sel("2027-01-01", "NY"))
	if c.Version != 1 {
		t.Fatalf("version %d", c.Version)
	}
	e.putCal(2027, "dits-working-time", 1, sel("2027-01-01", "NY"), sel("2027-01-02", "x"))
	if code := e.do("PUT", "/api/calendars/2027/dits-working-time", map[string]any{"version": 1, "dates": []any{}}, nil); code != 409 {
		t.Fatalf("stale save must conflict, got %d", code)
	}
	if got := e.getCal(2027, "dits-working-time"); len(got.Calendar.Dates) != 2 {
		t.Fatal("conflicting save changed data")
	}
	// invalid dates rejected
	for _, d := range []string{"2027-02-29", "2026-12-31", "bad"} {
		if code := e.do("PUT", "/api/calendars/2027/dits-working-time", map[string]any{"version": 2, "dates": []any{sel(d, "")}}, nil); code != 400 {
			t.Fatalf("invalid date %s accepted (%d)", d, code)
		}
	}
}

// Template copy semantics + bulk apply respecting manual exclusions.
func TestTemplateCopyAndBulkApply(t *testing.T) {
	e := setup(t)
	e.registerSample()
	var tr store.SaveTemplateResult
	e.must("POST", "/api/templates/2027", map[string]any{"dates": []store.CalendarDate{sel("2027-01-01", "New Year's Day"), sel("2027-04-13", "Songkran")}}, &tr)
	if len(tr.CreatedCalendars) != 4 {
		t.Fatalf("each registered variable should get a template copy: %+v", tr.CreatedCalendars)
	}
	// DITS removes Songkran explicitly (manual exclusion)
	d := e.getCal(2027, "dits-working-time")
	e.putCal(2027, "dits-working-time", d.Calendar.Version, sel("2027-01-01", "New Year's Day"),
		store.CalendarDate{Date: "2027-04-13", Label: "Songkran", Origin: "template", Selected: false})
	// template changes later: existing calendars unchanged
	e.must("POST", "/api/templates/2027", map[string]any{"dates": []store.CalendarDate{sel("2027-01-01", "New Year's Day"), sel("2027-04-13", "Songkran"), sel("2027-05-04", "Coronation Day")}}, &tr)
	if len(tr.AddedDates) != 1 || tr.AddedDates[0].Date != "2027-05-04" || len(tr.CreatedCalendars) != 0 {
		t.Fatalf("bad template result %+v", tr)
	}
	if j := e.getCal(2027, "jwd-working-time"); len(j.Calendar.Dates) != 2 {
		t.Fatal("template change silently altered an existing calendar")
	}
	// new variable registered later gets the *current* template copy
	e.must("POST", "/api/assignments", store.Assignment{CompanyID: firstCompany(e), VariableName: "new-working-time", Active: true}, nil)
	if n := e.getCal(2027, "new-working-time"); !n.Exists || len(n.Calendar.Dates) != 3 {
		t.Fatalf("new variable should start from template copy: %+v", n)
	}
	// bulk apply preview -> nothing written
	body := map[string]any{"year": 2027, "variables": []string{"dits-working-time", "jwd-working-time"},
		"dates": []store.CalendarDate{sel("2027-04-13", "Songkran"), sel("2027-05-04", "Coronation Day")}}
	var br struct {
		Items []store.BulkApplyItem `json:"items"`
	}
	e.must("POST", "/api/bulk-apply", body, &br)
	if len(e.getCal(2027, "jwd-working-time").Calendar.Dates) != 2 {
		t.Fatal("preview must not write")
	}
	dits := br.Items[0]
	if dits.VariableName != "dits-working-time" || len(dits.SkippedExcl) != 1 || dits.SkippedExcl[0] != "2027-04-13" || len(dits.WillAdd) != 1 {
		t.Fatalf("manual exclusion not respected: %+v", dits)
	}
	body["commit"] = true
	e.must("POST", "/api/bulk-apply", body, &br)
	x, _ := e.preview(2027)
	if strings.Contains(x, "<!-- Songkran -->\n    <condition mday=\"13\" mon=\"04\" year=\"2027\" break=\"never\">\n      <action application=\"set\" data=\"dits") {
		t.Fatal("excluded date re-added")
	}
	if !strings.Contains(x, `data="dits-working-time=false"`) || strings.Count(x, `mday="04" mon="05"`) != 1 {
		t.Fatalf("bulk apply not reflected:\n%s", x)
	}
}

func firstCompany(e *env) int64 {
	var reg store.Registry
	e.must("GET", "/api/registry", nil, &reg)
	return reg.Companies[0].ID
}

// AT-08, AT-09, AT-11, AT-12: consolidated output regardless of UI selection;
// inactive variables excluded; deterministic; well-formed.
func TestAT08_09_11_12_XML(t *testing.T) {
	e := setup(t)
	r := e.registerSample()
	e.must("POST", "/api/assignments", store.Assignment{CompanyID: r.IDMap["-3"], VariableName: "dits-Extra-working-time", Active: true}, nil)
	e.must("POST", "/api/assignments", store.Assignment{CompanyID: r.IDMap["-3"], VariableName: "inactive-working-time", Active: false}, nil)
	for _, v := range []string{"jwd-working-time", "mespace-bangkok-working-time", "mespace-Phuket-working-time", "dits-working-time", "dits-Extra-working-time", "inactive-working-time"} {
		e.putCal(2027, v, 0, sel("2027-01-01", "New Year's Day"))
	}
	e.putCal(2027, "jwd-working-time", 1, sel("2027-01-01", "New Year's Day"), sel("2027-12-31", "New Year's Eve"))
	x1, sum := e.preview(2027)
	if sum.UniqueVariables != 5 || sum.Conditions != 2 || sum.Actions != 6 || len(sum.InactiveSkipped) != 1 {
		t.Fatalf("summary %+v", sum)
	}
	if strings.Contains(x1, "inactive-working-time") {
		t.Fatal("inactive variable emitted")
	}
	var doc struct {
		Ext struct {
			Name  string `xml:"name,attr"`
			Conds []struct {
				Mday    string `xml:"mday,attr"`
				Actions []struct {
					Data   string `xml:"data,attr"`
					Inline string `xml:"inline,attr"`
					App    string `xml:"application,attr"`
				} `xml:"action"`
			} `xml:"condition"`
		} `xml:"extension"`
	}
	if err := xml.Unmarshal([]byte(x1), &doc); err != nil {
		t.Fatalf("not well formed: %v", err)
	}
	if doc.Ext.Name != "holidays_2027" || len(doc.Ext.Conds[0].Actions) != 5 {
		t.Fatalf("AT-09 failed: %+v", doc)
	}
	want := []string{"dits-Extra-working-time", "dits-working-time", "jwd-working-time", "mespace-Phuket-working-time", "mespace-bangkok-working-time"}
	for i, a := range doc.Ext.Conds[0].Actions {
		if a.Data != want[i]+"=false" || a.Inline != "true" || a.App != "set" {
			t.Fatalf("action %d: %+v", i, a)
		}
	}
	// deactivating a company removes its variables
	x2, _ := e.preview(2027)
	if x1 != x2 {
		t.Fatal("regeneration not byte-identical")
	}
	var dl string
	resp, err := http.Get(e.srv.URL + "/api/xml/download?year=2027")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	dl = string(b)
	if dl != x1 || !strings.Contains(resp.Header.Get("Content-Disposition"), "freeswitch_holidays_2027.xml") {
		t.Fatalf("download differs from preview or bad filename: %s", resp.Header.Get("Content-Disposition"))
	}
	e.must("POST", "/api/companies", store.Company{ID: r.IDMap["-2"], Name: "MeSpace", Active: false}, nil)
	if x3, _ := e.preview(2027); strings.Contains(x3, "mespace") {
		t.Fatal("inactive company variables emitted")
	}
	// break setting is configurable
	e.must("PUT", "/api/settings", map[string]string{"break": "on-true"}, nil)
	if x4, _ := e.preview(2027); !strings.Contains(x4, `break="on-true"`) {
		t.Fatal("break not configurable")
	}
	if code := e.do("PUT", "/api/settings", map[string]string{"break": "bogus"}, nil); code != 400 {
		t.Fatal("bad break accepted")
	}
	// export filename is configurable
	for _, tc := range []struct{ in, want string }{
		{"01-tod-holidays.xml", "01-tod-holidays.xml"},
		{"tod_{year}", "tod_2027.xml"},
	} {
		e.must("PUT", "/api/settings", map[string]string{"export_filename": tc.in}, nil)
		resp, err := http.Get(e.srv.URL + "/api/xml/download?year=2027")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, `"`+tc.want+`"`) {
			t.Fatalf("export_filename %q: got %s, want %s", tc.in, cd, tc.want)
		}
	}
	for _, bad := range []string{"../x.xml", `a"b.xml`, "a/b.xml", ".hidden.xml"} {
		if code := e.do("PUT", "/api/settings", map[string]string{"export_filename": bad}, nil); code != 400 {
			t.Fatalf("bad export filename %q accepted", bad)
		}
	}
	e.must("PUT", "/api/settings", map[string]string{"export_filename": ""}, nil)
}

// Empty data: download requires explicit confirmation.
func TestEmptyRequiresConfirm(t *testing.T) {
	e := setup(t)
	if code := e.do("GET", "/api/xml/download?year=2027", nil, nil); code != 409 {
		t.Fatalf("empty download without confirm should be 409, got %d", code)
	}
	var s string
	if code := e.do("GET", "/api/xml/download?year=2027&confirm_empty=1", nil, &s); code != 200 || !strings.Contains(s, `<extension name="holidays_2027" continue="true">`) {
		t.Fatalf("confirmed empty wrapper failed: %d %s", code, s)
	}
}

// Registry validation and rename behaviour.
func TestRegistryValidationAndRename(t *testing.T) {
	e := setup(t)
	r := e.registerSample()
	for _, bad := range []string{"", "has space", "a=b"} {
		if code := e.do("POST", "/api/assignments", store.Assignment{CompanyID: r.IDMap["-1"], VariableName: bad, Active: true}, nil); code != 400 {
			t.Fatalf("variable %q accepted", bad)
		}
	}
	if code := e.do("POST", "/api/companies", store.Company{Name: "  "}, nil); code != 400 {
		t.Fatal("empty company accepted")
	}
	// branch belonging to another company rejected
	if code := e.do("POST", "/api/assignments", store.Assignment{CompanyID: r.IDMap["-1"], BranchID: ptr(r.IDMap["-11"]), VariableName: "x", Active: true}, nil); code != 400 {
		t.Fatal("cross-company branch accepted")
	}
	// same variable twice on the same company/branch is rejected
	if code := e.do("POST", "/api/assignments", store.Assignment{CompanyID: r.IDMap["-1"], VariableName: "jwd-working-time", Active: true}, nil); code != 400 {
		t.Fatal("duplicate assignment accepted")
	}
	// typo fix: renaming an unshared variable keeps its calendar
	e.putCal(2027, "dits-working-time", 0, sel("2027-06-01", "x"))
	e.must("POST", "/api/assignments", store.Assignment{ID: r.IDMap["-24"], CompanyID: r.IDMap["-3"], VariableName: "DITS-working-time", Active: true}, nil)
	if c := e.getCal(2027, "DITS-working-time"); !c.Exists || len(c.Calendar.Dates) != 1 {
		t.Fatal("calendar should follow renamed variable")
	}
}
