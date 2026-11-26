package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"image"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const sampleSV = "ManaCodeRaidHelperDB = {\n" +
	"\t[\"fxVersion\"] = 5,\n" +
	"\t[\"loot\"] = {\n\t\t{\n\t\t\t[\"q\"] = 4,\n\t\t\t[\"name\"] = \"Меч\",\n\t\t\t[\"id\"] = 50000,\n\t\t\t[\"t\"] = 1000,\n\t\t}, -- [1]\n" +
	"\t\t{\n\t\t\t[\"name\"] = \"Старый\",\n\t\t\t[\"id\"] = 40000,\n\t\t\t[\"t\"] = 500000,\n\t\t}, -- [2]\n\t},\n" +
	"\t[\"segments\"] = {\n" +
	"\t\t{\n\t\t\t[\"t0\"] = 900,\n\t\t\t[\"t1\"] = 950,\n\t\t\t[\"v\"] = 2,\n\t\t\t[\"n\"] = 5,\n\t\t\t[\"chunks\"] = {\n\t\t\t\t\"a{b\",\n\t\t\t},\n\t\t}, -- [1]\n" +
	"\t\t{\n\t\t\t[\"t0\"] = 1000,\n\t\t\t[\"t1\"] = 1100,\n\t\t\t[\"raid\"] = {\n\t\t\t\t[\"id\"] = 77,\n\t\t\t\t[\"day\"] = \"2026-11-08\",\n\t\t\t\t[\"map\"] = \"IcecrownCitadel\",\n\t\t\t},\n" +
	"\t\t\t[\"bosses\"] = {\n\t\t\t\t[\"Лорд Ребрад\"] = true,\n\t\t\t},\n\t\t\t[\"totals\"] = {\n\t\t\t\t[\"x\"] = 1,\n\t\t\t},\n" +
	"\t\t\t[\"scan\"] = {\n\t\t\t\t{\n\t\t\t\t\t[\"killed\"] = true,\n\t\t\t\t\t[\"v\"] = 17,\n\t\t\t\t},\n\t\t\t},\n" +
	"\t\t\t[\"spells\"] = \"72\\031Удар щитом\\03075\\031Стрельба\",\n\t\t\t[\"v\"] = 2,\n\t\t\t[\"n\"] = 10,\n\t\t\t[\"pos\"] = {\n\t\t\t\t[\"A\"] = {\n\t\t\t\t\t\"0,1\",\n\t\t\t\t},\n\t\t\t},\n\t\t}, -- [2]\n" +
	"\t\t{\n\t\t\t[\"t0\"] = 1200,\n\t\t\t[\"t1\"] = 1300,\n\t\t\t[\"raid\"] = {\n\t\t\t\t[\"id\"] = 77,\n\t\t\t\t[\"day\"] = \"2026-11-08\",\n\t\t\t},\n" +
	"\t\t\t[\"bosses\"] = {\n\t\t\t\t[\"Лорд Ребрад\"] = true,\n\t\t\t},\n\t\t\t[\"v\"] = 2,\n\t\t}, -- [3]\n" +
	"\t\t{\n\t\t\t[\"t0\"] = 99000,\n\t\t\t[\"t1\"] = 99100,\n\t\t\t[\"v\"] = 2,\n\t\t}, -- [4]\n" +
	"\t},\n}\nManaCodeRaidLeadDB = {\n}\n"

