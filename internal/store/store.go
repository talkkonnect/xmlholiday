// Package store persists companies, branches, variable assignments, holiday
// templates and per-variable calendars in MariaDB/MySQL.
//
// Invariant: one (year, variable_name) maps to exactly one calendar. The
// variable_name columns use a binary collation so names are compared
// case-sensitively and stored exactly as entered.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/talkkonnect/xmlholiday/internal/holidays"
	"github.com/talkkonnect/xmlholiday/internal/xmlgen"
)

var (
	ErrConflict      = errors.New("calendar was changed by someone else since it was loaded; reload before saving")
	ErrNotRegistered = errors.New("variable is not registered in any assignment")
	ErrNotFound      = errors.New("not found")
)

// Validation error returned for bad user input.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(f string, a ...any) error { return &ValidationError{Msg: fmt.Sprintf(f, a...)} }

type Store struct{ DB *sql.DB }

type Company struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type Branch struct {
	ID        int64  `json:"id"`
	CompanyID int64  `json:"company_id"`
	Name      string `json:"name"`
	Active    bool   `json:"active"`
}

type Assignment struct {
	ID           int64  `json:"id"`
	CompanyID    int64  `json:"company_id"`
	BranchID     *int64 `json:"branch_id"`
	VariableName string `json:"variable_name"`
	Active       bool   `json:"active"`
}

// CalendarDate is one candidate/selected date in a calendar or template.
type CalendarDate struct {
	Date               string `json:"date"`
	Label              string `json:"label"`
	Origin             string `json:"origin"` // template|suggested|manual|copied|imported
	Selected           bool   `json:"selected"`
	Category           string `json:"category,omitempty"`
	SourceName         string `json:"source_name,omitempty"`
	SourceURL          string `json:"source_url,omitempty"`
	VerificationStatus string `json:"verification_status,omitempty"`
}

type Calendar struct {
	ID           int64          `json:"id"`
	Year         int            `json:"year"`
	VariableName string         `json:"variable_name"`
	Version      int            `json:"version"`
	UpdatedAt    string         `json:"updated_at"`
	Dates        []CalendarDate `json:"dates"`
}

var validOrigins = map[string]bool{"template": true, "suggested": true, "manual": true, "copied": true, "imported": true}

