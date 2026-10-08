package main

import (
	"encoding/base64"
	"fmt"
	"html"
	"math"
	"sort"
	"strings"
)

const (
	htmlW         = 1920
	htmlH         = 1080
	htmlHalfH     = 590
	htmlPerPage   = 4
	htmlBossRowH  = 21
	htmlBossBody  = 412
	htmlTargetMax = 5
)

type htmlPage struct {
	Name string
	HTML string
	W, H int
}

var consumableColors = map[string]string{
	"flask":  "#a06ad8",
	"elixir": "#3fbf55",
	"potion": "#e0483c",
	"food":   "#d9a520",
	"stone":  "#7fd04a",
	"scroll": "#69ccf0",
}

const htmlCSS = `*{box-sizing:border-box;margin:0;padding:0}
html,body{width:%dpx;height:%dpx;overflow:hidden}
body{color:#ededed;font-family:%s;font-size:15px;line-height:1.25;-webkit-font-smoothing:antialiased;text-shadow:0 1px 1px rgba(0,0,0,.95);
background-color:#0c0a07;background-image:radial-gradient(1400px 520px at 45%% -12%%,rgba(232,184,74,.11),transparent 70%%),radial-gradient(900px 700px at 105%% 115%%,rgba(140,80,30,.12),transparent 70%%)}
.page{height:100%%;padding:20px 28px 22px;display:flex;flex-direction:column;gap:16px}
.top{display:flex;align-items:flex-end;justify-content:space-between;gap:24px;padding-bottom:12px;border-bottom:1px solid #6b5326;box-shadow:0 1px 0 rgba(0,0,0,.7)}
.top h1{font-size:32px;line-height:1.1;color:#e8b84a;font-weight:normal;white-space:nowrap}
.top h1 .mode{display:inline-block;margin-left:14px;padding:3px 12px 2px;border:1px solid #8a6a2c;border-radius:14px;font-size:17px;color:#f2d68c;background:rgba(217,165,32,.12);vertical-align:5px}
.sub{margin-top:7px;color:#9a9a9a;font-size:17px}
.sub b{color:#ededed;font-weight:normal}
.sub i{font-style:normal;color:#6b5326;margin:0 9px}
.brand{text-align:right;color:#8a8a8a;font-size:14px;white-space:nowrap}
.brand em{display:block;font-style:normal;color:#e8b84a;font-size:19px;margin-bottom:3px}
.card{position:relative;background:linear-gradient(180deg,rgba(30,24,16,.96),rgba(16,13,9,.97));border:1px solid #6b5326;border-radius:4px;padding:10px 14px 12px 19px;box-shadow:0 2px 10px rgba(0,0,0,.55),inset 0 1px 0 rgba(255,220,140,.07);overflow:hidden}
.card::before{content:"";position:absolute;left:0;top:0;bottom:0;width:5px;background:var(--bar,#d9a520)}
.ch{display:flex;align-items:baseline;justify-content:space-between;gap:12px;margin-bottom:8px}
.ch h2{font-size:19px;color:#e8b84a;font-weight:normal;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.ch .r{color:#9a9a9a;font-size:14px;white-space:nowrap}
.kpis{display:grid;grid-template-columns:1fr .8fr 1.55fr 1fr 1fr .8fr;gap:14px}
.kpi{padding:12px 16px 13px 21px}
.kpi .l{color:#c2b89e;font-size:15px}
.kpi .v{margin-top:5px;font-size:34px;line-height:1.1;color:#fff;white-space:nowrap}
.kpi .v small{font-size:16px;color:#9a9a9a;margin-left:8px}
.n{font-variant-numeric:tabular-nums}
.dim{color:#9a9a9a}
table{width:100%%;border-collapse:collapse}
th{font-weight:normal;color:#c2b89e;font-size:14px;text-align:left;padding:7px 10px;background:rgba(102,92,77,.35);border-bottom:1px solid rgba(255,255,255,.08)}
td{padding:0 10px;font-size:18px;border-bottom:1px solid rgba(255,255,255,.055);white-space:nowrap}
tbody tr:nth-child(even) td{background:rgba(255,255,255,.025)}
th.r,td.r{text-align:right}
td.boss{color:#f4f4f4}
.pill{display:inline-block;padding:2px 11px 1px;border-radius:11px;font-size:14px}
.kill{color:#8dff99;background:rgba(64,204,77,.13);border:1px solid rgba(64,204,77,.55)}
.wipe{color:#ff7d6a;background:rgba(242,51,42,.13);border:1px solid rgba(242,51,42,.55)}
.db{position:relative;height:24px;display:flex;align-items:center;justify-content:flex-end;padding:0 8px}
.db i{position:absolute;left:0;top:0;bottom:0;background:linear-gradient(90deg,rgba(160,106,216,.42),rgba(160,106,216,.16));border-radius:2px}
.db span{position:relative}
tfoot td{color:#e8b84a;border-bottom:0;border-top:1px solid #6b5326}
.cols{display:grid;gap:14px;min-height:0}
.stack{display:flex;flex-direction:column;gap:14px;min-height:0}
.lh{display:flex;justify-content:space-between;color:#c2b89e;font-size:13px;padding:0 8px 3px 28px}
.row{display:grid;grid-template-columns:24px 1fr;align-items:center;height:%dpx}
.rk{color:#8a8a8a;font-size:13px;text-align:right;padding-right:6px}
.bar{position:relative;height:calc(100%% - 3px);display:flex;align-items:center;gap:10px;padding:0 8px;background:rgba(255,255,255,.035);border-radius:2px;overflow:hidden}
.bar i{position:absolute;left:0;top:0;bottom:0;border-radius:2px;border-bottom:2px solid var(--c)}
.bar .nm{position:relative;flex:1;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.bar .s{position:relative;color:#9a9a9a;font-size:13px;white-space:nowrap}
.bar .v{position:relative;color:#fff}
.bar .t{position:relative;color:#9a9a9a;min-width:62px;text-align:right}
.li{display:flex;align-items:center;gap:10px;height:28px;padding:0 6px;border-bottom:1px solid rgba(255,255,255,.05);font-size:17px}
.li:last-child{border-bottom:0}
.li .nm{flex:1;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.li .s{color:#9a9a9a;font-size:14px;white-space:nowrap}
.li .v{color:#fff;white-space:nowrap}
.dot{width:11px;height:11px;border-radius:50%%;box-shadow:0 0 0 1px rgba(0,0,0,.6),0 0 6px currentColor}
.two{display:grid;grid-template-columns:1fr 1fr;column-gap:18px}
.gold{display:flex;align-items:center;gap:12px;font-size:40px;color:#ffe08a;padding:4px 4px 2px}
.coin{width:28px;height:28px;border-radius:50%%;background:radial-gradient(circle at 35%% 30%%,#fff3b0,#f0c030 45%%,#a8740c 85%%);box-shadow:0 0 0 1px #5a3c06,0 0 10px rgba(240,192,48,.35)}
.empty{color:#9a9a9a;font-size:16px;padding:6px}
.grid{flex:1;display:grid;grid-template-columns:1fr 1fr;gap:16px;min-height:0}
.bcard{display:flex;flex-direction:column;min-height:0}
.bh{display:flex;align-items:center;gap:12px;margin-bottom:8px}
.bh h2{flex:1;font-size:22px;color:#e8b84a;font-weight:normal;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.bh .m{color:#ededed;font-size:17px;white-space:nowrap}
.bh .m small{color:#9a9a9a;font-size:15px}
.pair{display:grid;grid-template-columns:1fr 1fr;gap:16px}
.tgt{margin-top:10px;display:grid;grid-template-columns:1fr 1fr;gap:16px}
.tt{color:#e8b84a;font-size:14px;padding:0 8px 3px 28px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.ci{display:none}
.cons{display:grid;column-gap:22px;align-items:start}
.cg{display:flex;align-items:center;gap:9px;height:32px;padding:0 6px;font-size:16px;color:#e8b84a;border-bottom:1px solid rgba(232,184,74,.22)}
.cg .nm{flex:1;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.it{display:flex;align-items:center;gap:10px;height:38px;padding:0 6px 0 10px;font-size:16px;border-bottom:1px solid rgba(255,255,255,.05)}
.it .nm{flex:1;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.ic{width:32px;height:32px;flex:none;border-radius:3px;box-shadow:0 0 0 1px #000,0 0 0 2px rgba(150,120,60,.5)}
.ic.no{background:rgba(255,255,255,.06)}
.x{color:#c2b89e;min-width:46px;text-align:right;white-space:nowrap}
.pr{min-width:84px;display:flex;align-items:center;justify-content:flex-end;gap:5px;color:#ffe08a;white-space:nowrap}
.pr.no{color:#6f6f6f;font-size:14px}
.gi{width:1em;height:1em;flex:none}
.coin.sm{width:.9em;height:.9em;display:inline-block}
.gsum{display:inline-flex;align-items:center;gap:6px;margin-left:10px;font-size:22px;color:#ffe08a}
.wide{flex:1;display:grid;grid-template-columns:1.12fr 1fr;gap:22px;min-height:0}
.wide .stack{gap:16px}
.wide .row{font-size:17px}
.wide .lh,.wide .tt{font-size:15px}
.top5 .row{height:26px;font-size:16px}
`