func TestReadRecord(t *testing.T) {
	rec := readRecord(strings.ReplaceAll(sampleSV, "\n", "\r\n"))
	if len(rec.segs) != 4 {
		t.Fatalf("сегментов %d", len(rec.segs))
	}
	s := rec.segs[1]
	if s.day != "2026-11-08" || s.id != 77 || s.raidMap != "IcecrownCitadel" || !s.totals || !s.kill || len(s.bosses) != 1 || s.t0 != 1000 {
		t.Errorf("сегмент с боссом: %+v", s)
	}
	if rec.segs[2].kill || rec.segs[0].hasRaid {
		t.Errorf("вайп и треш: %+v %+v", rec.segs[2], rec.segs[0])
	}
	if !strings.HasPrefix(s.lua, "{") || !strings.HasSuffix(s.lua, "}") {
		t.Errorf("текст сегмента: %q", s.lua[len(s.lua)-20:])
	}
	if rec.top["fxVersion"] != "5" || !strings.HasPrefix(rec.top["loot"], "{") || !strings.HasSuffix(rec.top["loot"], "\t}") {
		t.Errorf("верх записи: %q", rec.top)
	}
	r, ok := pickRaid(rec.segs, discordPost{}, "")
	if !ok || r.from != 0 || r.to != 2 || r.id != 77 {
		t.Errorf("рейд по последнему дню: %+v %v", r, ok)
	}
	if r, ok := pickRaid(rec.segs, discordPost{ID: "77", Start: 1000, Finish: 1300}, ""); !ok || r.to != 2 {
		t.Errorf("рейд по посту: %+v", r)
	}
	if _, ok := pickRaid(rec.segs, discordPost{}, "2020-01-01"); ok {
		t.Error("чужой день найден")
	}
	sp := recordSpells(rec.segs[1:2])
	if sp[72] != "Удар щитом" || sp[75] != "Стрельба" {
		t.Errorf("имена заклинаний: %v", sp)
	}
	loot := raidLoot(rec.top["loot"], 900, 1300)
	if len(loot) != 1 || loot[50000].name != "Меч" || loot[50000].q != 4 {
		t.Errorf("добыча рейда: %v", loot)
	}
	keep, cut := keepFights(raidFights(rec.segs[r.from : r.to+1]))
	if cut != 1 || !keep[2] || keep[3] {
		t.Errorf("урезка: оставить убийство %v, урезано %d", keep, cut)
	}
	data := buildRaidData(rec, r, keep, func(id int) string {
		if id == 72 {
			return `Interface\Icons\Ability_Warrior_ShieldBash`
		}
		return ""
	})
	for _, want := range []string{`["fxVersion"] = 5,`, `[72]={"Удар щитом","Interface\\Icons\\Ability_Warrior_ShieldBash"}`,
		`[75]={"Стрельба"}`, `WEB_REALM, WEB_PLAYER = "2026-11-08"`, `["bare"] = true,`} {
		if !strings.Contains(data, want) {
			t.Errorf("в данных нет %s", want)
		}
	}
	if strings.Count(data, `["bare"] = true`) != 2 {
		t.Errorf("урезаны не треш и вайп: %d", strings.Count(data, `["bare"] = true`))
	}
}

func TestStripSeg(t *testing.T) {
	seg := "{\n\t\t\t[\"pos\"] = {\n\t\t\t\t[\"A\"] = 1,\n\t\t\t},\n\t\t\t[\"strs\"] = \"x\",\n\t\t\t[\"n\"] = 10,\n\t\t\t[\"t0\"] = 1,\n\t\t}"
	got := stripSeg(seg)
	want := "{\n\t\t\t[\"pos\"] = {},\n\t\t\t[\"strs\"] = {},\n\t\t\t[\"n\"] = 0,\n\t\t\t[\"t0\"] = 1,\n\t\t\t[\"bare\"] = true,\n\t\t}"
	if got != want {
		t.Errorf("урезка:\n%s\nждали\n%s", got, want)
	}
}

func TestWebRel(t *testing.T) {
	for in, want := range map[string]string{
		`Interface\Icons\Spell_Fire_Fire`:                       "interface/icons/spell_fire_fire.png",
		`Interface\AddOns\ManaCode_RaidHelper\art\rooms\x.tga`:  "addon/art/rooms/x.png",
		`Interface\\Glues\LoadingScreens\LoadScreenRubySanctum`: "interface/glues/loadingscreens/loadscreenrubysanctum.png",
	} {
		if got := webRel(in); got != want {
			t.Errorf("%s → %s, ждали %s", in, got, want)
		}
	}
}

func TestDecodeTGAAndHalf(t *testing.T) {
	b := make([]byte, 18+2*2*4)
	b[2], b[12], b[14], b[16], b[17] = 2, 2, 2, 32, 0x20
	copy(b[18:], []byte{0, 0, 255, 255, 0, 255, 0, 255, 255, 0, 0, 0, 255, 255, 255, 255})
	img, err := decodeTGA(b)
	if err != nil {
		t.Fatal(err)
	}
	if c := img.NRGBAAt(0, 0); c.R != 255 || c.B != 0 || c.A != 255 {
		t.Errorf("BGRA → RGBA: %v", c)
	}
	h := halfImage(img)
	if h.Rect != image.Rect(0, 0, 1, 1) || h.Pix[3] != 191 {
		t.Errorf("вдвое: %v %v", h.Rect, h.Pix)
	}
}

