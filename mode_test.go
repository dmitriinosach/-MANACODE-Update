package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for p, body := range files {
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

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "<нет>"
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func stubExe(t *testing.T, path string) {
	t.Helper()
	old := exePath
	exePath = func() (string, error) { return path, nil }
	t.Cleanup(func() { exePath = old })
}

func stubUserConfig(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	old := userConfigDir
	userConfigDir = func() (string, error) { return d, nil }
	t.Cleanup(func() { userConfigDir = old })
	return d
}

func TestFindAddOns(t *testing.T) {
	stubUserConfig(t)
	root, dir := layout(t)
	addons := filepath.Dir(dir)
	for name, from := range map[string]string{
		"папка аддона": dir,
		"AddOns":       addons,
		"Interface":    filepath.Dir(addons),
		"корень игры":  root,
		"глубже":       filepath.Join(dir, "bin", "art"),
	} {
		if got := findAddOns(from); got != addons {
			t.Errorf("%s: %q", name, got)
		}
	}
	away := t.TempDir()
	if got := findAddOns(away); got != "" {
		t.Errorf("вне игры и без сохранённой папки: %q", got)
	}
	saveSaved(addons)
	if got := findAddOns(away); got != addons {
		t.Errorf("вне игры — сохранённая папка: %q", got)
	}
	put(t, filepath.Join(root, gameExe), "exe")
	if got := pickedAddOns(root); got != addons {
		t.Errorf("выбрана папка с Wow.exe: %q", got)
	}
	bare := t.TempDir()
	put(t, filepath.Join(bare, gameExe), "exe")
	if got := pickedAddOns(bare); got != filepath.Join(bare, "Interface", "AddOns") || !isDir(got) {
		t.Errorf("игра без AddOns — папка создаётся: %q", got)
	}
}

func TestLegacyHomeFile(t *testing.T) {
	cfg := stubUserConfig(t)
	_, dir := layout(t)
	addons := filepath.Dir(dir)
	b, _ := json.Marshal(map[string]string{"folder": filepath.Join(addons, raidHelperName)})
	put(t, filepath.Join(cfg, "ManaCode", legacyHomeFile), string(b))
	if got := findAddOns(t.TempDir()); got != addons {
		t.Errorf("папка из настроек RaidHelperUpdate: %q", got)
	}
}

func TestSetupHomeMergesConfigs(t *testing.T) {
	stubUserConfig(t)
	_, dir := layout(t)
	addons := filepath.Dir(dir)
	put(t, filepath.Join(dataDir(dir), configFile), `{"seasons":[7,5],"auto":{"addonHours":24}}`)
	put(t, filepath.Join(addons, raidHelperName, configFile), `{"raidHelper":{"channel":"beta","packs":["ICC"]},"selfUpdate":true,"updaterExe":"x"}`)
	setupHome(addons)
	if !exists(filepath.Join(appHome(), configFile)) || exists(filepath.Join(addons, homeSub, configFile)) {
		t.Fatal(`настройки — только в %APPDATA%\ManaCode`)
	}
	if !exists(filepath.Join(dataDir(dir), configFile)) {
		t.Error("старый файл настроек не удаляется")
	}
	c := loadConfig()
	if len(c.Seasons) != 2 || c.auto().AddonHours != 24 || !c.beta() || len(*c.RaidHelper.Packs) != 1 || c.SelfUpdate || c.UpdaterExe != "" {
		t.Errorf("настройки слиты: %s", read(filepath.Join(appHome(), configFile)))
	}
	put(t, filepath.Join(addons, raidHelperName, configFile), `{"raidHelper":{"channel":""}}`)
	setupHome(addons)
	if !loadConfig().beta() {
		t.Error("готовые настройки повторно не перезаписываются")
	}
}

func TestSetupHomeTakesFirstOld(t *testing.T) {
	stubUserConfig(t)
	_, dir := layout(t)
	addons := filepath.Dir(dir)
	put(t, filepath.Join(addons, homeSub, configFile), `{"seasons":[3]}`)
	put(t, filepath.Join(dataDir(dir), configFile), `{"seasons":[7,5]}`)
	setupHome(addons)
	if c := loadConfig(); len(c.Seasons) != 1 || c.Seasons[0] != 3 {
		t.Errorf(`первым берётся AddOns\Manacode: %v`, c.Seasons)
	}
	if !exists(filepath.Join(addons, homeSub, configFile)) {
		t.Error("старый файл настроек не удаляется")
	}
}

func TestSetupHomeNextToExe(t *testing.T) {
	stubUserConfig(t)
	down := filepath.Join(t.TempDir(), "Загрузки")
	stubExe(t, filepath.Join(down, updaterExe))
	put(t, filepath.Join(down, dataSub, configFile), `{"raidHelper":{"packs":["RS"]}}`)
	setupHome("")
	if w := wantedPacks(loadConfig()); !w["RS"] || len(w) != 1 {
		t.Errorf("настройки рядом с exe перенесены: %v", w)
	}
}

func TestDropLegacy(t *testing.T) {
	stubGame(t, false)
	tasks := map[string]bool{legacyTasks[0]: true, legacyTasks[1]: true, "Чужая": true}
	stubTasks(t, tasks)
	_, dir := layout(t)
	addons := filepath.Dir(dir)
	self := filepath.Join(dir, updaterExe)
	put(t, self, "exe")
	files := []string{
		filepath.Join(addons, "ОбновитьАддоны.exe"),
		filepath.Join(dir, "ОбновитьДанные.exe"),
		filepath.Join(dir, "ОбновитьДанные.exe.old"),
		filepath.Join(addons, raidHelperName, "RaidHelperUpdate.exe"),
	}
	for _, f := range files {
		put(t, f, "старая")
	}
	dev := filepath.Join(addons, arcadeName)
	put(t, filepath.Join(dev, ".git", "HEAD"), "ref")
	put(t, filepath.Join(dev, "ОбновитьДанные.exe"), "своя сборка")
	if !dropLegacy(addons, self) {
		t.Error("старые задачи были — сообщаем, чтобы включить новую")
	}
	for _, f := range files {
		if exists(f) {
			t.Errorf("не удалён %s", f)
		}
	}
	if !exists(self) || !exists(filepath.Join(dev, "ОбновитьДанные.exe")) {
		t.Error("своя копия и папка разработки не трогаются")
	}
	if tasks[legacyTasks[0]] || tasks[legacyTasks[1]] || !tasks["Чужая"] {
		t.Errorf("задачи: %v", tasks)
	}
	if dropLegacy(addons, self) {
		t.Error("второй раз старых задач нет")
	}
}

func TestAutoAllAddons(t *testing.T) {
	e := newAutoEnv(t)
	arcTop := "MANACODE_RaidWaitingArcade"
	arcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			_ = json.NewEncoder(w).Encode([]ghRelease{{Tag: "v0.9.1", Assets: []releaseAsset{{Name: arcTop + "-0.9.1.zip", URL: "http://" + r.Host + "/dl/arc.zip"}}}})
		case "/dl/arc.zip":
			_, _ = w.Write(zipOf(t, map[string]string{
				arcTop + "/" + arcTop + ".toc": "## Version: 0.9.1\r\n",
				arcTop + "/arc.lua":            "новое",
				arcTop + "/" + updaterExe:      "новая обновлялка",
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(arcSrv.Close)
	old := arcadeRepo
	arcadeRepo = arcSrv.URL + "/releases"
	t.Cleanup(func() { arcadeRepo = old })

	rh := raidHelper(e.dir)
	writeToc(t, rh.folder, "0.1.0")
	put(t, filepath.Join(rh.folder, updaterExe), "обновлялка в RH")
	arc := filepath.Join(e.addons, arcadeName)
	put(t, filepath.Join(arc, arcadeName+".toc"), "## Version: 0.9.0\r\n")
	put(t, filepath.Join(arc, "old.lua"), "старое")
	e.set(func() { e.main, e.rh = "0.2.0", "0.2.0" })
	e.at(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))
	if code := e.run(); code != 0 {
		t.Fatalf("код %d, журнал %v", code, e.log())
	}
	if e.mainVer() != "0.2.0" || rh.localVersion() != "0.2.0" {
		t.Errorf("PlayerRaidsInfo %s, Raid Helper %s", e.mainVer(), rh.localVersion())
	}
	ad := catalog[2].at(e.addons)
	if v := ad.localVersion(); v != "0.9.1" {
		t.Errorf("Arcade из архива с другим регистром: %q", v)
	}
	if read(filepath.Join(arc, "arc.lua")) != "новое" || exists(filepath.Join(arc, "old.lua")) || read(filepath.Join(arc, updaterExe)) != "новая обновлялка" {
		t.Error("папка Arcade заменена целиком, обновлялка из архива легла")
	}
	if read(filepath.Join(rh.folder, updaterExe)) != "обновлялка в RH" {
		t.Error("обновлялка в папке RH пережила обновление без своей копии в архиве")
	}
	log := strings.Join(e.log(), "\n")
	for _, want := range []string{"PlayerRaidsInfo обновлён до 0.2.0", raidHelperName + " обновлён до 0.2.0", "Raid Waiting Arcade обновлён до 0.9.1"} {
		if !strings.Contains(log, want) {
			t.Errorf("в журнале нет %q: %s", want, log)
		}
	}
	if exists(filepath.Join(dataDir(e.dir), configFile)) || exists(filepath.Join(e.addons, homeSub, configFile)) || !exists(filepath.Join(appHome(), configFile)) {
		t.Error(`настройки — только в %APPDATA%\ManaCode`)
	}
}

func TestUnpackOtherCase(t *testing.T) {
	a := otherAddon{name: arcadeName}
	files, err := unpackOther(a, zipOf(t, map[string]string{
		"MANACODE_RaidWaitingArcade/MANACODE_RaidWaitingArcade.toc":         "## Version: 1.0.0\r\n",
		"MANACODE_RaidWaitingArcade/MANACODE_RaidWaitingArcade_Camelot.toc": "## Version: 1.0.0\r\n",
		"MANACODE_RaidWaitingArcade/core/x.lua":                             "x",
		"Other/evil.lua":                                                    "нет",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files[arcadeName+".toc"]; !ok || files["core/x.lua"] != nil && string(files["core/x.lua"]) != "x" || len(files) != 3 {
		t.Errorf("файлы: %v", keys(files))
	}
	rel := ghRelease{Tag: "v1.0.0", Assets: []releaseAsset{{Name: "MANACODE_RaidWaitingArcade-1.0.0.zip", URL: "u"}}}
	if assetZip(rel, arcadeName) != "u" {
		t.Error("ассет с другим регистром не найден")
	}
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestInstallLock(t *testing.T) {
	stubGame(t, false)
	oldNow := now
	t.Cleanup(func() { now = oldNow })
	t0 := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return t0 }

	addons := t.TempDir()
	unlock, err := lockInstall(addons)
	if err != nil {
		t.Fatal(err)
	}
	body := read(filepath.Join(addons, lockName))
	if !strings.Contains(body, "pid=") || !strings.Contains(body, "at=2026-09-30T12:00:00Z") {
		t.Errorf("в замке PID и время: %q", body)
	}
	if _, err := lockInstall(addons); !errors.Is(err, errBusy) {
		t.Errorf("второй замок: %v", err)
	}
	a := otherAddon{name: raidHelperName, folder: filepath.Join(addons, raidHelperName)}
	files, err := unpackOther(a, addonZip(t, raidHelperName, "0.2.0"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installOther(a, files); !errors.Is(err, errBusy) || exists(a.folder) {
		t.Errorf("при чужом замке RH не ставится: %v", err)
	}
	unlock()
	if exists(filepath.Join(addons, lockName)) {
		t.Error("замок не снят")
	}

	put(t, filepath.Join(addons, lockName), "pid=1 at="+t0.Add(-11*time.Minute).Format(time.RFC3339)+"\n")
	if unlock, err := lockInstall(addons); err != nil {
		t.Errorf("протухший замок снимается: %v", err)
	} else {
		unlock()
	}
	put(t, filepath.Join(addons, lockName), "pid=1 at="+t0.Add(-9*time.Minute).Format(time.RFC3339)+"\n")
	if _, err := lockInstall(addons); !errors.Is(err, errBusy) {
		t.Errorf("свежий замок держит: %v", err)
	}
	now = time.Now
	put(t, filepath.Join(addons, lockName), "мусор")
	if _, err := lockInstall(addons); !errors.Is(err, errBusy) {
		t.Errorf("замок без времени — по дате файла, свежий держит: %v", err)
	}
}

func TestAutoWaitsForLock(t *testing.T) {
	e := newAutoEnv(t)
	rh := raidHelper(e.dir).folder
	writeToc(t, rh, "0.1.0")
	t0 := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	e.at(t0)
	lock := filepath.Join(filepath.Dir(e.dir), lockName)
	put(t, lock, "pid=1 at="+t0.Format(time.RFC3339)+"\n")
	e.run()
	if v := raidHelper(e.dir).localVersion(); v != "0.1.0" {
		t.Errorf("под чужим замком RH не ставится: %s", v)
	}
	st := loadConfig().auto()
	if len(st.Pending) == 0 || !strings.Contains(strings.Join(e.log(), "\n"), "другая обновлялка") {
		t.Errorf("обновление ждёт замка: %+v %v", st.Pending, e.log())
	}
	_ = os.Remove(lock)
	e.at(t0.Add(time.Minute))
	e.run()
	if v := raidHelper(e.dir).localVersion(); v != "0.2.0" {
		t.Errorf("замок снят — RH ставится: %s", v)
	}
}

func stubTasks(t *testing.T, have map[string]bool) {
	t.Helper()
	old := schtasks
	t.Cleanup(func() { schtasks = old })
	schtasks = func(args ...string) ([]byte, error) {
		if len(args) >= 3 {
			switch args[0] {
			case "/Query":
				if have[args[2]] {
					return nil, nil
				}
				return nil, errors.New("нет задачи")
			case "/Create", "/Run":
				have[args[2]] = true
				return nil, nil
			case "/Delete":
				delete(have, args[2])
				return nil, nil
			}
		}
		return nil, errors.New("неожиданно")
	}
}

func TestSwapKeepsConfig(t *testing.T) {
	stubGame(t, false)
	_, dir := layout(t)
	a := raidHelper(dir)
	writeToc(t, a.folder, "0.1.0")
	put(t, filepath.Join(a.folder, configFile), `{"raidHelper":{"channel":"beta"}}`)
	put(t, filepath.Join(a.folder, logFile), "журнал")
	put(t, filepath.Join(a.folder, updaterExe), "обновлялка")
	put(t, filepath.Join(a.folder, "old.lua"), "старое")
	files, err := unpackOther(a, addonZip(t, raidHelperName, "0.2.0"))
	if err != nil {
		t.Fatal(err)
	}
	if ver, err := installOther(a, files); err != nil || ver != "0.2.0" {
		t.Fatalf("%q %v", ver, err)
	}
	if read(filepath.Join(a.folder, configFile)) != `{"raidHelper":{"channel":"beta"}}` || read(filepath.Join(a.folder, logFile)) != "журнал" || read(filepath.Join(a.folder, updaterExe)) != "обновлялка" {
		t.Error("конфиг, журнал и обновлялка пережили замену папки")
	}
	if exists(filepath.Join(a.folder, "old.lua")) {
		t.Error("старые файлы при замене папки уходят")
	}
}
