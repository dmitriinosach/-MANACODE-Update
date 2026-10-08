package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	updaterExe     = "ManacodeUpdate.exe"
	updaterVersion = "1.2.1"
	homeSub        = "Manacode"
	homeFile       = "ManacodeUpdate.json"
	legacyHomeFile = "RaidHelperUpdate.json"
	userAgent      = "ManacodeUpdate/" + updaterVersion
)

var (
	versionMark   = "MANACODE-UPDATER-VERSION=" + updaterVersion + ";"
	reVersionMark = regexp.MustCompile(`MANACODE-UPDATER-VERSION=(\d+\.\d+\.\d+);`)
	legacyExes    = []string{"ОбновитьДанные.exe", "ОбновитьАддоны.exe", "RaidHelperUpdate.exe"}
	legacyTasks   = []string{"Manacode_PlayerRaidsInfo_Update", "ManaCode_RaidHelper_Update"}
	exePath       = os.Executable
	userConfigDir = os.UserConfigDir
)

func ownVersion() string {
	if m := reVersionMark.FindStringSubmatch(versionMark); m != nil {
		return m[1]
	}
	return updaterVersion
}

func isLegacyName(name string) bool {
	for _, n := range legacyExes {
		if strings.EqualFold(name, n) {
			return true
		}
	}
	return false
}

func addOnsIn(d string) string {
	base := filepath.Base(d)
	switch {
	case strings.EqualFold(base, "AddOns"):
		return d
	case strings.EqualFold(filepath.Base(filepath.Dir(d)), "AddOns"):
		return filepath.Dir(d)
	case strings.EqualFold(base, "Interface") && isDir(filepath.Join(d, "AddOns")):
		return filepath.Join(d, "AddOns")
	case isDir(filepath.Join(d, "Interface", "AddOns")):
		return filepath.Join(d, "Interface", "AddOns")
	}
	return ""
}

func walkAddOns(d string) string {
	for d != "" {
		if a := addOnsIn(d); a != "" {
			return a
		}
		up := filepath.Dir(d)
		if up == d {
			break
		}
		d = up
	}
	return ""
}

func appHome() string {
	d, err := userConfigDir()
	if err != nil || d == "" {
		return ""
	}
	return filepath.Join(d, "ManaCode")
}

func homePath(name string) string {
	h := appHome()
	if h == "" {
		return ""
	}
	return filepath.Join(h, name)
}

type savedHome struct {
	AddOns string `json:"addons,omitempty"`
	Folder string `json:"folder,omitempty"`
}

func loadSaved() string {
	var h savedHome
	if b, err := os.ReadFile(homePath(homeFile)); err == nil && json.Unmarshal(b, &h) == nil && isDir(h.AddOns) {
		return h.AddOns
	}
	if b, err := os.ReadFile(homePath(legacyHomeFile)); err == nil && json.Unmarshal(b, &h) == nil && h.Folder != "" {
		if a := filepath.Dir(h.Folder); isDir(a) {
			return a
		}
	}
	return ""
}

func saveSaved(addons string) {
	p := homePath(homeFile)
	if p == "" {
		return
	}
	b, err := json.MarshalIndent(savedHome{AddOns: addons}, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, b, 0o644)
}

func findAddOns(exeDir string) string {
	if a := walkAddOns(exeDir); a != "" {
		return a
	}
	return loadSaved()
}

func pickedAddOns(d string) string {
	if a := walkAddOns(d); a != "" {
		return a
	}
	if st, err := os.Stat(filepath.Join(d, gameExe)); err == nil && !st.IsDir() {
		a := filepath.Join(d, "Interface", "AddOns")
		if os.MkdirAll(a, 0o755) == nil {
			return a
		}
	}
	return ""
}

func setupHome(addons string) {
	home := appHome()
	if home == "" {
		return
	}
	if _, err := os.Stat(filepath.Join(home, configFile)); err == nil {
		return
	}
	if cfg, ok := oldConfig(addons); ok {
		cfg.SelfUpdate, cfg.UpdaterExe = false, ""
		saveConfig(cfg)
	}
}

func readConfig(p string) (config, bool) {
	var c config
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &c) != nil {
		return config{}, false
	}
	return c, true
}

func oldConfig(addons string) (config, bool) {
	if addons != "" {
		if c, ok := readConfig(filepath.Join(addons, homeSub, configFile)); ok {
			return c, true
		}
		cfg, found := readConfig(filepath.Join(addons, addonName, dataSub, configFile))
		if rh, ok := readConfig(filepath.Join(folderIn(addons, raidHelperName), configFile)); ok {
			if cfg.RaidHelper == nil {
				cfg.RaidHelper = rh.RaidHelper
			}
			found = true
		}
		if found {
			return cfg, true
		}
	}
	if exe, err := exePath(); err == nil {
		d := filepath.Dir(exe)
		for _, p := range []string{filepath.Join(d, dataSub, configFile), filepath.Join(d, configFile)} {
			if c, ok := readConfig(p); ok {
				return c, true
			}
		}
	}
	return config{}, false
}

func folderIn(addons, name string) string {
	if addons == "" {
		return ""
	}
	if entries, err := os.ReadDir(addons); err == nil {
		for _, e := range entries {
			if e.IsDir() && strings.EqualFold(e.Name(), name) {
				return filepath.Join(addons, e.Name())
			}
		}
	}
	return filepath.Join(addons, name)
}

func sameFile(a, b string) bool {
	if strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) {
		return true
	}
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(sa, sb)
}

func hop(exe string, args []string) bool {
	cmd := hidden(exec.Command(exe, args...))
	cmd.Dir = filepath.Dir(exe)
	return cmd.Start() == nil
}

func tidyAddons(addons string) {
	exe, err := exePath()
	if err != nil || addons == "" || isLegacyName(filepath.Base(exe)) {
		return
	}
	if dropLegacy(addons, exe) && !autostartOn() {
		_ = setAutostart(true)
	}
	if dir := priDir(addons); readToc(dir) != nil {
		_ = migrateData(dir)
		if isDir(filepath.Join(dir, codeDir)) {
			cleanLegacy(dir)
		}
	}
}

func dropLegacy(addons, self string) (tasks bool) {
	stopLegacy(addons)
	dirs := []string{addons}
	for _, d := range catalog {
		dirs = append(dirs, folderIn(addons, d.name))
	}
	for _, dir := range dirs {
		if dir == "" || devFolder(dir) {
			continue
		}
		for _, name := range legacyExes {
			for _, p := range []string{filepath.Join(dir, name), filepath.Join(dir, name+".old"), filepath.Join(dir, name+"~")} {
				if !sameFile(p, self) {
					_ = os.Remove(p)
				}
			}
		}
	}
	for _, t := range legacyTasks {
		if _, err := schtasks("/Query", "/TN", t); err == nil {
			_, _ = schtasks("/Delete", "/TN", t, "/F")
			tasks = true
		}
	}
	return tasks
}
