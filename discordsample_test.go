package main

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
)

type lkv struct {
	k string
	v any
}

type lobj []lkv

func luaWrite(b *strings.Builder, v any, ind int) {
	tabs := strings.Repeat("\t", ind)
	switch x := v.(type) {
	case lobj:
		b.WriteString("{\n")
		for _, f := range x {
			b.WriteString(tabs + "\t[\"" + f.k + "\"] = ")
			luaWrite(b, f.v, ind+1)
			b.WriteString(",\n")
		}
		b.WriteString(tabs + "}")
	case []any:
		b.WriteString("{\n")
		for i, it := range x {
			b.WriteString(tabs + "\t")
			luaWrite(b, it, ind+1)
			fmt.Fprintf(b, ", -- [%d]\n", i+1)
		}
		b.WriteString(tabs + "}")
	case string:
		b.WriteString(strconv.Quote(x))
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case float64:
		b.WriteString(strconv.FormatFloat(x, 'f', -1, 64))
	}
}

type samplePlayer struct {
	n, c string
	role byte
	dps  float64
	hps  float64
}

var sampleRoster = []samplePlayer{
	{"Бронетрон", "DEATHKNIGHT", 't', 6200, 900}, {"Щитобой", "PALADIN", 't', 5100, 1300},
	{"Светозар", "PALADIN", 'h', 450, 6900}, {"Лучезара", "PALADIN", 'h', 380, 6300},
	{"Молитвенник", "PRIEST", 'h', 900, 5200}, {"Тотемия", "SHAMAN", 'h', 300, 5900},
	{"Листопад", "DRUID", 'h', 250, 6600}, {"Благодать", "PRIEST", 'h', 600, 5600},
	{"Огнеплёт", "MAGE", 'd', 14200, 0}, {"Чародейка", "MAGE", 'd', 13100, 0},
	{"Стрелолист", "HUNTER", 'd', 13600, 0}, {"Меткоглаз", "HUNTER", 'd', 12100, 0},
	{"Теневор", "ROGUE", 'd', 14600, 0}, {"Кинжалия", "ROGUE", 'd', 12800, 0},
	{"Скверноцвет", "WARLOCK", 'd', 13900, 300}, {"Бурелом", "SHAMAN", 'd', 12400, 200},
	{"Громовержец", "SHAMAN", 'd', 11900, 0}, {"Яростный", "WARRIOR", 'd', 13300, 0},
	{"Клинковод", "WARRIOR", 'd', 11200, 0}, {"Нечестивец", "DEATHKNIGHT", 'd', 13000, 600},
	{"Ледокол", "DEATHKNIGHT", 'd', 11700, 500}, {"Лунолика", "DRUID", 'd', 12600, 0},
	{"Когтерез", "DRUID", 'd', 12200, 0}, {"Тьмадушный", "PRIEST", 'd', 11800, 900},
	{"Воздаятель", "PALADIN", 'd', 10900, 1100},
}

type sampleBoss struct {
	name           string
	time           float64
	tries, deaths  int
	kill           bool
	mult           float64
	targets        [][3]string
	targetMultiple float64
}

var sampleBosses = []sampleBoss{
	{"Лорд Ребрад", 212, 1, 0, true, 1.05, [][3]string{{"Урон по Костяным шипам", "dmg", ""}}, 0.06},
	{"Леди Смертный Шёпот", 348, 1, 2, true, 0.95, [][3]string{{"Урон по культистам", "dmg", ""}}, 0.12},
	{"Битва на кораблях", 235, 1, 0, true, 0.7, [][3]string{{"Урон по Магу-штурмовику", "dmg", ""}}, 0.05},
	{"Саурфанг Смертоносный", 200, 1, 1, true, 1.15, [][3]string{{"Урон по Кровавым тварям", "dmg", ""}}, 0.09},
	{"Тухлопуз", 290, 2, 3, true, 1.0, [][3]string{{"Газовые споры", "count", "спор"}}, 0},
	{"Гниломорд", 245, 1, 1, true, 1.0, [][3]string{{"Взрывы нестабильной слизи", "count", "слизь"}}, 0},
	{"Профессор Мерзоцид", 460, 4, 9, true, 0.9, [][3]string{{"Урон по Сгущённому газу", "dmg", ""}, {"Урон по Изменчивой слизи", "dmg", ""}}, 0.08},
	{"Совет принцев крови", 330, 2, 4, true, 0.85, [][3]string{{"Кинетические бомбы", "count", "отбил"}, {"Урон по главному принцу", "pct", ""}}, 0},
	{"Кровавая королева Лана'тель", 310, 2, 5, true, 1.05, [][3]string{{"Укусы", "count", "укусов"}}, 0},
	{"Валитрия Сноходица", 415, 1, 2, true, 0.55, [][3]string{{"Урон по Скелетам-гигантам", "dmg", ""}, {"Урон по Ледяным зомби", "dmg", ""}}, 0.2},
	{"Синдрагоса", 440, 3, 7, true, 1.0, [][3]string{{"Урон по Ледяным глыбам", "dmg", ""}}, 0.07},
	{"Король-лич", 372, 5, 21, false, 0.95, [][3]string{{"Урон по Валь'кирам", "dmg", ""}, {"Урон по Ужасам", "dmg", ""}}, 0.1},
}