func esc(s string) string {
	return html.EscapeString(s)
}

func nbsp(s string) string {
	return strings.ReplaceAll(s, " ", "&#8239;")
}

func numRU(n float64) string {
	a := math.Abs(n)
	switch {
	case a >= 1e9:
		return fmt.Sprintf("%.2f&#8239;млрд", n/1e9)
	case a >= 1e6:
		return fmt.Sprintf("%.2fм", n/1e6)
	case a >= 1e5:
		return fmt.Sprintf("%.0fк", n/1e3)
	case a >= 1e3:
		return fmt.Sprintf("%.1fк", n/1e3)
	}
	return fmt.Sprintf("%.0f", n)
}

func ruPlural(n int, one, few, many string) string {
	m := n % 100
	if m >= 11 && m <= 14 {
		return many
	}
	switch n % 10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	}
	return many
}

func classCSS(class string, a float64) string {
	c := classColor(class)
	if a >= 1 {
		return fmt.Sprintf("rgb(%d,%d,%d)", c.R, c.G, c.B)
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%.2f)", c.R, c.G, c.B, a)
}

func classIcon(class string) string {
	if _, ok := classTCoords[class]; !ok {
		return ""
	}
	return `<span class="ci c-` + class + `"></span>`
}

func nameSpan(name, class string) string {
	return `<span class="nm" style="color:` + classCSS(class, 1) + `">` + classIcon(class) + esc(name) + `</span>`
}

