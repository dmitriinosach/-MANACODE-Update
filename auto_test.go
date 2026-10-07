package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	updaterRepo = ""
	schtasks = func(args ...string) ([]byte, error) {
		return nil, errors.New("в тестах Планировщик не трогаем")
	}
	// Разделитель гильдии с машины разработчика в тесты не попадает.
	dividerPic = func() []discordPic { return nil }
	os.Exit(m.Run())
}

type autoEnv struct {
	t       *testing.T
	srv     *httptest.Server
	dir     string
	addons  string
	mu      sync.Mutex
	baked   string
	etag    string
	main    string
	rh      string
	hits    map[string]int
	condHit int
}

func (e *autoEnv) hit(k string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hits[k]
}

func (e *autoEnv) set(f func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	f()
}

func dataLua(baked string) string {
	return "PlayerRaids = {}\nPlayerRaidsMeta = { baked = \"" + baked + "\", complete = true, count = 3 }\n"
}

func mainZip(t *testing.T, ver string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for p, body := range map[string]string{
		addonName + "/" + tocFile:       "## Version: " + ver + "\r\n## X-DataFormat: 12\r\n",
		addonName + "/bin/Core.lua":     "local x = 1\n",
		addonName + "/data/Data.lua":    "не должен лечь",
		addonName + "/bin/art/logo.tga": "tga",
	} {
		w, err := zw.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newAutoEnv(t *testing.T) *autoEnv {
	t.Helper()
	retryPause = 0
	stubGame(t, false)
	e := &autoEnv{t: t, baked: "2026-09-29T10:00:00Z", etag: `"v1"`, main: "0.1.0", rh: "0.1.0", hits: map[string]int{}}
	pass := "raids-circle"
	oldPass, oldData, oldRel, oldRH, oldPRI, oldNow, oldHome := zipPass, dataBase, releaseAPI, raidHelperRepo, priRepo, now, homeDir
	zipPass = pass
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.hits[r.URL.Path]++
		baked, etag, mainTag, rhTag := e.baked, e.etag, "v"+e.main, "v"+e.rh
		if r.URL.Path == "/data/index.json" && r.Header.Get("If-None-Match") != "" {
			e.condHit++
		}
		e.mu.Unlock()
		switch {
		case r.URL.Path == "/data/index.json":
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			z := aesZip(t, "Data.lua", []byte(dataLua(baked)), pass)
			idx := index{V: 12, Baked: baked, Season: 2, Seasons: []int{2}, Complete: true, Count: 3,
				Files: []fileInfo{{Name: mainFile, Season: 2, Count: 3, Bytes: 100, Zip: "season2.zip", ZipLen: int64(len(z))}}}
			w.Header().Set("ETag", etag)
			_ = json.NewEncoder(w).Encode(idx)
		case r.URL.Path == "/data/season2.zip":
			_, _ = w.Write(aesZip(t, "Data.lua", []byte(dataLua(baked)), pass))
		case r.URL.Path == "/latest":
			_ = json.NewEncoder(w).Encode(latestRelease{Tag: mainTag, HTML: "https://example/" + mainTag,
				Assets: []releaseAsset{{Name: addonName + "-" + mainTag + ".zip", URL: e.srv.URL + "/dl/main-" + mainTag + ".zip"}}})
		case r.URL.Path == "/prireleases":
			_ = json.NewEncoder(w).Encode([]ghRelease{{Tag: mainTag, HTML: "https://example/" + mainTag,
				Assets: []releaseAsset{{Name: addonName + "-" + mainTag + ".zip", URL: e.srv.URL + "/dl/main-" + mainTag + ".zip"}}}})
		case r.URL.Path == "/releases":
			var list []ghRelease
			for _, tag := range []string{rhTag, "v0.2.0"} {
				list = append(list, ghRelease{Tag: tag, HTML: "https://example/" + tag,
					Assets: []releaseAsset{{Name: zipName(tag), URL: e.srv.URL + "/dl/" + zipName(tag)}}})
			}
			_ = json.NewEncoder(w).Encode(list)
		case strings.HasPrefix(r.URL.Path, "/dl/main-v"):
			_, _ = w.Write(mainZip(t, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/dl/main-v"), ".zip")))
		case strings.HasPrefix(r.URL.Path, "/dl/"+raidHelperName):
			ver := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/dl/"+raidHelperName+"-v"), ".zip")
			_, _ = w.Write(addonZip(t, raidHelperName, ver))
		default:
			http.NotFound(w, r)
		}
	}))
	dataBase = e.srv.URL + "/data/"
	releaseAPI = e.srv.URL + "/latest"
	raidHelperRepo = e.srv.URL + "/releases"
	priRepo = e.srv.URL + "/prireleases"
	t.Cleanup(func() {
		e.srv.Close()
		zipPass, dataBase, releaseAPI, raidHelperRepo, priRepo, now, homeDir = oldPass, oldData, oldRel, oldRH, oldPRI, oldNow, oldHome
	})
	_, e.dir = layout(t)
	e.addons = filepath.Dir(e.dir)
	homeDir = filepath.Join(e.addons, homeSub)
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, tocFile), []byte("## Version: 0.1.0\r\n## X-DataFormat: 12\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(e.dir, codeDir), 0o755); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *autoEnv) at(t time.Time) { now = func() time.Time { return t } }

func (e *autoEnv) run() int { return runAuto(e.addons, &counter{}) }

func (e *autoEnv) log() []string {
	b, _ := os.ReadFile(filepath.Join(homeDir, logFile))
	s := strings.TrimSpace(string(b))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func (e *autoEnv) mainVer() string {
	if m := reVersion.FindSubmatch(readToc(e.dir)); m != nil {
		return string(m[1])
	}
	return ""
}

func TestAutoDataETag(t *testing.T) {
	e := newAutoEnv(t)
	t0 := time.Date(2026, 9, 29, 12, 0, 0, 0, time.Local)
	e.at(t0)
	if code := e.run(); code != 0 {
		t.Fatalf("первый прогон: код %d, журнал %v", code, e.log())
	}
	if got := localBaked(e.dir); got != e.baked {
		t.Fatalf("данные не легли: %q", got)
	}
	if n := e.hit("/data/season2.zip"); n != 1 {
		t.Fatalf("архив скачан %d раз", n)
	}
	if st := loadConfig(e.dir).auto(); st.ETag != `"v1"` || st.Baked != e.baked {
		t.Fatalf("заголовок не запомнен: %+v", st)
	}
	if e.hit("/releases") != 0 {
		t.Error("Raid Helper не стоит — его релизы не спрашиваем")
	}
	if _, err := os.Stat(raidHelper(e.dir).folder); err == nil {
		t.Error("Raid Helper поставлен с нуля без игрока")
	}

	e.at(t0.Add(15 * time.Minute))
	e.run()
	e.at(t0.Add(30 * time.Minute))
	lines := len(e.log())
	e.run()
	if n := e.hit("/data/season2.zip"); n != 1 {
		t.Errorf("304 по ETag — архив качать нельзя, скачан %d раз", n)
	}
	if e.condHit != 2 {
		t.Errorf("запросов с If-None-Match: %d", e.condHit)
	}
	if got := len(e.log()); got != lines {
		t.Errorf("тихий прогон дописал журнал: %v", e.log()[lines:])
	}

	e.set(func() { e.baked, e.etag = "2026-09-29T12:40:00Z", `"v2"` })
	e.at(t0.Add(45 * time.Minute))
	if code := e.run(); code != 0 {
		t.Fatalf("новая выгрузка: код %d", code)
	}
	if n := e.hit("/data/season2.zip"); n != 2 {
		t.Errorf("новая выгрузка не скачана: %d", n)
	}
	if got := localBaked(e.dir); got != "2026-09-29T12:40:00Z" {
		t.Errorf("baked после обновления: %q", got)
	}
	if last := e.log()[len(e.log())-1]; !strings.Contains(last, "данные обновлены до 2026-09-29T12:40:00Z") {
		t.Errorf("строка обновления: %q", last)
	}

	if err := os.Remove(filepath.Join(dataDir(e.dir), mainFile)); err != nil {
		t.Fatal(err)
	}
	e.at(t0.Add(time.Hour))
	e.run()
	if n := e.hit("/data/season2.zip"); n != 3 {
		t.Errorf("данные удалили — ETag не должен мешать скачать заново: %d", n)
	}

	e.at(t0.Add(2 * time.Hour))
	e.run()
	e.at(t0.Add(3 * time.Hour))
	lines = len(e.log())
	e.run()
	if got := len(e.log()); got != lines {
		t.Errorf("тихий прогон дописал журнал: %v", e.log()[lines:])
	}
	e.at(t0.Add(26 * time.Hour))
	e.run()
	if got := len(e.log()); got != lines+1 || !strings.Contains(e.log()[lines], "данные уже свежие") {
		t.Errorf("раз в сутки — короткая строка, что жив: %v", e.log()[lines:])
	}
}

func TestAutoAddonsInterval(t *testing.T) {
	e := newAutoEnv(t)
	rh := raidHelper(e.dir)
	writeToc(t, rh.folder, "0.1.0")
	e.set(func() { e.main, e.rh = "0.2.0", "0.2.0" })
	t0 := time.Date(2026, 9, 29, 12, 0, 0, 0, time.Local)
	e.at(t0)
	e.run()
	if e.hit("/prireleases") != 1 || e.hit("/releases") != 1 {
		t.Fatalf("первый прогон: запросов к API %d и %d", e.hit("/prireleases"), e.hit("/releases"))
	}
	if v := e.mainVer(); v != "0.2.0" {
		t.Errorf("основной аддон: %q", v)
	}
	if v := rh.localVersion(); v != "0.2.0" {
		t.Errorf("Raid Helper: %q", v)
	}
	if _, err := os.Stat(filepath.Join(dataDir(e.dir), mainFile)); err != nil {
		t.Error("data/ из архива аддона затёрла данные или данные не скачались")
	}

	e.set(func() { e.main, e.rh = "0.3.0", "0.3.0-beta.1" })
	for _, d := range []time.Duration{15 * time.Minute, time.Hour, 3*time.Hour + 59*time.Minute} {
		e.at(t0.Add(d))
		e.run()
	}
	if e.hit("/prireleases") != 1 || e.hit("/releases") != 1 {
		t.Errorf("до 4 ч аддоны не проверяются: %d и %d", e.hit("/prireleases"), e.hit("/releases"))
	}
	if v := e.mainVer(); v != "0.2.0" {
		t.Errorf("до срока аддон не трогаем: %q", v)
	}

	e.at(t0.Add(4 * time.Hour))
	e.run()
	if e.hit("/prireleases") != 2 || e.hit("/releases") != 2 {
		t.Errorf("через 4 ч аддоны проверяются: %d и %d", e.hit("/prireleases"), e.hit("/releases"))
	}
	if v := e.mainVer(); v != "0.3.0" {
		t.Errorf("основной аддон через 4 ч: %q", v)
	}
	if v := rh.localVersion(); v != "0.2.0" {
		t.Errorf("стабильный канал — бета Raid Helper не ставится: %q", v)
	}

	cfg := loadConfig(e.dir)
	cfg.rh().Channel = channelBeta
	saveConfig(e.dir, cfg)
	setAddonHours(e.dir, 24)
	e.at(t0.Add(8 * time.Hour))
	e.run()
	if e.hit("/prireleases") != 2 {
		t.Errorf("раз в сутки — через 4 ч не проверяем: %d", e.hit("/prireleases"))
	}
	e.at(t0.Add(28 * time.Hour))
	e.run()
	if e.hit("/prireleases") != 3 || e.hit("/releases") != 3 {
		t.Errorf("раз в сутки — через 24 ч проверяем: %d и %d", e.hit("/prireleases"), e.hit("/releases"))
	}
	if v := rh.localVersion(); v != "0.3.0-beta.1" {
		t.Errorf("канал бета: %q", v)
	}
	if c := loadConfig(e.dir); c.auto().AddonHours != 24 || !c.beta() {
		t.Errorf("прогон затёр настройки игрока: %+v %+v", c.auto(), c.RaidHelper)
	}
}

func TestAutoGameRunning(t *testing.T) {
	e := newAutoEnv(t)
	rh := raidHelper(e.dir)
	writeToc(t, rh.folder, "0.1.0")
	e.set(func() { e.main, e.rh = "0.2.0", "0.2.0" })
	stubGame(t, true)
	t0 := time.Date(2026, 9, 29, 20, 0, 0, 0, time.Local)
	e.at(t0)
	e.run()
	if v := e.mainVer(); v != "0.1.0" {
		t.Errorf("игра запущена — аддон не ставим: %q", v)
	}
	if v := rh.localVersion(); v != "0.1.0" {
		t.Errorf("игра запущена — Raid Helper не ставим: %q", v)
	}
	if e.hit("/dl/main-v0.2.0.zip")+e.hit("/dl/"+zipName("v0.2.0")) != 0 {
		t.Error("игра запущена — архивы не качаем")
	}
	if p := loadConfig(e.dir).auto().Pending; len(p) != 2 {
		t.Fatalf("отложено: %+v", p)
	}
	waits := 0
	for _, l := range e.log() {
		if strings.Contains(l, "ждёт: игра запущена") {
			waits++
		}
	}
	if waits != 2 {
		t.Errorf("в журнале про ожидание: %v", e.log())
	}

	e.at(t0.Add(15 * time.Minute))
	e.run()
	e.at(t0.Add(30 * time.Minute))
	lines := len(e.log())
	e.run()
	if got := len(e.log()); got != lines {
		t.Errorf("повтор той же строки через 15 мин: %v", e.log()[lines:])
	}

	stubGame(t, false)
	e.at(t0.Add(45 * time.Minute))
	e.run()
	if v := e.mainVer(); v != "0.2.0" {
		t.Errorf("игру закрыли — аддон ставится в следующий прогон: %q", v)
	}
	if v := rh.localVersion(); v != "0.2.0" {
		t.Errorf("игру закрыли — Raid Helper ставится: %q", v)
	}
	if e.hit("/prireleases") != 1 || e.hit("/releases") != 1 {
		t.Errorf("отложенное ставится без новых запросов к API: %d и %d", e.hit("/prireleases"), e.hit("/releases"))
	}
	if p := loadConfig(e.dir).auto().Pending; len(p) != 0 {
		t.Errorf("после установки отложенного не осталось: %+v", p)
	}
}

func TestAutoGitHubDown(t *testing.T) {
	e := newAutoEnv(t)
	priRepo = "http://127.0.0.1:1/releases"
	t0 := time.Date(2026, 9, 29, 12, 0, 0, 0, time.Local)
	e.at(t0)
	e.run()
	st := loadConfig(e.dir).auto()
	if !st.AddonsFailed || !st.addonsDue(t0.Add(time.Hour)) || st.addonsDue(t0.Add(59*time.Minute)) {
		t.Errorf("GitHub не ответил — повтор через час: %+v", st)
	}
}

func oldTaskXML() string {
	x := taskXML(`C:\Games\WoW\Interface\AddOns\`+addonName+`\ОбновитьДанные.exe`, "PC\\игрок", taskDesc)
	x = strings.Replace(x, "<Arguments>-watch</Arguments>", "<Arguments>-auto</Arguments>", 1)
	return strings.Replace(x, "</LogonTrigger>", "</LogonTrigger>\n    <TimeTrigger><Enabled>true</Enabled><Repetition><Interval>PT15M</Interval></Repetition></TimeTrigger>", 1)
}

func TestTaskXML(t *testing.T) {
	x := taskXML(`C:\Игры\a & b.exe`, "PC\\u", taskDesc)
	for _, want := range []string{"<LogonTrigger>", "<Delay>PT2M</Delay>", "<Arguments>-watch</Arguments>", "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>", "<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>"} {
		if !strings.Contains(x, want) {
			t.Errorf("в задаче нет %s", want)
		}
	}
	if strings.Contains(x, "<TimeTrigger>") {
		t.Error("задача не должна запускаться по таймеру: фоновый процесс проверяет сам")
	}
	if got := taskCommand(utf16File(x)); got != `C:\Игры\a & b.exe` {
		t.Errorf("путь из задачи в UTF-16: %q", got)
	}
	if got := taskCommand([]byte(x)); got != `C:\Игры\a & b.exe` {
		t.Errorf("путь из задачи: %q", got)
	}
}

func TestEnsureTask(t *testing.T) {
	var created []string
	task, runs := "", 0
	old := schtasks
	t.Cleanup(func() { schtasks = old })
	schtasks = func(args ...string) ([]byte, error) {
		switch {
		case len(args) >= 4 && args[0] == "/Query" && args[3] == "/XML":
			if task == "" {
				return []byte("ОШИБКА: задача не найдена"), errors.New("exit 1")
			}
			return utf16File(task), nil
		case args[0] == "/Query":
			if task == "" {
				return nil, errors.New("exit 1")
			}
			return nil, nil
		case args[0] == "/Create":
			b, err := os.ReadFile(args[4])
			if err != nil {
				return nil, err
			}
			x := decodeText(b)
			created = append(created, x)
			task = x
			return nil, nil
		case args[0] == "/Run":
			runs++
			return nil, nil
		}
		return nil, fmt.Errorf("неожиданно: %v", args)
	}
	exe := filepath.Join(t.TempDir(), addonName, updaterExe)
	put(t, exe, "exe")
	stubExe(t, exe)

	if moved, err := ensureTask(); moved || err != nil || len(created) != 0 {
		t.Errorf("задачи нет — не создаём: %t %v %d", moved, err, len(created))
	}

	task = oldTaskXML()
	moved, err := ensureTask()
	if !moved || err != nil || len(created) != 1 {
		t.Fatalf("старая задача с -auto пересоздаётся: %t %v %d", moved, err, len(created))
	}
	if !strings.Contains(created[0], "<Arguments>-watch</Arguments>") || taskCommand([]byte(created[0])) != exe || !strings.Contains(created[0], "<LogonTrigger>") {
		t.Errorf("новая задача: %s", created[0])
	}
	if runs != 1 {
		t.Errorf("новая задача сразу запускается: %d", runs)
	}

	if moved, err := ensureTask(); moved || err != nil || len(created) != 1 {
		t.Errorf("задача на живой exe с -watch не трогается: %t %v %d", moved, err, len(created))
	}

	task = taskXML(filepath.Join(t.TempDir(), "нет", updaterExe), "PC\\u", taskDesc)
	if moved, err := ensureTask(); !moved || err != nil || taskCommand([]byte(task)) != exe {
		t.Errorf("exe из задачи удалён — задача переходит на эту копию: %t %v %s", moved, err, task)
	}
}
