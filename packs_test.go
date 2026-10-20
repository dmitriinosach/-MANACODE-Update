package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func liteName(tag string) string { return raidHelperName + liteInfix + tag + ".zip" }

func rhZip(t *testing.T, ver string, packs ...string) []byte {
	t.Helper()
	toc := "## Interface: 30300\r\n## Version: " + ver + "\r\n## X-RecFormat: 2\r\n"
	files := map[string]string{
		raidHelperName + "/" + raidHelperName + ".toc": toc,
		raidHelperName + "/core/a.lua":                 "local x = 1\n",
		"../evil.lua":                                  "nope",
	}
	for _, code := range packs {
		n := packName(code)
		files[n+"/"+n+".toc"] = "## Version: " + ver + "\r\n## LoadOnDemand: 1\r\n"
		files[n+"/data/room.lua"] = "local r = 1\n"
		files[n+"/art/room.tga"] = strings.Repeat("x", 1000)
	}
	return zipOf(t, files)
}

func relWith(tag string, pre bool, assets ...string) ghRelease {
	r := ghRelease{Tag: tag, HTML: "https://example/" + tag, Prerelease: pre}
	for _, a := range assets {
		r.Assets = append(r.Assets, releaseAsset{Name: a, URL: "/dl/" + a})
	}
	return r
}

func packEnv(t *testing.T) (*srvHits, otherAddon) {
	t.Helper()
	retryPause = 0
	stubGame(t, false)
	list := []ghRelease{
		relWith("v0.2.1-beta.1", true, zipName("v0.2.1-beta.1"), liteName("v0.2.1-beta.1")),
		relWith("v0.2.0", false, zipName("v0.2.0"), liteName("v0.2.0")),
		relWith("v0.1.9", false, zipName("v0.1.9")),
	}
	zips := map[string][]byte{
		zipName("v0.2.1-beta.1"):  rhZip(t, "0.2.1-beta.1", "ICC", "ULD"),
		liteName("v0.2.1-beta.1"): rhZip(t, "0.2.1-beta.1"),
		zipName("v0.2.0"):         rhZip(t, "0.2.0", "ICC"),
		liteName("v0.2.0"):        rhZip(t, "0.2.0"),
		zipName("v0.1.9"):         rhZip(t, "0.1.9", "ICC"),
	}
	srv, hits := releaseServerHits(t, list, zips)
	oldRepo := raidHelperRepo
	raidHelperRepo = srv.URL + "/releases"
	t.Cleanup(func() { raidHelperRepo = oldRepo })
	_, dir := layout(t)
	stubUserConfig(t)
	return hits, raidHelper(dir)
}

func TestRHArchivesOldUpdater(t *testing.T) {
	r := relWith("v0.2.0", false, zipName("v0.2.0"), liteName("v0.2.0"))
	a := archiveOf(r)
	if a.full.Name != zipName("v0.2.0") || a.lite.Name != liteName("v0.2.0") {
		t.Errorf("полный и лёгкий: %+v", a)
	}
	if u := assetZip(r, raidHelperName); !strings.HasSuffix(u, zipName("v0.2.0")) {
		t.Errorf("основной архив релиза — полный: %s", u)
	}
	if u := assetZip(relWith("v0.2.0", false, liteName("v0.2.0"), zipName("v0.2.0")), raidHelperName); !strings.HasSuffix(u, zipName("v0.2.0")) {
		t.Errorf("лёгкий первым не мешает: %s", u)
	}
	rh := otherAddon{name: raidHelperName, folder: t.TempDir()}
	files, err := unpackOther(rh, rhZip(t, "0.2.0", "ICC"))
	if err != nil {
		t.Fatal(err)
	}
	for rel := range files {
		if strings.Contains(rel, "room") || strings.Contains(rel, "evil") {
			t.Errorf("из полного в основную папку попало чужое: %s", rel)
		}
	}
	if _, ok := files[raidHelperName+".toc"]; !ok || len(files) != 2 {
		t.Errorf("основная папка из полного архива: %v", files)
	}
	if _, err := unpackOther(packAddon(rh.folder, "RS"), rhZip(t, "0.2.0", "ICC")); err == nil {
		t.Error("пакета нет в архиве — ошибка «нет папки»")
	}
}