func (a pageArt) css() string {
	if len(a.Classes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`.ci{display:inline-block;width:1.15em;height:1.15em;margin-right:.45em;vertical-align:-.22em;border-radius:2px;background-size:cover;box-shadow:0 0 0 1px rgba(0,0,0,.85)}`)
	classes := make([]string, 0, len(a.Classes))
	for c := range a.Classes {
		classes = append(classes, c)
	}
	sort.Strings(classes)
	for _, c := range classes {
		b.WriteString(`.c-` + c + `{background-image:url(` + a.Classes[c] + `)}`)
	}
	return b.String() + "\n"
}

func htmlDoc(art pageArt, w, h, rowH int, body string) string {
	family := `"Segoe UI",Tahoma,sans-serif`
	face := ""
	if len(art.Font) > 0 {
		face = `@font-face{font-family:"Friz";src:url(data:font/ttf;base64,` + base64.StdEncoding.EncodeToString(art.Font) + `) format("truetype")}` + "\n"
		family = `"Friz",` + family
	}
	return `<!doctype html><html lang="ru"><head><meta charset="utf-8"><style>` + face +
		fmt.Sprintf(htmlCSS, w, h, family, rowH) + art.css() + `</style></head><body><div class="page">` + body + `</div></body></html>`
}

func (p discordPost) headHTML(what string) string {
	name := p.Zone
	if name == "" {
		name = p.Raid
	}
	var b strings.Builder
	b.WriteString(`<div class="top"><div><h1>` + esc(what) + esc(name))
	if m := p.mode(); m != "" && p.Zone != "" {
		b.WriteString(`<span class="mode">` + esc(m) + `</span>`)
	}
	b.WriteString(`</h1><div class="sub">`)
	var parts []string
	if d := p.date(); d != "" {
		parts = append(parts, `<b>`+d+`</b>`)
	}
	if s := p.span(); s != "" {
		parts = append(parts, `<b class="n">`+s+`</b>`)
	}
	if p.Combat > 0 {
		parts = append(parts, `в бою <b>`+nbsp(hoursRUShort(p.Combat))+`</b>`)
	}
	if p.Idle > 0 {
		parts = append(parts, `простой <b>`+nbsp(hoursRUShort(p.Idle))+`</b>`)
	}
	b.WriteString(strings.Join(parts, `<i>◆</i>`))
	b.WriteString(`</div></div><div class="brand"><em>Raid Helper</em>ManaCode</div></div>`)
	return b.String()
}

