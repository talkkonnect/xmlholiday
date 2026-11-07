package report

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/go-pdf/fpdf"
)

type rgb [3]int

var (
	cInk     = rgb{15, 23, 42}
	cMuted   = rgb{100, 116, 139}
	cLine    = rgb{203, 213, 225}
	cHead    = rgb{226, 232, 240}
	cPrimary = rgb{30, 64, 175}
	cWeekend = rgb{255, 247, 237}
	cWarn    = rgb{180, 83, 9}
)

const (
	fam     = "body"
	marginL = 12.0
	marginT = 18.0
	marginB = 16.0
	lineH   = 4.3
)

type renderer struct {
	pdf      *fpdf.Fpdf
	d        Data
	pageW    float64
	pageH    float64
	contentW float64
}

func newRenderer(d Data, opt Options) *renderer {
	p := fpdf.New("P", "mm", "A4", "")
	reg, bold := fontRegular, fontBold
	if len(opt.FontRegular) > 0 {
		reg, bold = opt.FontRegular, opt.FontRegular
		if len(opt.FontBold) > 0 {
			bold = opt.FontBold
		}
	}
	p.AddUTF8FontFromBytes(fam, "", reg)
	p.AddUTF8FontFromBytes(fam, "B", bold)
	p.SetMargins(marginL, marginT, marginL)
	p.SetAutoPageBreak(false, marginB)
	p.SetCreationDate(d.Generated)
	p.SetModificationDate(d.Generated)
	p.SetCatalogSort(true)
	p.SetTitle(fmt.Sprintf("Holiday Calendar Report %d", d.Year), true)
	p.SetCreator("xmlholiday", true)
	p.AliasNbPages("{nb}")
	w, h := p.GetPageSize()
	r := &renderer{pdf: p, d: d, pageW: w, pageH: h, contentW: w - 2*marginL}
	p.SetHeaderFuncMode(r.header, true)
	p.SetFooterFunc(r.footer)
	return r
}

func (r *renderer) font(style string, size float64) { r.pdf.SetFont(fam, style, size) }
func (r *renderer) text(c rgb)                      { r.pdf.SetTextColor(c[0], c[1], c[2]) }
func (r *renderer) fill(c rgb)                      { r.pdf.SetFillColor(c[0], c[1], c[2]) }
func (r *renderer) draw(c rgb)                      { r.pdf.SetDrawColor(c[0], c[1], c[2]) }
func (r *renderer) bottom() float64                 { return r.pageH - marginB }

func (r *renderer) header() {
	p := r.pdf
	r.font("", 7.5)
	r.text(cMuted)
	p.SetXY(marginL, 8)
	p.CellFormat(r.contentW/2, 4, fmt.Sprintf("Holiday Calendar Report · %d", r.d.Year), "", 0, "L", false, 0, "")
	p.CellFormat(r.contentW/2, 4, "Generated "+r.d.Generated.Format("2006-01-02 15:04"), "", 0, "R", false, 0, "")
	r.draw(cLine)
	p.SetLineWidth(0.2)
	p.Line(marginL, 13, r.pageW-marginL, 13)
	p.SetXY(marginL, marginT)
}

func (r *renderer) footer() {
	p := r.pdf
	r.font("", 7.5)
	r.text(cMuted)
	p.SetXY(marginL, r.pageH-10)
	p.CellFormat(r.contentW, 4, fmt.Sprintf("Page %d of {nb}", p.PageNo()), "", 0, "R", false, 0, "")
}

// width measures s in mm with the current font. Combining marks (Thai vowels
// and tone marks above/below) are zero-width; fpdf would count them as 65535
// units in SplitText, so wrapping is done here instead.
func (r *renderer) width(s string) float64 {
	b := make([]rune, 0, len(s))
	for _, c := range s {
		if !unicode.Is(unicode.Mn, c) {
			b = append(b, c)
		}
	}
	return r.pdf.GetStringWidth(string(b))
}