func unpackPage(t *testing.T, html []byte) map[string][]byte {
	t.Helper()
	m := regexp.MustCompile(`(?s)<script id="mrh-bundle"[^>]*>([^<]*)</script>`).FindSubmatch(html)
	if m == nil {
		t.Fatal("нет тела страницы")
	}
	var body []byte
	if bytes.Contains(html, []byte(`charset="windows-1252"`)) {
		for i := 0; i < len(m[1]); i++ {
			c := m[1][i]
			if c == '=' && i+1 < len(m[1]) {
				i++
				c = [4]byte{0, 13, '<', '='}[m[1][i]-'0']
			}
			body = append(body, c)
		}
	} else {
		var err error
		body, err = base64.StdEncoding.DecodeString(strings.TrimSpace(string(m[1])))
		if err != nil {
			t.Fatal(err)
		}
	}
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	n := binary.LittleEndian.Uint32(raw)
	var head [][3]any
	if err := json.Unmarshal(raw[4:4+n], &head); err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	base := 4 + int(n)
	for _, h := range head {
		at, ln := int(h[1].(float64)), int(h[2].(float64))
		out[h[0].(string)] = raw[base+at : base+at+ln]
	}
	return out
}

func TestPackBundle(t *testing.T) {
	tpl := `<title>{{TITLE}}</title><script id="mrh-bundle" type="application/octet-stream">{{BUNDLE}}</script><i>{{BUILD}}</i>`
	html := webPage(tpl, "Рейд <1>", packBundle([]webPart{{"a.js", []byte("aa")}, {"tex/b.png", []byte{1, 2, 3}}}))
	if !bytes.Contains(html, []byte("Рейд &lt;1&gt;")) || bytes.Contains(html, []byte("{{")) {
		t.Errorf("шаблон: %s", html)
	}
	files := unpackPage(t, html)
	if string(files["a.js"]) != "aa" || !bytes.Equal(files["tex/b.png"], []byte{1, 2, 3}) {
		t.Errorf("тело: %v", files)
	}
}

