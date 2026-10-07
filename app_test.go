package main

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevFolderThroughJunction(t *testing.T) {
	repo := t.TempDir()
	put(t, filepath.Join(repo, ".git", "HEAD"), "ref")
	put(t, filepath.Join(repo, "addon", tocFile), "## Version: 0.1.0\r\n")
	_, dir := layout(t)
	link := filepath.Join(filepath.Dir(dir), "Linked")
	if out, err := hidden(exec.Command("cmd", "/c", "mklink", "/J", link, filepath.Join(repo, "addon"))).CombinedOutput(); err != nil {
		t.Skipf("junction не создать: %v %s", err, out)
	}
	if !devFolder(link) {
		t.Error("папка разработки за junction не узнана: .git лежит над её целью")
	}
	if !packDev(link) {
		t.Error("пакет за junction — не трогаем")
	}
	if devFolder(dir) {
		t.Error("обычная папка принята за папку разработки")
	}
}

func appWith(t *testing.T) (*app, string) {
	t.Helper()
	_, dir := layout(t)
	addons := filepath.Dir(dir)
	stubHome(t, "")
	a := newApp(addons, nil)
	t.Cleanup(func() { homeDir = "" })
	return a, dir
}

func TestCardViews(t *testing.T) {
	a, dir := appWith(t)
	pri, rh, arc := a.cards[0], a.cards[1], a.cards[2]
	writeToc(t, raidHelper(dir).folder, "0.1.0")
	put(t, filepath.Join(dir, tocFile), "## Version: 0.17.0\r\n## X-DataFormat: 12\r\n")
	put(t, filepath.Join(dataDir(dir), mainFile), dataLua("2026-10-05T08:00:00Z"))
	pri.meta = readLocal(dir)
	pri.rel = &otherRelease{local: "0.17.0", tag: "v0.18.0", zip: "z", newer: true}
	pri.idx = &index{V: 12, Baked: "2026-10-05T09:00:00Z", Season: 7, Files: []fileInfo{{Season: 7, ZipLen: 1 << 20}, {Season: 6, ZipLen: 2 << 20}}}
	pri.pick = map[int]bool{6: false}
	rh.rel = &otherRelease{local: "0.1.0", tag: "v0.1.0"}
	arc.rel = &otherRelease{missing: true, tag: "v0.9.0", zip: "z", newer: true}

	v := a.view()
	p := v.cards[0]
	if p.action != "Обновить" || !p.primary || !p.actionOn || p.short != "вышла 0.18.0" || p.dot != kindNew {
		t.Errorf("PlayerRaidsInfo с новой версией: %+v", p)
	}
	if p.dataAction != "Обновить данные" || p.dataPrimary || !p.dataOn {
		t.Errorf("данные устарели, но главная кнопка уже есть: %q primary=%t on=%t", p.dataAction, p.dataPrimary, p.dataOn)
	}
	if len(p.seasons) != 2 || !p.seasons[0].locked || !p.seasons[0].on || p.seasons[1].on || !strings.Contains(p.seasonsSub, "1 из 2") {
		t.Errorf("сезоны: %+v %q", p.seasons, p.seasonsSub)
	}
	if r := v.cards[1]; r.action != "Проверить" || r.primary || r.short != "0.1.0" || r.dot != kindOK || len(r.packs) != len(roomPacks) || !r.testOn {
		t.Errorf("Raid Helper последний: %+v", r)
	}
	if c := v.cards[2]; c.action != "Установить" || !c.primary || c.ver != "не установлен" || c.short != "не установлен" {
		t.Errorf("Arcade не установлен: %+v", c)
	}

	a.toggleSeason(6)
	a.toggleSeason(7)
	if sel := a.selected(pri); !sel[6] || !sel[7] {
		t.Errorf("текущий сезон снять нельзя, прошлый отмечается: %v", sel)
	}

	a.busy, a.job = "arc", "Raid Waiting Arcade 0.9.0"
	a.cnt.total.Store(100)
	a.cnt.got.Store(50)
	v = a.view()
	if c := v.cards[2]; !c.busy || c.got != 50 || c.short != "скачиваю…" {
		t.Errorf("идёт установка: %+v", c)
	}
	if v.cards[0].actionOn || v.cards[0].dataOn || v.cards[1].channelOn {
		t.Error("пока идёт установка, остальные кнопки выключены")
	}
	a.busy = ""

	put(t, filepath.Join(dir, ".git", "HEAD"), "ref")
	pri.rel = &otherRelease{local: "0.17.0", tag: "v0.18.0", dev: true}
	if p := a.view().cards[0]; p.action != "" || p.short != "папка разработки" {
		t.Errorf("папка разработки: %+v", p)
	}
}

