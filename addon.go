package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	addonName = "Manacode_PlayerRaidsInfo"
	tocFile   = addonName + ".toc"
	codeDir   = "bin"
	dataSub   = "data"
)

func dataDir(dir string) string { return filepath.Join(dir, dataSub) }

func isDataFile(name string) bool {
	if name == mainFile || name == configFile {
		return true
	}
	return strings.HasPrefix(name, "Data_s") && strings.HasSuffix(name, ".lua")
}

func migrateData(dir string) error {
	dd := dataDir(dir)
	if err := os.MkdirAll(dd, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !isDataFile(e.Name()) {
			continue
		}
		src := filepath.Join(dir, e.Name())
		dst := filepath.Join(dd, e.Name())
		if di, err := os.Stat(dst); err == nil {
			si, serr := os.Stat(src)
			if serr != nil || !si.ModTime().After(di.ModTime()) {
				_ = os.Remove(src)
				continue
			}
			_ = os.Remove(dst)
		}
		if err := renameAddon(src, dst); err != nil {
			return fmt.Errorf(tr("%s занят другой программой — закройте игру и запустите обновлялку снова"), e.Name())
		}
	}
	return nil
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	for range 8 {
		st, err := os.Lstat(p)
		if err != nil || st.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
			break
		}
		link, err := os.Readlink(p)
		if err != nil || link == "" {
			break
		}
		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(p), link)
		}
		p = filepath.Clean(link)
	}
	return p
}

func devFolder(dir string) bool {
	real := realPath(dir)
	for _, d := range []string{dir, real, filepath.Dir(dir), filepath.Dir(real)} {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return true
		}
	}
	return false
}

func fetchRaw(url, what string, cnt *counter) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s", tr("не удалось соединиться с GitHub"))
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, fmt.Errorf(tr("на GitHub нет файла %s"), path.Base(url))
	default:
		return nil, fmt.Errorf(tr("GitHub ответил %s"), resp.Status)
	}
	var r io.Reader = resp.Body
	if cnt != nil {
		if resp.ContentLength > 0 {
			cnt.total.Store(resp.ContentLength)
		}
		r = &countingReader{r: resp.Body, cnt: cnt}
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf(tr("оборвалось скачивание %s"), what)
	}
	return data, nil
}

func fetchAddonZip(url string, cnt *counter) ([]byte, error) {
	return fetchRaw(url, tr("аддона"), cnt)
}

