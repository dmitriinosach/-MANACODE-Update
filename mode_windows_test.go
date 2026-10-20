package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func holdLikeRunning(t *testing.T, path string) {
	t.Helper()
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.CloseHandle(h) })
}

func TestRHSelfUpdateInPlace(t *testing.T) {
	stubGame(t, false)
	_, dir := layout(t)
	a := raidHelper(dir)
	writeToc(t, a.folder, "0.1.0")
	exe := filepath.Join(a.folder, updaterExe)
	put(t, exe, "старый exe")
	put(t, filepath.Join(a.folder, configFile), `{"raidHelper":{"channel":"beta"}}`)
	put(t, filepath.Join(a.folder, logFile), "журнал")
	put(t, filepath.Join(a.folder, "core", "a.lua"), "старое")
	put(t, filepath.Join(a.folder, "core", "stale.lua"), "лишнее")
	put(t, filepath.Join(a.folder, "gone", "x.lua"), "лишнее")
	put(t, filepath.Join(a.folder, "fw.lua"), "старое")
	stubExe(t, exe)
	t.Chdir(a.folder)
	holdLikeRunning(t, exe)

	if err := os.Rename(a.folder, a.folder+"-x"); err == nil {
		_ = os.Rename(a.folder+"-x", a.folder)
		t.Fatal("стенд не держит папку: замена папки целиком тоже прошла бы")
	}

	n := raidHelperName + "/"
	files, err := unpackOther(a, zipOf(t, map[string]string{
		n + raidHelperName + ".toc": "## Version: 0.2.0\r\n## X-RecFormat: 2\r\n",
		n + "core/a.lua":            "новое",
		n + "data/d.lua":            "новое",
		n + "fw.lua":                "новое",
		n + updaterExe:              "новый exe",
	}))
	if err != nil {
		t.Fatal(err)
	}
	ver, err := installOther(a, files)
	if err != nil || ver != "0.2.0" {
		t.Fatalf("установка из работающего exe в своей папке: %q %v", ver, err)
	}
	checks := map[string]string{
		exe:                                      "новый exe",
		exe + ".old":                             "старый exe",
		filepath.Join(a.folder, "core", "a.lua"): "новое",
		filepath.Join(a.folder, "data", "d.lua"): "новое",
		filepath.Join(a.folder, "fw.lua"):        "новое",
		filepath.Join(a.folder, configFile):      `{"raidHelper":{"channel":"beta"}}`,
		filepath.Join(a.folder, logFile):         "журнал",
	}
	for p, want := range checks {
		if got := read(p); got != want {
			t.Errorf("%s: %q, ждал %q", p, got, want)
		}
	}
	if a.localVersion() != "0.2.0" {
		t.Errorf("версия: %s", a.localVersion())
	}
	for _, p := range []string{filepath.Join(a.folder, "core", "stale.lua"), filepath.Join(a.folder, "gone"), filepath.Join(a.folder, "core.old"),
		filepath.Join(a.folder, "core.new"), filepath.Join(filepath.Dir(a.folder), lockName)} {
		if exists(p) {
			t.Errorf("лишнее: %s", p)
		}
	}
}
