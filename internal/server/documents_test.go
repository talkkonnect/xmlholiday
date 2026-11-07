package server

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/talkkonnect/xmlholiday/internal/store"
)

func (e *env) upload(companyID int64, year, filename, note string, data []byte) (int, string) {
	e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("year", year)
	mw.WriteField("note", note)
	fw, _ := mw.CreateFormFile("file", filename)
	fw.Write(data)
	mw.Close()
	resp, err := http.Post(fmt.Sprintf("%s/api/companies/%d/documents", e.srv.URL, companyID), mw.FormDataContentType(), &buf)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestCompanyDocuments(t *testing.T) {
	e := setup(t)
	r := e.registerSample()
	jwd, dits := r.IDMap["-1"], r.IDMap["-3"]
	pdf := []byte("%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n")

	if code, body := e.upload(jwd, "2027", `C:\HR\JWD holidays "2027".pdf`, " HR circular 01 ", pdf); code != 200 {
		t.Fatalf("upload: %d %s", code, body)
	}
	if code, body := e.upload(jwd, "2026", "old.pdf", "", pdf); code != 200 {
		t.Fatalf("upload 2026: %d %s", code, body)
	}
	if code, body := e.upload(dits, "2027", "dits", "", pdf); code != 200 {
		t.Fatalf("upload dits: %d %s", code, body)
	}

	// rejected inputs
	for name, c := range map[string]struct {
		cid  int64
		year string
		data []byte
		want int
	}{
		"not pdf":         {jwd, "2027", []byte("hello world"), 400},
		"empty":           {jwd, "2027", nil, 400},
		"bad year":        {jwd, "27", pdf, 400},
		"unknown company": {999999, "2027", pdf, 404},
		"too large":       {jwd, "2027", append(append([]byte{}, pdf...), make([]byte, store.MaxDocumentSize)...), 400},
	} {
		if code, body := e.upload(c.cid, c.year, "x.pdf", "", c.data); code != c.want {
			t.Errorf("%s: got %d want %d: %s", name, code, c.want, body)
		}
	}

	var docs []store.Document
	e.must("GET", fmt.Sprintf("/api/documents?company_id=%d", jwd), nil, &docs)
	if len(docs) != 2 || docs[0].Year != 2027 || docs[1].Year != 2026 {
		t.Fatalf("jwd docs: %+v", docs)
	}
	d := docs[0]
	if d.Filename != "JWD holidays 2027.pdf" || d.Note != "HR circular 01" || d.Size != int64(len(pdf)) || len(d.SHA256) != 64 {
		t.Fatalf("metadata: %+v", d)
	}
	e.must("GET", "/api/documents", nil, &docs)
	if len(docs) != 3 {
		t.Fatalf("all docs: %d", len(docs))
	}

	resp, err := http.Get(fmt.Sprintf("%s/api/documents/%d?download=1", e.srv.URL, d.ID))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Equal(got, pdf) || resp.Header.Get("Content-Type") != "application/pdf" ||
		!strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("download: %d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Content-Disposition"))
	}

	if code := e.do("DELETE", fmt.Sprintf("/api/documents/%d", d.ID), nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	if code := e.do("DELETE", fmt.Sprintf("/api/documents/%d", d.ID), nil, nil); code != 404 {
		t.Fatalf("second delete: %d", code)
	}
	if code := e.do("GET", fmt.Sprintf("/api/documents/%d", d.ID), nil, nil); code != 404 {
		t.Fatalf("get deleted: %d", code)
	}
}
