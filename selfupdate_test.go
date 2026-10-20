package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func selfServer(t *testing.T, list []ghRelease, exe string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			for i := range list {
				for j := range list[i].Assets {
					list[i].Assets[j].URL = "http://" + r.Host + "/dl/" + list[i].Assets[j].Name
				}
			}
			_ = json.NewEncoder(w).Encode(list)
		case "/dl/" + updaterExe:
			_, _ = w.Write([]byte(exe))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := updaterRepo
	updaterRepo = srv.URL + "/releases"
	t.Cleanup(func() { updaterRepo = old })
	return srv.URL
}

func selfRel(tag string, pre bool) ghRelease {
	return ghRelease{Tag: tag, Prerelease: pre, Assets: []releaseAsset{{Name: updaterExe}}}
}

func TestCheckSelf(t *testing.T) {
	retryPause = 0
	selfServer(t, []ghRelease{selfRel("v99.0.0", true), selfRel("v9.0.0", false), selfRel("v8.0.0", false), {Tag: "v50.0.0"}}, "")
	info, err := checkSelf()
	if err != nil || info == nil || info.version != "9.0.0" || !strings.HasSuffix(info.exe, "/dl/"+updaterExe) {
		t.Fatalf("новейший не пре-релиз с exe: %+v %v", info, err)
	}
	selfServer(t, []ghRelease{selfRel("v"+updaterVersion, false)}, "")
	if info, err := checkSelf(); info != nil || err != nil {
		t.Errorf("своя версия — последняя: %+v %v", info, err)
	}
	updaterRepo = "http://127.0.0.1:1/releases"
	if _, err := checkSelf(); err == nil {
		t.Error("GitHub не ответил — ошибка")
	}
	updaterRepo = ""
	if info, err := checkSelf(); info != nil || err != nil {
		t.Error("репозиторий не задан — не проверяем")
	}
}

func TestSelfLocked(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, arcadeName, updaterExe)
	put(t, exe, "MZ "+versionMark)
	stubExe(t, exe)
	if selfLocked() {
		t.Error("обычная папка — проверяем новую версию")
	}
	put(t, filepath.Join(dir, arcadeName, ".git", "HEAD"), "ref")
	if !selfLocked() {
		t.Error("в папке разработки новую версию не проверяем")
	}
}

func TestAutoSelf(t *testing.T) {
	e := newAutoEnv(t)
	exe := filepath.Join(e.addons, raidHelperName, updaterExe)
	old := "MZ старая " + versionMark
	put(t, exe, old)
	stubExe(t, exe)
	fresh := "MZ новая " + strings.Replace(versionMark, updaterVersion, "9.0.0", 1)
	bad := selfRel("v9.0.0", false)
	bad.Assets[0].Digest = "sha256:" + strings.Repeat("0", 64)
	selfServer(t, []ghRelease{bad}, fresh)
	if err := applySelf(mustSelf(t), nil); err == nil || read(exe) != old {
		t.Fatalf("сумма не совпала — exe не меняется: %v %q", err, read(exe))
	}
	sum := sha256.Sum256([]byte(fresh))
	good := selfRel("v9.0.0", false)
	good.Assets[0].Digest = "sha256:" + hex.EncodeToString(sum[:])
	selfServer(t, []ghRelease{good}, fresh)
	e.run()
	if read(exe) != fresh || read(exe+".old") != old || !strings.Contains(strings.Join(e.log(), "\n"), "обновлялка обновлена до 9.0.0") {
		t.Errorf("фоновый прогон ставит новую версию с GitHub: %q %v", read(exe), e.log())
	}
}

func mustSelf(t *testing.T) *selfInfo {
	t.Helper()
	info, err := checkSelf()
	if err != nil || info == nil {
		t.Fatalf("нет новой версии: %v", err)
	}
	return info
}
