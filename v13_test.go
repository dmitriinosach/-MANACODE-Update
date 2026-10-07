package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type v13Env struct {
	t     *testing.T
	dir   string
	mu    sync.Mutex
	body  map[string]string
	etag  string
	baked string
	hits  map[string]int
	bad   string
}

func (e *v13Env) hit(p string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hits[p]
}

func (e *v13Env) total() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for p, k := range e.hits {
		if strings.HasSuffix(p, ".zip") {
			n += k
		}
	}
	return n
}

func luaFor(path, baked string) string {
	if path == playersFile {
		return "PlayerRaids13 = PlayerRaids13 or {}\nPlayerRaids13.players = {\n[1]=\"Имя|MAGE||||||7\",\n}\nPlayerRaids13.meta = { v = 13, baked = \"" + baked + "\", season = 7, seasons = { 7, 6 }, count = 1 }\n"
	}
	return "PlayerRaids13 = PlayerRaids13 or {}\n-- " + path + "\nPlayerRaids13.x = {\n[1]=\"1\",\n}\n"
}

func (e *v13Env) manifest() manifest {
	e.mu.Lock()
	defer e.mu.Unlock()
	man := manifest{Format: 13, Baked: e.baked, Season: 7, Seasons: []int{7, 6},
		Zones: map[string]v13Zone{"icc": {Title: "ЦЛК"}, "rs": {Title: "РС"}, "toc": {Title: "ИВК"}}}
	for _, z := range []string{"icc", "rs", "toc"} {
		for _, sz := range []int{25, 10} {
			for _, d := range []string{"h", "n"} {
				id := z + map[int]string{25: "25", 10: "10"}[sz] + d
				man.Modes = append(man.Modes, v13Mode{ID: id, Zone: z, Size: sz, Diff: d, Title: id})
			}
		}
	}
	add := func(path, kind string, season int, mode string) {
		body := luaFor(path, e.baked)
		e.body[path] = body
		sum := sha256.Sum256([]byte(body))
		sha := hex.EncodeToString(sum[:])
		if path == e.bad {
			sha = strings.Repeat("0", 64)
		}
		man.Files = append(man.Files, v13File{Path: path, Kind: kind, Season: season, Mode: mode, Rows: 1,
			Bytes: int64(len(body)), SHA256: sha, Zip: "v13/" + strings.TrimSuffix(path, ".lua") + ".zip", ZipLen: 100})
	}
	add(playersFile, "players", 0, "")
	add("s7.lua", "season", 7, "")
	add("s6.lua", "season", 6, "")
	for _, md := range []string{"icc25h", "icc25n", "rs25h", "toc25h"} {
		add("s7_"+md+".lua", "mode", 7, md)
	}
	add("s6_icc25h.lua", "mode", 6, "icc25h")
	return man
}

func newV13Env(t *testing.T) *v13Env {
	t.Helper()
	retryPause = 0
	stubGame(t, false)
	e := &v13Env{t: t, body: map[string]string{}, etag: `"m1"`, baked: "2026-10-06T20:00:00Z", hits: map[string]int{}}
	oldPass, oldData, oldRel, oldRH, oldNow := zipPass, dataBase, releaseAPI, raidHelperRepo, now
	zipPass = "raids-circle"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.hits[r.URL.Path]++
		etag := e.etag
		e.mu.Unlock()
		switch {
		case r.URL.Path == "/data/"+manifestPath:
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", etag)
			_ = json.NewEncoder(w).Encode(e.manifest())
		case strings.HasPrefix(r.URL.Path, "/data/v13/") && strings.HasSuffix(r.URL.Path, ".zip"):
			name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/data/v13/"), ".zip") + ".lua"
			e.mu.Lock()
			body, ok := e.body[name]
			e.mu.Unlock()
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(aesZip(t, addonName+"/data/"+name, []byte(body), zipPass))
		default:
			http.NotFound(w, r)
		}
	}))
	dataBase = srv.URL + "/data/"
	releaseAPI = srv.URL + "/latest"
	raidHelperRepo = ""
	t.Cleanup(func() {
		srv.Close()
		zipPass, dataBase, releaseAPI, raidHelperRepo, now = oldPass, oldData, oldRel, oldRH, oldNow
	})
	_, e.dir = layout(t)
	if err := os.WriteFile(filepath.Join(e.dir, tocFile), []byte("## Version: 0.19.0\r\n## X-DataFormat: 13\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(e.dir, codeDir), 0o755); err != nil {
		t.Fatal(err)
	}
	return e
}

func dataFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, _ := os.ReadDir(dataDir(dir))
	var out []string
	for _, e := range entries {
		if !e.IsDir() && e.Name() != configFile && e.Name() != logFile {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func putData(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dataDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir(dir), name), []byte("старое"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestV13Defaults(t *testing.T) {
	e := newV13Env(t)
	man := e.manifest()
	sel := choose13(config{}, &man, nil)
	if !sel.seasons[7] || !sel.seasons[6] {
		t.Errorf("без выбора — все сезоны: %v", sel.seasons)
	}
	for _, md := range man.Modes {
		want := md.Zone == "icc" || md.Zone == "rs"
		if sel.modes[md.ID] != want {
			t.Errorf("режим %s по умолчанию %v, ждали %v", md.ID, sel.modes[md.ID], want)
		}
	}
	sel = choose13(config{Seasons: []int{7}}, &man, nil)
	if sel.seasons[6] || !sel.seasons[7] {
		t.Errorf("сезоны из выбора v12: %v", sel.seasons)
	}
	sel = choose13(config{}, &man, map[int]bool{6: true})
	if !sel.seasons[6] || !sel.seasons[7] {
		t.Errorf("сезоны из игры + текущий: %v", sel.seasons)
	}
	sel = choose13(config{V13: &v13Choice{Seasons: []int{}, Modes: []string{"toc25h"}}}, &man, nil)
	if sel.seasons[6] || !sel.seasons[7] || !sel.modes["toc25h"] || sel.modes["icc25h"] {
		t.Errorf("сохранённый выбор v13: %+v", sel)
	}
}

func TestV13DownloadAndClean(t *testing.T) {
	e := newV13Env(t)
	for _, n := range []string{"Data.lua", "Data_s6.lua", "s7_toc10n.lua", "s5.lua", "Gm.lua"} {
		putData(t, e.dir, n)
	}
	man := e.manifest()
	written, err := download13(&man, e.dir, choose13(config{}, &man, nil), &counter{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Gm.lua", "Players.lua", "s6.lua", "s6_icc25h.lua", "s7.lua", "s7_icc25h.lua", "s7_icc25n.lua", "s7_rs25h.lua"}
	if got := dataFiles(t, e.dir); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("в data/:\n got %v\nwant %v", got, want)
	}
	if len(written) != 7 {
		t.Errorf("записано %d: %v", len(written), written)
	}
	if got := localBaked(e.dir); got != e.baked {
		t.Errorf("baked из Players.lua: %q", got)
	}
	lm := readLocal13(e.dir)
	if lm.Count != 1 || lm.Seasons[7] != 1 || !lm.Modes["rs25h"] || lm.Modes["toc25h"] {
		t.Errorf("readLocal13: %+v", lm)
	}

	before := e.total()
	written, err = download13(&man, e.dir, choose13(config{}, &man, nil), &counter{})
	if err != nil || len(written) != 0 || e.total() != before {
		t.Errorf("повтор без изменений качает заново: %v %v, архивов %d", written, err, e.total()-before)
	}

	sel := choose13(config{V13: &v13Choice{Seasons: []int{7}, Modes: []string{"icc25h"}}}, &man, nil)
	if _, err := download13(&man, e.dir, sel, &counter{}); err != nil {
		t.Fatal(err)
	}
	want = []string{"Gm.lua", "Players.lua", "s7.lua", "s7_icc25h.lua"}
	if got := dataFiles(t, e.dir); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("снятые галочки не убрались:\n got %v\nwant %v", got, want)
	}
	if e.total() != before {
		t.Errorf("уборка качала архивы: %d", e.total()-before)
	}

	if err := os.WriteFile(filepath.Join(dataDir(e.dir), "s7.lua"), []byte("порчено"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := download13(&man, e.dir, sel, &counter{}); err != nil {
		t.Fatal(err)
	}
	if e.hit("/data/v13/s7.zip") != 2 || e.hit("/data/v13/s7_icc25h.zip") != 1 {
		t.Errorf("перекачан только изменившийся: s7 %d, s7_icc25h %d", e.hit("/data/v13/s7.zip"), e.hit("/data/v13/s7_icc25h.zip"))
	}
}

func TestV13BadChecksumKeepsOld(t *testing.T) {
	e := newV13Env(t)
	e.bad = "s7_icc25h.lua"
	putData(t, e.dir, "Data.lua")
	man := e.manifest()
	_, err := download13(&man, e.dir, choose13(config{}, &man, nil), &counter{})
	if err == nil || !strings.Contains(err.Error(), "контрольная сумма") {
		t.Fatalf("ждали ошибку суммы, а %v", err)
	}
	if got := dataFiles(t, e.dir); strings.Join(got, ",") != "Data.lua" {
		t.Errorf("при ошибке данные трогать нельзя: %v", got)
	}
}

func TestV13Auto(t *testing.T) {
	e := newV13Env(t)
	t0 := time.Date(2026, 10, 7, 12, 0, 0, 0, time.Local)
	now = func() time.Time { return t0 }
	cfg := loadConfig(e.dir)
	cfg.Auto = &autoConfig{AddonsAt: t0, AddonHours: 4}
	cfg.V13 = &v13Choice{Seasons: []int{7}, Modes: []string{"icc25h"}}
	saveConfig(e.dir, cfg)
	putData(t, e.dir, "Data.lua")
	if code := runAuto(filepath.Dir(e.dir), &counter{}); code != 0 {
		t.Fatalf("код %d", code)
	}
	if got := strings.Join(dataFiles(t, e.dir), ","); got != "Players.lua,s7.lua,s7_icc25h.lua" {
		t.Errorf("после -auto: %s", got)
	}
	if st := loadConfig(e.dir).auto(); st.ETag != `"m1"` || st.Baked != e.baked {
		t.Errorf("заголовок не запомнен: %+v", st)
	}
	n := e.total()
	now = func() time.Time { return t0.Add(15 * time.Minute) }
	runAuto(filepath.Dir(e.dir), &counter{})
	if e.total() != n {
		t.Error("304 — качать нельзя")
	}
	e.mu.Lock()
	e.etag, e.baked = `"m2"`, "2026-10-07T12:30:00Z"
	e.mu.Unlock()
	now = func() time.Time { return t0.Add(30 * time.Minute) }
	runAuto(filepath.Dir(e.dir), &counter{})
	if got := localBaked(e.dir); got != "2026-10-07T12:30:00Z" {
		t.Errorf("новая выгрузка не легла: %q", got)
	}
	if e.hit("/data/v13/s7.zip") != 1 {
		t.Errorf("неизменившееся ядро перекачано: %d", e.hit("/data/v13/s7.zip"))
	}
}

func TestV13CardView(t *testing.T) {
	e := newV13Env(t)
	man := e.manifest()
	for i := range man.Files {
		man.Files[i].ZipLen = 1 << 20
	}
	stubHome(t, "")
	a := newApp(filepath.Dir(e.dir), nil)
	t.Cleanup(func() { homeDir = "" })
	c := a.cards[0]
	c.rel = &otherRelease{local: "0.19.0", tag: "v0.19.0"}
	c.man = &man
	c.sel13 = choose13(config{}, &man, nil)
	v := a.cardView(c)
	if v.dataTitle != "Данных нет" || v.dataAction != "Скачать" || !v.dataPrimary {
		t.Errorf("пусто — скачать: %q %q %t", v.dataTitle, v.dataAction, v.dataPrimary)
	}
	if len(v.seasons) != 2 || !v.seasons[0].locked || !v.seasons[0].on {
		t.Errorf("сезоны: %+v", v.seasons)
	}
	if len(v.gridHeads) != 4 || len(v.grid) != 3 || v.grid[0].title != "ЦЛК" || v.grid[2].title != "ИВК" {
		t.Fatalf("сетка: %v %+v", v.gridHeads, v.grid)
	}
	icc25h := v.grid[0].cells[3]
	if icc25h.id != "icc25h" || !icc25h.on || icc25h.label != mb(2<<20) {
		t.Errorf("ЦЛК 25 гер — оба сезона по умолчанию, 2 архива: %+v", icc25h)
	}
	if toc := v.grid[2].cells[3]; toc.on || toc.label != mb(1<<20) {
		t.Errorf("ИВК по умолчанию выключен: %+v", toc)
	}
	a.toggleMode("toc25h")
	a.toggleSeason(6)
	a.toggleSeason(7)
	v = a.cardView(c)
	if !v.grid[2].cells[3].on || v.seasons[1].on || !v.seasons[0].on || v.grid[0].cells[3].label != mb(1<<20) {
		t.Errorf("переключения: ИВК %+v, сезоны %+v, ЦЛК %+v", v.grid[2].cells[3], v.seasons, v.grid[0].cells[3])
	}
	if !strings.Contains(v.seasonsSub, "архивы") {
		t.Errorf("сумма: %q", v.seasonsSub)
	}
}
