package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	webEngineDir  = "web"
	webManifestV  = 1
	webFillShare  = 0.98
	webB64        = 4.0 / 3.0
	webRawGrowth  = 1.02
	webRawMark    = "{{BUNDLE8}}"
	webFillGrowth = 1.01
	webTexKind    = ".w2"
	webpQuant     = 12
	webpWin       = 90
)

var webLimit = 10<<20 - 64<<10

var webCacheDir = func() string {
	d, err := os.UserCacheDir()
	if err != nil || d == "" {
		return ""
	}
	return filepath.Join(d, "ManaCode", "web")
}

type webZone struct {
	Map string   `json:"map"`
	Tex []string `json:"tex"`
}

type webManifest struct {
	V        int                `json:"v"`
	Name     string             `json:"name"`
	Version  string             `json:"version"`
	Files    []string           `json:"files"`
	UI       []string           `json:"ui"`
	Spare    []string           `json:"spare"`
	Zones    map[string]webZone `json:"zones"`
	Achs     []int              `json:"achs"`
	Items    map[string]string  `json:"items"`
	Small    string             `json:"small"`
	SpareMax int                `json:"spareMax"`
}

type webExport struct {
	Name    string
	HTML    []byte
	Day     string
	Fights  int
	Trimmed int
	Images  int
	Filled  int
	FillAll int
	Missing int
	Parts   string
}

var errWebTooBig = errors.New("выгрузка не влезла в предел Discord")

func loadWebManifest(web string) (webManifest, error) {
	var m webManifest
	b, err := os.ReadFile(filepath.Join(web, "manifest.json"))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("manifest.json не читается: %w", err)
	}
	if m.V != webManifestV {
		return m, fmt.Errorf("движок выгрузки формата v%d, обновлялка знает v%d", m.V, webManifestV)
	}
	return m, nil
}

type webPart struct {
	name string
	data []byte
}

func findRecord(addons string, p discordPost, day string) (webRecord, raidRange, error) {
	if err := gameFileAccess.check(true); err != nil {
		return webRecord{}, raidRange{}, err
	}
	files := svFiles(addons)
	sort.SliceStable(files, func(i, j int) bool { return fileSize(files[i]) > fileSize(files[j]) })
	for _, f := range files {
		b, err := readSavedVariables(f)
		if err != nil || !bytes.Contains(b, []byte(`["segments"]`)) {
			continue
		}
		rec := readRecord(string(b))
		if r, ok := pickRaid(rec.segs, p, day); ok {
			return rec, r, nil
		}
	}
	return webRecord{}, raidRange{}, errors.New("рейда нет в записи Raid Helper")
}

func fileSize(p string) int64 {
	if st, err := os.Stat(p); err == nil {
		return st.Size()
	}
	return 0
}

var reEngineSpell = regexp.MustCompile(`(?m)^\[(\d+)\]=("(?:[^"\\]|\\.)*")`)

func engineSpells(src []byte, icon func(int) string) string {
	var rows []string
	for _, m := range reEngineSpell.FindAllSubmatch(src, -1) {
		id, _ := strconv.Atoi(string(m[1]))
		row := "[" + string(m[1]) + "]={" + string(m[2])
		if ic := icon(id); ic != "" {
			row += "," + luaQuote(ic)
		}
		rows = append(rows, row+"}")
	}
	return "WEB_SPELLS={" + strings.Join(rows, ",\n") + "}\n"
}

func engineLuaJS(root, web string, m webManifest, spells string) ([]byte, error) {
	src := map[string]string{}
	for _, f := range m.Files {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			return nil, fmt.Errorf("нет файла аддона %s", f)
		}
		src[f] = luaStrip(string(b))
	}
	read := func(n string) string {
		b, _ := os.ReadFile(filepath.Join(web, n))
		return string(b)
	}
	v := struct {
		Name    string            `json:"name"`
		Version string            `json:"version"`
		Files   []string          `json:"files"`
		Packs   string            `json:"packs"`
		Src     map[string]string `json:"src"`
		Shim    string            `json:"shim"`
		API     string            `json:"api"`
		Boot    string            `json:"boot"`
		Spells  string            `json:"spells"`
	}{m.Name, m.Version, m.Files, "", src, read("shim.lua"), read("api.lua"), read("boot.lua"), spells}
	var b bytes.Buffer
	b.WriteString("self.MRH_LUA=")
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	b.Truncate(b.Len() - 1)
	b.WriteString(";")
	return b.Bytes(), nil
}