var schema = []string{
	`CREATE TABLE IF NOT EXISTS companies (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(200) NOT NULL,
		active TINYINT(1) NOT NULL DEFAULT 1
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS branches (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		company_id BIGINT NOT NULL,
		name VARCHAR(200) NOT NULL,
		active TINYINT(1) NOT NULL DEFAULT 1,
		FOREIGN KEY (company_id) REFERENCES companies(id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS assignments (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		company_id BIGINT NOT NULL,
		branch_id BIGINT NULL,
		variable_name VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
		active TINYINT(1) NOT NULL DEFAULT 1,
		FOREIGN KEY (company_id) REFERENCES companies(id),
		FOREIGN KEY (branch_id) REFERENCES branches(id),
		INDEX (variable_name)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS calendars (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		year SMALLINT NOT NULL,
		variable_name VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
		version INT NOT NULL DEFAULT 1,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		UNIQUE KEY uniq_year_var (year, variable_name)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS calendar_dates (
		calendar_id BIGINT NOT NULL,
		date DATE NOT NULL,
		label VARCHAR(500) NOT NULL DEFAULT '',
		origin VARCHAR(20) NOT NULL,
		selected TINYINT(1) NOT NULL,
		category VARCHAR(40) NOT NULL DEFAULT '',
		source_name VARCHAR(200) NOT NULL DEFAULT '',
		source_url VARCHAR(1000) NOT NULL DEFAULT '',
		verification_status VARCHAR(20) NOT NULL DEFAULT '',
		PRIMARY KEY (calendar_id, date),
		FOREIGN KEY (calendar_id) REFERENCES calendars(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS templates (
		year SMALLINT PRIMARY KEY,
		approved_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS template_dates (
		year SMALLINT NOT NULL,
		date DATE NOT NULL,
		label VARCHAR(500) NOT NULL DEFAULT '',
		origin VARCHAR(20) NOT NULL,
		category VARCHAR(40) NOT NULL DEFAULT '',
		source_name VARCHAR(200) NOT NULL DEFAULT '',
		source_url VARCHAR(1000) NOT NULL DEFAULT '',
		verification_status VARCHAR(20) NOT NULL DEFAULT '',
		PRIMARY KEY (year, date)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS holiday_suggestions (
		year SMALLINT NOT NULL,
		date DATE NOT NULL,
		source_name VARCHAR(200) NOT NULL,
		label VARCHAR(500) NOT NULL,
		category VARCHAR(40) NOT NULL,
		source_url VARCHAR(1000) NOT NULL,
		retrieved_at DATETIME NOT NULL,
		verification_status VARCHAR(20) NOT NULL,
		also_listed_by VARCHAR(1000) NOT NULL DEFAULT '',
		PRIMARY KEY (year, date, source_name)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS company_documents (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		company_id BIGINT NOT NULL,
		year SMALLINT NOT NULL,
		filename VARCHAR(255) NOT NULL,
		content_type VARCHAR(100) NOT NULL,
		size BIGINT NOT NULL,
		sha256 CHAR(64) NOT NULL,
		note VARCHAR(1000) NOT NULL DEFAULT '',
		uploaded_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		data LONGBLOB NOT NULL,
		FOREIGN KEY (company_id) REFERENCES companies(id),
		INDEX (company_id, year)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS settings (
		k VARCHAR(64) PRIMARY KEY,
		v TEXT NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
}

// Open connects, creates the database if needed and migrates the schema.
func Open(dsn string) (*Store, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("bad DSN: %w", err)
	}
	dbName := cfg.DBName
	if dbName == "" {
		return nil, fmt.Errorf("DSN must include a database name")
	}
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["charset"] = "utf8mb4"
	cfg.ParseTime = false
	admin := cfg.Clone()
	admin.DBName = ""
	adb, err := sql.Open("mysql", admin.FormatDSN())
	if err != nil {
		return nil, err
	}
	if _, err := adb.Exec("CREATE DATABASE IF NOT EXISTS `" + strings.ReplaceAll(dbName, "`", "``") + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		adb.Close()
		return nil, fmt.Errorf("create database: %w", err)
	}
	adb.Close()
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(5 * time.Minute)
	for _, q := range schema {
		if _, err := db.Exec(q); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate: %w", err)
		}
	}
	return &Store{DB: db}, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// ---------- registry ----------

type Registry struct {
	Companies   []Company    `json:"companies"`
	Branches    []Branch     `json:"branches"`
	Assignments []Assignment `json:"assignments"`
}

func (s *Store) Registry(ctx context.Context) (*Registry, error) {
	r := &Registry{Companies: []Company{}, Branches: []Branch{}, Assignments: []Assignment{}}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,active FROM companies ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c Company
		if err := rows.Scan(&c.ID, &c.Name, &c.Active); err != nil {
			rows.Close()
			return nil, err
		}
		r.Companies = append(r.Companies, c)
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT id,company_id,name,active FROM branches ORDER BY company_id,name,id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var b Branch
		if err := rows.Scan(&b.ID, &b.CompanyID, &b.Name, &b.Active); err != nil {
			rows.Close()
			return nil, err
		}
		r.Branches = append(r.Branches, b)
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT id,company_id,branch_id,variable_name,active FROM assignments ORDER BY company_id,branch_id,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Assignment
		var bid sql.NullInt64
		if err := rows.Scan(&a.ID, &a.CompanyID, &bid, &a.VariableName, &a.Active); err != nil {
			return nil, err
		}
		if bid.Valid {
			v := bid.Int64
			a.BranchID = &v
		}
		r.Assignments = append(r.Assignments, a)
	}
	return r, rows.Err()
}

// SaveRegistry upserts the given rows in one transaction. Rows with id <= 0
// are created; negative ids act as temporary client ids and may be referenced
// by company_id / branch_id of other rows in the same request. Nothing is
// deleted (deactivate instead). Returns the mapping temp id -> real id.
//
// When an assignment's variable changes and the old variable is no longer
// referenced by any assignment, that variable's calendars move to the new
// name (typo fix) unless the new name already has a calendar for the year.
// Finally, for every year that has an approved template, variables without a
// calendar get a fresh copy of that template.
func (s *Store) SaveRegistry(ctx context.Context, in Registry) (map[int64]int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	companyIDs := map[int64]int64{}
	branchIDs := map[int64]int64{}
	mapping := map[int64]int64{}

	for _, c := range in.Companies {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			return nil, invalid("company name must not be empty")
		}
		if c.ID > 0 {
			res, err := tx.ExecContext(ctx, `UPDATE companies SET name=?, active=? WHERE id=?`, name, c.Active, c.ID)
			if err != nil {
				return nil, err
			}
			if err := mustExist(ctx, tx, "companies", c.ID, res); err != nil {
				return nil, err
			}
			companyIDs[c.ID] = c.ID
		} else {
			res, err := tx.ExecContext(ctx, `INSERT INTO companies(name,active) VALUES(?,?)`, name, c.Active)
			if err != nil {
				return nil, err
			}
			id, _ := res.LastInsertId()
			companyIDs[c.ID] = id
			mapping[c.ID] = id
		}
	}
	resolveCompany := func(id int64) (int64, error) {
		if id <= 0 {
			if real, ok := companyIDs[id]; ok {
				return real, nil
			}
			return 0, invalid("unknown temporary company id %d", id)
		}
		return id, nil
	}
	for _, b := range in.Branches {
		name := strings.TrimSpace(b.Name)
		if name == "" {
			return nil, invalid("branch name must not be empty")
		}
		cid, err := resolveCompany(b.CompanyID)
		if err != nil {
			return nil, err
		}
		if b.ID > 0 {
			res, err := tx.ExecContext(ctx, `UPDATE branches SET company_id=?, name=?, active=? WHERE id=?`, cid, name, b.Active, b.ID)
			if err != nil {
				return nil, err
			}
			if err := mustExist(ctx, tx, "branches", b.ID, res); err != nil {
				return nil, err
			}
			branchIDs[b.ID] = b.ID
		} else {
			res, err := tx.ExecContext(ctx, `INSERT INTO branches(company_id,name,active) VALUES(?,?,?)`, cid, name, b.Active)
			if err != nil {
				return nil, err
			}
			id, _ := res.LastInsertId()
			branchIDs[b.ID] = id
			mapping[b.ID] = id
		}
	}
	type rename struct{ from, to string }
	var renames []rename
	for _, a := range in.Assignments {
		if err := xmlgen.ValidateVariable(a.VariableName); err != nil {
			return nil, invalid("%s", err.Error())
		}
		cid, err := resolveCompany(a.CompanyID)
		if err != nil {
			return nil, err
		}
		var bid any
		if a.BranchID != nil {
			id := *a.BranchID
			if id <= 0 {
				real, ok := branchIDs[id]
				if !ok {
					return nil, invalid("unknown temporary branch id %d", id)
				}
				id = real
			}
			var owner int64
			if err := tx.QueryRowContext(ctx, `SELECT company_id FROM branches WHERE id=?`, id).Scan(&owner); err != nil {
				return nil, invalid("branch %d does not exist", id)
			}
			if owner != cid {
				return nil, invalid("branch %d does not belong to company %d", id, cid)
			}
			bid = id
		}
		if a.ID > 0 {
			var old string
			if err := tx.QueryRowContext(ctx, `SELECT variable_name FROM assignments WHERE id=?`, a.ID).Scan(&old); err != nil {
				return nil, invalid("assignment %d does not exist", a.ID)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE assignments SET company_id=?, branch_id=?, variable_name=?, active=? WHERE id=?`, cid, bid, a.VariableName, a.Active, a.ID); err != nil {
				return nil, err
			}
			if old != a.VariableName {
				renames = append(renames, rename{old, a.VariableName})
			}
		} else {
			res, err := tx.ExecContext(ctx, `INSERT INTO assignments(company_id,branch_id,variable_name,active) VALUES(?,?,?,?)`, cid, bid, a.VariableName, a.Active)
			if err != nil {
				return nil, err
			}
			id, _ := res.LastInsertId()
			mapping[a.ID] = id
		}
	}
	for _, r := range renames {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM assignments WHERE variable_name=?`, r.from).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			continue // old variable still used elsewhere: leave its calendar alone
		}
		if err := moveCalendars(ctx, tx, r.from, r.to); err != nil {
			return nil, err
		}
	}
	var dupVar string
	err = tx.QueryRowContext(ctx, `SELECT variable_name FROM assignments GROUP BY company_id, COALESCE(branch_id,0), variable_name HAVING COUNT(*)>1 LIMIT 1`).Scan(&dupVar)
	if err == nil {
		return nil, invalid("variable %q is assigned more than once to the same company/branch", dupVar)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	years, err := templateYears(ctx, tx)
	if err != nil {
		return nil, err
	}
	for _, y := range years {
		if _, err := ensureCalendars(ctx, tx, y); err != nil {
			return nil, err
		}
	}
	return mapping, tx.Commit()
}

// moveCalendars renames from's calendars to "to" for years where "to" has none.
func moveCalendars(ctx context.Context, tx *sql.Tx, from, to string) error {
	rows, err := tx.QueryContext(ctx, `SELECT f.id FROM calendars f WHERE f.variable_name=?
		AND f.year NOT IN (SELECT t.year FROM calendars t WHERE t.variable_name=?)`, from, to)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE calendars SET variable_name=?, version=version+1 WHERE id=?`, to, id); err != nil {
			return err
		}
	}
	return nil
}

