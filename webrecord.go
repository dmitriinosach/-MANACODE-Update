package main

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const webHour = 3600

type recSeg struct {
	lua     string
	t0, t1  float64
	day     string
	id      int64
	hasRaid bool
	raidMap string
	bosses  []string
	totals  bool
	kill    bool
}

type webRecord struct {
	top  map[string]string
	segs []recSeg
}

var (
	reTopKey   = regexp.MustCompile(`^\t\["(\w+)"\] = (.*)$`)
	reLuaStr   = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	reSegNum   = regexp.MustCompile(`^\["(t0|t1|v)"\]\s*=\s*([\d.]+)`)
	reSegBoss  = regexp.MustCompile(`^\["(.+?)"\]\s*=\s*true`)
	reSegRaid  = regexp.MustCompile(`^\["(\w+)"\]\s*=\s*(?:"(.*)"|([-\d.]+)|(true|false)),?$`)
	reSegTail  = regexp.MustCompile(`,?\s*(--\s*\[\d+\])?\s*$`)
	reSegField = regexp.MustCompile(`^\t\t\t\["(\w+)"\] = (.*)$`)
	reSegClose = regexp.MustCompile(`^\t\t\t\},?$`)
	reSpellsF  = regexp.MustCompile(`\n\t\t\t\["spells"\] = "((?:[^"\\]|\\.)*)"`)
	reLootRow  = regexp.MustCompile(`\{([^{}]*)\}`)
)

var webTopKeys = []string{"classes", "pets", "fx", "fxVersion", "fxUser", "fmt", "gp", "gpIssued", "faults", "settings", "loot",
	"prices", "ahPrices", "finals", "flaskLog"}

var webRawKeys = map[string]bool{"actors": true, "chunks": true, "hp": true, "mana": true, "pos": true, "maps": true,
	"spells": true, "strs": true, "buf": true}

func readRecord(text string) webRecord {
	rec := webRecord{top: map[string]string{}}
	var (
		topKey string
		topBuf []string
		inSegs bool
		depth  int
		cur    *recSeg
		v      int
		mode   string
		start  int
	)
	for pos := 0; pos < len(text); {
		end := strings.IndexByte(text[pos:], '\n')
		next := len(text)
		if end < 0 {
			end = len(text) - pos
		} else {
			next = pos + end + 1
		}
		raw := text[pos : pos+end]
		line := strings.TrimSuffix(raw, "\r")
		lineAt := pos
		pos = next
		if topKey != "" {
			topBuf = append(topBuf, line)
			if line == "\t}," || line == "\t}" {
				rec.top[topKey] = strings.TrimRight(strings.TrimSuffix(strings.TrimRight(strings.Join(topBuf, "\n"), " \t"), ","), " \t")
				topKey, topBuf = "", nil
			}
			continue
		}
		if !inSegs {
			m := reTopKey.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			switch {
			case m[1] == "segments" && m[2] == "{":
				inSegs, depth, cur = true, 0, nil
			case m[2] == "{":
				topKey, topBuf = m[1], []string{"{"}
			default:
				rec.top[m[1]] = strings.TrimRight(strings.TrimSuffix(strings.TrimRight(m[2], " \t"), ","), " \t")
			}
			continue
		}
		t := strings.TrimSpace(line)
		if depth == 0 {
			if t == "{" {
				cur, v, mode, depth, start = &recSeg{}, 0, "", 1, lineAt
			} else if strings.HasPrefix(t, "}") {
				inSegs = false
			}
			continue
		}
		bare := t
		if strings.IndexByte(t, '"') >= 0 {
			bare = reLuaStr.ReplaceAllString(t, "")
		}
		delta := strings.Count(bare, "{") - strings.Count(bare, "}")
		if depth == 1 {
			switch {
			case strings.HasPrefix(t, `["raid"] = {`):
				mode, cur.hasRaid = "raid", true
				depth += delta
				continue
			case strings.HasPrefix(t, `["bosses"] = {`):
				mode = "bosses"
				depth += delta
				continue
			case strings.HasPrefix(t, `["totals"] = {`):
				mode, cur.totals = "skip", true
				depth += delta
				continue
			case delta > 0:
				mode = "skip"
				depth += delta
				continue
			}
			if m := reSegNum.FindStringSubmatch(t); m != nil {
				f, _ := strconv.ParseFloat(m[2], 64)
				switch m[1] {
				case "t0":
					cur.t0 = f
				case "t1":
					cur.t1 = f
				case "v":
					v = int(f)
				}
				continue
			}
		}
		depth += delta
		if depth <= 0 {
			if v == 2 {
				cur.lua = strings.TrimSpace(reSegTail.ReplaceAllString(strings.TrimSpace(text[start:lineAt+len(raw)]), ""))
				rec.segs = append(rec.segs, *cur)
			}
			depth, cur, mode = 0, nil, ""
			continue
		}
		if depth == 1 {
			mode = ""
			continue
		}
		if strings.Contains(t, `["killed"] = true`) {
			cur.kill = true
		}
		switch {
		case mode == "bosses":
			if m := reSegBoss.FindStringSubmatch(t); m != nil {
				cur.bosses = append(cur.bosses, m[1])
			}
		case mode == "raid" && depth == 2:
			if m := reSegRaid.FindStringSubmatch(t); m != nil {
				switch m[1] {
				case "day":
					cur.day = m[2]
				case "map":
					cur.raidMap = m[2]
				case "id":
					cur.id, _ = strconv.ParseInt(m[3], 10, 64)
				}
			}
		}
	}
	return rec
}

