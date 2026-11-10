package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
)

func TestCustomGithubLinks(t *testing.T) {
	for _, input := range []string{
		"https://github.com/suprepupre/DefileAlert",
		" https://github.com/suprepupre/DefileAlert/ ",
		"https://github.com/suprepupre/DefileAlert/releases/latest",
		"https://github.com/suprepupre/DefileAlert/releases/tag/1.2.0?x=1#assets",
		"https://github.com/suprepupre/DefileAlert/tree/main/Sounds",
		"https://github.com/suprepupre/DefileAlert/blob/main/DefileAlert.toc",
		"https://www.github.com/suprepupre/DefileAlert.git",
		"github.com/suprepupre/DefileAlert", "suprepupre/DefileAlert",
	} {
		got, err := githubRepo(input)
		if err != nil || got != "suprepupre/DefileAlert" {
			t.Errorf("%q => %q, %v", input, got, err)
		}
	}
	for _, input := range []string{
		"", "https://gitlab.com/a/b", "https://github.com.evil/a/b", "http://github.com/a/b",
		"https://github.com@evil.test/a/b", "https://user@github.com/a/b", "file:///a/b",
		"https://github.com/a", "https://github.com/a/..", "https://github.com/a/b/../c",
		"https://github.com:444/a/b", "https://github.com/a/b%2f..", "a/..",
	} {
		if got, err := githubRepo(input); err == nil {
			t.Errorf("accepted %q => %q", input, got)
		}
	}
}

func customTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func customTestTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func customTestRAR(files map[string]string) []byte {
	var buf bytes.Buffer
	buf.WriteString("Rar!\x1a\x07\x00")
	header := func(kind byte, flags uint16, body []byte) {
		var h bytes.Buffer
		h.WriteByte(kind)
		_ = binary.Write(&h, binary.LittleEndian, flags)
		_ = binary.Write(&h, binary.LittleEndian, uint16(7+len(body)))
		h.Write(body)
		_ = binary.Write(&buf, binary.LittleEndian, uint16(crc32.ChecksumIEEE(h.Bytes())))
		buf.Write(h.Bytes())
	}
	header(0x73, 0, make([]byte, 6))
	for name, body := range files {
		var h bytes.Buffer
		_ = binary.Write(&h, binary.LittleEndian, uint32(len(body)))
		_ = binary.Write(&h, binary.LittleEndian, uint32(len(body)))
		h.WriteByte(2)
		_ = binary.Write(&h, binary.LittleEndian, crc32.ChecksumIEEE([]byte(body)))
		_ = binary.Write(&h, binary.LittleEndian, uint32(0))
		h.WriteByte(20)
		h.WriteByte(0x30)
		_ = binary.Write(&h, binary.LittleEndian, uint16(len(name)))
		_ = binary.Write(&h, binary.LittleEndian, uint32(0x20))
		h.WriteString(name)
		header(0x74, 0x8000, h.Bytes())
		buf.WriteString(body)
	}
	header(0x7b, 0, nil)
	return buf.Bytes()
}

func TestCustomArchiveFormats(t *testing.T) {
	files := map[string]string{
		"MiniAddon/MiniAddon.toc": "## Interface: 30300\n## Version: 1.0\nCore.lua\n",
		"MiniAddon/Core.lua":      "local hello = true",
	}
	plain := customTestTar(t, files)
	var gz, xzb bytes.Buffer
	gw := gzip.NewWriter(&gz)
	_, _ = gw.Write(plain)
	_ = gw.Close()
	xw, err := xz.NewWriter(&xzb)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = xw.Write(plain)
	_ = xw.Close()
	seven, err := os.ReadFile("testdata/custom/addon.7z")
	if err != nil {
		t.Fatal(err)
	}
	bz, err := os.ReadFile("testdata/custom/addon.tar.bz2")
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"zip": customTestZip(t, files), "7z": seven, "rar": customTestRAR(files),
		"tar": plain, "tar.gz": gz.Bytes(), "tar.bz2": bz, "tar.xz": xzb.Bytes(),
	} {
		t.Run(name, func(t *testing.T) {
			bundles, err := unpackCustom(data)
			if err != nil {
				t.Fatal(err)
			}
			if len(bundles) != 1 || bundles[0].Name != "MiniAddon" {
				t.Fatalf("unexpected addon folders: %+v", bundles)
			}
			if string(bundles[0].Files["Core.lua"]) != files["MiniAddon/Core.lua"] {
				t.Fatal("addon contents changed")
			}
		})
	}
	for _, name := range []string{"a.ZIP", "a.7z", "a.rar", "a.tar", "a.tgz", "a.tar.gz", "a.tar.bz2", "a.tar.xz"} {
		if !customArchiveName(name) {
			t.Errorf("unsupported %s", name)
		}
	}
	for _, name := range []string{"a.exe", "a.zip.exe", "a.7z.001", "a.lua", "a.gz"} {
		if customArchiveName(name) {
			t.Errorf("accepted %s", name)
		}
	}
}

