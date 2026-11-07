package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// MaxDocumentSize is the largest accepted PDF. It stays below MariaDB's
// default max_allowed_packet (16 MiB) so inserts never fail mid-request.
const MaxDocumentSize = 10 << 20

// Document is the metadata of a holiday-announcement PDF that a company
// published for a year. It is kept for reference when the configured
// calendar is disputed; it does not influence the generated XML.
type Document struct {
	ID          int64  `json:"id"`
	CompanyID   int64  `json:"company_id"`
	Year        int    `json:"year"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Note        string `json:"note"`
	UploadedAt  string `json:"uploaded_at"`
}

const docCols = `id, company_id, year, filename, content_type, size, sha256, note, DATE_FORMAT(uploaded_at,'%Y-%m-%d %H:%i:%s')`

func scanDoc(sc interface{ Scan(...any) error }) (Document, error) {
	var d Document
	err := sc.Scan(&d.ID, &d.CompanyID, &d.Year, &d.Filename, &d.ContentType, &d.Size, &d.SHA256, &d.Note, &d.UploadedAt)
	return d, err
}

// cleanFilename keeps only the base name, strips control characters and
// quotes (it is echoed in Content-Disposition) and ensures a .pdf suffix.
func cleanFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(strings.ToValidUTF8(name, ""), `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' || r == '/' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		name = "announcement.pdf"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
		name += ".pdf"
	}
	if r := []rune(name); len(r) > 200 {
		name = string(r[:196]) + ".pdf"
	}
	return name
}

// AddDocument stores a PDF for (company, year).
func (s *Store) AddDocument(ctx context.Context, companyID int64, year int, filename, note string, data []byte) (*Document, error) {
	if year < 1900 || year > 9999 {
		return nil, invalid("invalid year %d", year)
	}
	if len(data) == 0 {
		return nil, invalid("file is empty")
	}
	if len(data) > MaxDocumentSize {
		return nil, invalid("file is larger than %d MB", MaxDocumentSize>>20)
	}
	// PDF header must appear within the first 1024 bytes.
	if !bytes.Contains(data[:min(len(data), 1024)], []byte("%PDF-")) {
		return nil, invalid("only PDF files can be uploaded")
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > 1000 {
		return nil, invalid("note is too long (max 1000 characters)")
	}
	var x int
	if err := s.DB.QueryRowContext(ctx, `SELECT 1 FROM companies WHERE id=?`, companyID).Scan(&x); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	sum := sha256.Sum256(data)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO company_documents(company_id, year, filename, content_type, size, sha256, note, data) VALUES(?,?,?,?,?,?,?,?)`,
		companyID, year, cleanFilename(filename), "application/pdf", len(data), hex.EncodeToString(sum[:]), note, data)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	d, err := scanDoc(s.DB.QueryRowContext(ctx, `SELECT `+docCols+` FROM company_documents WHERE id=?`, id))
	return &d, err
}

// Documents lists document metadata, newest year first. companyID 0 lists all.
func (s *Store) Documents(ctx context.Context, companyID int64) ([]Document, error) {
	q := `SELECT ` + docCols + ` FROM company_documents`
	var args []any
	if companyID != 0 {
		q += ` WHERE company_id=?`
		args = append(args, companyID)
	}
	q += ` ORDER BY company_id, year DESC, uploaded_at DESC, id DESC`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Document{}
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DocumentData returns a document's metadata and file contents.
func (s *Store) DocumentData(ctx context.Context, id int64) (*Document, []byte, error) {
	var d Document
	var data []byte
	err := s.DB.QueryRowContext(ctx, `SELECT `+docCols+`, data FROM company_documents WHERE id=?`, id).
		Scan(&d.ID, &d.CompanyID, &d.Year, &d.Filename, &d.ContentType, &d.Size, &d.SHA256, &d.Note, &d.UploadedAt, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return &d, data, nil
}

// DeleteDocument removes a document.
func (s *Store) DeleteDocument(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM company_documents WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
