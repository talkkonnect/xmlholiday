package report

import (
	"fmt"
	"regexp"
	"strings"
)

// Thai names for Thai holidays, ported from the web UI (web/app.js TH_NAMES);
// keep both lists in sync. Ordered most specific first.
var thNames = []struct {
	re *regexp.Regexp
	th string
}{
	{regexp.MustCompile(`bhumibol.*(birthday|father)|father'?s day|national day`), "วันคล้ายวันพระบรมราชสมภพ พระบาทสมเด็จพระบรมชนกาธิเบศร มหาภูมิพลอดุลยเดชมหาราช บรมนาถบพิตร วันชาติ และวันพ่อแห่งชาติ"},
	{regexp.MustCompile(`bhumibol`), "วันนวมินทรมหาราช"},
	{regexp.MustCompile(`chulalongkorn`), "วันปิยมหาราช"},
	{regexp.MustCompile(`sirikit.*anniversary|queen mother.*anniversary`), "วันคล้ายวันพระราชสมภพ สมเด็จพระบรมราชชนนีพันปีหลวง และวันแม่แห่งชาติ"},
	{regexp.MustCompile(`sirikit|queen mother|mother'?s day`), "วันเฉลิมพระชนมพรรษา สมเด็จพระบรมราชชนนีพันปีหลวง และวันแม่แห่งชาติ"},
	{regexp.MustCompile(`vajiralongkorn|king'?s birthday`), "วันเฉลิมพระชนมพรรษา พระบาทสมเด็จพระวชิรเกล้าเจ้าอยู่หัว"},
	{regexp.MustCompile(`suthida|queen'?s birthday`), "วันเฉลิมพระชนมพรรษา สมเด็จพระนางเจ้าสุทิดา พัชรสุธาพิมลลักษณ พระบรมราชินี"},
	{regexp.MustCompile(`chakri`), "วันจักรี"},
	{regexp.MustCompile(`songkran`), "วันสงกรานต์"},
	{regexp.MustCompile(`labou?r day`), "วันแรงงานแห่งชาติ"},
	{regexp.MustCompile(`coronation`), "วันฉัตรมงคล"},
	{regexp.MustCompile(`visakha|vesak|wisakha`), "วันวิสาขบูชา"},
	{regexp.MustCompile(`makha|magha`), "วันมาฆบูชา"},
	{regexp.MustCompile(`asa\w*ha\b|asalha|asahna`), "วันอาสาฬหบูชา"},
	{regexp.MustCompile(`end of (the )?buddhist lent|ok phansa|awk phansa`), "วันออกพรรษา"},
	{regexp.MustCompile(`buddhist lent|khao phansa`), "วันเข้าพรรษา"},
	{regexp.MustCompile(`constitution`), "วันรัฐธรรมนูญ"},
	{regexp.MustCompile(`new year'?s eve`), "วันสิ้นปี"},
	{regexp.MustCompile(`chinese new year|lunar new year`), "วันตรุษจีน"},
	{regexp.MustCompile(`new year`), "วันขึ้นปีใหม่"},
	{regexp.MustCompile(`loy krathong`), "วันลอยกระทง"},
	{regexp.MustCompile(`children'?s day`), "วันเด็กแห่งชาติ"},
	{regexp.MustCompile(`teacher'?s day`), "วันครู"},
	{regexp.MustCompile(`plou?ghing|plowing`), "วันพืชมงคล"},
	{regexp.MustCompile(`mid-?year`), "วันหยุดภาคครึ่งปีของสถาบันการเงิน"},
	{regexp.MustCompile(`special (public )?holiday|bridge (public )?holiday`), "วันหยุดพิเศษ"},
}

var (
	substRe    = regexp.MustCompile(`^(?:substitution (?:for|of)|substitute (?:for|holiday for)|day off for|compensatory (?:holiday )?for)\s+(.*)$`)
	trailParen = regexp.MustCompile(`\s*\([^)]*\)\s*$`)
	observedRe = regexp.MustCompile(`\(observed\)|\bobserved\b`)
	spaceRe    = regexp.MustCompile(`\s+`)
	thaiRe     = regexp.MustCompile(`[\x{0E00}-\x{0E7F}]`)
)

// thaiName returns the Thai name of an English holiday label, or "".
func thaiName(label string) string {
	l := strings.ToLower(label)
	l = strings.NewReplacer("’", "'", "‘", "'", "`", "'").Replace(l)
	l = strings.TrimSpace(spaceRe.ReplaceAllString(l, " "))
	if l == "" {
		return ""
	}
	prefix := ""
	if m := substRe.FindStringSubmatch(l); m != nil {
		prefix = "ชดเชย"
		l = trailParen.ReplaceAllString(m[1], "")
	} else if observedRe.MatchString(l) {
		prefix = "ชดเชย"
	}
	for _, n := range thNames {
		if n.re.MatchString(l) {
			return prefix + n.th
		}
	}
	return ""
}

// bilingualLabel returns "Thai\nEnglish". Labels already in Thai, or with no
// known Thai name, are returned unchanged.
func bilingualLabel(label string) string {
	if thaiRe.MatchString(label) {
		return label
	}
	if th := thaiName(label); th != "" {
		return th + "\n" + label
	}
	return label
}

var (
	thMonths   = []string{"ม.ค.", "ก.พ.", "มี.ค.", "เม.ย.", "พ.ค.", "มิ.ย.", "ก.ค.", "ส.ค.", "ก.ย.", "ต.ค.", "พ.ย.", "ธ.ค."}
	thWeekdays = []string{"วันอาทิตย์", "วันจันทร์", "วันอังคาร", "วันพุธ", "วันพฤหัสบดี", "วันศุกร์", "วันเสาร์"}
)

// bilingualDate: "1 ม.ค. 2570\n01 Jan 2027" (Thai Buddhist Era first).
func bilingualDate(s string) string {
	t, ok := parseDate(s)
	if !ok {
		return s
	}
	return fmt.Sprintf("%d %s %d\n%s", t.Day(), thMonths[t.Month()-1], t.Year()+543, t.Format("02 Jan 2006"))
}

// bilingualWeekday: "วันศุกร์\nFriday".
func bilingualWeekday(s string) string {
	t, ok := parseDate(s)
	if !ok {
		return ""
	}
	return thWeekdays[t.Weekday()] + "\n" + t.Format("Monday")
}
