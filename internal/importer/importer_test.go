package importer

import (
	"strings"
	"testing"
)

// Synthetic file in the shape described for 01-tod-group.xml.
const sample = `<include>
  <extension name="group_holidays_2026" continue="true">
    <!-- New Year -->
    <condition mday="01" mon="01" year="2026" break="never">
      <action application="set" data="jwd-working-time=false" inline="true" />
      <action application="set" data="dits-working-time=false" inline="true" />
    </condition>
    <condition mday="13-15" mon="04" year="2026" break="never">
      <action application="set" data="jwd-working-time=false" inline="true" />
      <action application="set" data="jwd-working-time=false" inline="true" />
    </condition>
    <condition wday="1">
      <action application="set" data="x=false"/>
    </condition>
    <condition mday="05" mon="12" year="2026" break="on-true">
      <action application="set" data="mespace-Ramkamhaeng-working-time=false" inline="true" />
      <action application="log" data="INFO hi" />
    </condition>
  </extension>
</include>`

func TestParse(t *testing.T) {
	r, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	j := r.Variables["jwd-working-time"]
	if len(j) != 4 || j[0].Date != "2026-01-01" || j[0].Label != "New Year" || j[3].Date != "2026-04-15" || j[1].Label != "" {
		t.Fatalf("jwd: %+v", j)
	}
	if len(r.Variables["dits-working-time"]) != 1 {
		t.Fatal("dits")
	}
	if len(r.Variables["mespace-Ramkamhaeng-working-time"]) != 1 {
		t.Fatal("case-sensitive variable lost")
	}
	if _, ok := r.Variables["x"]; ok {
		t.Fatal("non-date condition must be ignored")
	}
	if r.Breaks["never"] != 2 || r.Breaks["on-true"] != 1 {
		t.Fatalf("breaks %+v", r.Breaks)
	}
	if len(r.Skipped) != 2 {
		t.Fatalf("skipped %+v", r.Skipped)
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{
		`<include><condition mday="30" mon="02" year="2026"></condition></include>`,
		`<include><condition mday="aa" mon="02" year="2026"></condition></include>`,
		`<include><condition`,
	} {
		if _, err := Parse([]byte(bad)); err == nil || strings.TrimSpace(err.Error()) == "" {
			t.Fatalf("accepted %s", bad)
		}
	}
}