func unpackAddon(data []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf(tr("архив аддона битый: %w"), err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		rest, ok := strings.CutPrefix(name, addonName+"/")
		if !ok || rest == "" || strings.HasSuffix(rest, "/") {
			continue
		}
		clean := path.Clean(rest)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || strings.ContainsAny(clean, ":") {
			return nil, fmt.Errorf(tr("в архиве странный путь: %s"), f.Name)
		}
		top := strings.SplitN(clean, "/", 2)[0]
		if top == dataSub {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf(tr("архив аддона битый: %w"), err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf(tr("архив аддона битый: %w"), err)
		}
		files[clean] = b
	}
	toc, ok := files[tocFile]
	if !ok || reVersion.FindSubmatch(toc) == nil {
		return nil, fmt.Errorf(tr("в архиве нет %s"), tocFile)
	}
	for rel := range files {
		if strings.HasPrefix(rel, codeDir+"/") {
			return files, nil
		}
	}
	return nil, fmt.Errorf(tr("в архиве нет папки %s"), codeDir)
}

func writeFile(dst string, b []byte) error {
	if strings.Contains(strings.ToLower(filepath.ToSlash(dst)), "/interface/addons/") {
		return writeFileChecked(dst, b, addonWriteReady)
	}
	return writeFileChecked(dst, b, nil)
}

func writeFileChecked(dst string, b []byte, ready func() error) error {
	if ready != nil {
		if err := ready(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if ready != nil {
		if err := ready(); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf(tr("%s занят другой программой — закройте игру и запустите обновлялку снова"), filepath.Base(dst))
	}
	return nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func installAddon(dir string, files map[string][]byte) (string, error) {
	if devFolder(dir) {
		return "", fmt.Errorf("%s", tr("это папка разработки — аддон здесь не обновляю"))
	}
	if err := gameClosed(); err != nil {
		return "", err
	}
	if err := migrateData(dir); err != nil {
		return "", err
	}
	unlock, err := lockInstall(filepath.Dir(dir))
	if err != nil {
		return "", err
	}
	defer unlock()
	fresh := filepath.Join(dir, codeDir+".new")
	stale := filepath.Join(dir, codeDir+".old")
	_ = os.RemoveAll(fresh)
	_ = clearStale(filepath.Join(dir, codeDir), stale)
	exe, _ := exePath()
	exeName := strings.ToLower(filepath.Base(exe))
	var rootFiles []string
	for rel, b := range files {
		if strings.HasPrefix(rel, codeDir+"/") {
			if err := writeFile(filepath.Join(fresh, filepath.FromSlash(strings.TrimPrefix(rel, codeDir+"/"))), b); err != nil {
				return "", err
			}
			continue
		}
		if rel == tocFile || isLegacyName(rel) {
			continue
		}
		rootFiles = append(rootFiles, rel)
	}
	code := filepath.Join(dir, codeDir)
	if _, err := os.Stat(code); err == nil {
		if err := renameAddon(code, stale); err != nil {
			_ = os.RemoveAll(fresh)
			return "", fmt.Errorf(tr("папка %s занята — закройте игру и запустите обновлялку снова"), codeDir)
		}
	}
	if err := renameAddon(fresh, code); err != nil {
		_ = rollbackRename(stale, code)
		return "", fmt.Errorf(tr("не смог поставить новую папку %s"), codeDir)
	}
	_ = os.RemoveAll(stale)
	for _, rel := range rootFiles {
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		old := ""
		if strings.ToLower(filepath.Base(dst)) == exeName && strings.EqualFold(filepath.Dir(dst), filepath.Dir(exe)) {
			old = dst + ".old"
			_ = os.Remove(old)
			if renameAddon(dst, old) != nil {
				old = ""
			}
		}
		if err := writeFile(dst, files[rel]); err != nil {
			if old != "" {
				_ = rollbackRename(old, dst)
			}
			return "", err
		}
	}
	if err := writeFile(filepath.Join(dir, tocFile), files[tocFile]); err != nil {
		return "", err
	}
	cleanLegacy(dir)
	return string(reVersion.FindSubmatch(files[tocFile])[1]), nil
}

func cleanLegacy(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case e.IsDir() && name == "art":
			_ = os.RemoveAll(filepath.Join(dir, name))
		case !e.IsDir() && strings.HasSuffix(strings.ToLower(name), ".lua"):
			_ = os.Remove(filepath.Join(dir, name))
		case !e.IsDir() && name == "README.md":
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

func cleanOldExe() {
	if exe, err := exePath(); err == nil {
		_ = os.Remove(exe + ".old")
	}
}

func updateAddon(dir, url string, cnt *counter) (string, error) {
	if err := gameClosed(); err != nil {
		return "", err
	}
	cnt.file.Store(tr("аддон"))
	data, err := fetchAddonZip(url, cnt)
	if err != nil {
		return "", err
	}
	files, err := unpackAddon(data)
	if err != nil {
		return "", err
	}
	return installAddon(dir, files)
}

func addonCLI(dir string, cnt *counter) int {
	var files map[string][]byte
	var err error
	if os.Args[1] == "-addon-zip" {
		if len(os.Args) < 3 {
			fmt.Println("ОШИБКА: -addon-zip файл.zip")
			return 1
		}
		data, rerr := os.ReadFile(os.Args[2])
		if rerr != nil {
			fmt.Println("ОШИБКА:", rerr)
			return 1
		}
		files, err = unpackAddon(data)
	} else {
		rel := checkRelease(dir)
		switch {
		case rel.tag == "":
			fmt.Println("ОШИБКА: GitHub не ответил")
			return 1
		case !rel.newer:
			fmt.Printf("аддон %s — последняя версия\n", rel.local)
			return 0
		case rel.zip == "":
			fmt.Println("ОШИБКА: у релиза нет архива аддона")
			return 1
		}
		data, ferr := fetchAddonZip(rel.zip, cnt)
		if ferr != nil {
			fmt.Println("ОШИБКА:", ferr)
			return 1
		}
		files, err = unpackAddon(data)
	}
	if err != nil {
		fmt.Println("ОШИБКА:", err)
		return 1
	}
	ver, err := installAddon(dir, files)
	if err != nil {
		fmt.Println("ОШИБКА:", err)
		return 1
	}
	fmt.Println("аддон обновлён до", ver)
	return 0
}
