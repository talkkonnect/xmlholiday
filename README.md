# xmlholiday — FreeSWITCH Holiday Calendar & XML Generator

A small Go web application that manages Thai holiday calendars for FreeSWITCH
working-time variables and generates **one consolidated holiday-override XML file**
per year. It only *sets existing working-time variables to `false`* on holidays.
It never touches ordinary working hours, weekends or routing, and it **never
contacts, deploys to, or reloads FreeSWITCH**. You download the XML and deploy it yourself.

* Single static binary (UI embedded, no CDN). Works offline with saved data.
* Storage: local MariaDB (`root` / `bj4free` by default; database `xmlholiday` is created automatically).
* Holiday suggestions come through pluggable providers. The official Bank of Thailand source is preferred.

## Quick start

```bash
cd ~/gocode/src/github.com/talkkonnect/xmlholiday
go build -o xmlholiday .
./xmlholiday seed            # optional: sample JWD / MeSpace (Patong, Ramkamhaeng) registry, no holiday dates
./xmlholiday serve           # http://<host>:8086/
```

Configuration (flags or environment):

| flag | env | default |
|------|-----|---------|
| `-dsn` | `XMLH_DSN` | `root:bj4free@tcp(127.0.0.1:3306)/xmlholiday?charset=utf8mb4` |
| `-listen` | `XMLH_LISTEN` | `:8086` |

Other commands:

```bash
./xmlholiday import -company "JWD Group" 01-tod-group.xml   # import an existing FreeSWITCH tod file
./xmlholiday xml -year 2027 -out freeswitch_holidays_2027.xml # generate from saved data (CLI)
./xmlholiday report -year 2027 [-out report.pdf]              # PDF holiday report (see below)
```

`import` reads `<condition mday mon year>` blocks with `set VAR=false` actions, expands ranges such as `mday="13-15"`,
preserves variable case, takes the preceding XML comment as the label, and reports the `break` values it found.
Unknown variables are registered under `-company`. The two reference files (`01-tod-group.xml`, `01-tod-mespace.xml`)
were not on this server, so seed data comes from their documented variable names only. Import the real files with the
command above to load their dates. `samples/example-tod-input.xml` is a **synthetic** file for trying it out.

## Workflow

1. **Screen A — Companies & Variables.** Add companies, optional branches, and the exact FreeSWITCH variable for each
   company or branch. Edits are kept as a draft until you click **Save**. Variables used by more than one assignment show
   a **Shared calendar ×N** badge with the linked assignments. You are warned before attaching an assignment to, or
   detaching it from, a shared variable. Deactivate instead of delete.
2. **Screen B — build the year's standard template.** Pick the year and choose **★ Standard template**. Click
   **Load Thai Holiday Suggestions**, review the left-hand list (date, name, source link, status), move approved dates
   right, and click **Save**. Saving creates a calendar for every registered variable that has none for that year, as a
   **copy** of the template.
3. **Per-variable editing.** Select company → branch → variable to load that variable's calendar. Move dates left and
   right (click to highlight, shift-click for a range, double-click to move), use **Select All** / **Remove All**, add
   custom dates with the date picker or by clicking an empty calendar day, or use **Copy From** another variable or year.
   An unsaved-changes indicator is shown, and switching selection, year or screen asks before discarding.
4. **Template changes later** never alter existing calendars silently. After saving a template with new dates you are
   offered **Apply to variables…**, which previews per-variable additions and requires an explicit save. Dates that a
   calendar explicitly removed (manual exclusions) are never overwritten.
5. **Preview XML / Download XML** always use **saved** data and offer to save pending edits first. The summary shows
   unique variables, selected dates, XML blocks and actions. Downloading an empty wrapper requires confirmation.

## Holiday sources

| provider | status | notes |
|----------|--------|-------|
| `bot` — Bank of Thailand *Financial Institutions Holidays* JSON (`…/holidaycalendar.model.{year}.json`) | **official** | Weekday bank holidays, including substitution and special announced holidays. The years currently published are 2026 and 2027; other years return a clear "not published" error. |
| `ics` — Google public "Holidays in Thailand" calendar | **unverified** | Only events marked "Public holiday" are used. It also lists weekend-falling holidays (e.g. Makha Bucha on Sunday). Never treated as official. |

Suggestions are merged per date. An official source wins, and other sources are listed in "also listed by".
Results are cached in the database for offline use. On failure the UI shows each provider's error and falls back to
the cache. Nothing is ever invented. If nothing is available, enter dates manually. Provider choice and URLs are under
**⚙ Settings**. Approving non-official dates into a template asks for explicit confirmation.

## XML output

```xml
<include>
  <!-- Generated by xmlholiday for 2027. ... -->
  <!-- break="never" is a PROVISIONAL setting; ... -->
  <extension name="holidays_2027" continue="true">
    <!-- New Year’s Day -->
    <condition mday="01" mon="01" year="2027" break="never">
      <action application="set" data="jwd-working-time=false" inline="true" />
      <action application="set" data="mespace-Ramkamhaeng-working-time=false" inline="true" />
    </condition>
  </extension>
</include>
```

* One `<condition>` per date (no range compression), dates sorted chronologically, variables sorted by exact byte order.
* Only **active** variables are included: an active assignment whose company and branch are active.
* There are no duplicate actions. Shared variables produce one action. No `=true` actions are ever generated.
* The label comment is emitted only when all contributing calendars agree on the label.
* Output is byte-for-byte deterministic (no timestamps). The file name is `freeswitch_holidays_YEAR.xml`.
* See `samples/freeswitch_holidays_2027.xml`, generated from the seed registry and the official BOT 2027 list.

