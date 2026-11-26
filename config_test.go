package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBrokenConfigKeptAside(t *testing.T) {
	old := configPause
	configPause = 0
	t.Cleanup(func() { configPause = old })
	path := filepath.Join(appHome(), configFile)
	t.Cleanup(func() { _ = os.Remove(path); _ = os.Remove(path + ".bad") })
	put(t, path, `{"seenNews": "x", "customProj`)
	if _, err := currentConfig(); !errors.Is(err, errConfigBroken) {
		t.Fatalf("broken config read: %v", err)
	}
	saveConfig(config{SeenNews: "y"})
	if bad, _ := os.ReadFile(path + ".bad"); string(bad) != `{"seenNews": "x", "customProj` {
		t.Fatalf("broken config not kept: %q", bad)
	}
	if c, err := currentConfig(); err != nil || c.SeenNews != "y" {
		t.Fatalf("config not written: %+v %v", c, err)
	}
	entries, _ := os.ReadDir(appHome())
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("temp left: %s", e.Name())
		}
	}
}

func TestDataNamesChecked(t *testing.T) {
	for name, want := range map[string]bool{
		"Data_s3.lua": true, "s5_icc25h.lua": true, "": false, "../x.lua": false,
		`..\x.lua`: false, "sub/x.lua": false, `C:\x.lua`: false, "x.txt": false, "..": false,
	} {
		if got := plainDataName(name); got != want {
			t.Errorf("plainDataName(%q) = %v", name, got)
		}
	}
	for name, want := range map[string]bool{
		"s5.lua.tmp": true, "s5.lua.12345.tmp": true, "Players.lua.9.tmp": true, "Data.lua.tmp": false, "s5.lua": false,
	} {
		if got := v13Temp(name); got != want {
			t.Errorf("v13Temp(%q) = %v", name, got)
		}
	}
	if _, err := download(index{Season: 3, Files: []fileInfo{{Name: "../evil.lua", Season: 1}}}, t.TempDir(), nil, &counter{}); err == nil {
		t.Fatal("unsafe archive name accepted")
	}
}

func TestGMKeepsFileOnBrokenArchive(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, gmKeyFile), "secret")
	gm := filepath.Join(dataDir(dir), gmDataFile)
	put(t, gm, "PlayerRaidsGM = {}")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("not a zip")) }))
	defer server.Close()
	old := dataBase
	dataBase = server.URL + "/"
	t.Cleanup(func() { dataBase = old })
	if _, err := syncGM(dir, &gmInfo{Zip: "gm.zip", Baked: "2026"}, &counter{}); err == nil {
		t.Fatal("broken archive accepted")
	}
	if _, err := os.Stat(gm); err != nil {
		t.Fatalf("Gm.lua dropped on non-password error: %v", err)
	}
	zip := aesZip(t, gmDataFile, []byte("PlayerRaidsGM = {}"), "other")
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(zip) })
	if _, err := syncGM(dir, &gmInfo{Zip: "gm.zip", Baked: "2026"}, &counter{}); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err := os.Stat(gm); !os.IsNotExist(err) {
		t.Fatal("Gm.lua kept after wrong key")
	}
}