func sampleRows(r *rand.Rand, pick func(samplePlayer) float64, secs float64, mult float64) []any {
	type row struct {
		p    samplePlayer
		v, t float64
	}
	var rows []row
	for _, p := range sampleRoster {
		base := pick(p)
		if base <= 0 {
			continue
		}
		v := math.Round(base * mult * (0.85 + 0.3*r.Float64()))
		rows = append(rows, row{p, v, math.Round(v * secs)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].v > rows[j].v })
	out := make([]any, 0, len(rows))
	for _, x := range rows {
		out = append(out, lobj{{"n", x.p.n}, {"c", x.p.c}, {"v", x.v}, {"t", x.t}})
	}
	return out
}

func sampleTarget(r *rand.Rand, spec [3]string, secs, share float64) lobj {
	var rows []any
	type row struct {
		p samplePlayer
		v float64
		s string
	}
	var list []row
	for _, p := range sampleRoster {
		switch spec[1] {
		case "dmg":
			if p.role != 'd' {
				continue
			}
			list = append(list, row{p, math.Round(p.dps * share * secs * (0.4 + r.Float64())), ""})
		case "pct":
			if p.role != 'd' {
				continue
			}
			list = append(list, row{p, math.Round(55 + 40*r.Float64()), ""})
		case "count":
			n := float64(r.IntN(6))
			if n == 0 {
				continue
			}
			list = append(list, row{p, n, spec[2]})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
	for i, x := range list {
		if i >= 8 {
			break
		}
		f := lobj{{"n", x.p.n}, {"c", x.p.c}, {"v", x.v}}
		if x.s != "" {
			f = append(f, lkv{"s", x.s})
		}
		rows = append(rows, f)
	}
	return lobj{{"title", spec[0]}, {"unit", spec[1]}, {"rows", rows}}
}

func samplePostLua() string {
	r := rand.New(rand.NewPCG(2026, 10))
	start := int64(1791230400)
	at := start + 600
	var bosses []any
	var combat, damage, healed float64
	deaths := 0
	for _, sb := range sampleBosses {
		dps := sampleRows(r, func(p samplePlayer) float64 { return p.dps }, sb.time, sb.mult)
		hps := sampleRows(r, func(p samplePlayer) float64 { return healer(p) }, sb.time, 1)
		dmg := 0.0
		for _, it := range dps {
			dmg += it.(lobj)[3].v.(float64)
		}
		heal := 0.0
		for _, it := range hps {
			heal += it.(lobj)[3].v.(float64)
		}
		var targets []any
		for _, spec := range sb.targets {
			targets = append(targets, sampleTarget(r, spec, sb.time, sb.targetMultiple))
		}
		fights := sb.time * float64(sb.tries) * 0.9
		combat += fights
		damage += dmg * float64(sb.tries) * 0.8
		healed += heal * float64(sb.tries) * 0.8
		deaths += sb.deaths
		bosses = append(bosses, lobj{
			{"name", sb.name}, {"kill", sb.kill}, {"tries", sb.tries}, {"wipes", sb.tries - b2i(sb.kill)},
			{"time", sb.time}, {"start", at}, {"damage", dmg}, {"deaths", sb.deaths},
			{"dps", dps}, {"hps", hps}, {"targets", targets},
		})
		at += int64(fights) + 600 + int64(r.IntN(400))
	}
	combat = math.Round(combat * 1.35)
	finish := at
	immortal := []any{}
	for _, n := range []int{0, 2, 5, 8, 12, 13, 20} {
		p := sampleRoster[n]
		immortal = append(immortal, lobj{{"n", p.n}, {"c", p.c}, {"tries", 24}})
	}
	rod := []any{
		lobj{{"n", "Чародейка"}, {"c", "MAGE"}, {"v", 16}, {"s", "выбран 16 раз"}},
		lobj{{"n", "Тотемия"}, {"c", "SHAMAN"}, {"v", 11}, {"s", "выбран 11 раз"}},
		lobj{{"n", "Скверноцвет"}, {"c", "WARLOCK"}, {"v", 9}, {"s", "выбран 9 раз"}},
		lobj{{"n", "Лунолика"}, {"c", "DRUID"}, {"v", 7}, {"s", "выбран 7 раз"}},
		lobj{{"n", "Меткоглаз"}, {"c", "HUNTER"}, {"v", 6}, {"s", "выбран 6 раз"}},
	}
	buffed := []any{
		lobj{{"n", "Теневор"}, {"c", "ROGUE"}, {"v", 412}, {"s", "412 усилений"}},
		lobj{{"n", "Огнеплёт"}, {"c", "MAGE"}, {"v", 388}, {"s", "388 усилений"}},
		lobj{{"n", "Яростный"}, {"c", "WARRIOR"}, {"v", 371}, {"s", "371 усиление"}},
		lobj{{"n", "Нечестивец"}, {"c", "DEATHKNIGHT"}, {"v", 344}, {"s", "344 усиления"}},
		lobj{{"n", "Стрелолист"}, {"c", "HUNTER"}, {"v", 330}, {"s", "330 усилений"}},
	}
	cons, cost := sampleConsumables()
	top := []any{}
	for _, n := range []int{12, 8, 14, 10, 17} {
		p := sampleRoster[n]
		v := math.Round(p.dps * 1.04)
		top = append(top, lobj{{"n", p.n}, {"c", p.c}, {"v", v}, {"t", math.Round(v * combat * 0.92)}})
	}
	post := lobj{
		{"id", fmt.Sprintf("%d-12-%d", start, finish)}, {"raid", "ЦЛК 25 гер."}, {"zone", "Цитадель Ледяной Короны"},
		{"size", 25}, {"heroic", true}, {"start", start}, {"finish", finish},
		{"combat", combat}, {"idle", float64(finish-start) - combat}, {"deaths", deaths + 6},
		{"damage", math.Round(damage * 1.6)}, {"healed", math.Round(healed * 1.5)},
		{"bosses", bosses}, {"immortal", immortal}, {"rod", rod}, {"buffed", buffed},
		{"consumables", cons}, {"cost", cost}, {"top", top},
	}
	var b strings.Builder
	b.WriteString("\nManaCodeRaidHelperDB = {\n\t[\"recording\"] = true,\n\t[\"discord\"] = ")
	luaWrite(&b, lobj{{"v", 2}, {"posts", []any{post}}}, 1)
	b.WriteString(",\n\t[\"fxVersion\"] = 5,\n}\n")
	return b.String()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

type sampleItem struct {
	icon, name string
	n          int
	each       float64
}

var sampleCons = []struct {
	k, label string
	items    []sampleItem
}{
	{"flask", "Настои", []sampleItem{
		{"INV_Alchemy_EndlessFlask_06", "Настой бесконечной ярости", 14, 330000},
		{"INV_Alchemy_EndlessFlask_04", "Настой ледяного змея", 9, 285000},
		{"INV_Alchemy_EndlessFlask_05", "Настой каменной крови", 5, 260000},
		{"INV_Alchemy_EndlessFlask_02", "Настой чистого мохо", 3, 0},
	}},
	{"elixir", "Эликсиры", []sampleItem{
		{"INV_Alchemy_Elixir_01", "Эликсир могучей ловкости", 6, 42000},
		{"INV_Alchemy_Elixir_06", "Эликсир превосходства", 4, 38000},
		{"INV_Alchemy_Elixir_03", "Эликсир мощи заклинаний", 2, 0},
	}},
	{"potion", "Зелья", []sampleItem{
		{"INV_Alchemy_Elixir_04", "Зелье скорости", 41, 61000},
		{"INV_Alchemy_Elixir_01", "Зелье дикой магии", 28, 54000},
		{"INV_Alchemy_Elixir_05", "Рунический флакон с лечебным зельем", 12, 9500},
		{"INV_Alchemy_Elixir_02", "Рунический флакон с зельем маны", 6, 7000},
	}},
	{"food", "Еда", []sampleItem{
		{"INV_Misc_Fish_52", "Рыбный пир", 98, 12000},
		{"INV_Misc_Fish_50", "Филе драконьего плавника", 31, 4500},
		{"INV_Misc_Food_100", "Большой пир", 18, 9000},
		{"INV_Misc_Food_DimSum", "Пельмени", 9, 0},
	}},
	{"stone", "Камни чернокнижника", []sampleItem{
		{"INV_Stone_04", "Камень здоровья", 19, 0},
	}},
	{"scroll", "Свитки", []sampleItem{
		{"INV_Scroll_02", "Свиток силы VIII", 5, 3500},
		{"INV_Scroll_07", "Свиток ловкости VIII", 3, 3000},
	}},
	{"other", "Прочее", []sampleItem{
		{"", "Руна неизвестного мастера", 4, 0},
	}},
}

func sampleConsumables() ([]any, float64) {
	var out []any
	total := 0.0
	for _, c := range sampleCons {
		var items []any
		n, sum := 0, 0.0
		for _, it := range c.items {
			row := lobj{}
			if it.icon != "" {
				row = append(row, lkv{"icon", it.icon})
			}
			row = append(row, lkv{"name", it.name}, lkv{"v", it.n})
			if it.each > 0 {
				row = append(row, lkv{"cost", it.each * float64(it.n)})
				sum += it.each * float64(it.n)
			}
			items = append(items, row)
			n += it.n
		}
		cat := lobj{{"k", c.k}, {"label", c.label}, {"v", n}}
		if sum > 0 {
			cat = append(cat, lkv{"cost", sum})
		}
		out = append(out, append(cat, lkv{"items", items}))
		total += sum
	}
	return out, total
}

func healer(p samplePlayer) float64 {
	if p.role != 'h' {
		return 0
	}
	return p.hps
}