### ⚠ Open compatibility issue (AT-15) — break semantics are NOT validated

The reference group file uses `break="never"` and the MeSpace file uses `break="on-true"`. This prototype emits
`break="never"` by default (configurable in **⚙ Settings**: never / on-true / on-false / always). **This has not been
tested against FreeSWITCH.** Before production use, on a test FreeSWITCH with the deployed dialplan:

1. Include the generated file where the existing tod files are included, then run `reloadxml`.
2. Place test calls with the system date (or a test `year/mon/mday` condition) set to:
   a holiday shared by several variables, a holiday for one variable only, and a non-holiday weekday.
3. Verify with `uuid_dump` / `info` / `eval ${jwd-working-time}` that each variable is `false` only on its own
   holidays, that unaffected variables keep their normal value, and that later extensions (`continue="true"`) still run.
4. Repeat with `break="on-true"` if the MeSpace dialplan depends on it.

## Calendar report (PDF)

Screen B has a **Calendar Report (PDF)** button (or `./xmlholiday report -year 2027`). It opens a dialog where you
choose **All companies** or **Specific companies** (checklist with a filter box; the choice is remembered while the
page is open). From the command line, `-company "JWD,MeSpace"` limits the report to those companies (names,
case-insensitive); the API takes `GET /api/report/pdf?year=2027&companies=1,5` (company ids, omitted = all). A
filtered report says so on its summary page, and the summary covers only the chosen companies. It prints **one page per
active company that has holidays**, for the selected year, from **saved** data only. Each page shows the company name,
its variable(s) and branch(es), and a table of holidays. Date, weekday and holiday name are shown in **Thai first, then
English** (e.g. `1 ม.ค. 2570` / `01 Jan 2027`, `วันศุกร์` / `Friday`, `วันขึ้นปีใหม่` / `New Year’s Day`; Thai names
use the same mapping as the UI, and labels without a known Thai name are shown in English only). Weekend holidays are
tinted. Variables of one company with identical holidays share one table, and the text shrinks slightly when needed so
each company fits on one page.

The **last page is the summary**: every active company with its variables, holiday count and page number; a
**Needs attention** list (active company with no variable, or a variable with no saved calendar or no holidays — these
companies get no page of their own); and the **inactive companies**, which are not written to the XML.

The embedded font is Sarabun (Thai + Latin, SIL Open Font License, `internal/report/fonts/`). `XMLH_REPORT_FONT`
(and optionally `XMLH_REPORT_FONT_BOLD`) can point to another TrueType (`.ttf`) font.

## Holiday announcement documents

On screen A each saved company has a **Docs** button. Upload the company's official holiday
announcement (PDF) for each year so it can be consulted if a calendar is disputed later. Several
PDFs per company/year are allowed (e.g. a revised circular). Files are stored in MariaDB
(`company_documents`, with size and SHA-256) so they are included in normal database backups.
They are reference only and never change calendars or the generated XML.

## API

```
GET/POST /api/registry                         full registry; POST = transactional batch upsert (negative ids = new)
GET/POST /api/companies | /api/branches | /api/assignments
GET      /api/variables                        variables with assignments and shared flag
GET      /api/calendars                        index of saved calendars
GET/PUT  /api/calendars/{year}/{variable}      PUT {version, dates[]} (optimistic locking, 409 on conflict)
GET      /api/holiday-suggestions?year=        live fetch (falls back to cache), &cached=1 for cache only
GET/POST /api/templates/{year}                 approved standard template
POST     /api/bulk-apply                       {year, dates, variables, commit:false|true}
GET      /api/xml/preview?year=                {xml, summary}
GET      /api/xml/download?year=[&confirm_empty=1]
GET      /api/report/pdf?year=[&companies=1,5][&download=1]  PDF holiday report (all or chosen companies): page per active company + summary
GET/PUT  /api/settings                         break, providers, bot_url, ics_url, default_year
GET      /api/documents[?company_id=]            holiday-announcement PDF metadata (per company/year)
POST     /api/companies/{id}/documents         multipart: file (PDF, max 10 MB), year, note
GET      /api/documents/{id}[?download=1]      the stored PDF (inline, or attachment)
DELETE   /api/documents/{id}
```

## Tests

```bash
go test ./...                                  # unit + integration
XMLH_TEST_DSN=skip go test ./...               # skip DB integration tests
```

* `internal/xmlgen`: a pure generator (no I/O). Tests cover AT-09 to AT-12, escaping, leap years, year boundaries,
  invalid input and the empty wrapper.
* `internal/holidays`: BOT and ICS parsers against real captured fixtures, merge rules, and failure handling.
* `internal/importer`: parsing of FreeSWITCH tod files.
* `internal/server`: end-to-end HTTP API tests against MariaDB. They use and wipe database `xmlholiday_test`.
  Tests cover AT-01 to AT-04, AT-06 to AT-13, template copy semantics, bulk apply with manual exclusions,
  optimistic-lock conflicts, case sensitivity and variable rename.
* AT-14: the code has no FreeSWITCH client of any kind. AT-15: see the checklist above. It is manual by design.

## Layout

```
main.go                     CLI: serve | seed | import | xml | report
internal/xmlgen             XML generator (pure function)
internal/report             PDF holiday report: page per active company + summary (pure)
internal/holidays           provider adapters (BOT official, ICS unverified) + merge
internal/store              MariaDB schema & persistence
internal/server             JSON API + XML build from saved data
internal/importer           FreeSWITCH tod XML importer
web/                        embedded UI (index.html, app.js, style.css)
samples/                    sample output and synthetic import input
```