func TestCardViewNoGame(t *testing.T) {
	stubHome(t, "")
	a := newApp("", nil)
	v := a.view()
	if v.addons != "" || v.autoOn || v.cards[0].short != "папка игры не найдена" || v.cards[0].action != "" {
		t.Errorf("без папки игры: %+v", v.cards[0])
	}
}

func TestNoReleasesYet(t *testing.T) {
	retryPause = 0
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/empty" {
			_, _ = w.Write([]byte("[]"))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	a := otherAddon{name: buffName, folder: filepath.Join(t.TempDir(), buffName), repo: srv.URL + "/nope"}
	if o := checkOther(a, false); !o.none || o.tag != "" || hits != 1 {
		t.Errorf("репозитория нет — «релизов пока нет», без повторов: %+v, запросов %d", o, hits)
	}
	a.repo = srv.URL + "/empty"
	if o := checkOther(a, false); !o.none {
		t.Errorf("релизов нет — «релизов пока нет»: %+v", o)
	}

	app, _ := appWith(t)
	c := app.cardOf("buff")
	c.rel = &otherRelease{none: true}
	v := app.cardView(c)
	if v.verSub != "релизов на GitHub пока нет" || v.short != "скоро" || v.action != "Проверить" || v.primary || v.dot != kindNone {
		t.Errorf("карточка без релизов: %+v", v)
	}
	st := autoConfig{Pending: []pendingAddon{{Name: buffName}}}
	old := buffRepo
	buffRepo = srv.URL + "/nope"
	t.Cleanup(func() { buffRepo = old })
	put(t, filepath.Join(app.addons, buffName, buffName+".toc"), "## Version: 0.1\r\n")
	checkAddons(app.addons, false, &st)
	if st.AddonsFailed || len(st.Pending) != 0 {
		t.Errorf("нет релизов — не ошибка и не в очереди: %+v", st)
	}
}

func TestLegacyRootsThroughJunction(t *testing.T) {
	repo := t.TempDir()
	put(t, filepath.Join(repo, raidHelperName+".toc"), "## Version: 0.1.0\r\n")
	_, dir := layout(t)
	addons := filepath.Dir(dir)
	link := filepath.Join(addons, raidHelperName)
	if out, err := hidden(exec.Command("cmd", "/c", "mklink", "/J", link, repo)).CombinedOutput(); err != nil {
		t.Skipf("junction не создать: %v %s", err, out)
	}
	roots := legacyRoots(addons)
	if !underAny(filepath.Join(repo, "dist", "RaidHelperUpdate.exe"), roots) {
		t.Error("процесс из цели junction аддона — наш: Windows отдаёт путь с раскрытым junction")
	}
	if !underAny(filepath.Join(addons, "ОбновитьАддоны.exe"), roots) || underAny(filepath.Join(t.TempDir(), "RaidHelperUpdate.exe"), roots) {
		t.Error("корень AddOns — наш, чужая папка — нет")
	}
}

func TestChooseFolderTidies(t *testing.T) {
	stubGame(t, false)
	stubUserConfig(t)
	for _, r := range []*string{&priRepo, &raidHelperRepo, &arcadeRepo, &buffRepo, &vendorRepo, &dataBase} {
		old := *r
		*r = "http://127.0.0.1:1/x"
		t.Cleanup(func() { *r = old })
	}
	tasks := map[string]bool{legacyTasks[0]: true}
	stubTasks(t, tasks)
	_, dir := layout(t)
	addons := filepath.Dir(dir)
	old := filepath.Join(dir, legacyExes[0])
	put(t, old, "старая")
	stubExe(t, filepath.Join(t.TempDir(), "Рабочий стол", updaterExe))
	stubHome(t, "")
	a := newApp("", nil)
	a.choose(addons)
	t.Cleanup(func() { homeDir = "" })
	if exists(old) || tasks[legacyTasks[0]] {
		t.Errorf("папку указали в окне — старая обновлялка убрана: файл %t, задачи %v", exists(old), tasks)
	}
	if !tasks[taskName] {
		t.Errorf("старая задача была — включено своё автообновление: %v", tasks)
	}
}