// lines word-wraps txt to width w (mm); explicit newlines are kept. Words
// longer than a line are broken between characters, never before a
// combining mark.
func (r *renderer) lines(txt string, w float64) []string {
	w -= 2 * 1.0 // fpdf default cell margin is 1 mm each side
	var out []string
	for _, para := range strings.Split(txt, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			cand := word
			if line != "" {
				cand = line + " " + word
			}
			if r.width(cand) <= w {
				line = cand
				continue
			}
			if line != "" {
				out = append(out, line)
				line = ""
			}
			for r.width(word) > w { // break an over-long word
				rs := []rune(word)
				cut := 1
				for i := 1; i <= len(rs); i++ {
					if i < len(rs) && unicode.Is(unicode.Mn, rs[i]) {
						continue
					}
					if r.width(string(rs[:i])) > w {
						break
					}
					cut = i
				}
				out = append(out, string(rs[:cut]))
				word = string(rs[cut:])
			}
			line = word
		}
		out = append(out, line)
	}
	return out
}

// para writes wrapped text across the content width.
func (r *renderer) para(txt, style string, size float64, c rgb) {
	r.font(style, size)
	r.text(c)
	lh := size * 0.5
	for _, l := range r.lines(txt, r.contentW) {
		if r.pdf.GetY()+lh > r.bottom() {
			r.pdf.AddPage()
		}
		r.pdf.SetX(marginL)
		r.pdf.CellFormat(r.contentW, lh, l, "", 1, "L", false, 0, "")
	}
}

type col struct {
	title string
	w     float64 // fraction of content width
	align string
}

func (r *renderer) tableHeader(cols []col) {
	p := r.pdf
	r.font("B", 9)
	r.text(cInk)
	r.fill(cHead)
	r.draw(cLine)
	x, y := marginL, p.GetY()
	for _, c := range cols {
		w := c.w * r.contentW
		p.Rect(x, y, w, 6, "FD")
		p.SetXY(x, y+1)
		p.CellFormat(w, 4, c.title, "", 0, c.align, false, 0, "")
		x += w
	}
	p.SetXY(marginL, y+6)
}

// holidayTable lists the days; weekend rows are tinted.
var holidayCols = []col{{"#", 0.05, "R"}, {"วันที่ / Date", 0.15, "L"}, {"วัน / Day", 0.13, "L"}, {"วันหยุด / Holiday", 0.67, "L"}}

// holidayRows wraps the table cells at font scale sc and returns the lines
// per cell and the height of each row.
func (r *renderer) holidayRows(days []Day, sc float64) ([][][]string, []float64) {
	cols := holidayCols
	cells := make([][][]string, len(days))
	hs := make([]float64, len(days))
	r.font("", 8.5*sc)
	for i, x := range days {
		txt := []string{fmt.Sprint(i + 1), bilingualDate(x.Date), bilingualWeekday(x.Date), bilingualLabel(x.Label)}
		cells[i] = make([][]string, len(cols))
		n := 1
		for j := range cols {
			cells[i][j] = r.lines(txt[j], cols[j].w*r.contentW-1)
			n = max(n, len(cells[i][j]))
		}
		hs[i] = float64(n)*3.9*sc + 1.4*sc
	}
	return cells, hs
}

// holidayTable lists the days at font scale sc; weekend rows are tinted.
func (r *renderer) holidayTable(days []Day, sc float64) {
	p := r.pdf
	lh := 3.9 * sc
	cols := holidayCols
	all, hs := r.holidayRows(days, sc)
	r.tableHeader(cols)
	p.SetLineWidth(0.15)
	for i, x := range days {
		cl, h := all[i], hs[i]
		if p.GetY()+h > r.bottom() {
			p.AddPage()
			r.tableHeader(cols)
		}
		weekend := isWeekend(x.Date)
		xx, y := marginL, p.GetY()
		for j, c := range cols {
			w := c.w * r.contentW
			r.draw(cLine)
			if weekend {
				r.fill(cWeekend)
				p.Rect(xx, y, w, h, "FD")
			} else {
				p.Rect(xx, y, w, h, "D")
			}
			r.text(cInk)
			r.font("", 8.5*sc)
			if j == 2 && weekend {
				r.text(cWarn)
				r.font("B", 8.5*sc)
			}
			for k, l := range cl[j] {
				p.SetXY(xx, y+0.7*sc+float64(k)*lh)
				p.CellFormat(w, lh, l, "", 0, c.align, false, 0, "")
			}
			xx += w
		}
		p.SetXY(marginL, y+h)
	}
	p.Ln(4)
}