func TestRHProbe(t *testing.T) {
	hits, a := packEnv(t)
	o := checkOther(a, true)
	arch, ok := o.archive(true)
	if !ok || arch.tag != "v0.2.1-beta.1" {
		t.Fatalf("архив новой версии: %+v", arch)
	}
	arch.probe()
	if arch.packs["ICC"] != 1000+int64(len("local r = 1\n"))+int64(len("## Version: 0.2.1-beta.1\r\n## LoadOnDemand: 1\r\n")) || arch.packs["ULD"] == 0 || arch.packs["RS"] != 0 {
		t.Errorf("состав по оглавлению архива: %v", arch.packs)
	}
	if hits.get(zipName("v0.2.1-beta.1")) != 0 || hits.ranged(zipName("v0.2.1-beta.1")) == 0 {
		t.Error("состав читается по частям, без скачивания архива")
	}

	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(rhZip(t, "0.2.0", "ICC"))
	}))
	defer plain.Close()
	b := rhArchive{full: releaseAsset{URL: plain.URL, Size: int64(len(rhZip(t, "0.2.0", "ICC")))}}
	b.probe()
	if b.packs != nil {
		t.Errorf("сервер без частей — состав неизвестен: %v", b.packs)
	}
}

func TestRoomPacksSelectInstallRemove(t *testing.T) {
	hits, a := packEnv(t)
	o := checkOther(a, false)
	if o.tag != "v0.2.0" || !strings.HasSuffix(o.zip, zipName("v0.2.0")) {
		t.Fatalf("лёгкий архив не принят за основной: %q %q", o.tag, o.zip)
	}
	if w := wantedPacks(loadConfig()); !w["ICC"] || !w["RS"] || !w["ULD"] || !w["TOC"] {
		t.Errorf("без выбора в конфиге отмечены все: %v", w)
	}
	res, err := syncRH(o, true, wantedPacks(loadConfig()), &counter{})
	icc := packAddon(a.folder, "ICC")
	if err != nil || res.ver != "0.2.0" || icc.localVersion() != "0.2.0" || !exists(filepath.Join(icc.folder, "art", "room.tga")) {
		t.Fatalf("установка с залами: %+v %v, ЦЛК %q", res, err, icc.localVersion())
	}
	if exists(packAddon(a.folder, "RS").folder) || exists(packAddon(a.folder, "ULD").folder) || exists(filepath.Join(filepath.Dir(a.folder), "evil.lua")) {
		t.Error("пакетов РС/Ульдуар в релизе нет — папок быть не должно; путь наружу не лёг")
	}
	if hits.get(zipName("v0.2.0")) != 1 || hits.get(liteName("v0.2.0")) != 0 {
		t.Errorf("залы отмечены — один полный архив: полный %d, лёгкий %d", hits.get(zipName("v0.2.0")), hits.get(liteName("v0.2.0")))
	}

	o = checkOther(a, false)
	if res, err := syncRH(o, false, wantedPacks(loadConfig()), &counter{}); err != nil || len(res.lines) != 0 {
		t.Errorf("всё стоит — ничего не делаем: %+v %v", res, err)
	}

	savePacks(map[string]bool{"RS": true})
	if !strings.Contains(read(filepath.Join(appHome(), configFile)), `"packs": [`) {
		t.Errorf("выбор в конфиге обновлялки: %s", read(filepath.Join(appHome(), configFile)))
	}
	res, err = syncRH(o, false, wantedPacks(loadConfig()), &counter{})
	if err != nil || exists(icc.folder) || res.text() != "3D-залы ЦЛК удалены" {
		t.Fatalf("снятая галочка — папка удалена: %+v %v", res, err)
	}
	if hits.get(zipName("v0.2.0")) != 1 {
		t.Error("удаление без скачивания")
	}

	savePacks(map[string]bool{})
	if w := wantedPacks(loadConfig()); len(w) != 0 {
		t.Errorf("пустой выбор не превращается во «все»: %v", w)
	}
	o = checkOther(a, true)
	if res, err := syncRH(o, true, map[string]bool{}, &counter{}); err != nil || res.ver != "0.2.1-beta.1" {
		t.Fatalf("бета без залов: %+v %v", res, err)
	}
	if hits.get(liteName("v0.2.1-beta.1")) != 1 || hits.get(zipName("v0.2.1-beta.1")) != 0 {
		t.Error("залы не нужны — качается лёгкий")
	}

	writeToc(t, icc.folder, "0.0.1")
	put(t, filepath.Join(icc.folder, ".git", "HEAD"), "ref")
	if res, _ := syncRH(checkOther(a, true), false, map[string]bool{}, &counter{}); len(res.lines) != 0 || !exists(icc.folder) {
		t.Errorf("папку разработки пакета не трогаем: %+v", res)
	}
	_ = os.RemoveAll(icc.folder)

	stubGame(t, true)
	if _, err := syncRH(checkOther(a, true), false, map[string]bool{"ICC": true}, &counter{}); err == nil || !strings.Contains(err.Error(), "игра запущена") {
		t.Errorf("игра запущена: %v", err)
	}
}

