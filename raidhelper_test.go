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
	"sync"
	"testing"
	"time"
)

func stubGame(t *testing.T, running bool) {
	t.Helper()
	old := gameRunning
	gameRunning = func() bool { return running }
	t.Cleanup(func() { gameRunning = old })
}

func rel(tag string, draft, pre bool, asset string) ghRelease {
	r := ghRelease{Tag: tag, HTML: "https://example/" + tag, Draft: draft, Prerelease: pre}
	if asset != "" {
		r.Assets = []releaseAsset{{Name: asset, URL: "/dl/" + asset}}
	}
	return r
}

func zipName(tag string) string { return raidHelperName + "-" + tag + ".zip" }

func TestPickRelease(t *testing.T) {
	list := []ghRelease{
		rel("v0.3.0", true, false, zipName("v0.3.0")),
		rel("v0.2.1-beta.1", false, true, zipName("v0.2.1-beta.1")),
		rel("v0.9.0", false, false, ""),
		rel("v0.2.0", false, false, zipName("v0.2.0")),
		rel("v0.2.0-beta.2", false, true, zipName("v0.2.0-beta.2")),
		rel("v0.1.5", false, false, "Other-v0.1.5.zip"),
	}
	if r, ok := pickRelease(list, raidHelperName, false); !ok || r.Tag != "v0.2.0" {
		t.Errorf("стабильный канал: %q", r.Tag)
	}
	if r, ok := pickRelease(list, raidHelperName, true); !ok || r.Tag != "v0.2.1-beta.1" {
		t.Errorf("канал бета: %q", r.Tag)
	}
	onlyBeta := []ghRelease{
		rel("v0.2.0-beta.1", false, true, zipName("v0.2.0-beta.1")),
		rel("v0.2.0-beta.2", false, true, zipName("v0.2.0-beta.2")),
	}
	if r, ok := pickRelease(onlyBeta, raidHelperName, false); !ok || r.Tag != "v0.2.0-beta.2" {
		t.Errorf("стабильных нет — берётся бета: %q", r.Tag)
	}
	unmarked := []ghRelease{rel("v0.3.0-beta.1", false, false, zipName("v0.3.0-beta.1")), rel("v0.2.0", false, false, zipName("v0.2.0"))}
	if r, _ := pickRelease(unmarked, raidHelperName, false); r.Tag != "v0.2.0" {
		t.Errorf("тег с -beta без флага пре-релиза — всё равно бета: %q", r.Tag)
	}
	if _, ok := pickRelease([]ghRelease{rel("v1.0.0", true, false, zipName("v1.0.0"))}, raidHelperName, true); ok {
		t.Error("черновик не берётся")
	}
}

