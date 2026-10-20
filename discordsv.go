package main

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"time"
)

const discordVersion = 2

var (
	svHeadRH    = []byte(rhDBVar + " = {")
	svDiscord   = []byte("\n\t[\"discord\"] = {")
	errNoTable  = errors.New("ключа discord нет")
	errLuaShape = errors.New("таблица discord не читается")
)

type discordRow struct {
	Name  string
	Class string
	V     float64
	T     float64
	S     string
}

type discordTarget struct {
	Title string
	Unit  string
	Rows  []discordRow
}

type discordBoss struct {
	Name    string
	Kill    bool
	Tries   int
	Wipes   int
	Time    float64
	Start   int64
	Damage  float64
	Deaths  int
	DPS     []discordRow
	HPS     []discordRow
	Targets []discordTarget
}

type discordImmortal struct {
	Name  string
	Class string
	Tries int
}

type discordItem struct {
	Icon    string
	Name    string
	V       float64
	Cost    float64
	HasCost bool
}

type discordConsumable struct {
	Key     string
	Label   string
	V       float64
	Cost    float64
	HasCost bool
	Items   []discordItem
}

type discordPost struct {
	ID          string
	Raid        string
	Zone        string
	Size        int
	Heroic      bool
	Start       int64
	Finish      int64
	Combat      float64
	Idle        float64
	Deaths      int
	Damage      float64
	Healed      float64
	Bosses      []discordBoss
	Immortal    []discordImmortal
	Rod         []discordRow
	Buffed      []discordRow
	Top         []discordRow
	Consumables []discordConsumable
	Cost        float64
	HasCost     bool
}

func discordBlock(src []byte) []byte {
	start := -1
	for off := 0; off < len(src); {
		i := bytes.Index(src[off:], svHeadRH)
		if i < 0 {
			return nil
		}
		i += off
		if i == 0 || src[i-1] == '\n' {
			start = i
			break
		}
		off = i + 1
	}
	if start < 0 {
		return nil
	}
	body := src[start:]
	if end := bytes.Index(body, []byte("\n}")); end >= 0 {
		body = body[:end+1]
	}
	k := bytes.Index(body, svDiscord)
	if k < 0 {
		return nil
	}
	return body[k+len(svDiscord)-1:]
}

func readDiscord(src []byte) ([]discordPost, error) {
	blk := discordBlock(src)
	if blk == nil {
		return nil, errNoTable
	}
	p := &luaParser{s: blk}
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	box, ok := v.(*luaTable)
	if !ok {
		return nil, errLuaShape
	}
	if box.num("v") != discordVersion {
		return nil, fmt.Errorf("таблица discord версии %v, а читаю %d — обновите Raid Helper и ManacodeUpdate.exe", box.get("v"), discordVersion)
	}
	posts := box.table("posts")
	if posts == nil {
		return nil, errLuaShape
	}
	var out []discordPost
	for _, t := range posts.list() {
		if t.str("id") == "" {
			continue
		}
		out = append(out, decodePost(t))
	}
	return out, nil
}

func decodeRows(t *luaTable) []discordRow {
	var out []discordRow
	for _, r := range t.list() {
		out = append(out, discordRow{Name: r.str("n"), Class: r.str("c"), V: r.num("v"), T: r.num("t"), S: r.text("s")})
	}
	return out
}