func wantedLua(m webManifest, book clientBook, loot map[int]lootItem) (string, []string) {
	items := map[int]lootItem{}
	for k, name := range m.Items {
		if id, err := strconv.Atoi(k); err == nil {
			items[id] = lootItem{name: unLuaString(name), q: 1}
		}
	}
	for id, it := range loot {
		items[id] = it
	}
	ids := make([]int, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	var rows, tex []string
	for _, id := range ids {
		it, ic := items[id], book.items[id]
		icon := "nil"
		if ic != "" {
			icon = luaQuote(ic)
			tex = append(tex, ic)
		}
		rows = append(rows, fmt.Sprintf("[%d]={%s,%s,%d}", id, luaQuote(it.name), icon, it.q))
	}
	var achs []string
	for _, id := range m.Achs {
		a, ok := book.achs[id]
		if !ok {
			continue
		}
		icon := "nil"
		if a.icon != "" {
			icon = luaQuote(a.icon)
			tex = append(tex, a.icon)
		}
		achs = append(achs, fmt.Sprintf("[%d]={%s,%d,%s,%s}", id, luaQuote(a.name), a.points, luaQuote(a.desc), icon))
	}
	return "local I = {" + strings.Join(rows, ",\n") + "}\nfor k, v in pairs(I) do WEB_ITEMS[k] = v end\n" +
		"local A = {" + strings.Join(achs, ",\n") + "}\nfor k, v in pairs(A) do WEB_ACHS[k] = v end\n", tex
}

func raidZone(m webManifest, p discordPost, segs []recSeg) []string {
	if z, ok := m.Zones[p.Zone]; ok {
		return z.Tex
	}
	for _, s := range segs {
		for _, z := range m.Zones {
			if s.raidMap != "" && strings.EqualFold(z.Map, s.raidMap) {
				return z.Tex
			}
		}
	}
	return nil
}

func gzipSize(b []byte) int {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return buf.Len()
}

func packBundle(parts []webPart) []byte {
	type entry [3]any
	head := make([]entry, len(parts))
	at := 0
	for i, p := range parts {
		head[i] = entry{p.name, at, len(p.data)}
		at += len(p.data)
	}
	hb, _ := json.Marshal(head)
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	var n [4]byte
	binary.LittleEndian.PutUint32(n[:], uint32(len(hb)))
	_, _ = zw.Write(n[:])
	_, _ = zw.Write(hb)
	for _, p := range parts {
		_, _ = zw.Write(p.data)
	}
	_ = zw.Close()
	return buf.Bytes()
}

func webTemplate(web string) ([]byte, error) {
	for _, n := range []string{"standalone8.html", "standalone.html"} {
		if b, err := os.ReadFile(filepath.Join(web, n)); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("нет шаблона страницы в движке выгрузки")
}

func webGrowth(tpl string) float64 {
	if strings.Contains(tpl, webRawMark) {
		return webRawGrowth
	}
	return webB64
}

func asciiRefs(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x80 {
			b.WriteRune(r)
		} else {
			b.WriteString("&#" + strconv.Itoa(int(r)) + ";")
		}
	}
	return b.String()
}

func appendRaw(out, body []byte) []byte {
	for _, c := range body {
		switch c {
		case 0:
			out = append(out, '=', '0')
		case '\r':
			out = append(out, '=', '1')
		case '<':
			out = append(out, '=', '2')
		case '=':
			out = append(out, '=', '3')
		default:
			out = append(out, c)
		}
	}
	return out
}

func webPage(tpl, title string, body []byte) []byte {
	raw := strings.Contains(tpl, webRawMark)
	t := esc(title)
	if raw {
		t = asciiRefs(t)
	}
	page := strings.Replace(tpl, "{{TITLE}}", t, 1)
	page = strings.Replace(page, "{{BUILD}}", strconv.FormatInt(time.Now().UnixMilli(), 36), 1)
	mark := "{{BUNDLE}}"
	if raw {
		mark = webRawMark
	}
	i := strings.Index(page, mark)
	if i < 0 {
		return []byte(page)
	}
	out := make([]byte, 0, len(page)+base64.StdEncoding.EncodedLen(len(body)))
	out = append(out, page[:i]...)
	if raw {
		out = appendRaw(out, body)
	} else {
		out = base64.StdEncoding.AppendEncode(out, body)
	}
	return append(out, page[i+len(mark):]...)
}

type webImage struct {
	path string
	data []byte
	err  error
}

func bakeImages(src *texSource, list []string, small *regexp.Regexp) []webImage {
	out := make([]webImage, len(list))
	var wg sync.WaitGroup
	jobs := make(chan int)
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				b, err := src.tex(list[i], small != nil && small.MatchString(webRel(list[i])))
				out[i] = webImage{path: list[i], data: b, err: err}
			}
		}()
	}
	for i := range list {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return out
}

