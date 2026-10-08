package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	lockName  = ".manacode-update.lock"
	lockStale = 10 * time.Minute
)

var (
	errBusy   = errors.New("другая обновлялка сейчас ставит аддон — повторите через пару минут")
	keepNames = []string{configFile, logFile, updaterExe}
)

func lockInstall(addons string) (func(), error) {
	p := filepath.Join(addons, lockName)
	for try := 0; try < 2; try++ {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "pid=%d at=%s\n", os.Getpid(), now().UTC().Format(time.RFC3339))
			f.Close()
			return func() { _ = os.Remove(p) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("не смог поставить замок %s: %w", lockName, err)
		}
		if !lockExpired(p) {
			return nil, errBusy
		}
		_ = os.Remove(p)
	}
	return nil, errBusy
}

func lockExpired(p string) bool {
	at := time.Time{}
	if data, err := os.ReadFile(p); err == nil {
		for _, f := range strings.Fields(string(data)) {
			if v, ok := strings.CutPrefix(f, "at="); ok {
				at, _ = time.Parse(time.RFC3339, v)
			}
		}
	}
	if at.IsZero() {
		st, err := os.Stat(p)
		if err != nil {
			return true
		}
		at = st.ModTime()
	}
	return now().Sub(at) > lockStale
}

func sameDir(a, b string) bool {
	return a != "" && b != "" && strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func selfInside(folder string) bool {
	if exe, err := exePath(); err == nil && sameDir(filepath.Dir(exe), folder) {
		return true
	}
	if wd, err := os.Getwd(); err == nil {
		rel, err := filepath.Rel(folder, wd)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

func keepName(name string) bool {
	for _, k := range keepNames {
		if strings.EqualFold(name, k) {
			return true
		}
	}
	return false
}

func installInPlace(a otherAddon, files map[string][]byte) error {
	if err := os.MkdirAll(a.folder, 0o755); err != nil {
		return err
	}
	exe, _ := exePath()
	exeName := ""
	if sameDir(filepath.Dir(exe), a.folder) {
		exeName = filepath.Base(exe)
	}
	toc := a.name + ".toc"
	tops := map[string]bool{}
	dirs := map[string]map[string][]byte{}
	root := map[string][]byte{}
	for rel, b := range files {
		top, rest, sub := strings.Cut(rel, "/")
		tops[strings.ToLower(top)] = true
		if !sub {
			root[top] = b
			continue
		}
		if dirs[top] == nil {
			dirs[top] = map[string][]byte{}
		}
		dirs[top][rest] = b
	}
	for name, sub := range dirs {
		dst := filepath.Join(a.folder, name)
		fresh, stale := dst+".new", dst+".old"
		_ = os.RemoveAll(fresh)
		_ = os.RemoveAll(stale)
		for rel, b := range sub {
			if err := writeFile(filepath.Join(fresh, filepath.FromSlash(rel)), b); err != nil {
				_ = os.RemoveAll(fresh)
				return err
			}
		}
		if _, err := os.Stat(dst); err == nil {
			if err := os.Rename(dst, stale); err != nil {
				_ = os.RemoveAll(fresh)
				return fmt.Errorf("папка %s\\%s занята — закройте игру и запустите обновлялку снова", a.name, name)
			}
		}
		if err := os.Rename(fresh, dst); err != nil {
			_ = os.Rename(stale, dst)
			return fmt.Errorf("не смог поставить новую папку %s\\%s", a.name, name)
		}
		_ = os.RemoveAll(stale)
	}
	for name, b := range root {
		if name == toc {
			continue
		}
		dst := filepath.Join(a.folder, name)
		old := ""
		if exeName != "" && strings.EqualFold(name, exeName) {
			old = dst + ".old"
			_ = os.Remove(old)
			if os.Rename(dst, old) != nil {
				old = ""
			}
		}
		if err := writeFile(dst, b); err != nil {
			if old != "" {
				_ = os.Rename(old, dst)
			}
			return err
		}
	}
	entries, _ := os.ReadDir(a.folder)
	for _, e := range entries {
		name := e.Name()
		if tops[strings.ToLower(name)] || keepName(name) || strings.EqualFold(name, exeName) || strings.HasSuffix(strings.ToLower(name), ".old") {
			continue
		}
		_ = os.RemoveAll(filepath.Join(a.folder, name))
	}
	return writeFile(filepath.Join(a.folder, toc), files[toc])
}

func carryKept(a otherAddon, fresh string, files map[string][]byte) error {
	for _, k := range keepNames {
		if _, ok := files[k]; ok {
			continue
		}
		b, err := os.ReadFile(filepath.Join(a.folder, k))
		if err != nil {
			continue
		}
		if err := writeFile(filepath.Join(fresh, k), b); err != nil {
			return err
		}
	}
	return nil
}