func decodePost(t *luaTable) discordPost {
	p := discordPost{
		ID: t.str("id"), Raid: t.str("raid"), Zone: t.str("zone"), Size: int(t.num("size")), Heroic: t.flag("heroic"),
		Start: int64(t.num("start")), Finish: int64(t.num("finish")), Combat: t.num("combat"), Idle: t.num("idle"),
		Deaths: int(t.num("deaths")), Damage: t.num("damage"), Healed: t.num("healed"),
		Rod: decodeRows(t.table("rod")), Buffed: decodeRows(t.table("buffed")), Top: decodeRows(t.table("top")),
	}
	if c, ok := t.get("cost").(float64); ok {
		p.Cost, p.HasCost = c, true
	}
	for _, b := range t.table("bosses").list() {
		boss := discordBoss{Name: b.str("name"), Kill: b.flag("kill"), Tries: int(b.num("tries")), Wipes: int(b.num("wipes")),
			Time: b.num("time"), Start: int64(b.num("start")), Damage: b.num("damage"), Deaths: int(b.num("deaths")),
			DPS: decodeRows(b.table("dps")), HPS: decodeRows(b.table("hps"))}
		for _, g := range b.table("targets").list() {
			boss.Targets = append(boss.Targets, discordTarget{Title: g.str("title"), Unit: g.str("unit"), Rows: decodeRows(g.table("rows"))})
		}
		p.Bosses = append(p.Bosses, boss)
	}
	for _, m := range t.table("immortal").list() {
		p.Immortal = append(p.Immortal, discordImmortal{Name: m.str("n"), Class: m.str("c"), Tries: int(m.num("tries"))})
	}
	for _, c := range t.table("consumables").list() {
		cons := discordConsumable{Key: c.str("k"), Label: c.str("label"), V: c.num("v")}
		cons.Cost, cons.HasCost = c.money("cost")
		for _, it := range c.table("items").list() {
			item := discordItem{Icon: it.str("icon"), Name: it.str("name"), V: it.num("v")}
			item.Cost, item.HasCost = it.money("cost")
			cons.Items = append(cons.Items, item)
		}
		p.Consumables = append(p.Consumables, cons)
	}
	return p
}

func (p discordPost) startTime() time.Time {
	return time.Unix(p.Start, 0)
}

func (p discordPost) date() string {
	if p.Start <= 0 {
		return ""
	}
	return p.startTime().Format("02.01.2006")
}

type luaTable struct {
	arr []any
	m   map[string]any
}

func (t *luaTable) list() []*luaTable {
	if t == nil {
		return nil
	}
	out := make([]*luaTable, 0, len(t.arr))
	for _, it := range t.arr {
		if v, ok := it.(*luaTable); ok {
			out = append(out, v)
		}
	}
	return out
}

func (t *luaTable) text(k string) string {
	switch v := t.get(k).(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func (t *luaTable) get(k string) any {
	if t == nil || t.m == nil {
		return nil
	}
	return t.m[k]
}

func (t *luaTable) str(k string) string {
	s, _ := t.get(k).(string)
	return s
}

func (t *luaTable) num(k string) float64 {
	n, _ := t.get(k).(float64)
	return n
}

func (t *luaTable) money(k string) (float64, bool) {
	n, ok := t.get(k).(float64)
	return n, ok
}

func (t *luaTable) flag(k string) bool {
	b, _ := t.get(k).(bool)
	return b
}

func (t *luaTable) table(k string) *luaTable {
	v, _ := t.get(k).(*luaTable)
	return v
}

func (t *luaTable) set(k, v any) {
	if n, ok := k.(float64); ok && n == float64(len(t.arr)+1) {
		t.arr = append(t.arr, v)
		return
	}
	if t.m == nil {
		t.m = map[string]any{}
	}
	switch kk := k.(type) {
	case string:
		t.m[kk] = v
	case float64:
		t.m[strconv.FormatFloat(kk, 'f', -1, 64)] = v
	}
}

type luaParser struct {
	s     []byte
	i     int
	depth int
}

func (p *luaParser) fail(what string) error {
	return fmt.Errorf("%w: %s на байте %d", errLuaShape, what, p.i)
}

func (p *luaParser) ws() {
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			p.i++
		case c == '-' && p.i+1 < len(p.s) && p.s[p.i+1] == '-':
			if e := bytes.IndexByte(p.s[p.i:], '\n'); e >= 0 {
				p.i += e + 1
			} else {
				p.i = len(p.s)
			}
		default:
			return
		}
	}
}