func kpi(color, label, value, small string) string {
	if small != "" {
		value += `<small>` + small + `</small>`
	}
	return `<div class="card kpi" style="--bar:` + color + `"><div class="l">` + label + `</div><div class="v n">` + value + `</div></div>`
}

const (
	sumBody     = 842
	cardChrome  = 56
	liH         = 28
	cgH         = 32
	itH         = 38
	top5H       = cardChrome + 20 + 5*26
	sideLimit   = 6
	immortalMax = 8
	hpsMax      = 6
	wideBody    = 886
	wideRowMax  = 34
	wideTgtMax  = 8
)

func summaryHTML(p discordPost, art pageArt) htmlPage {
	var b strings.Builder
	b.WriteString(p.headHTML("Итог рейда: "))
	kill := p.killed()
	killCol := "#3fbf55"
	if kill < len(p.Bosses) {
		killCol = "#d9a520"
	}
	b.WriteString(`<div class="kpis">`)
	b.WriteString(kpi(killCol, "Боссы", fmt.Sprintf("%d / %d", kill, len(p.Bosses)), "убито"))
	b.WriteString(kpi("#e0483c", "Вайпов", fmt.Sprint(p.wipes()), ""))
	b.WriteString(kpi("#c9a227", "В бою", nbsp(hoursRUShort(p.Combat)), "простой "+nbsp(hoursRUShort(p.Idle))))
	b.WriteString(kpi("#a06ad8", "Урон", numRU(p.Damage), ""))
	b.WriteString(kpi("#3f8fe0", "Нахилено", numRU(p.Healed), ""))
	b.WriteString(kpi("#b4364a", "Смертей", fmt.Sprint(p.Deaths), ""))
	b.WriteString(`</div>`)

	left := sumBody
	top := topHTML(p)
	if top != "" {
		left -= top5H + 14
	}
	rh := min(56, (left-cardChrome-36)/(max(len(p.Bosses), 1)+1))

	imm, immRows := immortalHTML(p)
	side := []string{imm}
	sideRows := immRows
	for _, blk := range []struct {
		title, color string
		rows         []discordRow
	}{{"Громоотвод", "#69ccf0", p.Rod}, {"Обмазанный", "#a06ad8", p.Buffed}} {
		if s := rowsBlock(blk.title, blk.color, blk.rows, sideLimit); s != "" {
			side = append(side, s)
			sideRows = max(sideRows, min(len(blk.rows), sideLimit))
		}
	}
	area := sumBody - (cardChrome + sideRows*liH) - 14 - cardChrome - 4
	cons, need := consumablesHTML(p, art, area)
	h := htmlH + max(0, need-area)

	b.WriteString(`<div class="cols" style="flex:1;grid-template-columns:1.08fr 1fr">`)
	b.WriteString(`<div class="stack">` + encountersHTML(p, rh) + top + `</div>`)
	b.WriteString(fmt.Sprintf(`<div class="stack"><div class="cols" style="grid-template-columns:repeat(%d,minmax(0,1fr));align-items:start">`, len(side)))
	b.WriteString(strings.Join(side, ""))
	b.WriteString(`</div>` + cons + `</div></div>`)
	return htmlPage{Name: "1", HTML: htmlDoc(art, htmlW, h, 22, b.String()), W: htmlW, H: h}
}

