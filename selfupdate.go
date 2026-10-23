package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const noteSelf = "self"

var updaterRepo = "dmitriinosach/-MANACODE-Update"

type selfInfo struct {
	version string
	exe     string
	digest  string
	page    string
}

func checkSelf() (*selfInfo, error) {
	if updaterRepo == "" {
		return nil, nil
	}
	var list []ghRelease
	status := 0
	for try := 0; try < 3 && status != http.StatusOK && status != http.StatusNotFound; try++ {
		if try > 0 {
			time.Sleep(retryPause)
		}
		list, status = askReleases(releasesAPI(updaterRepo))
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, nil
	default:
		return nil, errors.New("GitHub не ответил про обновлялку")
	}
	var best *selfInfo
	for _, r := range list {
		if r.Draft || r.Prerelease || isPrerelease(r.Tag) {
			continue
		}
		v := tagVersion(r.Tag)
		if _, ok := parseSemver(v); !ok {
			continue
		}
		url, digest := "", ""
		for _, as := range r.Assets {
			if strings.EqualFold(as.Name, updaterExe) {
				url, digest = as.URL, as.Digest
			}
		}
		if url == "" {
			continue
		}
		if best == nil || newer(v, best.version) {
			best = &selfInfo{version: v, exe: url, digest: digest, page: r.HTML}
		}
	}
	if best == nil || !newer(best.version, ownVersion()) {
		return nil, nil
	}
	return best, nil
}

func selfLocked() bool {
	exe, err := exePath()
	return err != nil || quietRun || devFolder(filepath.Dir(exe)) || throwawayExe(exe)
}

func applySelf(info *selfInfo, cnt *counter) error {
	exe, err := exePath()
	if err != nil {
		return err
	}
	if devFolder(filepath.Dir(exe)) {
		return errors.New("обновлялка лежит в папке разработки — себя не меняю")
	}
	if cnt != nil {
		cnt.file.Store(updaterExe)
	}
	data, err := fetchRaw(info.exe, "обновлялки", cnt)
	if err != nil {
		return err
	}
	if len(data) < 2 || data[0] != 0x4D || data[1] != 0x5A {
		return errors.New("скачалась не программа")
	}
	if m := reVersionMark.FindSubmatch(data); m == nil || string(m[1]) != info.version {
		return fmt.Errorf("скачанная обновлялка не версии %s", info.version)
	}
	if want, ok := strings.CutPrefix(info.digest, "sha256:"); ok {
		sum := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), want) {
			return errors.New("контрольная сумма скачанной обновлялки не совпала с релизом")
		}
	}
	old, fresh := exe+".old", exe+"~"
	if err := os.WriteFile(fresh, data, 0o755); err != nil {
		_ = os.Remove(fresh)
		return fmt.Errorf("не смог записать новую обновлялку: %w", err)
	}
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		_ = os.Remove(fresh)
		return fmt.Errorf("не смог заменить %s: файл занят", filepath.Base(exe))
	}
	if err := os.Rename(fresh, exe); err != nil {
		_ = os.Rename(old, exe)
		_ = os.Remove(fresh)
		return fmt.Errorf("не смог записать новую обновлялку: %w", err)
	}
	return nil
}

func autoSelf(dir string, st *autoConfig) {
	if selfLocked() {
		return
	}
	info, err := checkSelf()
	switch {
	case err != nil:
		st.note(noteSelf, "обновлялка: "+err.Error())
	case info != nil:
		if err := applySelf(info, nil); err != nil {
			st.note(noteSelf, "ManacodeUpdate "+info.version+" не встала: "+err.Error()+" — скачайте вручную: "+info.page)
			return
		}
		st.note(noteSelf, "обновлялка обновлена до "+info.version)
	}
}
