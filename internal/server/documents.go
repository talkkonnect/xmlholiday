package server

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/talkkonnect/xmlholiday/internal/store"
)

// Company holiday-announcement PDFs, kept per company and year as reference
// evidence. They never affect the generated XML.

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, &store.ValidationError{Msg: fmt.Sprintf("invalid id %q", r.PathValue("id"))}
	}
	return id, nil
}

// GET /api/documents[?company_id=N] — metadata only.
func (s *Server) getDocuments(w http.ResponseWriter, r *http.Request) {
	var cid int64
	if v := r.URL.Query().Get("company_id"); v != "" {
		var err error
		if cid, err = strconv.ParseInt(v, 10, 64); err != nil || cid <= 0 {
			badRequest(w, "invalid company_id %q", v)
			return
		}
	}
	docs, err := s.Store.Documents(r.Context(), cid)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, docs)
}

// POST /api/companies/{id}/documents — multipart: file, year, note.
func (s *Server) postDocument(w http.ResponseWriter, r *http.Request) {
	cid, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, store.MaxDocumentSize+1<<20)
	if err := r.ParseMultipartForm(store.MaxDocumentSize + 1<<20); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			badRequest(w, "file is larger than %d MB", store.MaxDocumentSize>>20)
			return
		}
		badRequest(w, "invalid upload: %v", err)
		return
	}
	defer r.MultipartForm.RemoveAll()
	year, err := parseYear(r.FormValue("year"))
	if err != nil {
		writeErr(w, err)
		return
	}
	f, fh, err := r.FormFile("file")
	if err != nil {
		badRequest(w, "a PDF file is required")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, store.MaxDocumentSize+1))
	if err != nil {
		writeErr(w, err)
		return
	}
	doc, err := s.Store.AddDocument(r.Context(), cid, year, fh.Filename, r.FormValue("note"), data)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, doc)
}

// GET /api/documents/{id}[?download=1] — serves the PDF (inline by default).
func (s *Server) getDocument(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	doc, data, err := s.Store.DocumentData(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	disp := "inline"
	if r.URL.Query().Get("download") == "1" {
		disp = "attachment"
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": doc.Filename}))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", `"`+doc.SHA256+`"`)
	w.Write(data)
}

// DELETE /api/documents/{id}
func (s *Server) deleteDocument(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Store.DeleteDocument(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