func TestCustomArchiveLayouts(t *testing.T) {
	for _, prefix := range []string{"", "DefileAlert/", "suprepupre-DefileAlert-abc123/"} {
		bundles, err := unpackCustom(customTestZip(t, map[string]string{
			prefix + "DefileAlert.toc":  "## Interface: 30300\nCore.lua\n",
			prefix + "Core.lua":         "local a = true",
			prefix + "Sounds/alert.ogg": "ogg",
		}))
		if err != nil || len(bundles) != 1 || bundles[0].Name != "DefileAlert" {
			t.Fatalf("layout %q: %+v %v", prefix, bundles, err)
		}
	}
	files := map[string]string{
		"Pack/Foo/Foo.toc":                  "## Interface: 30300\nCore.lua\n",
		"Pack/Foo/Core.lua":                 "foo",
		"Pack/Foo/Libs/LibStub/LibStub.toc": "## Interface: 30300",
		"Pack/Bar/Bar.toc":                  "## Interface: 30300",
		"Pack/Bar/Main.lua":                 "bar", "Pack/README.md": "not an addon",
	}
	bundles, err := unpackCustom(customTestZip(t, files))
	if err != nil || len(bundles) != 2 {
		t.Fatalf("multi-addon archive: %+v %v", bundles, err)
	}
}

func TestCustomRejectsUnsafeArchives(t *testing.T) {
	for _, name := range []string{
		"../outside.lua", "/outside.lua", "C:/outside.lua", "Foo/../../outside.lua",
		"Foo/file.lua:stream", "Foo/NUL.lua", "Foo/CON", "Foo/LPT1.txt", "Foo/COM¹.txt",
		"Foo/file.lua.", "Foo/file.lua ", "Foo//bad.lua",
	} {
		files := map[string]string{"Foo/Foo.toc": "## Interface: 30300", name: "bad"}
		if _, err := unpackCustom(customTestZip(t, files)); err == nil {
			t.Errorf("accepted ZIP path %q", name)
		}
		if _, err := unpackCustom(customTestTar(t, files)); err == nil {
			t.Errorf("accepted TAR path %q", name)
		}
	}
	for _, files := range []map[string]string{
		{"Foo/Foo.toc": "not an addon"}, {"README.md": "not an addon"},
		{"Foo/Foo.toc": "## Interface: 30300", "Foo/Core.lua": "a", "foo/core.lua": "b"},
		{"Foo/Foo.toc": "## Interface: 30300", "Foo/setup.exe": "MZ"},
	} {
		if _, err := unpackCustom(customTestZip(t, files)); err == nil {
			t.Errorf("accepted unsafe archive: %+v", files)
		}
	}
	for _, kind := range []byte{tar.TypeSymlink, tar.TypeLink, tar.TypeChar} {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		_ = tw.WriteHeader(&tar.Header{Name: "Foo/link", Linkname: "../../outside", Typeflag: kind, Mode: 0o777})
		_ = tw.Close()
		if _, err := unpackCustom(buf.Bytes()); err == nil {
			t.Errorf("accepted TAR link/special file %d", kind)
		}
	}
}

type customRoundTripper func(*http.Request) (*http.Response, error)

func (f customRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func customTestServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	original := http.DefaultTransport
	base, _ := url.Parse(server.URL)
	http.DefaultTransport = customRoundTripper(func(req *http.Request) (*http.Response, error) {
		copy := req.Clone(req.Context())
		u := *req.URL
		u.Scheme, u.Host = base.Scheme, base.Host
		copy.URL = &u
		return original.RoundTrip(copy)
	})
	t.Cleanup(func() {
		http.DefaultTransport = original
		server.Close()
	})
}