func encountersHTML(p discordPost, rh int) string {
	var b strings.Builder
	top := 0.0
	for _, x := range p.Bosses {
		top = math.Max(top, x.Damage)
	}
	b.WriteString(`<div class="card" style="--bar:#d9a520;align-self:start;width:100%"><div class="ch"><h2>Энкаунтеры</h2>`)
	b.WriteString(fmt.Sprintf(`<span class="r">убито %d из %d</span></div>`, p.killed(), len(p.Bosses)))
	b.WriteString(`<table><thead><tr><th style="width:36px">№</th><th>Босс</th><th>Итог</th><th class="r">Вайпов</th><th class="r">Бой</th><th class="r">Смертей</th><th class="r" style="width:30%">Урон рейда</th></tr></thead><tbody>`)
	for i, x := range p.Bosses {
		pill := `<span class="pill kill">Убит</span>`
		if !x.Kill {
			pill = `<span class="pill wipe">Вайп</span>`
		}
		w := 0.0
		if top > 0 {
			w = x.Damage / top
		}
		wipes := fmt.Sprint(x.Wipes)
		if x.Wipes == 0 {
			wipes = `<span class="dim">0</span>`
		}
		deaths := fmt.Sprint(x.Deaths)
		if x.Deaths == 0 {
			deaths = `<span class="dim">0</span>`
		}
		b.WriteString(fmt.Sprintf(`<tr style="height:%dpx"><td class="dim n">%d</td><td class="boss">%s</td><td>%s</td><td class="r n">%s</td><td class="r n">%s</td><td class="r n">%s</td><td><div class="db"><i style="width:calc((100%% - 92px) * %.3f)"></i><span class="n">%s</span></div></td></tr>`,
			rh, i+1, esc(x.Name), pill, wipes, clock(x.Time), deaths, w, numRU(x.Damage)))
	}
	b.WriteString(`</tbody>`)
	b.WriteString(fmt.Sprintf(`<tfoot><tr style="height:%dpx"><td></td><td>Всего</td><td>%d / %d</td><td class="r n">%d</td><td class="r n">%s</td><td class="r n">%d</td><td class="r n">%s</td></tr></tfoot>`,
		rh, p.killed(), len(p.Bosses), p.wipes(), nbsp(hoursRUShort(p.Combat)), p.Deaths, numRU(p.Damage)))
	b.WriteString(`</table></div>`)
	return b.String()
}

func topHTML(p discordPost) string {
	if len(p.Top) == 0 {
		return ""
	}
	return `<div class="card top5" style="--bar:#e0483c"><div class="ch"><h2>Топ-5 рейда</h2><span class="r">ДПС за рейд</span></div>` +
		barList("Игрок", "ДПС · урон", p.Top, 5, "") + `</div>`
}