func (p *luaParser) value() (any, error) {
	p.ws()
	if p.i >= len(p.s) {
		return nil, p.fail("конец файла")
	}
	c := p.s[p.i]
	switch {
	case c == '{':
		return p.table()
	case c == '"' || c == '\'':
		return p.str()
	case bytes.HasPrefix(p.s[p.i:], []byte("true")):
		p.i += 4
		return true, nil
	case bytes.HasPrefix(p.s[p.i:], []byte("false")):
		p.i += 5
		return false, nil
	case bytes.HasPrefix(p.s[p.i:], []byte("nil")):
		p.i += 3
		return nil, nil
	}
	return p.number()
}

func (p *luaParser) number() (any, error) {
	j := p.i
	for j < len(p.s) {
		c := p.s[j]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E' || c == 'x' || c == 'X' ||
			(c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') || c == 'i' || c == 'n' {
			j++
			continue
		}
		break
	}
	raw := string(p.s[p.i:j])
	if raw == "" {
		return nil, p.fail("неожиданный символ")
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		if v, err2 := strconv.ParseInt(raw, 0, 64); err2 == nil {
			n, err = float64(v), nil
		}
	}
	if err != nil {
		return nil, p.fail("число " + raw)
	}
	p.i = j
	return n, nil
}

func (p *luaParser) str() (any, error) {
	q := p.s[p.i]
	p.i++
	var b []byte
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == q:
			p.i++
			return string(b), nil
		case c == '\\' && p.i+1 < len(p.s):
			p.i++
			e := p.s[p.i]
			switch e {
			case 'n', '\n':
				b = append(b, '\n')
			case 'r':
				b = append(b, '\r')
			case 't':
				b = append(b, '\t')
			case 'a':
				b = append(b, 7)
			case 'b':
				b = append(b, 8)
			case 'f':
				b = append(b, 12)
			case 'v':
				b = append(b, 11)
			case '\r':
				if p.i+1 < len(p.s) && p.s[p.i+1] == '\n' {
					p.i++
				}
				b = append(b, '\n')
			default:
				if e >= '0' && e <= '9' {
					v, k := 0, 0
					for k < 3 && p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
						v = v*10 + int(p.s[p.i]-'0')
						p.i++
						k++
					}
					b = append(b, byte(v))
					continue
				}
				b = append(b, e)
			}
			p.i++
		default:
			b = append(b, c)
			p.i++
		}
	}
	return nil, p.fail("строка не закрыта")
}

func (p *luaParser) table() (any, error) {
	p.depth++
	if p.depth > 32 {
		return nil, p.fail("слишком глубоко")
	}
	defer func() { p.depth-- }()
	p.i++
	t := &luaTable{}
	for {
		p.ws()
		if p.i >= len(p.s) {
			return nil, p.fail("таблица не закрыта")
		}
		c := p.s[p.i]
		if c == '}' {
			p.i++
			return t, nil
		}
		var key any
		switch {
		case c == '[':
			p.i++
			k, err := p.value()
			if err != nil {
				return nil, err
			}
			p.ws()
			if p.i >= len(p.s) || p.s[p.i] != ']' {
				return nil, p.fail("нет ]")
			}
			p.i++
			p.ws()
			if p.i >= len(p.s) || p.s[p.i] != '=' {
				return nil, p.fail("нет =")
			}
			p.i++
			key = k
		case isIdentStart(c):
			j := p.i
			for j < len(p.s) && isIdent(p.s[j]) {
				j++
			}
			k := p.i
			p.i = j
			p.ws()
			if p.i < len(p.s) && p.s[p.i] == '=' && string(p.s[k:j]) != "true" && string(p.s[k:j]) != "false" {
				p.i++
				key = string(p.s[k:j])
			} else {
				p.i = k
			}
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		if key == nil {
			key = float64(len(t.arr) + 1)
		}
		if v != nil {
			t.set(key, v)
		}
		p.ws()
		if p.i < len(p.s) && (p.s[p.i] == ',' || p.s[p.i] == ';') {
			p.i++
		}
	}
}