func TestRoomPacksFollowAddonVersion(t *testing.T) {
	_, a := packEnv(t)
	writeToc(t, a.folder, "0.1.9")
	o := checkOther(a, true)
	if o.tag != "v0.2.1-beta.1" {
		t.Fatalf("канал бета: %q", o.tag)
	}
	if arch, _ := o.archive(false); arch.tag != "v0.1.9" {
		t.Errorf("залы под стоящую версию аддона: %q", arch.tag)
	}
	if arch, _ := o.archive(true); arch.tag != "v0.2.1-beta.1" {
		t.Errorf("с обновлением — залы того же релиза: %q", arch.tag)
	}
	if _, err := syncRH(o, false, map[string]bool{"ICC": true}, &counter{}); err != nil {
		t.Fatal(err)
	}
	if v, main := packAddon(a.folder, "ICC").localVersion(), a.localVersion(); v != "0.1.9" || main != "0.1.9" {
		t.Errorf("«Применить» не обновляет аддон, версия залов = версия аддона: %q %q", v, main)
	}
}

func TestRoomPacksAuto(t *testing.T) {
	hits, a := packEnv(t)
	writeToc(t, a.folder, "0.1.9")
	put(t, filepath.Join(appHome(), configFile), `{"raidHelper":{"packs":["ICC"]}}`)
	st := autoConfig{Pending: []pendingAddon{{Name: packName("ICC"), Tag: "v0.1.0"}}}
	checkAddons(filepath.Dir(a.folder), false, &st)
	if len(st.Pending) != 1 || st.Pending[0].Name != raidHelperName || st.Pending[0].Tag != "v0.2.0" || !strings.HasSuffix(st.Pending[0].Zip, zipName("v0.2.0")) {
		t.Fatalf("в очереди аддон с отмеченными залами — полный архив, старые строки пакетов ушли: %+v", st.Pending)
	}
	installPending(filepath.Dir(a.folder), &st, &counter{})
	if a.localVersion() != "0.2.0" || packAddon(a.folder, "ICC").localVersion() != "0.2.0" || len(st.Pending) != 0 {
		t.Fatalf("автообновление: аддон %q, ЦЛК %q, очередь %v", a.localVersion(), packAddon(a.folder, "ICC").localVersion(), st.Pending)
	}
	log := read(filepath.Join(logsDir(), logFile))
	if !strings.Contains(log, raidHelperName+" обновлён до 0.2.0") || !strings.Contains(log, "3D-залы ЦЛК 0.2.0 поставлены") {
		t.Errorf("журнал: %s", log)
	}
	st = autoConfig{}
	checkAddons(filepath.Dir(a.folder), false, &st)
	if len(st.Pending) != 0 {
		t.Errorf("всё стоит — очередь пуста: %+v", st.Pending)
	}

	put(t, filepath.Join(appHome(), configFile), `{"raidHelper":{"packs":["RS"]}}`)
	st = autoConfig{}
	checkAddons(filepath.Dir(a.folder), false, &st)
	if len(st.Pending) != 1 || st.Pending[0].Zip != "" {
		t.Fatalf("снятая галочка — удаление без архива: %+v", st.Pending)
	}
	installPending(filepath.Dir(a.folder), &st, &counter{})
	if exists(packAddon(a.folder, "ICC").folder) || !strings.Contains(read(filepath.Join(logsDir(), logFile)), "3D-залы ЦЛК удалены") {
		t.Errorf("отметка снята — автообновление убирает залы: %s", read(filepath.Join(logsDir(), logFile)))
	}
	if hits.get(zipName("v0.2.0")) != 1 {
		t.Errorf("полный архив качали %d раз", hits.get(zipName("v0.2.0")))
	}
}