func immortalHTML(p discordPost) (string, int) {
	var b strings.Builder
	b.WriteString(`<div class="card" style="--bar:#3fbf55"><div class="ch"><h2>Бессмертные</h2>`)
	if len(p.Immortal) == 0 {
		b.WriteString(`</div><div class="empty">Умирали все.</div></div>`)
		return b.String(), 1
	}
	b.WriteString(fmt.Sprintf(`<span class="r">%d из %d</span></div>`, len(p.Immortal), max(p.Size, len(p.Immortal))))
	list := p.Immortal
	more := 0
	if len(list) > immortalMax {
		more = len(list) - immortalMax + 1
		list = list[:immortalMax-1]
	}
	for _, m := range list {
		b.WriteString(`<div class="li">` + nameSpan(m.Name, m.Class))
		if m.Tries > 0 {
			b.WriteString(fmt.Sprintf(`<span class="s n">за %d %s</span>`, m.Tries, ruPlural(m.Tries, "попытку", "попытки", "попыток")))
		}
		b.WriteString(`</div>`)
	}
	if more > 0 {
		b.WriteString(fmt.Sprintf(`<div class="li dim">и ещё %d</div>`, more))
	}
	b.WriteString(`</div>`)
	return b.String(), min(len(p.Immortal), immortalMax)
}

func rowsBlock(title, color string, rows []discordRow, limit int) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<div class="card" style="--bar:` + color + `"><div class="ch"><h2>` + esc(title) + `</h2></div>`)
	for i, r := range rows {
		if i >= limit {
			break
		}
		b.WriteString(`<div class="li">` + nameSpan(r.Name, r.Class))
		val := nbsp(groupNum(r.V))
		if r.S != "" {
			val = esc(r.S)
		}
		b.WriteString(`<span class="v n">` + val + `</span></div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func goldAmount(copper float64) string {
	g := copper / 10000
	if g > 0 && g < 10 {
		s := strings.Replace(fmt.Sprintf("%.1f", g), ".", ",", 1)
		return strings.TrimSuffix(s, ",0")
	}
	return nbsp(groupNum(g))
}

func (a pageArt) coin() string {
	if a.Gold != "" {
		return `<img class="gi" src="` + a.Gold + `">`
	}
	return `<span class="coin sm"></span>`
}

func (a pageArt) money(copper float64) string {
	return `<span class="n">` + goldAmount(copper) + `</span>` + a.coin()
}

type consLine struct {
	html string
	h    int
	head bool
	cat  int
}

func (c discordConsumable) cost() (float64, bool) {
	if c.HasCost {
		return c.Cost, true
	}
	sum, ok := 0.0, false
	for _, it := range c.Items {
		if it.HasCost {
			sum, ok = sum+it.Cost, true
		}
	}
	return sum, ok
}

func consumablesHTML(p discordPost, art pageArt, area int) (string, int) {
	var b strings.Builder
	total := 0.0
	for _, c := range p.Consumables {
		total += c.V
	}
	b.WriteString(`<div class="card" style="--bar:#d9a520"><div class="ch"><h2>Расходники</h2>`)
	if len(p.Consumables) == 0 {
		b.WriteString(`</div><div class="empty">Расходников в записи нет.</div></div>`)
		return b.String(), 0
	}
	b.WriteString(`<span class="r n">` + nbsp(groupNum(total)) + ` шт.`)
	if p.HasCost {
		b.WriteString(`<span class="gsum">` + art.money(p.Cost) + `</span>`)
	}
	b.WriteString(`</span></div>`)
	var lines []consLine
	heads := map[int]string{}
	for ci, c := range p.Consumables {
		col, ok := consumableColors[c.Key]
		if !ok {
			col = "#9a9a9a"
		}
		label := c.Label
		if label == "" {
			label = c.Key
		}
		price := ""
		if v, ok := c.cost(); ok {
			price = art.money(v)
		}
		head := `<div class="cg"><span class="dot" style="background:` + col + `;color:` + col + `"></span><span class="nm">` + esc(label) +
			`</span><span class="x n">x` + nbsp(groupNum(c.V)) + `</span><span class="pr">` + price + `</span></div>`
		heads[ci] = head
		lines = append(lines, consLine{head, cgH, true, ci})
		for _, it := range c.Items {
			icon := `<span class="ic no"></span>`
			if uri := art.icon(it.Icon); uri != "" {
				icon = `<img class="ic" src="` + uri + `">`
			}
			price := `<span class="pr no">нет цены</span>`
			if it.HasCost {
				price = `<span class="pr">` + art.money(it.Cost) + `</span>`
			}
			lines = append(lines, consLine{`<div class="it">` + icon + `<span class="nm">` + esc(it.Name) + `</span><span class="x n">x` +
				nbsp(groupNum(it.V)) + `</span>` + price + `</div>`, itH, false, ci})
		}
	}
	height := func(ls []consLine) int {
		s := 0
		for _, l := range ls {
			s += l.h
		}
		return s
	}
	write := func(ls []consLine) {
		for _, l := range ls {
			b.WriteString(l.html)
		}
	}
	need := height(lines)
	split := -1
	if need > area {
		best := need
		for k := 1; k < len(lines); k++ {
			if lines[k-1].head {
				continue
			}
			h2 := height(lines[k:])
			if !lines[k].head {
				h2 += cgH
			}
			if m := max(height(lines[:k]), h2); m < best {
				split, best = k, m
			}
		}
		if split > 0 {
			need = best
		}
	}
	if split < 0 {
		b.WriteString(`<div class="cons">`)
		write(lines)
		b.WriteString(`</div></div>`)
		return b.String(), need
	}
	b.WriteString(`<div class="cons" style="grid-template-columns:minmax(0,1fr) minmax(0,1fr)"><div>`)
	write(lines[:split])
	b.WriteString(`</div><div>`)
	if !lines[split].head {
		b.WriteString(heads[lines[split].cat])
	}
	write(lines[split:])
	b.WriteString(`</div></div></div>`)
	return b.String(), need
}