func mustExist(ctx context.Context, tx *sql.Tx, table string, id int64, res sql.Result) error {
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	var x int
	if err := tx.QueryRowContext(ctx, "SELECT 1 FROM "+table+" WHERE id=?", id).Scan(&x); err != nil {
		return invalid("%s id %d does not exist", strings.TrimSuffix(table, "s"), id)
	}
	return nil
}

// AssignmentInfo describes one assignment with its company/branch names.
type AssignmentInfo struct {
	AssignmentID int64  `json:"assignment_id"`
	CompanyID    int64  `json:"company_id"`
	CompanyName  string `json:"company_name"`
	BranchID     *int64 `json:"branch_id"`
	BranchName   string `json:"branch_name"`
	VariableName string `json:"variable_name"`
	Active       bool   `json:"active"` // assignment, company and branch all active
}

// Assignments lists all assignments with names, optionally filtered by variable.
func (s *Store) Assignments(ctx context.Context, variable string) ([]AssignmentInfo, error) {
	q := `SELECT a.id, c.id, c.name, b.id, COALESCE(b.name,''), a.variable_name,
	             (a.active AND c.active AND COALESCE(b.active,1))
	      FROM assignments a JOIN companies c ON c.id=a.company_id LEFT JOIN branches b ON b.id=a.branch_id`
	var args []any
	if variable != "" {
		q += ` WHERE a.variable_name=?`
		args = append(args, variable)
	}
	q += ` ORDER BY c.name, b.name, a.id`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssignmentInfo{}
	for rows.Next() {
		var a AssignmentInfo
		var bid sql.NullInt64
		if err := rows.Scan(&a.AssignmentID, &a.CompanyID, &a.CompanyName, &bid, &a.BranchName, &a.VariableName, &a.Active); err != nil {
			return nil, err
		}
		if bid.Valid {
			v := bid.Int64
			a.BranchID = &v
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ActiveVariables returns the distinct variables having at least one active
// assignment whose company (and branch, if any) is active. Sorted.
func (s *Store) ActiveVariables(ctx context.Context) ([]string, error) {
	return s.variables(ctx, `SELECT DISTINCT a.variable_name FROM assignments a
		JOIN companies c ON c.id=a.company_id LEFT JOIN branches b ON b.id=a.branch_id
		WHERE a.active=1 AND c.active=1 AND COALESCE(b.active,1)=1`)
}

// RegisteredVariables returns every distinct variable in any assignment.
func (s *Store) RegisteredVariables(ctx context.Context) ([]string, error) {
	return s.variables(ctx, `SELECT DISTINCT variable_name FROM assignments`)
}

func (s *Store) variables(ctx context.Context, q string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	sort.Strings(out)
	return out, rows.Err()
}

func (s *Store) isRegistered(ctx context.Context, q queryer, v string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM assignments WHERE variable_name=?`, v).Scan(&n)
	return n > 0, err
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// ---------- calendars ----------

// GetCalendar returns the saved calendar or ErrNotFound.
func (s *Store) GetCalendar(ctx context.Context, year int, variable string) (*Calendar, error) {
	return getCalendar(ctx, s.DB, year, variable)
}

func getCalendar(ctx context.Context, q queryer, year int, variable string) (*Calendar, error) {
	c := &Calendar{Year: year, VariableName: variable, Dates: []CalendarDate{}}
	err := q.QueryRowContext(ctx, `SELECT id, version, DATE_FORMAT(updated_at,'%Y-%m-%d %H:%i:%s') FROM calendars WHERE year=? AND variable_name=?`, year, variable).
		Scan(&c.ID, &c.Version, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT DATE_FORMAT(date,'%Y-%m-%d'), label, origin, selected, category, source_name, source_url, verification_status
		FROM calendar_dates WHERE calendar_id=? ORDER BY date`, c.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d CalendarDate
		if err := rows.Scan(&d.Date, &d.Label, &d.Origin, &d.Selected, &d.Category, &d.SourceName, &d.SourceURL, &d.VerificationStatus); err != nil {
			return nil, err
		}
		c.Dates = append(c.Dates, d)
	}
	return c, rows.Err()
}

func validateDates(year int, dates []CalendarDate) ([]CalendarDate, error) {
	seen := map[string]bool{}
	out := make([]CalendarDate, 0, len(dates))
	for _, d := range dates {
		if _, err := xmlgen.ParseDate(d.Date, year); err != nil {
			return nil, invalid("%s", err.Error())
		}
		if seen[d.Date] {
			return nil, invalid("date %s appears more than once", d.Date)
		}
		seen[d.Date] = true
		if d.Origin == "" {
			d.Origin = "manual"
		}
		if !validOrigins[d.Origin] {
			return nil, invalid("invalid origin %q", d.Origin)
		}
		d.Label = strings.TrimSpace(d.Label)
		if len(d.Label) > 500 {
			return nil, invalid("label too long for %s", d.Date)
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

// SaveCalendar replaces the full set of dates of (year, variable). expectedVersion
// 0 means "create; fail if it exists"; otherwise it must match the stored version.
func (s *Store) SaveCalendar(ctx context.Context, year int, variable string, expectedVersion int, dates []CalendarDate) (*Calendar, error) {
	dates, err := validateDates(year, dates)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if ok, err := s.isRegistered(ctx, tx, variable); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNotRegistered
	}
	var id int64
	var ver int
	err = tx.QueryRowContext(ctx, `SELECT id, version FROM calendars WHERE year=? AND variable_name=? FOR UPDATE`, year, variable).Scan(&id, &ver)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if expectedVersion != 0 {
			return nil, ErrConflict
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO calendars(year, variable_name) VALUES(?,?)`, year, variable)
		if err != nil {
			if isDup(err) {
				return nil, ErrConflict
			}
			return nil, err
		}
		id, _ = res.LastInsertId()
	case err != nil:
		return nil, err
	default:
		if ver != expectedVersion {
			return nil, ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE calendars SET version=version+1 WHERE id=?`, id); err != nil {
			return nil, err
		}
	}
	if err := replaceDates(ctx, tx, id, dates); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetCalendar(ctx, year, variable)
}

func replaceDates(ctx context.Context, tx *sql.Tx, calID int64, dates []CalendarDate) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM calendar_dates WHERE calendar_id=?`, calID); err != nil {
		return err
	}
	for _, d := range dates {
		if _, err := tx.ExecContext(ctx, `INSERT INTO calendar_dates(calendar_id,date,label,origin,selected,category,source_name,source_url,verification_status)
			VALUES(?,?,?,?,?,?,?,?,?)`, calID, d.Date, d.Label, d.Origin, d.Selected, d.Category, d.SourceName, d.SourceURL, d.VerificationStatus); err != nil {
			return err
		}
	}
	return nil
}

func isDup(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

// SelectedByVariable returns, for year, variable -> selected entries for all
// saved calendars.
func (s *Store) SelectedByVariable(ctx context.Context, year int) (map[string][]xmlgen.Entry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT c.variable_name, DATE_FORMAT(d.date,'%Y-%m-%d'), d.label FROM calendars c
		JOIN calendar_dates d ON d.calendar_id=c.id WHERE c.year=? AND d.selected=1 ORDER BY c.variable_name, d.date`, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]xmlgen.Entry{}
	for rows.Next() {
		var v string
		var e xmlgen.Entry
		if err := rows.Scan(&v, &e.Date, &e.Label); err != nil {
			return nil, err
		}
		out[v] = append(out[v], e)
	}
	return out, rows.Err()
}

// CalendarSummary is a lightweight listing row.
type CalendarSummary struct {
	Year         int    `json:"year"`
	VariableName string `json:"variable_name"`
	Version      int    `json:"version"`
	UpdatedAt    string `json:"updated_at"`
	Selected     int    `json:"selected"`
}

// CalendarIndex lists all saved calendars (all years).
func (s *Store) CalendarIndex(ctx context.Context) ([]CalendarSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT c.year, c.variable_name, c.version, DATE_FORMAT(c.updated_at,'%Y-%m-%d %H:%i:%s'),
		(SELECT COUNT(*) FROM calendar_dates d WHERE d.calendar_id=c.id AND d.selected=1) FROM calendars c ORDER BY c.year, c.variable_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarSummary{}
	for rows.Next() {
		var c CalendarSummary
		if err := rows.Scan(&c.Year, &c.VariableName, &c.Version, &c.UpdatedAt, &c.Selected); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------- templates ----------

type Template struct {
	Year       int            `json:"year"`
	Exists     bool           `json:"exists"`
	ApprovedAt string         `json:"approved_at"`
	Dates      []CalendarDate `json:"dates"`
}

func (s *Store) GetTemplate(ctx context.Context, year int) (*Template, error) {
	return getTemplate(ctx, s.DB, year)
}

func getTemplate(ctx context.Context, q queryer, year int) (*Template, error) {
	t := &Template{Year: year, Dates: []CalendarDate{}}
	err := q.QueryRowContext(ctx, `SELECT DATE_FORMAT(approved_at,'%Y-%m-%d %H:%i:%s') FROM templates WHERE year=?`, year).Scan(&t.ApprovedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return t, nil
	}
	if err != nil {
		return nil, err
	}
	t.Exists = true
	rows, err := q.QueryContext(ctx, `SELECT DATE_FORMAT(date,'%Y-%m-%d'), label, origin, category, source_name, source_url, verification_status
		FROM template_dates WHERE year=? ORDER BY date`, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		d := CalendarDate{Selected: true}
		if err := rows.Scan(&d.Date, &d.Label, &d.Origin, &d.Category, &d.SourceName, &d.SourceURL, &d.VerificationStatus); err != nil {
			return nil, err
		}
		t.Dates = append(t.Dates, d)
	}
	return t, rows.Err()
}

// SaveTemplateResult reports side effects of approving a template.
type SaveTemplateResult struct {
	Template         *Template      `json:"template"`
	CreatedCalendars []string       `json:"created_calendars"`
	AddedDates       []CalendarDate `json:"added_dates"` // dates new versus the previous template
}

// SaveTemplate stores the approved standard template for year (selected dates
// only). Existing calendars are NOT modified; registered variables that have
// no calendar for year receive a copy.
func (s *Store) SaveTemplate(ctx context.Context, year int, dates []CalendarDate) (*SaveTemplateResult, error) {
	var sel []CalendarDate
	for _, d := range dates {
		if d.Selected {
			sel = append(sel, d)
		}
	}
	sel, err := validateDates(year, sel)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	prev, err := getTemplate(ctx, tx, year)
	if err != nil {
		return nil, err
	}
	prevSet := map[string]bool{}
	for _, d := range prev.Dates {
		prevSet[d.Date] = true
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO templates(year) VALUES(?) ON DUPLICATE KEY UPDATE approved_at=CURRENT_TIMESTAMP`, year); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM template_dates WHERE year=?`, year); err != nil {
		return nil, err
	}
	res := &SaveTemplateResult{AddedDates: []CalendarDate{}}
	for _, d := range sel {
		if _, err := tx.ExecContext(ctx, `INSERT INTO template_dates(year,date,label,origin,category,source_name,source_url,verification_status) VALUES(?,?,?,?,?,?,?,?)`,
			year, d.Date, d.Label, d.Origin, d.Category, d.SourceName, d.SourceURL, d.VerificationStatus); err != nil {
			return nil, err
		}
		if prev.Exists && !prevSet[d.Date] {
			res.AddedDates = append(res.AddedDates, d)
		}
	}
	created, err := ensureCalendars(ctx, tx, year)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	res.CreatedCalendars = created
	res.Template, err = s.GetTemplate(ctx, year)
	return res, err
}

func templateYears(ctx context.Context, q queryer) ([]int, error) {
	rows, err := q.QueryContext(ctx, `SELECT year FROM templates ORDER BY year`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ys []int
	for rows.Next() {
		var y int
		if err := rows.Scan(&y); err != nil {
			return nil, err
		}
		ys = append(ys, y)
	}
	return ys, rows.Err()
}

// ensureCalendars gives every registered variable without a calendar for year
// a copy of that year's template (if a template exists).
func ensureCalendars(ctx context.Context, tx *sql.Tx, year int) ([]string, error) {
	t, err := getTemplate(ctx, tx, year)
	if err != nil || !t.Exists {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT a.variable_name FROM assignments a
		WHERE NOT EXISTS (SELECT 1 FROM calendars c WHERE c.year=? AND c.variable_name=a.variable_name)`, year)
	if err != nil {
		return nil, err
	}
	var vars []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return nil, err
		}
		vars = append(vars, v)
	}
	rows.Close()
	sort.Strings(vars)
	for _, v := range vars {
		res, err := tx.ExecContext(ctx, `INSERT INTO calendars(year, variable_name) VALUES(?,?)`, year, v)
		if err != nil {
			return nil, err
		}
		id, _ := res.LastInsertId()
		copyDates := make([]CalendarDate, len(t.Dates))
		for i, d := range t.Dates {
			d.Selected = true
			d.Origin = "template"
			copyDates[i] = d
		}
		if err := replaceDates(ctx, tx, id, copyDates); err != nil {
			return nil, err
		}
	}
	return vars, nil
}

// ---------- bulk apply ----------

type BulkApplyItem struct {
	VariableName string   `json:"variable_name"`
	WillAdd      []string `json:"will_add"`
	AlreadyHas   []string `json:"already_selected"`
	SkippedExcl  []string `json:"skipped_manual_exclusion"`
	NoCalendar   bool     `json:"no_calendar"`
}

// BulkApply adds dates as selected to the given variables' calendars. Dates
// that a calendar explicitly lists as unselected (manual exclusion) are never
// overwritten. With commit=false nothing is written (preview).
func (s *Store) BulkApply(ctx context.Context, year int, dates []CalendarDate, variables []string, commit bool) ([]BulkApplyItem, error) {
	for i := range dates {
		dates[i].Selected = true
	}
	dates, err := validateDates(year, dates)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	vs := append([]string(nil), variables...)
	sort.Strings(vs)
	var out []BulkApplyItem
	for _, v := range vs {
		item := BulkApplyItem{VariableName: v, WillAdd: []string{}, AlreadyHas: []string{}, SkippedExcl: []string{}}
		cal, err := getCalendar(ctx, tx, year, v)
		if errors.Is(err, ErrNotFound) {
			item.NoCalendar = true
			out = append(out, item)
			continue
		}
		if err != nil {
			return nil, err
		}
		existing := map[string]CalendarDate{}
		for _, d := range cal.Dates {
			existing[d.Date] = d
		}
		changed := false
		for _, d := range dates {
			e, ok := existing[d.Date]
			switch {
			case ok && e.Selected:
				item.AlreadyHas = append(item.AlreadyHas, d.Date)
			case ok && !e.Selected:
				item.SkippedExcl = append(item.SkippedExcl, d.Date)
			default:
				item.WillAdd = append(item.WillAdd, d.Date)
				existing[d.Date] = d
				changed = true
			}
		}
		if commit && changed {
			all := make([]CalendarDate, 0, len(existing))
			for _, d := range existing {
				all = append(all, d)
			}
			sort.Slice(all, func(i, j int) bool { return all[i].Date < all[j].Date })
			if _, err := tx.ExecContext(ctx, `UPDATE calendars SET version=version+1 WHERE id=?`, cal.ID); err != nil {
				return nil, err
			}
			if err := replaceDates(ctx, tx, cal.ID, all); err != nil {
				return nil, err
			}
		}
		out = append(out, item)
	}
	if commit {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ---------- suggestion cache ----------

func (s *Store) CacheSuggestions(ctx context.Context, year int, sugg []holidays.Suggestion) error {
	if len(sugg) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM holiday_suggestions WHERE year=?`, year); err != nil {
		return err
	}
	for _, x := range sugg {
		if _, err := tx.ExecContext(ctx, `INSERT INTO holiday_suggestions(year,date,source_name,label,category,source_url,retrieved_at,verification_status,also_listed_by)
			VALUES(?,?,?,?,?,?,?,?,?)`, year, x.Date, x.SourceName, x.Label, x.Category, x.SourceURL, x.RetrievedAt.UTC().Format("2006-01-02 15:04:05"),
			x.VerificationStatus, strings.Join(x.AlsoListedBy, "|")); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) CachedSuggestions(ctx context.Context, year int) ([]holidays.Suggestion, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DATE_FORMAT(date,'%Y-%m-%d'), source_name, label, category, source_url,
		DATE_FORMAT(retrieved_at,'%Y-%m-%dT%H:%i:%sZ'), verification_status, also_listed_by FROM holiday_suggestions WHERE year=? ORDER BY date, source_name`, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []holidays.Suggestion{}
	for rows.Next() {
		var x holidays.Suggestion
		var ts, also string
		if err := rows.Scan(&x.Date, &x.SourceName, &x.Label, &x.Category, &x.SourceURL, &ts, &x.VerificationStatus, &also); err != nil {
			return nil, err
		}
		x.RetrievedAt, _ = time.Parse(time.RFC3339, ts)
		if also != "" {
			x.AlsoListedBy = strings.Split(also, "|")
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ---------- settings ----------

func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT k, v FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *Store) SetSettings(ctx context.Context, kv map[string]string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range kv {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings(k,v) VALUES(?,?) ON DUPLICATE KEY UPDATE v=VALUES(v)`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}