type raidRange struct {
	day      string
	id       int64
	from, to int
}

func pickRaid(segs []recSeg, p discordPost, day string) (raidRange, bool) {
	if day == "" && p.Start > 0 {
		for _, s := range segs {
			if s.day != "" && s.t0 >= float64(p.Start-webHour) && s.t0 <= float64(p.Finish+webHour) {
				day = s.day
			}
		}
	}
	if day == "" {
		for _, s := range segs {
			if s.day != "" && len(s.bosses) > 0 {
				day = s.day
			}
		}
	}
	if day == "" {
		return raidRange{}, false
	}
	id, err := strconv.ParseInt(p.ID, 10, 64)
	if err != nil || id <= 0 {
		id = 0
		for _, s := range segs {
			if s.day == day && s.id != 0 && len(s.bosses) > 0 {
				id = s.id
			}
		}
	}
	lo, hi := -1, -1
	for i, s := range segs {
		if s.day == day && (id == 0 || s.id == id) {
			if lo < 0 {
				lo = i
			}
			hi = i
		}
	}
	if lo < 0 {
		return raidRange{}, false
	}
	for lo > 0 && segs[lo-1].hasRaid && segs[lo-1].day == day && segs[lo-1].id == 0 && segs[lo].t0-segs[lo-1].t1 <= webHour {
		lo--
	}
	for lo > 0 && !segs[lo-1].hasRaid && segs[lo].t0-segs[lo-1].t1 <= webHour {
		lo--
	}
	for hi < len(segs)-1 && !segs[hi+1].hasRaid && segs[hi+1].t0-segs[hi].t1 <= webHour {
		hi++
	}
	return raidRange{day: day, id: id, from: lo, to: hi}, true
}

func stripSeg(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		if skip {
			if reSegClose.MatchString(line) {
				skip = false
			}
			continue
		}
		if m := reSegField.FindStringSubmatch(line); m != nil {
			switch {
			case webRawKeys[m[1]]:
				out = append(out, "\t\t\t[\""+m[1]+"\"] = {},")
				if m[2] == "{" {
					skip = true
				}
				continue
			case m[1] == "n" || m[1] == "bytes":
				out = append(out, "\t\t\t[\""+m[1]+"\"] = 0,")
				continue
			}
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return text
	}
	last := out[len(out)-1]
	out = append(out[:len(out)-1], "\t\t\t[\"bare\"] = true,", last)
	return strings.Join(out, "\n")
}

type webFight struct {
	boss string
	kill bool
	seg  int
}

func raidFights(segs []recSeg) []webFight {
	var out []webFight
	for i, s := range segs {
		if len(s.bosses) == 0 {
			continue
		}
		b := append([]string(nil), s.bosses...)
		sort.Strings(b)
		out = append(out, webFight{boss: strings.Join(b, "+"), kill: s.kill, seg: i + 1})
	}
	return out
}