func TestWebClient(t *testing.T) {
	game := os.Getenv("WEB_CLIENT")
	if game == "" {
		t.Skip("WEB_CLIENT=<корень клиента> — сборка выгрузки на настоящей записи")
	}
	oldCfg, oldCache := userConfigDir, webCacheDir
	userConfigDir = os.UserConfigDir
	webCacheDir = func() string {
		d, _ := os.UserCacheDir()
		return filepath.Join(d, "ManaCode", "web")
	}
	t.Cleanup(func() { userConfigDir, webCacheDir = oldCfg, oldCache })
	start := time.Now()
	ex, err := buildRaidHTML(filepath.Join(game, "Interface", "AddOns"), discordPost{}, os.Getenv("WEB_DAY"))
	if err != nil {
		t.Fatalf("%v (%s)", err, ex.Parts)
	}
	t.Logf("%s: %d байт за %v; %s", ex.Name, len(ex.HTML), time.Since(start), ex.Parts)
	files := unpackPage(t, ex.HTML)
	for _, n := range []string{"wasmoon.js", "glue.wasm", "lua.js", "data.lua", "fonts/frizqt.ttf", "fonts/arialn.ttf",
		"tex/interface/glues/charactercreate/ui-charactercreate-classes.png"} {
		if len(files[n]) == 0 {
			t.Errorf("нет %s", n)
		}
	}
	if out := os.Getenv("WEB_OUT"); out != "" {
		if err := os.WriteFile(out, ex.HTML, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWebEngineFetch(t *testing.T) {
	stubUserConfig(t)
	cache := t.TempDir()
	old := webCacheDir
	webCacheDir = func() string { return cache }
	t.Cleanup(func() { webCacheDir = old })
	webFetchMu.Lock()
	webFetchFail = map[string]webFetchMiss{}
	webFetchMu.Unlock()
	withWeb := rhFiles("0.2.0")
	withWeb[raidHelperName+"/web/manifest.json"] = `{"v":1}`
	withWeb[raidHelperName+"/web/web.js"] = "MRHWeb"
	withWeb[raidHelperName+"/web/../../evil.js"] = "nope"
	list := []ghRelease{
		relWith("v0.2.0", false, zipName("v0.2.0"), liteName("v0.2.0")),
		relWith("v0.1.9", false, zipName("v0.1.9"), liteName("v0.1.9")),
	}
	zips := map[string][]byte{
		zipName("v0.2.0"):  rhZip(t, "0.2.0", "ICC"),
		liteName("v0.2.0"): zipOf(t, withWeb),
		zipName("v0.1.9"):  rhZip(t, "0.1.9"),
		liteName("v0.1.9"): rhZip(t, "0.1.9"),
	}
	srv, hits := releaseServerHits(t, list, zips)
	oldRepo := raidHelperRepo
	raidHelperRepo = srv.URL + "/releases"
	t.Cleanup(func() { raidHelperRepo = oldRepo })

	addons := filepath.Join(t.TempDir(), "AddOns")
	rh := filepath.Join(addons, raidHelperName)
	put := func(ver string) {
		if err := os.MkdirAll(rh, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(rh, raidHelperName+".toc"), []byte("## Version: "+ver+"\r\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("0.2.0")
	src, web, err := findWebEngine(addons)
	if err != nil || src != rh || web != filepath.Join(cache, "0.2.0", raidHelperName, "web") {
		t.Fatalf("движок из лёгкого архива: %q %q %v", src, web, err)
	}
	if b, _ := os.ReadFile(filepath.Join(web, "web.js")); string(b) != "MRHWeb" {
		t.Errorf("web.js не распакован: %q", b)
	}
	if _, err := os.Stat(filepath.Join(cache, "evil.js")); err == nil {
		t.Error("путь с .. распакован")
	}
	if hits.full[liteName("v0.2.0")] != 0 || hits.parts[liteName("v0.2.0")] == 0 || hits.parts[zipName("v0.2.0")] != 0 {
		t.Errorf("лёгкий архив читается частями, полный не трогается: %v %v", hits.full, hits.parts)
	}
	before := hits.parts[liteName("v0.2.0")]
	if _, _, err := findWebEngine(addons); err != nil || hits.parts[liteName("v0.2.0")] != before {
		t.Errorf("второй раз — из кэша без сети: %v", err)
	}

	put("0.1.9")
	if _, _, err := findWebEngine(addons); err == nil || !strings.Contains(err.Error(), "нет папки web") {
		t.Errorf("в архиве нет web: %v", err)
	}
	asked := hits.parts[liteName("v0.1.9")]
	if _, _, err := findWebEngine(addons); err == nil || hits.parts[liteName("v0.1.9")] != asked {
		t.Errorf("неудача помнится час, GitHub не дёргаем: %v", err)
	}
	put("0.3.0")
	if _, _, err := findWebEngine(addons); err == nil || !strings.Contains(err.Error(), "такой версии нет") {
		t.Errorf("нет релиза версии: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(rh, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rh, "web", "manifest.json"), []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if src, web, err := findWebEngine(addons); err != nil || src != rh || web != filepath.Join(rh, "web") {
		t.Errorf("web в папке аддона главнее кэша: %q %q %v", src, web, err)
	}
}

func TestRaidPageWithoutEngine(t *testing.T) {
	stubUserConfig(t)
	page, err := raidPage(filepath.Join(t.TempDir(), "AddOns"), discordPost{Raid: "ЦЛК 25"})
	if page != nil || err == nil || !strings.Contains(err.Error(), "Raid Helper") {
		t.Errorf("без аддона — пост без страницы с причиной: %v %v", page, err)
	}
}

func itemCacheBytes(recs ...[]byte) []byte {
	b := make([]byte, 24)
	copy(b, "BDIW")
	binary.LittleEndian.PutUint32(b[4:], 12340)
	for _, r := range recs {
		b = append(b, r...)
	}
	return b
}

func itemRec(id uint32, body []byte) []byte {
	r := binary.LittleEndian.AppendUint32(nil, id)
	r = binary.LittleEndian.AppendUint32(r, uint32(len(body)))
	return append(r, body...)
}

func TestItemCacheCorrupt(t *testing.T) {
	good := append(make([]byte, 12), 0, 0, 0, 0)
	good = binary.LittleEndian.AppendUint32(good, 77)
	b := itemCacheBytes(itemRec(1, []byte{1, 2, 3, 4}), itemRec(2, good), itemRec(3, nil))
	out := parseItemCache(b)
	if out[2] != 77 || len(out) != 1 {
		t.Errorf("короткая запись пропускается, следующая читается: %v", out)
	}
	huge := itemCacheBytes(itemRec(5, nil))
	binary.LittleEndian.PutUint32(huge[28:], 0xFFFFFFFF)
	if out := parseItemCache(huge); len(out) != 0 {
		t.Errorf("длина за концом файла: %v", out)
	}
}

func TestParseDBCBounds(t *testing.T) {
	head := func(recs, fields, size, strLen uint32) []byte {
		b := []byte("WDBC")
		for _, v := range []uint32{recs, fields, size, strLen} {
			b = binary.LittleEndian.AppendUint32(b, v)
		}
		return b
	}
	ok := append(head(1, 1, 4, 3), 1, 0, 0, 0, 0, 'a', 0)
	if d := parseDBC(ok); d == nil || d.text(0, 0) != "a" {
		t.Fatalf("правильный DBC: %+v", d)
	}
	bad := map[string][]byte{
		"recs*size переполняет": append(head(0xFFFFFFFF, 1, 0xFFFFFFFF, 0), make([]byte, 8)...),
		"записи за концом":      append(head(100, 1, 4, 0), make([]byte, 8)...),
		"размер записи ноль":    append(head(1000000, 0, 0, 0), make([]byte, 8)...),
		"строки длиннее файла":  append(head(1, 1, 4, 1000), 1, 0, 0, 0, 0),
		"короткий":              []byte("WDBC"),
	}
	for why, b := range bad {
		if d := parseDBC(b); d != nil {
			t.Errorf("%s: принят %+v", why, d)
		}
	}
}

func TestReadMPQBadHeader(t *testing.T) {
	head := func(shift uint16, hashAt, hashN uint32) []byte {
		b := make([]byte, 64)
		copy(b, "MPQ")
		binary.LittleEndian.PutUint16(b[14:], shift)
		binary.LittleEndian.PutUint32(b[16:], hashAt)
		binary.LittleEndian.PutUint32(b[20:], 32)
		binary.LittleEndian.PutUint32(b[24:], hashN)
		binary.LittleEndian.PutUint32(b[28:], 1)
		return b
	}
	for why, b := range map[string][]byte{
		"сдвиг сектора 60":  head(60, 32, 1),
		"сдвиг сектора 54":  head(54, 32, 1),
		"таблица за концом": head(3, 32, 1000),
		"таблица далеко":    head(3, 0xFFFFFF00, 1),
	} {
		if _, err := readMPQ(bytes.NewReader(b), int64(len(b))); err == nil {
			t.Errorf("%s: архив принят", why)
		}
	}
}

func TestGlobBrackets(t *testing.T) {
	game := filepath.Join(t.TempDir(), "WoW [3.3.5a]")
	wdb := filepath.Join(game, "Cache", "WDB", "ruRU")
	if err := os.MkdirAll(wdb, 0o755); err != nil {
		t.Fatal(err)
	}
	good := append(make([]byte, 12), 0, 0, 0, 0)
	good = binary.LittleEndian.AppendUint32(good, 42)
	if err := os.WriteFile(filepath.Join(wdb, "itemcache.wdb"), itemCacheBytes(itemRec(9, good)), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := itemCacheDisplays(game); out[9] != 42 {
		t.Errorf("кэш предметов в папке со скобками: %v", out)
	}
	sv := filepath.Join(game, "x.lua")
	for _, n := range []string{sv + mineSuffix + "1", sv + mineSuffix + "2", sv + mineSuffix + "3.tmp"} {
		if err := os.WriteFile(n, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := mineCopies(sv); len(got) != 2 {
		t.Errorf("копии в папке со скобками: %v", got)
	}
	if q := globQuote("a[b]*?"); q != "a[[]b][*][?]" {
		t.Errorf("globQuote: %s", q)
	}
}