func addonZip(t *testing.T, name, ver string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for p, body := range map[string]string{
		name + "/" + name + ".toc": "## Interface: 30300\r\n## Version: " + ver + "\r\n## X-RecFormat: 2\r\n",
		name + "/core/a.lua":       "local x = 1\n",
		name + "/lead/art/pic.tga": "tga",
		"../evil.lua":              "nope",
		"Other/" + name + ".toc":   "## Version: 9.9.9\n",
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

type srvHits struct {
	mu    sync.Mutex
	full  map[string]int
	parts map[string]int
}

func (h *srvHits) get(name string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.full[name]
}

func (h *srvHits) ranged(name string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.parts[name]
}

func releaseServer(t *testing.T, list []ghRelease, zips map[string][]byte) *httptest.Server {
	srv, _ := releaseServerHits(t, list, zips)
	return srv
}

func releaseServerHits(t *testing.T, list []ghRelease, zips map[string][]byte) (*httptest.Server, *srvHits) {
	t.Helper()
	hits := &srvHits{full: map[string]int{}, parts: map[string]int{}}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/releases":
			out := make([]ghRelease, len(list))
			for i, rl := range list {
				out[i] = rl
				out[i].Assets = nil
				for _, as := range rl.Assets {
					size := as.Size
					if b, ok := zips[as.Name]; ok {
						size = int64(len(b))
					}
					out[i].Assets = append(out[i].Assets, releaseAsset{Name: as.Name, URL: srv.URL + as.URL, Size: size})
				}
			}
			_ = json.NewEncoder(w).Encode(out)
		case strings.HasPrefix(r.URL.Path, "/dl/"):
			name := strings.TrimPrefix(r.URL.Path, "/dl/")
			b, ok := zips[name]
			if !ok {
				http.NotFound(w, r)
				return
			}
			hits.mu.Lock()
			if r.Header.Get("Range") != "" {
				hits.parts[name]++
			} else {
				hits.full[name]++
			}
			hits.mu.Unlock()
			http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func layout(t *testing.T) (root, dir string) {
	t.Helper()
	root = t.TempDir()
	dir = filepath.Join(root, "Interface", "AddOns", addonName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "WTF", "Account"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, dir
}

func writeToc(t *testing.T, folder, ver string) {
	t.Helper()
	body := "## Version: " + ver + "\r\n## X-RecFormat: 2\r\n"
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, raidHelperName+".toc"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRaidHelperInstallAndChannels(t *testing.T) {
	retryPause = 0
	stubGame(t, false)
	list := []ghRelease{
		rel("v0.2.0-beta.1", false, true, zipName("v0.2.0-beta.1")),
		rel("v0.2.1-beta.1", false, true, zipName("v0.2.1-beta.1")),
		rel("v0.2.0", false, false, zipName("v0.2.0")),
		rel("v0.3.0", true, false, zipName("v0.3.0")),
	}
	zips := map[string][]byte{
		zipName("v0.2.0"):        addonZip(t, raidHelperName, "0.2.0"),
		zipName("v0.2.1-beta.1"): addonZip(t, raidHelperName, "0.2.1-beta.1"),
	}
	srv := releaseServer(t, list, zips)
	oldRepo := raidHelperRepo
	raidHelperRepo = srv.URL + "/releases"
	t.Cleanup(func() { raidHelperRepo = oldRepo })

	_, dir := layout(t)
	a := raidHelper(dir)

	st := checkOther(a, false)
	if !st.missing || !st.newer || st.tag != "v0.2.0" || st.pre {
		t.Fatalf("нет папки, стабильный канал: %+v", st)
	}
	res, err := syncRH(st, true, map[string]bool{}, &counter{})
	if err != nil || res.ver != "0.2.0" {
		t.Fatalf("установка с нуля: %q %v", res.ver, err)
	}
	if _, err := os.Stat(filepath.Join(a.folder, "core", "a.lua")); err != nil {
		t.Fatal("файл аддона не лёг")
	}
	for _, junk := range []string{filepath.Join(filepath.Dir(a.folder), "evil.lua"), a.folder + ".new", a.folder + ".old"} {
		if _, err := os.Stat(junk); err == nil {
			t.Errorf("лишнее: %s", junk)
		}
	}

	if st := checkOther(a, false); st.newer || st.missing || st.local != "0.2.0" {
		t.Errorf("0.2.0 стоит, стабильный канал: %+v", st)
	}
	st = checkOther(a, true)
	if !st.newer || st.tag != "v0.2.1-beta.1" || !st.pre {
		t.Fatalf("канал бета: %+v", st)
	}
	if res, err := syncRH(st, true, map[string]bool{}, &counter{}); err != nil || res.ver != "0.2.1-beta.1" {
		t.Fatalf("обновление на бету: %q %v", res.ver, err)
	}
	if st := checkOther(a, false); st.newer {
		t.Errorf("со беты 0.2.1 на стабильную 0.2.0 не откатываемся: %+v", st)
	}
}

func TestRaidHelperOldBetaToStable(t *testing.T) {
	retryPause = 0
	stubGame(t, false)
	srv := releaseServer(t, []ghRelease{rel("v0.2.0", false, false, zipName("v0.2.0")), rel("v0.2.0-beta.3", false, true, zipName("v0.2.0-beta.3"))}, nil)
	oldRepo := raidHelperRepo
	raidHelperRepo = srv.URL + "/releases"
	t.Cleanup(func() { raidHelperRepo = oldRepo })
	_, dir := layout(t)
	a := raidHelper(dir)
	writeToc(t, a.folder, "0.2.0-beta.3")
	if st := checkOther(a, false); !st.newer || st.tag != "v0.2.0" {
		t.Errorf("с беты на финальную: %+v", st)
	}
	if st := checkOther(a, true); !st.newer || st.tag != "v0.2.0" {
		t.Errorf("канал бета тоже видит финальную: %+v", st)
	}
}

func TestRaidHelperGuards(t *testing.T) {
	retryPause = 0
	srv := releaseServer(t, []ghRelease{rel("v0.2.0", false, false, zipName("v0.2.0"))}, map[string][]byte{zipName("v0.2.0"): addonZip(t, raidHelperName, "0.2.0")})
	oldRepo := raidHelperRepo
	raidHelperRepo = srv.URL + "/releases"
	t.Cleanup(func() { raidHelperRepo = oldRepo })

	_, dir := layout(t)
	a := raidHelper(dir)
	stubGame(t, true)
	st := checkOther(a, false)
	if _, err := syncRH(st, true, map[string]bool{}, &counter{}); !errors.Is(err, errGameRunning) {
		t.Errorf("игра запущена: %v", err)
	}
	if _, err := os.Stat(a.folder); err == nil {
		t.Error("при запущенной игре папка появилась")
	}
	if _, err := installAddon(dir, map[string][]byte{}); !errors.Is(err, errGameRunning) {
		t.Errorf("основной аддон при запущенной игре: %v", err)
	}

	stubGame(t, false)
	writeToc(t, a.folder, "0.1.0")
	if err := os.Mkdir(filepath.Join(a.folder, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	st = checkOther(a, false)
	if !st.dev || st.newer {
		t.Errorf("папка разработки: %+v", st)
	}
	if _, err := syncRH(st, true, map[string]bool{}, &counter{}); err == nil {
		t.Error("папку разработки обновлять нельзя")
	}
	if v := a.localVersion(); v != "0.1.0" {
		t.Errorf("папку разработки тронули: %s", v)
	}

	raidHelperRepo = srv.URL + "/nope"
	if st := checkOther(raidHelper(t.TempDir()), false); st.tag != "" || st.newer {
		t.Errorf("GitHub молчит: %+v", st)
	}
}
