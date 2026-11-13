package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestGameFileWindows(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	var processes []gameProcess
	p := &gameFilePolicy{now: func() time.Time { return now }, processes: func() ([]gameProcess, error) { return processes, nil }}
	check := func(read, write bool) {
		t.Helper()
		if got := p.check(true) == nil; got != read {
			t.Fatalf("read=%v want %v", got, read)
		}
		if got := p.check(false) == nil; got != write {
			t.Fatalf("write=%v want %v", got, write)
		}
	}
	check(true, true)
	processes = []gameProcess{{pid: 1, started: now}}
	check(false, false)
	now = now.Add(119 * time.Second)
	check(false, false)
	now = now.Add(time.Second)
	check(false, true)
	processes = append(processes, gameProcess{pid: 2, started: now})
	check(false, false)
	processes = processes[:1]
	check(false, false)
	now = now.Add(2 * time.Minute)
	processes = nil
	check(false, true)
	now = now.Add(59 * time.Second)
	check(false, true)
	now = now.Add(time.Second)
	check(true, true)
	p.lastExit = now
	check(false, true)
	now = now.Add(time.Minute)
	check(true, true)
	p.processes = func() ([]gameProcess, error) { return nil, errors.New("snapshot failed") }
	check(false, false)
}

func holdFile(t *testing.T, path string, access, share uint32) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, access, share, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { windows.CloseHandle(handle) })
}

func TestSavedVariablesSharedAndDeferred(t *testing.T) {
	stubGame(t, false)
	path := filepath.Join(t.TempDir(), "ManaCode_RaidHelper.lua")
	put(t, path, "saved variables")
	holdFile(t, path, windows.GENERIC_WRITE|windows.DELETE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE)
	if data, err := readSavedVariables(path); err != nil || string(data) != "saved variables" {
		t.Fatalf("shared read: %q %v", data, err)
	}
	gameFileAccess.processes = func() ([]gameProcess, error) {
		return []gameProcess{{pid: 1, started: time.Now().Add(-time.Hour)}}, nil
	}
	if _, err := readSavedVariables(path + "-missing"); !errors.Is(err, errGameFilesWait) {
		t.Fatalf("opened while running: %v", err)
	}
	w := &discordWatch{seen: map[string]svSeen{}}
	if w.refresh([]string{path}) || len(w.seen) != 0 {
		t.Fatal("deferred Discord input marked as read")
	}
}

func TestWriteRechecksBeforeRename(t *testing.T) {
	stubGame(t, false)
	path := filepath.Join(t.TempDir(), "inbox.lua")
	put(t, path, "old")
	calls := 0
	err := writeFileChecked(path, []byte("new"), func() error {
		calls++
		if calls == 2 {
			gameFileAccess.processes = func() ([]gameProcess, error) { return []gameProcess{{pid: 1, started: time.Now()}}, nil }
		}
		return addonWriteReady()
	})
	data, _ := os.ReadFile(path)
	if !errors.Is(err, errGameFilesWait) || string(data) != "old" {
		t.Fatalf("rename crossed start: %q %v", data, err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("staged file left behind")
	}
	files := marketInboxFiles(marketInbox{})
	if filepath.Ext(files[len(files)-1].path) != ".toc" {
		t.Fatal("toc must be last")
	}
}

func TestFetchToPreservesLockedOriginal(t *testing.T) {
	stubGame(t, false)
	path := filepath.Join(t.TempDir(), "Data.lua")
	put(t, path, "original")
	holdFile(t, path, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
	zip := aesZip(t, "Data.lua", []byte("PlayerRaids = {new=true}"), zipPass)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(zip) }))
	defer server.Close()
	err := fetchTo(path, fileInfo{Name: "Data.lua", Zip: server.URL}, &counter{})
	data, _ := os.ReadFile(path)
	if err == nil || string(data) != "original" {
		t.Fatalf("locked original replaced: %q %v", data, err)
	}
}

func TestV13PreservesLockedOriginal(t *testing.T) {
	e := newV13Env(t)
	man := e.manifest()
	sel := choose13(config{}, &man, nil)
	_, todo := plan13(&man, e.dir, sel)
	if len(todo) == 0 {
		t.Fatal("no test files")
	}
	path := filepath.Join(dataDir(e.dir), todo[0].Path)
	put(t, path, "original")
	holdFile(t, path, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
	_, err := download13(&man, e.dir, sel, &counter{})
	data, _ := os.ReadFile(path)
	if err == nil || string(data) != "original" {
		t.Fatalf("locked v13 original replaced: %q %v", data, err)
	}
}

func TestRollbackIgnoresGameAndRestoresStale(t *testing.T) {
	stubGame(t, true)
	gameFileAccess.processes = func() ([]gameProcess, error) { return []gameProcess{{pid: 1, started: time.Now()}}, nil }
	root := t.TempDir()
	dst, stale := filepath.Join(root, "Addon"), filepath.Join(root, "Addon.old")
	put(t, filepath.Join(stale, "a.lua"), "old")
	if err := renameAddon(stale, dst); !errors.Is(err, errGameFilesWait) {
		t.Fatalf("renameAddon while running: %v", err)
	}
	if err := clearStale(dst, stale); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dst, "a.lua")); string(data) != "old" || isDir(stale) {
		t.Fatalf("stale not restored: %q", data)
	}
	put(t, filepath.Join(stale, "b.lua"), "older")
	if err := clearStale(dst, stale); err != nil || isDir(stale) || !isDir(dst) {
		t.Fatalf("stale next to folder not removed: %v", err)
	}
	if err := rollbackRename(dst, stale); err != nil || !isDir(stale) {
		t.Fatalf("rollback blocked by game: %v", err)
	}
}

func TestAutoFilesWaitKeepsPending(t *testing.T) {
	e := newAutoEnv(t)
	rh := raidHelper(e.dir)
	writeToc(t, rh.folder, "0.1.0")
	e.set(func() { e.main, e.rh = "0.2.0", "0.2.0" })
	gameFileAccess.processes = func() ([]gameProcess, error) { return nil, errors.New("snapshot failed") }
	e.at(time.Date(2026, 9, 29, 20, 0, 0, 0, time.Local))
	e.run()
	if p := loadConfig().auto().Pending; len(p) != 2 {
		t.Fatalf("pending dropped on errGameFilesWait: %+v", p)
	}
}
