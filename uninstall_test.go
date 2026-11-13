package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func removalFixture(t *testing.T, names ...string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Interface", "AddOns")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		writeToc(t, filepath.Join(root, name), "1.0.0")
	}
	saveConfig(config{})
	stubGame(t, false)
	return root
}

func TestRemoveAddonPreservesOtherAddonsAndWTF(t *testing.T) {
	root := removalFixture(t, vendorName, buffName)
	saved := filepath.Join(gameDir(root), "WTF", "Account", "Player", "SavedVariables", vendorName+".lua")
	if err := writeFile(saved, []byte("VendorSettings = {}")); err != nil {
		t.Fatal(err)
	}
	saveConfig(config{Auto: &autoConfig{Pending: []pendingAddon{{Name: vendorName}, {Name: buffName}}}})
	if _, err := removeAddon(root, "vendor"); err != nil {
		t.Fatal(err)
	}
	if isDir(filepath.Join(root, vendorName)) || !isDir(filepath.Join(root, buffName)) {
		t.Fatal("wrong addon folders removed")
	}
	if data, err := os.ReadFile(saved); err != nil || string(data) != "VendorSettings = {}" {
		t.Fatalf("saved variables changed: %q, %v", data, err)
	}
	pending := loadConfig().auto().Pending
	if len(pending) != 1 || pending[0].Name != buffName {
		t.Fatalf("wrong pending queue: %+v", pending)
	}
}

func TestRemoveRaidHelperIncludesRoomPacks(t *testing.T) {
	root := removalFixture(t, raidHelperName, packName("ICC"), packName("RS"), packName("Lead"), raidHelperName+"_Other")
	if _, err := removeAddon(root, "rh"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{raidHelperName, packName("ICC"), packName("RS"), packName("Lead")} {
		if isDir(filepath.Join(root, name)) {
			t.Errorf("not removed: %s", name)
		}
	}
	if !isDir(filepath.Join(root, raidHelperName+"_Other")) {
		t.Fatal("unrelated folder removed")
	}
}

func TestRemoveCustomAddonKeepsProjectAndOtherGame(t *testing.T) {
	root := removalFixture(t, "Example", "Example_Config")
	project := customProject{Repo: "author/Example", Asset: "addon.zip", Installs: map[string]customInstall{
		customRootKey(root): {Tag: "1", Folders: []string{"Example", "Example_Config"}},
		"other-game":        {Tag: "1", Folders: []string{"Example"}},
	}}
	saveConfig(config{Custom: []customProject{project}})
	if _, err := removeAddon(root, project.key()); err != nil {
		t.Fatal(err)
	}
	got, ok := customByKey(loadConfig(), project.key())
	if !ok || got.Asset != project.Asset || len(got.Installs) != 1 || got.Installs["other-game"].Tag != "1" {
		t.Fatalf("project preferences changed: %+v", got)
	}
	if isDir(filepath.Join(root, "Example")) || isDir(filepath.Join(root, "Example_Config")) {
		t.Fatal("custom addon files remain")
	}
}

func TestRemoveAddonRefusesUnsafeConditions(t *testing.T) {
	for _, condition := range []string{"game running", "install lock", "development", "updater inside"} {
		t.Run(condition, func(t *testing.T) {
			root := removalFixture(t, vendorName)
			target := filepath.Join(root, vendorName)
			switch condition {
			case "game running":
				stubGame(t, true)
			case "install lock":
				unlock, err := lockInstall(root)
				if err != nil {
					t.Fatal(err)
				}
				defer unlock()
			case "development":
				if err := os.Mkdir(filepath.Join(target, ".git"), 0o755); err != nil {
					t.Fatal(err)
				}
			case "updater inside":
				stubExe(t, filepath.Join(target, updaterExe))
			}
			if _, err := removeAddon(root, "vendor"); err == nil || !isDir(target) {
				t.Fatalf("unsafe removal allowed: %v", err)
			}
		})
	}
}

func TestRemoveCustomRejectsInvalidAndSharedFolders(t *testing.T) {
	for _, folder := range []string{"..", vendorName, "Shared"} {
		t.Run(folder, func(t *testing.T) {
			root := removalFixture(t, vendorName, "Shared")
			p := customProject{Repo: "author/Example", Installs: map[string]customInstall{
				customRootKey(root): {Tag: "1", Folders: []string{folder}},
			}}
			other := customProject{Repo: "other/Shared", Installs: map[string]customInstall{
				customRootKey(root): {Tag: "1", Folders: []string{"Shared"}},
			}}
			saveConfig(config{Custom: []customProject{p, other}})
			if _, err := removeAddon(root, p.key()); err == nil {
				t.Fatal("invalid removal allowed")
			}
			if !isDir(filepath.Join(root, vendorName)) || !isDir(filepath.Join(root, "Shared")) {
				t.Fatal("protected folder removed")
			}
		})
	}
}

func TestRemoveAddonRollsBackOnMoveFailure(t *testing.T) {
	root := removalFixture(t, raidHelperName, packName("ICC"))
	original := renameRemoval
	renameRemoval = func(source, target string) error {
		if filepath.Base(source) == packName("ICC") {
			return errors.New("folder busy")
		}
		return os.Rename(source, target)
	}
	t.Cleanup(func() { renameRemoval = original })
	if _, err := removeAddon(root, "rh"); err == nil {
		t.Fatal("expected move failure")
	}
	if !isDir(filepath.Join(root, raidHelperName)) || !isDir(filepath.Join(root, packName("ICC"))) {
		t.Fatal("partial removal was not rolled back")
	}
}

func TestRemovableSaysWhy(t *testing.T) {
	dir := t.TempDir()
	if on, why := removable(dir, true, false); on || why == "" {
		t.Fatalf("game running: %v %q", on, why)
	}
	if on, why := removable(dir, false, false); !on || why != "" {
		t.Fatalf("free folder: %v %q", on, why)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if on, why := removable(dir, false, false); on || why == "" {
		t.Fatalf("dev folder: %v %q", on, why)
	}
}
