package main

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const noteSelf = "self"

var updaterRepo = "dmitriinosach/-MANACODE-Update"

type selfInfo struct {
	version string
	exe     string
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
		url := ""
		for _, as := range r.Assets {
			if strings.EqualFold(as.Name, updaterExe) {
				url = as.URL
			}
		}
		if url == "" {
			continue
		}
		if best == nil || newer(v, best.version) {
			best = &selfInfo{version: v, exe: url, page: r.HTML}
		}
	}
	if best == nil || !newer(best.version, ownVersion()) {
		return nil, nil
	}
	return best, nil
}

func selfLocked() bool {
	exe, err := exePath()
	return err != nil || devFolder(filepath.Dir(exe))
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
		st.note(noteSelf, "вышла ManacodeUpdate "+info.version+" — скачайте вручную: "+info.page)
	}
}