func buildRaidHTML(addons string, p discordPost, day string) (webExport, error) {
	var ex webExport
	root, web, err := findWebEngine(addons)
	if err != nil {
		return ex, err
	}
	m, err := loadWebManifest(web)
	if err != nil {
		return ex, err
	}
	tpl, err := webTemplate(web)
	if err != nil {
		return ex, err
	}
	rec, r, err := findRecord(addons, p, day)
	if err != nil {
		return ex, err
	}
	ex.Day = r.day
	game := gameDir(addons)
	set := gameMPQ(game)
	defer set.Close()
	book := loadClientBook(game, set)
	spellIcon := func(id int) string { return book.spells[id] }
	segs := rec.segs[r.from : r.to+1]

	spellsSrc, _ := os.ReadFile(filepath.Join(web, "spells.lua"))
	luaJS, err := engineLuaJS(root, web, m, engineSpells(spellsSrc, spellIcon))
	if err != nil {
		return ex, err
	}
	var code []webPart
	for _, n := range []string{"wasmoon.js", "glue.wasm", "host.js", "runtime.js", "web.js"} {
		b, err := os.ReadFile(filepath.Join(web, n))
		if err != nil {
			return ex, fmt.Errorf("нет %s в движке выгрузки", n)
		}
		code = append(code, webPart{name: n, data: b})
	}
	code = append(code, webPart{name: "lua.js", data: luaJS})

	t0, t1 := segs[0].t0, segs[len(segs)-1].t1
	extra, itemTex := wantedLua(m, book, raidLoot(rec.top["loot"], t0, t1))
	fonts := []webPart{
		{name: "fonts/frizqt.ttf", data: clientFont(game, "FRIZQT__.TTF")},
		{name: "fonts/arialn.ttf", data: clientFont(game, "ARIALN.TTF")},
	}

	small, _ := regexp.Compile(m.Small)
	src := &texSource{set: set, addon: root}
	if c := webCacheDir(); c != "" {
		src.cache = filepath.Join(c, "tex")
	}
	seen := map[string]bool{}
	uniq := func(list []string) []string {
		var out []string
		for _, p := range list {
			rel := webRel(p)
			if p == "" || seen[rel] {
				continue
			}
			seen[rel] = true
			out = append(out, p)
		}
		return out
	}
	recSpells := recordSpells(segs)
	var always []string
	always = append(always, m.UI...)
	always = append(always, raidZone(m, p, segs)...)
	always = append(always, itemTex...)
	ids := make([]int, 0, len(recSpells))
	for id := range recSpells {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		always = append(always, book.spells[id])
	}
	always = uniq(always)
	spare := uniq(m.Spare)
	var fill []string
	for _, mm := range reEngineSpell.FindAllSubmatch(spellsSrc, -1) {
		id, _ := strconv.Atoi(string(mm[1]))
		fill = append(fill, book.spells[id])
	}
	fill = uniq(fill)

	var images []webPart
	for _, im := range bakeImages(src, always, small) {
		if im.err != nil {
			ex.Missing++
			continue
		}
		images = append(images, webPart{name: "tex/" + webRel(im.path), data: im.data})
	}
	for _, im := range bakeImages(src, spare, nil) {
		if im.err != nil || gzipSize(im.data) > m.SpareMax {
			continue
		}
		if small != nil && small.MatchString(webRel(im.path)) {
			if b, err := src.tex(im.path, true); err == nil {
				im.data = b
			}
		}
		images = append(images, webPart{name: "tex/" + webRel(im.path), data: im.data})
	}

	fights := raidFights(segs)
	ex.Fights = len(fights)
	data := buildRaidData(rec, r, nil, spellIcon) + extra
	tplLen, grow := len(tpl), webGrowth(string(tpl))
	codeGz := map[string]int{}
	est := func(dataGz int, imgs []webPart) float64 {
		n := dataGz
		for _, part := range code {
			n += codeGz[part.name]
		}
		for _, f := range fonts {
			n += len(f.data)
		}
		for _, im := range imgs {
			n += len(im.data)
		}
		return float64(n)*grow + float64(tplLen)
	}
	for _, part := range code {
		codeGz[part.name] = gzipSize(part.data)
	}
	dataGz := gzipSize([]byte(data))
	if est(dataGz, images) > float64(webLimit)*webFillShare {
		keep, cut := keepFights(fights)
		data = buildRaidData(rec, r, keep, spellIcon) + extra
		dataGz = gzipSize([]byte(data))
		ex.Trimmed = cut
	}
	base := len(images)
	budget := float64(webLimit)*webFillShare - est(dataGz, images)
	ex.FillAll = len(fill)
	if budget > 0 {
		for _, im := range bakeImages(src, fill, small) {
			if im.err != nil {
				continue
			}
			cost := float64(len(im.data)) * webFillGrowth * grow
			if cost > budget {
				break
			}
			budget -= cost
			images = append(images, webPart{name: "tex/" + webRel(im.path), data: im.data})
		}
	}
	parts := func() []webPart {
		out := append([]webPart{}, code...)
		out = append(out, webPart{name: "data.lua", data: []byte(data)})
		out = append(out, fonts...)
		return append(out, images...)
	}
	title := "Рейд " + r.day
	html := webPage(string(tpl), title, packBundle(parts()))
	for len(html) > webLimit && len(images) > base {
		images = images[:max(base, len(images)-40)]
		html = webPage(string(tpl), title, packBundle(parts()))
	}
	ex.Images, ex.Filled = len(images), len(images)-base
	ex.HTML = html
	ex.Name = "raid-" + r.day + ".html"
	ex.Parts = fmt.Sprintf("данные %d КБ gzip, движок %d КБ, картинок %d (добивка %d из %d, нет %d), развёртка урезана у %d из %d попыток",
		dataGz/1024, sumGz(codeGz)/1024, ex.Images, ex.Filled, ex.FillAll, ex.Missing, ex.Trimmed, ex.Fights)
	if len(html) > webLimit {
		return ex, errWebTooBig
	}
	return ex, nil
}

func sumGz(sizes map[string]int) int {
	n := 0
	for _, v := range sizes {
		n += v
	}
	return n
}

func raidHTMLCLI(addons, day string) int {
	start := time.Now()
	ex, err := buildRaidHTML(addons, discordPost{}, day)
	if err != nil && len(ex.HTML) == 0 {
		fmt.Println("ОШИБКА:", tr(err.Error()))
		autoLog("выгрузка рейда: %v", err)
		return 1
	}
	folder := picturesDir(addons)
	file := filepath.Join(folder, ex.Name)
	if e := os.MkdirAll(folder, 0o755); e != nil {
		fmt.Println("ОШИБКА:", e)
		return 1
	}
	if e := os.WriteFile(file, ex.HTML, 0o644); e != nil {
		fmt.Println("ОШИБКА:", e)
		return 1
	}
	fmt.Printf("%s — %s за %v\n%s\n", file, mb(int64(len(ex.HTML))), time.Since(start).Round(time.Millisecond), ex.Parts)
	autoLog("выгрузка рейда %s: %s, %d байт; %s", ex.Day, file, len(ex.HTML), ex.Parts)
	if err != nil {
		fmt.Println("ОШИБКА:", tr(err.Error()))
		return 1
	}
	return 0
}