func TestCustomReleaseAndUpdate(t *testing.T) {
	e := newAutoEnv(t)
	tag := "v1.0"
	customTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/latest") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": tag, "zipball_url": "https://api.github.com/source.zip",
				"assets": []releaseAsset{
					{Name: "DefileAlert-" + tag + ".zip", URL: "https://github.com/asset.zip"},
					{Name: "DefileAlert.7z", URL: "https://github.com/asset.7z"},
					{Name: "installer.exe", URL: "https://github.com/installer.exe"},
				},
			})
			return
		}
		_, _ = w.Write(customTestZip(t, map[string]string{
			"DefileAlert/DefileAlert.toc": "## Interface: 30300\n## Version: unrelated\n",
			"DefileAlert/Core.lua":        tag,
		}))
	})
	rel, err := findCustomRelease("suprepupre/DefileAlert")
	if err != nil || len(rel.Assets) != 3 {
		t.Fatalf("release assets: %+v %v", rel, err)
	}
	project := customProject{Repo: "suprepupre/DefileAlert", Asset: rel.Assets[0].Pattern}
	cfg := loadConfig()
	cfg.Custom = []customProject{project}
	saveConfig(cfg)
	if _, err := installCustom(e.addons, project, rel, &counter{}); err != nil {
		t.Fatal(err)
	}
	installed, _ := customByKey(loadConfig(), project.key())
	if !customInstalled(e.addons, installed) || installed.installed(e.addons).Tag != tag {
		t.Fatal("installed release not saved")
	}
	if customInstalled(filepath.Join(t.TempDir(), "Interface", "AddOns"), installed) {
		t.Fatal("installation state leaked to another game folder")
	}
	tag = "v1.1"
	st := autoConfig{}
	if checkCustomAddons(e.addons, &st) || len(st.Pending) != 1 {
		t.Fatalf("update not queued: %+v", st)
	}
	stubGame(t, true)
	if _, err := installOne(e.addons, st.Pending[0], &counter{}); !errors.Is(err, errGameRunning) {
		t.Fatalf("installation during game: %v", err)
	}
	stubGame(t, false)
	if _, err := installOne(e.addons, st.Pending[0], &counter{}); err != nil {
		t.Fatal(err)
	}
	if got := read(filepath.Join(e.addons, "DefileAlert", "Core.lua")); got != tag {
		t.Fatalf("update content: %q", got)
	}
	a := newApp(e.addons, nil)
	a.removeCustom(project.key())
	if len(loadConfig().Custom) != 0 || !isDir(filepath.Join(e.addons, "DefileAlert")) {
		t.Fatal("removal must forget the project and keep its files")
	}
	if _, err := installOne(e.addons, st.Pending[0], &counter{}); !errors.Is(err, errUpToDate) {
		t.Fatalf("removed project installed from stale queue: %v", err)
	}
}

func TestCustomPlacementRestrictions(t *testing.T) {
	e := newAutoEnv(t)
	p := customProject{Repo: "a/Foo", Asset: "Foo.zip"}
	cfg := loadConfig()
	cfg.Custom = []customProject{p}
	saveConfig(cfg)
	bundle := func(name string) []customBundle {
		return []customBundle{{Name: name, Files: map[string][]byte{name + ".toc": []byte("## Interface: 30300")}}}
	}
	for _, root := range []string{t.TempDir(), filepath.Join(t.TempDir(), "AddOns"), filepath.Dir(e.addons)} {
		if _, err := placeCustom(root, p, "1", bundle("Foo")); err == nil {
			t.Errorf("accepted target %q", root)
		}
	}
	for _, name := range []string{addonName, raidHelperName, homeSub, "../outside", "C:outside"} {
		if _, err := placeCustom(e.addons, p, "1", bundle(name)); err == nil {
			t.Errorf("accepted reserved folder %q", name)
		}
	}
	outside := t.TempDir()
	put(t, filepath.Join(outside, "sentinel.txt"), "untouched")
	link := filepath.Join(e.addons, "Foo")
	if out, err := hidden(exec.Command("cmd", "/c", "mklink", "/J", link, outside)).CombinedOutput(); err != nil {
		t.Fatalf("junction: %s %v", out, err)
	}
	if _, err := placeCustom(e.addons, p, "1", bundle("Foo")); err == nil {
		t.Fatal("accepted junction outside AddOns")
	}
	if got := read(filepath.Join(outside, "sentinel.txt")); got != "untouched" {
		t.Fatal("wrote outside AddOns")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(e.addons, "Foo", "mine.lua"), "mine")
	if _, err := placeCustom(e.addons, p, "1", bundle("Foo")); err == nil {
		t.Fatal("overwrote an existing unowned folder")
	}
}

func TestCustomNewsOnce(t *testing.T) {
	stubUserConfig(t)
	a := newApp("", nil)
	a.showNews(true)
	if a.view().page != pageNews {
		t.Fatal("news not shown on first launch")
	}
	a.closeNews(false)
	b := newApp("", nil)
	b.showNews(true)
	if b.view().page != pageMain {
		t.Fatal("news shown again")
	}
	b.showNews(false)
	if b.view().page != pageNews {
		t.Fatal("cannot reopen news")
	}
}

