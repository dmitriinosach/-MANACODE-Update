package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	webFetchRetry = time.Hour
	webFileMax    = 64 << 20
)

var (
	webFetchMu   sync.Mutex
	webFetchFail = map[string]webFetchMiss{}
)

type webFetchMiss struct {
	at  time.Time
	err error
}

func findWebEngine(addons string) (string, string, error) {
	src := filepath.Join(addons, raidHelperName)
	if web := filepath.Join(src, webEngineDir); isFile(filepath.Join(web, "manifest.json")) {
		return src, web, nil
	}
	toc, _ := os.ReadFile(filepath.Join(src, raidHelperName+".toc"))
	m := reVersion.FindSubmatch(toc)
	if m == nil {
		return "", "", errors.New("нет Raid Helper — выгрузку собрать не из чего")
	}
	ver := string(m[1])
	cache := webCacheDir()
	if cache == "" {
		return "", "", errors.New("нет движка выгрузки: в папке Raid Helper нет web, а папки кэша нет")
	}
	web := filepath.Join(cache, ver, raidHelperName, webEngineDir)
	if isFile(filepath.Join(web, "manifest.json")) {
		return src, web, nil
	}
	if err := fetchWebEngine(ver, web); err != nil {
		return "", "", fmt.Errorf("нет движка выгрузки: в папке Raid Helper нет web, из релиза %s не скачался — %v", ver, err)
	}
	return src, web, nil
}

func fetchWebEngine(ver, dst string) error {
	webFetchMu.Lock()
	defer webFetchMu.Unlock()
	if miss, ok := webFetchFail[ver]; ok && now().Sub(miss.at) < webFetchRetry {
		return miss.err
	}
	err := downloadWebEngine(ver, dst)
	if err != nil {
		webFetchFail[ver] = webFetchMiss{at: now(), err: err}
		dcLog("движок выгрузки %s не скачан: %v", ver, err)
		return err
	}
	delete(webFetchFail, ver)
	dcLog("движок выгрузки %s скачан из релиза в %s", ver, dst)
	return nil
}

func downloadWebEngine(ver, dst string) error {
	list, code := askReleases(releasesAPI(raidHelperRepo))
	if list == nil {
		return fmt.Errorf("GitHub не ответил про релизы (%d)", code)
	}
	rel, ok := releaseByVersion(list, ver)
	if !ok {
		return errors.New("такой версии нет в релизах")
	}
	arch := archiveOf(rel)
	as := arch.lite
	if as.URL == "" {
		as = arch.full
	}
	if as.URL == "" || as.Size <= 0 {
		return errors.New("в релизе нет архива аддона")
	}
	zr, err := zip.NewReader(newRangeReader(as.URL, as.Size), as.Size)
	if err != nil {
		return fmt.Errorf("архив %s не читается: %v", as.Name, err)
	}
	prefix := raidHelperName + "/" + webEngineDir + "/"
	tmp := dst + ".part"
	_ = os.RemoveAll(tmp)
	n, manifest := 0, false
	for _, f := range zr.File {
		rel, ok := strings.CutPrefix(strings.ReplaceAll(f.Name, `\`, "/"), prefix)
		if !ok || rel == "" || strings.HasSuffix(rel, "/") || strings.Contains(rel, "..") {
			continue
		}
		if err := unzipTo(f, filepath.Join(tmp, filepath.FromSlash(rel))); err != nil {
			_ = os.RemoveAll(tmp)
			return err
		}
		n++
		manifest = manifest || rel == "manifest.json"
	}
	if n == 0 || !manifest {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("в архиве %s нет папки web", as.Name)
	}
	_ = os.RemoveAll(dst)
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	return nil
}

func unzipTo(f *zip.File, path string) error {
	if f.UncompressedSize64 > webFileMax {
		return fmt.Errorf("%s слишком большой", f.Name)
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, webFileMax+1))
	if err != nil {
		return fmt.Errorf("%s не распаковался: %v", f.Name, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