func (r *renderer) build() error {
	pages := map[int]int{} // company index -> page
	for i, c := range r.d.Companies {
		if c.Active && hasHolidays(r.d, c) {
			pages[i] = r.companyPage(c)
		}
	}
	r.summaryPage(pages)
	return r.pdf.Error()
}

// table draws a simple wrapped table, repeating the header on page breaks.
func (r *renderer) table(cols []col, rows [][]string) {
	p := r.pdf
	if p.GetY()+6+lineH+2 > r.bottom() {
		p.AddPage()
	}
	r.tableHeader(cols)
	p.SetLineWidth(0.15)
	for _, row := range rows {
		r.font("", 9)
		cl := make([][]string, len(cols))
		n := 1
		for j := range cols {
			cl[j] = r.lines(row[j], cols[j].w*r.contentW-1)
			n = max(n, len(cl[j]))
		}
		h := float64(n)*lineH + 1.6
		if p.GetY()+h > r.bottom() {
			p.AddPage()
			r.tableHeader(cols)
		}
		x, y := marginL, p.GetY()
		r.text(cInk)
		r.draw(cLine)
		for j, c := range cols {
			w := c.w * r.contentW
			p.Rect(x, y, w, h, "D")
			for k, l := range cl[j] {
				p.SetXY(x, y+0.8+float64(k)*lineH)
				p.CellFormat(w, lineH, l, "", 0, c.align, false, 0, "")
			}
			x += w
		}
		p.SetXY(marginL, y+h)
	}
	p.Ln(4)
}

func (r *renderer) sectionTitle(t string) {
	if r.pdf.GetY()+20 > r.bottom() {
		r.pdf.AddPage()
	}
	r.para(t, "B", 12, cPrimary)
	r.pdf.Ln(1.5)
}

// summaryPage is the last page: overview of active companies, problems
// found, and the inactive companies.
func (r *renderer) summaryPage(pages map[int]int) {
	p := r.pdf
	d := r.d
	p.AddPage()
	p.Bookmark("Summary", 0, -1)
	r.font("B", 16)
	r.text(cInk)
	p.SetX(marginL)
	p.CellFormat(r.contentW, 8, "Summary", "", 1, "L", false, 0, "")
	r.para(fmt.Sprintf("Holidays %d · saved data only", d.Year), "", 10, cMuted)
	if d.Filtered {
		r.para(fmt.Sprintf("Selected companies only: %d of %d registered companies are included in this report.", len(d.Companies), d.TotalCompanies), "B", 10, cPrimary)
	}
	p.Ln(3)

	var active, inactive [][]string
	for i, c := range d.Companies {
		var vars, counts []string
		for _, g := range groups(d, c) {
			vars = append(vars, g.Variables...)
			if !g.HasCal {
				counts = append(counts, "no calendar")
			} else {
				counts = append(counts, fmt.Sprint(len(g.Days)))
			}
		}
		if len(vars) == 0 {
			vars, counts = []string{"(none)"}, []string{"—"}
		}
		if c.Active {
			pg := "—"
			if n, ok := pages[i]; ok {
				pg = fmt.Sprint(n)
			}
			active = append(active, []string{c.Name, strings.Join(vars, "\n"), strings.Join(counts, " / "), pg})
		} else {
			inactive = append(inactive, []string{c.Name, strings.Join(vars, "\n")})
		}
	}

	r.sectionTitle(fmt.Sprintf("Active companies (%d)", len(active)))
	if len(active) == 0 {
		r.para("None.", "", 9, cMuted)
		p.Ln(3)
	} else {
		r.table([]col{{"Company", 0.32, "L"}, {"Variables", 0.46, "L"}, {"Holidays", 0.13, "C"}, {"Page", 0.09, "C"}}, active)
	}

	r.sectionTitle("Needs attention")
	if is := issues(d); len(is) == 0 {
		r.para("Nothing found: every active company has a variable with saved holidays.", "", 9, cMuted)
		p.Ln(3)
	} else {
		rows := make([][]string, len(is))
		for i, x := range is {
			rows[i] = []string{x[0], x[1]}
		}
		r.table([]col{{"Company", 0.4, "L"}, {"Problem", 0.6, "L"}}, rows)
	}

	r.sectionTitle(fmt.Sprintf("Inactive companies (%d) — not written to the XML", len(inactive)))
	if len(inactive) == 0 {
		r.para("None.", "", 9, cMuted)
	} else {
		r.table([]col{{"Company", 0.45, "L"}, {"Variables", 0.55, "L"}}, inactive)
	}
}