func TestCustomAddAndChangeArchive(t *testing.T) {
	e := newAutoEnv(t)
	customTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v1.0", "zipball_url": "https://api.github.com/source.zip",
			"assets": []releaseAsset{{Name: "Foo-v1.0.7z", URL: "https://github.com/Foo-v1.0.7z"}},
		})
	})
	a := newApp(e.addons, nil)
	a.openCustom()
	a.findCustom("https://github.com/owner/Foo/releases")
	waitIdle(t, a)
	d := a.customDraftView()
	if d.Err != "" || d.Repo != "owner/Foo" || d.Release == nil {
		t.Fatalf("search: %+v", d)
	}
	a.addCustom()
	if len(a.view().cards) != len(catalog)+1 || a.view().cards[a.view().cur].title != "Foo" {
		t.Fatal("project card not added/selected")
	}
	p, ok := customByKey(loadConfig(), "github/owner/foo")
	if !ok || p.Asset != "Foo-{tag}.7z" {
		t.Fatalf("project not saved with version pattern: %+v", p)
	}
	a.openCustom()
	a.findCustom("owner/Foo")
	waitIdle(t, a)
	a.selectCustomAsset(1)
	a.addCustom()
	if len(a.view().cards) != len(catalog)+1 || len(loadConfig().Custom) != 1 {
		t.Fatal("changing archive duplicated the project")
	}
	p, _ = customByKey(loadConfig(), "github/owner/foo")
	if p.Asset != customSource {
		t.Fatal("archive selection not updated")
	}
}

func TestCustomGithubFailuresAndRedirects(t *testing.T) {
	status := http.StatusNotFound
	redirect := false
	customTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if redirect {
			http.Redirect(w, r, "https://example.com/outside.zip", http.StatusFound)
			return
		}
		w.WriteHeader(status)
	})
	for _, code := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError} {
		status = code
		if _, err := findCustomRelease("a/b"); err == nil {
			t.Errorf("HTTP %d accepted", code)
		}
	}
	redirect = true
	if _, err := customRequest("https://github.com/asset.zip", 1000, nil); err == nil {
		t.Fatal("redirect outside GitHub accepted")
	}
	for _, raw := range []string{
		"http://github.com/asset.zip", "https://example.com/asset.zip",
		"https://github.com.evil/asset.zip", "https://github.com:444/asset.zip",
	} {
		if githubDownload(raw) {
			t.Errorf("download outside GitHub accepted: %s", raw)
		}
	}
}

func TestCustomLiveDefileAlert(t *testing.T) {
	fixture := os.Getenv("CUSTOM_GITHUB_FIXTURE")
	if os.Getenv("CUSTOM_GITHUB_LIVE") != "1" && fixture == "" {
		t.Skip("CUSTOM_GITHUB_LIVE=1 to check the actual DefileAlert release")
	}
	if fixture != "" {
		metadata, err := os.ReadFile(filepath.Join(fixture, "release.json"))
		if err != nil {
			t.Fatal(err)
		}
		archive, err := os.ReadFile(filepath.Join(fixture, "DefileAlert.7z"))
		if err != nil {
			t.Fatal(err)
		}
		metadata = bytes.TrimPrefix(metadata, []byte{0xef, 0xbb, 0xbf})
		customTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/latest") {
				_, _ = w.Write(metadata)
				return
			}
			_, _ = w.Write(archive)
		})
	}
	stubUserConfig(t)
	stubGame(t, false)
	addons := filepath.Join(t.TempDir(), "Interface", "AddOns")
	if err := os.MkdirAll(addons, 0o755); err != nil {
		t.Fatal(err)
	}
	rel, err := findCustomRelease("https://github.com/suprepupre/DefileAlert")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Assets[0].Name != "DefileAlert.7z" {
		t.Fatalf("unexpected example asset: %+v", rel.Assets)
	}
	p := customProject{Repo: "suprepupre/DefileAlert", Asset: rel.Assets[0].Name}
	saveConfig(config{Custom: []customProject{p}})
	if _, err := installCustom(addons, p, rel, &counter{}); err != nil {
		t.Fatal(err)
	}
	if got := read(filepath.Join(addons, "DefileAlert", "DefileAlert.toc")); !strings.Contains(got, "30300") {
		t.Fatalf("example addon not installed: %q", got)
	}
	t.Logf("installed actual %s %s in temporary AddOns from %s", p.Repo, rel.Tag, p.Asset)
}
