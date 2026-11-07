// Command xmlholiday manages Thai holiday calendars for FreeSWITCH
// working-time variables and generates one consolidated holiday XML file.
//
//	xmlholiday serve  [-listen :8086] [-dsn ...]
//	xmlholiday seed   [-dsn ...]                      sample companies/variables (no holiday dates)
//	xmlholiday import [-dsn ...] -company NAME file.xml...  import existing FreeSWITCH tod XML
//	xmlholiday xml    [-dsn ...] -year 2027 [-out file]     print/write XML from saved data
//	xmlholiday report [-dsn ...] -year 2027 [-out file.pdf]  PDF holiday report, one page per company
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/talkkonnect/xmlholiday/internal/importer"
	"github.com/talkkonnect/xmlholiday/internal/report"
	"github.com/talkkonnect/xmlholiday/internal/server"
	"github.com/talkkonnect/xmlholiday/internal/store"
	"github.com/talkkonnect/xmlholiday/web"
)

const defaultDSN = "root:bj4free@tcp(127.0.0.1:3306)/xmlholiday?charset=utf8mb4"

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	dsn := fs.String("dsn", envOr("XMLH_DSN", defaultDSN), "MariaDB DSN (env XMLH_DSN)")
	listen := fs.String("listen", envOr("XMLH_LISTEN", ":8086"), "HTTP listen address (serve)")
	company := fs.String("company", "", "company to register unknown variables under (import)")
	year := fs.Int("year", 0, "year (xml, report)")
	out := fs.String("out", "", "output file (xml: default stdout; report: default holiday_calendar_report_YEAR.pdf)")
	fs.Parse(args)

	st, err := store.Open(*dsn)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer st.Close()
	ctx := context.Background()

	switch cmd {
	case "serve":
		srv := server.New(st, web.FS)
		log.Printf("xmlholiday listening on %s (this tool never contacts FreeSWITCH)", *listen)
		hs := &http.Server{Addr: *listen, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
		log.Fatal(hs.ListenAndServe())
	case "seed":
		if err := seed(ctx, st); err != nil {
			log.Fatal(err)
		}
	case "import":
		if fs.NArg() == 0 {
			log.Fatal("usage: xmlholiday import -company NAME file.xml...")
		}
		for _, f := range fs.Args() {
			if err := importFile(ctx, st, f, *company); err != nil {
				log.Fatalf("%s: %v", f, err)
			}
		}
	case "xml":
		if *year == 0 {
			log.Fatal("-year is required")
		}
		res, sum, err := server.New(st, nil).BuildXML(ctx, *year)
		if err != nil {
			log.Fatal(err)
		}
		for _, w := range sum.Warnings {
			fmt.Fprintln(os.Stderr, "warning:", w)
		}
		if *out == "" {
			os.Stdout.Write(res.XML)
		} else if err := os.WriteFile(*out, res.XML, 0o644); err != nil {
			log.Fatal(err)
		}
	case "report":
		if *year == 0 {
			log.Fatal("-year is required")
		}
		pdf, err := server.New(st, nil).BuildReport(ctx, *year)
		if err != nil {
			log.Fatal(err)
		}
		name := *out
		if name == "" {
			name = report.Filename(*year)
		}
		if err := os.WriteFile(name, pdf, 0o644); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s (%d bytes)", name, len(pdf))
	default:
		log.Fatalf("unknown command %q (serve|seed|import|xml|report)", cmd)
	}
}

// seed registers sample companies/branches/variables taken from the reference
// file descriptions. It does NOT create any holiday dates.
func seed(ctx context.Context, st *store.Store) error {
	reg, err := st.Registry(ctx)
	if err != nil {
		return err
	}
	if len(reg.Companies) > 0 {
		return errors.New("database already has companies; seed skipped")
	}
	b1, b2 := int64(-11), int64(-12)
	in := store.Registry{
		Companies: []store.Company{{ID: -1, Name: "JWD", Active: true}, {ID: -2, Name: "MeSpace", Active: true}},
		Branches: []store.Branch{
			{ID: b1, CompanyID: -2, Name: "Patong", Active: true},
			{ID: b2, CompanyID: -2, Name: "Ramkamhaeng", Active: true},
		},
		Assignments: []store.Assignment{
			{ID: -21, CompanyID: -1, VariableName: "jwd-working-time", Active: true},
			{ID: -22, CompanyID: -2, BranchID: &b1, VariableName: "mespace-patong-working-time", Active: true},
			{ID: -23, CompanyID: -2, BranchID: &b2, VariableName: "mespace-Ramkamhaeng-working-time", Active: true},
		},
	}
	if _, err := st.SaveRegistry(ctx, in); err != nil {
		return err
	}
	log.Print("seeded sample companies JWD and MeSpace (Patong, Ramkamhaeng); no holiday dates were created")
	return nil
}

func importFile(ctx context.Context, st *store.Store, path, company string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	res, err := importer.Parse(data)
	if err != nil {
		return err
	}
	for _, s := range res.Skipped {
		log.Printf("skipped: %s", s)
	}
	log.Printf("break values found: %v", res.Breaks)
	registered, err := st.RegisteredVariables(ctx)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, v := range registered {
		known[v] = true
	}
	var missing []string
	for v := range res.Variables {
		if !known[v] {
			missing = append(missing, v)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		if company == "" {
			return fmt.Errorf("variables not registered: %s (pass -company NAME to register them)", strings.Join(missing, ", "))
		}
		reg, err := st.Registry(ctx)
		if err != nil {
			return err
		}
		in := store.Registry{}
		cid := int64(-1)
		for _, c := range reg.Companies {
			if c.Name == company {
				cid = c.ID
			}
		}
		if cid == -1 {
			in.Companies = []store.Company{{ID: -1, Name: company, Active: true}}
		}
		for i, v := range missing {
			in.Assignments = append(in.Assignments, store.Assignment{ID: int64(-100 - i), CompanyID: cid, VariableName: v, Active: true})
		}
		if _, err := st.SaveRegistry(ctx, in); err != nil {
			return err
		}
		log.Printf("registered %d variable(s) under company %q", len(missing), company)
	}
	vars := make([]string, 0, len(res.Variables))
	for v := range res.Variables {
		vars = append(vars, v)
	}
	sort.Strings(vars)
	for _, v := range vars {
		byYear := map[int][]importer.Date{}
		for _, d := range res.Variables[v] {
			var y int
			fmt.Sscanf(d.Date, "%d", &y)
			byYear[y] = append(byYear[y], d)
		}
		for y, ds := range byYear {
			cal, err := st.GetCalendar(ctx, y, v)
			version := 0
			merged := map[string]store.CalendarDate{}
			if err == nil {
				version = cal.Version
				for _, d := range cal.Dates {
					merged[d.Date] = d
				}
			} else if !errors.Is(err, store.ErrNotFound) {
				return err
			}
			for _, d := range ds {
				e := merged[d.Date]
				e.Date, e.Selected = d.Date, true
				if e.Origin == "" {
					e.Origin = "imported"
				}
				if e.Label == "" {
					e.Label = d.Label
				}
				merged[d.Date] = e
			}
			list := make([]store.CalendarDate, 0, len(merged))
			for _, d := range merged {
				list = append(list, d)
			}
			if _, err := st.SaveCalendar(ctx, y, v, version, list); err != nil {
				return fmt.Errorf("%s %d: %w", v, y, err)
			}
			log.Printf("imported %d date(s) for %s in %d", len(ds), v, y)
		}
	}
	return nil
}