func keepFights(fights []webFight) (map[int]bool, int) {
	pick := map[string]webFight{}
	var order []string
	for _, f := range fights {
		cur, ok := pick[f.boss]
		if !ok {
			order = append(order, f.boss)
		}
		if f.kill || !ok || !cur.kill {
			pick[f.boss] = f
		}
	}
	keep := map[int]bool{}
	for _, b := range order {
		keep[pick[b].seg] = true
	}
	return keep, len(fights) - len(keep)
}

func unLuaString(s string) string {
	if strings.IndexByte(s, '\\') < 0 {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch d := s[i]; {
		case d >= '0' && d <= '9':
			n, j := 0, i
			for ; j < len(s) && j < i+3 && s[j] >= '0' && s[j] <= '9'; j++ {
				n = n*10 + int(s[j]-'0')
			}
			b.WriteByte(byte(n))
			i = j - 1
		case d == 'n':
			b.WriteByte('\n')
		case d == 'r':
			b.WriteByte('\r')
		case d == 't':
			b.WriteByte('\t')
		default:
			b.WriteByte(d)
		}
	}
	return b.String()
}

func luaQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)
	return `"` + r.Replace(s) + `"`
}

func recordSpells(segs []recSeg) map[int]string {
	out := map[int]string{}
	for _, s := range segs {
		m := reSpellsF.FindStringSubmatch(s.lua)
		if m == nil {
			continue
		}
		for _, e := range strings.Split(unLuaString(m[1]), "\x1e") {
			p := strings.Split(e, "\x1f")
			if len(p) < 2 || p[1] == "" {
				continue
			}
			if id, err := strconv.Atoi(p[0]); err == nil {
				out[id] = p[1]
			}
		}
	}
	return out
}

func buildRaidData(rec webRecord, r raidRange, keep map[int]bool, spellIcon func(int) string) string {
	var b strings.Builder
	b.WriteString("ManaCodeRaidHelperDB = {\n")
	for _, k := range webTopKeys {
		if v, ok := rec.top[k]; ok {
			b.WriteString(`["` + k + `"] = ` + v + ",\n")
		}
	}
	b.WriteString("[\"segments\"] = {\n")
	segs := rec.segs[r.from : r.to+1]
	for i, s := range segs {
		if keep == nil || keep[i+1] {
			b.WriteString(s.lua)
		} else {
			b.WriteString(stripSeg(s.lua))
		}
		b.WriteString(",\n")
	}
	b.WriteString("},\n}\n")
	spells := recordSpells(segs)
	ids := make([]int, 0, len(spells))
	for id := range spells {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	rows := make([]string, 0, len(ids))
	for _, id := range ids {
		row := "[" + strconv.Itoa(id) + "]={" + luaQuote(spells[id])
		if ic := spellIcon(id); ic != "" {
			row += "," + luaQuote(ic)
		}
		rows = append(rows, row+"}")
	}
	b.WriteString("local S = {" + strings.Join(rows, ",\n") + "}\nfor k, v in pairs(S) do WEB_SPELLS[k] = v end\n")
	b.WriteString("WEB_REALM, WEB_PLAYER = " + luaQuote(r.day) + ", \"Зритель\"\n")
	return b.String()
}

type lootItem struct {
	name string
	q    int
}

func raidLoot(top string, from, to float64) map[int]lootItem {
	out := map[int]lootItem{}
	for _, m := range reLootRow.FindAllStringSubmatch(top, -1) {
		f := map[string]string{}
		for _, line := range strings.Split(m[1], "\n") {
			if r := reSegRaid.FindStringSubmatch(strings.TrimSpace(line)); r != nil {
				f[r[1]] = r[2] + r[3]
			}
		}
		id, _ := strconv.Atoi(f["id"])
		t, _ := strconv.ParseFloat(f["t"], 64)
		if id == 0 || f["name"] == "" || t < from-webHour || t > to+webHour {
			continue
		}
		q, err := strconv.Atoi(f["q"])
		if err != nil || q == 0 {
			q = 4
		}
		out[id] = lootItem{name: unLuaString(f["name"]), q: q}
	}
	return out
}
