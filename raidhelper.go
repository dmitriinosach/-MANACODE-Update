package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var (
	raidHelperRepo = "dmitriinosach/-MANACODE-RaidHelper"
	retryPause     = time.Second
)

const (
	raidHelperName  = "ManaCode_RaidHelper"
	raidHelperTitle = "Raid Helper"
	channelBeta     = "beta"
)

var errNotInArchive = errors.New("папки нет в архиве")

var errGameRunning = errors.New("игра запущена — закройте её полностью (Wow.exe) и повторите")

var gameExe = "Wow.exe"

var gameRunning = func() bool { return processRunning(gameExe) }

func gameClosed() error {
	if gameRunning() {
		return errGameRunning
	}
	return nil
}

type rhConfig struct {
	Channel string            `json:"channel,omitempty"`
	Sets    map[string]string `json:"testSets,omitempty"`
	Packs   *[]string         `json:"packs,omitempty"`
}

func (c config) beta() bool {
	return c.RaidHelper != nil && c.RaidHelper.Channel == channelBeta
}

func (c *config) rh() *rhConfig {
	if c.RaidHelper == nil {
		c.RaidHelper = &rhConfig{}
	}
	return c.RaidHelper
}

func channelName(beta bool) string {
	if beta {
		return "«бета»"
	}
	return "«стабильный»"
}

type otherAddon struct {
	name   string
	folder string
	repo   string
}

type otherRelease struct {
	addon   otherAddon
	local   string
	tag     string
	zip     string
	url     string
	pre     bool
	beta    bool
	newer   bool
	missing bool
	dev     bool
	none    bool
	all     []ghRelease
}

type ghRelease struct {
	Tag        string         `json:"tag_name"`
	HTML       string         `json:"html_url"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

func raidHelper(dir string) otherAddon {
	return otherAddon{name: raidHelperName, folder: folderIn(filepath.Dir(dir), raidHelperName), repo: raidHelperRepo}
}

func (a otherAddon) toc() []byte {
	data, _ := os.ReadFile(filepath.Join(a.folder, a.name+".toc"))
	return data
}

func (a otherAddon) localVersion() string {
	if m := reVersion.FindSubmatch(a.toc()); m != nil {
		return string(m[1])
	}
	return ""
}

func releasesAPI(repo string) string {
	if strings.HasPrefix(repo, "http") {
		return repo
	}
	return "https://api.github.com/repos/" + repo + "/releases?per_page=20"
}

func askReleases(api string) ([]ghRelease, int) {
	req, err := http.NewRequest(http.MethodGet, api, nil)
	if err != nil {
		return nil, 0
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode
	}
	var list []ghRelease
	if json.NewDecoder(resp.Body).Decode(&list) != nil {
		return nil, 0
	}
	return list, http.StatusOK
}

func assetZip(rel ghRelease, name string) string {
	low := strings.ToLower(name)
	for _, as := range rel.Assets {
		n := strings.ToLower(as.Name)
		if strings.HasPrefix(n, low+"-") && !strings.HasPrefix(n, low+liteInfix) && strings.HasSuffix(n, ".zip") {
			return as.URL
		}
	}
	return ""
}

func pickRelease(list []ghRelease, name string, beta bool) (ghRelease, bool) {
	var best, stable ghRelease
	hasBest, hasStable := false, false
	for _, r := range list {
		if r.Draft || r.Tag == "" || assetZip(r, name) == "" {
			continue
		}
		pre := r.Prerelease || isPrerelease(r.Tag)
		if !hasBest || newer(r.Tag, best.Tag) {
			best, hasBest = r, true
		}
		if !pre && (!hasStable || newer(r.Tag, stable.Tag)) {
			stable, hasStable = r, true
		}
	}
	if !beta && hasStable {
		return stable, true
	}
	return best, hasBest
}

func checkOther(a otherAddon, beta bool) otherRelease {
	out := otherRelease{addon: a, local: a.localVersion(), dev: devFolder(a.folder), beta: beta}
	out.missing = out.local == ""
	if a.repo == "" {
		return out
	}
	var list []ghRelease
	status := 0
	for try := 0; try < 3 && status != http.StatusOK && status != http.StatusNotFound; try++ {
		if try > 0 {
			time.Sleep(retryPause)
		}
		list, status = askReleases(releasesAPI(a.repo))
	}
	if status == http.StatusNotFound {
		out.none = true
		return out
	}
	if status != http.StatusOK {
		return out
	}
	out.all = list
	rel, ok := pickRelease(list, a.name, beta)
	if !ok {
		out.none = true
		return out
	}
	out.tag, out.url, out.zip = rel.Tag, rel.HTML, assetZip(rel, a.name)
	out.pre = rel.Prerelease || isPrerelease(rel.Tag)
	out.newer = !out.dev && (out.missing || newer(rel.Tag, out.local))
	return out
}

func unpackOther(a otherAddon, data []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("архив %s битый: %w", a.name, err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		top, rest, ok := strings.Cut(name, "/")
		if !ok || !strings.EqualFold(top, a.name) || rest == "" || strings.HasSuffix(rest, "/") {
			continue
		}
		clean := path.Clean(rest)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || strings.ContainsAny(clean, ":") {
			return nil, fmt.Errorf("в архиве %s странный путь: %s", a.name, f.Name)
		}
		if !strings.Contains(clean, "/") && strings.EqualFold(clean, a.name+".toc") {
			clean = a.name + ".toc"
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("архив %s битый: %w", a.name, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("архив %s битый: %w", a.name, err)
		}
		files[clean] = b
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: %w", a.name, errNotInArchive)
	}
	toc, ok := files[a.name+".toc"]
	if !ok || reVersion.FindSubmatch(toc) == nil {
		return nil, fmt.Errorf("в архиве нет %s.toc", a.name)
	}
	return files, nil
}

func installOther(a otherAddon, files map[string][]byte) (string, error) {
	if devFolder(a.folder) {
		return "", fmt.Errorf("%s — папка разработки, не обновляю", a.name)
	}
	if err := gameClosed(); err != nil {
		return "", err
	}
	unlock, err := lockInstall(filepath.Dir(a.folder))
	if err != nil {
		return "", err
	}
	defer unlock()
	ver := string(reVersion.FindSubmatch(files[a.name+".toc"])[1])
	if selfInside(a.folder) {
		if err := installInPlace(a, files); err != nil {
			return "", err
		}
		return ver, nil
	}
	fresh, stale := a.folder+".new", a.folder+".old"
	_ = os.RemoveAll(fresh)
	_ = os.RemoveAll(stale)
	for rel, b := range files {
		if err := writeFile(filepath.Join(fresh, filepath.FromSlash(rel)), b); err != nil {
			_ = os.RemoveAll(fresh)
			return "", err
		}
	}
	if err := carryKept(a, fresh, files); err != nil {
		_ = os.RemoveAll(fresh)
		return "", err
	}
	if _, err := os.Stat(a.folder); err == nil {
		if err := os.Rename(a.folder, stale); err != nil {
			_ = os.RemoveAll(fresh)
			return "", fmt.Errorf("папка %s занята — закройте игру и запустите обновлялку снова", a.name)
		}
	}
	if err := os.Rename(fresh, a.folder); err != nil {
		_ = os.Rename(stale, a.folder)
		return "", fmt.Errorf("не смог поставить новую папку %s", a.name)
	}
	_ = os.RemoveAll(stale)
	return ver, nil
}