// hasHolidays reports whether the company has a variable with saved holidays
// (only those companies get a page; problems go to the summary).
func hasHolidays(d Data, c Company) bool {
	for _, g := range groups(d, c) {
		if len(g.Days) > 0 {
			return true
		}
	}
	return false
}

// issues lists [company, problem] pairs for active companies.
func issues(d Data) [][2]string {
	var out [][2]string
	for _, c := range d.Companies {
		if !c.Active {
			continue
		}
		gs := groups(d, c)
		if len(gs) == 0 {
			out = append(out, [2]string{c.Name, "No FreeSWITCH variable assigned, so no holidays are set."})
		}
		for _, g := range gs {
			switch {
			case !g.HasCal:
				out = append(out, [2]string{c.Name, fmt.Sprintf("No calendar saved for %d: %s", d.Year, strings.Join(g.Variables, ", "))})
			case len(g.Days) == 0:
				out = append(out, [2]string{c.Name, "Calendar has no holidays selected: " + strings.Join(g.Variables, ", ")})
			}
		}
	}
	return out
}

// groupsHeight estimates the height of the variable headings and holiday
// tables at font scale sc (mirrors companyPage / holidayTable).
func (r *renderer) groupsHeight(gs []group, sc float64) float64 {
	h := 0.0
	for _, g := range gs {
		r.font("B", 10*sc)
		h += float64(len(r.lines("Variable: "+strings.Join(g.Variables, ", "), r.contentW)))*5*sc + sc
		h += 8.5*sc*0.5 + sc
		h += 6 // table header
		_, hs := r.holidayRows(g.Days, sc)
		for _, x := range hs {
			h += x
		}
		h += 4 // space after the table
	}
	return h
}

// companyPage renders one active company and returns its page number.
func (r *renderer) companyPage(c Company) int {
	p := r.pdf
	p.AddPage()
	page := p.PageNo()
	p.Bookmark(c.Name, 0, -1)
	r.font("B", 16)
	r.text(cInk)
	for _, l := range r.lines(c.Name, r.contentW) {
		p.SetX(marginL)
		p.CellFormat(r.contentW, 8, l, "", 1, "L", false, 0, "")
	}
	r.para(fmt.Sprintf("วันหยุดประจำปี %d / Holidays %d", r.d.Year+543, r.d.Year), "", 10, cMuted)
	p.Ln(2)
	var gs []group
	for _, g := range groups(r.d, c) {
		if len(g.Days) > 0 { // no calendar / no holidays: listed on the summary page
			gs = append(gs, g)
		}
	}
	// Shrink text only as much as needed to keep the company on one page.
	sc := 1.0
	for avail := r.bottom() - p.GetY(); sc > 0.7 && r.groupsHeight(gs, sc) > avail; sc -= 0.03 {
	}
	for _, g := range gs {
		r.para("Variable: "+strings.Join(g.Variables, ", "), "B", 10*sc, cPrimary)
		p.Ln(sc)
		r.para(fmt.Sprintf("%d วันหยุด / holiday(s)", len(g.Days)), "", 8.5*sc, cMuted)
		p.Ln(sc)
		r.holidayTable(g.Days, sc)
	}
	return page
}