func bossRows(b discordBoss) (int, int) {
	m := 0
	for i, t := range b.Targets {
		if i >= 2 {
			break
		}
		m = max(m, min(len(t.Rows), htmlTargetMax))
	}
	room := htmlBossBody - 18
	if m > 0 {
		room -= 10 + 20 + m*htmlBossRowH
	}
	return max(room/htmlBossRowH, 3), m
}

func barList(head, tail string, rows []discordRow, limit int, unit string) string {
	var b strings.Builder
	b.WriteString(`<div>`)
	if head != "" || tail != "" {
		b.WriteString(`<div class="lh"><span>` + head + `</span><span>` + tail + `</span></div>`)
	}
	if len(rows) == 0 {
		b.WriteString(`<div class="empty">нет данных</div></div>`)
		return b.String()
	}
	top := 0.0
	for _, r := range rows {
		top = math.Max(top, r.V)
	}
	for i, r := range rows {
		if i >= limit {
			break
		}
		w := 0.0
		if top > 0 {
			w = 100 * r.V / top
		}
		b.WriteString(fmt.Sprintf(`<div class="row"><span class="rk n">%d</span><div class="bar"><i style="width:%.1f%%;background:linear-gradient(90deg,%s,%s);--c:%s"></i>%s`,
			i+1, w, classCSS(r.Class, .20), classCSS(r.Class, .07), classCSS(r.Class, .85), nameSpan(r.Name, r.Class)))
		switch unit {
		case "":
			b.WriteString(`<span class="v n">` + nbsp(groupNum(r.V)) + `</span><span class="t n">` + numRU(r.T) + `</span>`)
		default:
			if r.S != "" {
				b.WriteString(`<span class="s">` + esc(r.S) + `</span>`)
			}
			b.WriteString(`<span class="v n">` + unitValue(unit, r.V) + `</span>`)
		}
		b.WriteString(`</div></div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func unitValue(unit string, v float64) string {
	switch unit {
	case "pct":
		return fmt.Sprintf("%.0f%%", v)
	case "count":
		return nbsp(groupNum(v))
	}
	return numRU(v)
}

func bossHead(b discordBoss) string {
	bar, pill := "#3fbf55", `<span class="pill kill">Убит</span>`
	if !b.Kill {
		bar, pill = "#e0483c", `<span class="pill wipe">Вайп</span>`
	}
	wipes := "без вайпов"
	if b.Wipes > 0 {
		wipes = fmt.Sprintf("вайпов %d", b.Wipes)
	}
	return `<div class="card bcard" style="--bar:` + bar + `"><div class="bh"><h2>` + esc(b.Name) + `</h2>` + pill +
		`<span class="m n">` + clock(b.Time) + `</span><span class="m"><small>` + wipes + `</small></span></div>`
}

func targetHTML(t discordTarget, limit int) string {
	return `<div><div class="tt">` + esc(t.Title) + `</div>` + barList("", "", t.Rows, limit, unitOr(t.Unit)) + `</div>`
}

func bossCardHTML(b discordBoss) string {
	var s strings.Builder
	s.WriteString(bossHead(b))
	n, m := bossRows(b)
	s.WriteString(`<div class="pair">`)
	s.WriteString(barList("ДПС", "урон", b.DPS, n, ""))
	s.WriteString(barList("ХПС", "нахилено", b.HPS, min(n, hpsMax), ""))
	s.WriteString(`</div>`)
	if m > 0 {
		s.WriteString(`<div class="tgt">`)
		for i, t := range b.Targets {
			if i >= 2 {
				break
			}
			s.WriteString(targetHTML(t, m))
		}
		s.WriteString(`</div>`)
	}
	s.WriteString(`</div>`)
	return s.String()
}

func wideRowH(bosses []discordBoss) int {
	n := 1
	for _, b := range bosses {
		n = max(n, len(b.DPS))
	}
	return min(wideRowMax, (wideBody-20)/n)
}

func wideCardHTML(b discordBoss, rowH int) string {
	var s strings.Builder
	s.WriteString(bossHead(b))
	s.WriteString(`<div class="wide">`)
	s.WriteString(barList("ДПС", "урон", b.DPS, len(b.DPS), ""))
	s.WriteString(`<div class="stack">`)
	s.WriteString(barList("ХПС", "нахилено", b.HPS, hpsMax, ""))
	nt := min(len(b.Targets), 2)
	if nt > 0 {
		hps := max(min(len(b.HPS), hpsMax), 1)
		room := wideBody - 20 - hps*rowH - 16*nt - nt*21
		m := min(wideTgtMax, room/(nt*rowH))
		for _, t := range b.Targets[:nt] {
			s.WriteString(targetHTML(t, m))
		}
	}
	s.WriteString(`</div></div></div>`)
	return s.String()
}

func unitOr(u string) string {
	if u == "" {
		return "dmg"
	}
	return u
}

func bossPerPage(p discordPost) int {
	for _, b := range p.Bosses {
		if n, _ := bossRows(b); len(b.DPS) > n {
			return 2
		}
	}
	return htmlPerPage
}

func bossPagesHTML(p discordPost, art pageArt) []htmlPage {
	var out []htmlPage
	total := len(p.Bosses)
	per := bossPerPage(p)
	for i := 0; i < total; i += per {
		j := min(i+per, total)
		var b strings.Builder
		what := fmt.Sprintf("Боссы %d–%d из %d: ", i+1, j, total)
		if j-i == 1 {
			what = fmt.Sprintf("Босс %d из %d: ", i+1, total)
		}
		b.WriteString(p.headHTML(what))
		b.WriteString(`<div class="grid">`)
		h, rowH := htmlH, htmlBossRowH
		if per == htmlPerPage {
			for _, x := range p.Bosses[i:j] {
				b.WriteString(bossCardHTML(x))
			}
			if j-i <= 2 {
				h = htmlHalfH
			}
		} else {
			rowH = wideRowH(p.Bosses[i:j])
			for _, x := range p.Bosses[i:j] {
				b.WriteString(wideCardHTML(x, rowH))
			}
		}
		b.WriteString(`</div>`)
		out = append(out, htmlPage{Name: fmt.Sprint(len(out) + 2), HTML: htmlDoc(art, htmlW, h, rowH, b.String()), W: htmlW, H: h})
	}
	return out
}

func postPages(p discordPost, art pageArt) []htmlPage {
	return append([]htmlPage{summaryHTML(p, art)}, bossPagesHTML(p, art)...)
}
