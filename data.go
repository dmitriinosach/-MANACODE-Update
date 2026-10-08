package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var (
	dataBase = "https://raw.githubusercontent.com/dmitriinosach/-MANACODE-PlayerRaidsInfo/data/"
	zipPass  = ""
)

var releaseAPI = "https://api.github.com/repos/dmitriinosach/-MANACODE-PlayerRaidsInfo/releases/latest"

const (
	mainFile    = "Data.lua"
	archiveDir  = "Manacode_PlayerRaidsInfo_Archive"
	configFile  = "updater.json"
	addonSVName = "Manacode_PlayerRaidsInfo.lua"
)

type fileInfo struct {
	Name   string `json:"name"`
	Season int    `json:"season"`
	Count  int    `json:"count"`
	Bytes  int64  `json:"bytes"`
	Zip    string `json:"zip"`
	ZipLen int64  `json:"zipBytes"`
}

type index struct {
	V        int        `json:"v"`
	Baked    string     `json:"baked"`
	Season   int        `json:"season"`
	Seasons  []int      `json:"seasons"`
	Complete bool       `json:"complete"`
	Count    int        `json:"count"`
	Files    []fileInfo `json:"files"`
	GM       *gmInfo    `json:"gm"`
}

type config struct {
	SeasonsFrom *int        `json:"seasonsFrom,omitempty"`
	Seasons     []int       `json:"seasons,omitempty"`
	RaidHelper  *rhConfig   `json:"raidHelper,omitempty"`
	Auto        *autoConfig `json:"auto,omitempty"`
	V13         *v13Choice  `json:"v13,omitempty"`
	SelfUpdate  bool        `json:"selfUpdate,omitempty"`
	UpdaterExe  string      `json:"updaterExe,omitempty"`
}

type localMeta struct {
	Baked    string
	Count    int
	Complete bool
	Seasons  map[int]int
	Modes    map[string]bool
}

type counter struct {
	got   atomic.Int64
	total atomic.Int64
	file  atomic.Value
}

type releaseMsg struct {
	tag     string
	url     string
	zip     string
	local   string
	newer   bool
	missing bool
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

var reVersion = regexp.MustCompile(`(?m)^## Version:\s*(\S+)`)

type latestRelease struct {
	Tag    string         `json:"tag_name"`
	HTML   string         `json:"html_url"`
	Assets []releaseAsset `json:"assets"`
}

func askRelease(api string) (latestRelease, bool) {
	var rel latestRelease
	req, err := http.NewRequest(http.MethodGet, api, nil)
	if err != nil {
		return rel, false
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return rel, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return rel, false
	}
	if json.NewDecoder(resp.Body).Decode(&rel) != nil || rel.Tag == "" {
		return rel, false
	}
	return rel, true
}

func checkRelease(dir string) releaseMsg {
	local := ""
	if data, err := os.ReadFile(filepath.Join(dir, tocFile)); err == nil {
		if m := reVersion.FindStringSubmatch(string(data)); m != nil {
			local = m[1]
		}
	}
	var rel latestRelease
	ok := false
	for try := 0; try < 3 && !ok; try++ {
		if try > 0 {
			time.Sleep(retryPause)
		}
		rel, ok = askRelease(releaseAPI)
	}
	if !ok {
		return releaseMsg{local: local}
	}
	missing := local == "" || !isDir(filepath.Join(dir, codeDir))
	out := releaseMsg{tag: rel.Tag, url: rel.HTML, local: local, missing: missing, newer: missing || newer(rel.Tag, local)}
	for _, a := range rel.Assets {
		if strings.HasPrefix(a.Name, addonName) && strings.HasSuffix(a.Name, ".zip") {
			out.zip = a.URL
		}
	}
	return out
}

func parseIndex(body []byte) (*index, error) {
	var idx index
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("сервер прислал не то: %w", err)
	}
	if idx.V < 7 || len(idx.Files) == 0 {
		return nil, fmt.Errorf("на сервере старый формат выгрузки (v%d)", idx.V)
	}
	return &idx, nil
}

type indexCheck struct {
	idx     *index
	same    bool
	etag    string
	lastMod string
	err     error
}

func fetchIndexIf(etag, lastMod string) indexCheck {
	resp, err := requestIf("index.json", etag, lastMod)
	if err != nil {
		return indexCheck{err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return indexCheck{same: true, etag: etag, lastMod: lastMod}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return indexCheck{err: fmt.Errorf("оборвалось скачивание index.json")}
	}
	idx, err := parseIndex(body)
	return indexCheck{idx: idx, err: err, etag: resp.Header.Get("ETag"), lastMod: resp.Header.Get("Last-Modified")}
}

func seasonsOf(idx index) []int {
	seen := map[int]bool{}
	var out []int
	for _, f := range idx.Files {
		if !seen[f.Season] {
			seen[f.Season] = true
			out = append(out, f.Season)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out
}

func mb(n int64) string { return fmt.Sprintf("%.1f МБ", float64(n)/1048576) }

func num(n int) string {
	s := strconv.Itoa(n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func bakedRU(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.In(time.FixedZone("MSK", 3*3600)).Format("02.01 15:04") + " МСК"
}

func request(name string) (*http.Response, error) {
	return requestIf(name, "", "")
}

func requestIf(name, etag, lastMod string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, dataBase+name, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Cache-Control", "no-cache")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastMod != "" {
		req.Header.Set("If-Modified-Since", lastMod)
	}
	var resp *http.Response
	for try := 0; try < 3; try++ {
		if try > 0 {
			time.Sleep(3 * time.Second)
		}
		resp, err = (&http.Client{Timeout: 10 * time.Minute}).Do(req)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("не удалось соединиться с GitHub")
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return resp, nil
	case resp.StatusCode == http.StatusNotModified && etag+lastMod != "":
		return resp, nil
	case resp.StatusCode == http.StatusNotFound:
		resp.Body.Close()
		return nil, fmt.Errorf("на GitHub нет файла %s — выгрузка ещё не опубликована", name)
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("GitHub ответил %s", resp.Status)
	}
}

func get(name string) ([]byte, error) {
	resp, err := request(name)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

type countingReader struct {
	r   io.Reader
	cnt *counter
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.cnt.got.Add(int64(n))
	return n, err
}

func fetchTo(path string, f fileInfo, cnt *counter) error {
	cnt.file.Store(f.Name)
	resp, err := request(f.Zip)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	zip, err := io.ReadAll(&countingReader{r: resp.Body, cnt: cnt})
	if err != nil {
		return fmt.Errorf("%s: оборвалось скачивание", f.Zip)
	}
	_, data, err := unzipFirstAES(zip, zipPass)
	if err != nil {
		return fmt.Errorf("%s: %w", f.Zip, err)
	}
	s := strings.TrimSpace(string(data))
	if !strings.HasSuffix(s, "}") || !strings.Contains(s, "PlayerRaids") {
		return fmt.Errorf("%s: внутри не тот файл", f.Zip)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		if werr := os.WriteFile(path, data, 0o644); werr != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("%s занят другой программой — закройте игру и запустите обновлялку снова", filepath.Base(path))
		}
		_ = os.Remove(tmp)
	}
	return nil
}

func dropStaleSeasons(adir string, idx index) {
	keep := map[int]bool{}
	for _, f := range idx.Files {
		if f.Season < idx.Season {
			keep[f.Season] = true
		}
	}
	entries, _ := os.ReadDir(adir)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "Data_s") || !strings.HasSuffix(name, ".lua") {
			continue
		}
		s, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "Data_s"), ".lua"))
		if err == nil && !keep[s] {
			_ = os.Remove(filepath.Join(adir, name))
		}
	}
}

func download(idx index, dir string, sel map[int]bool, cnt *counter) ([]string, error) {
	adir := dataDir(dir)
	if err := os.MkdirAll(adir, 0o755); err != nil {
		return nil, err
	}
	var written []string
	var archive []fileInfo
	for _, f := range idx.Files {
		if f.Season < idx.Season {
			archive = append(archive, f)
		}
	}
	sort.Slice(archive, func(i, j int) bool { return archive[i].Season > archive[j].Season })
	for _, f := range archive {
		path := filepath.Join(adir, f.Name)
		if sel[f.Season] {
			if err := fetchTo(path, f, cnt); err != nil {
				return written, err
			}
			written = append(written, f.Name)
			continue
		}
		stub := fmt.Sprintf("PlayerRaidsArchive = PlayerRaidsArchive or {}\nPlayerRaidsArchive[%d] = false\n", f.Season)
		if err := os.WriteFile(path, []byte(stub), 0o644); err != nil {
			return written, err
		}
	}
	_ = os.RemoveAll(filepath.Join(filepath.Dir(dir), archiveDir))
	dropStaleSeasons(adir, idx)
	for _, f := range idx.Files {
		if f.Season == idx.Season {
			if err := fetchTo(filepath.Join(adir, mainFile), f, cnt); err != nil {
				return written, err
			}
			written = append(written, mainFile)
		}
	}
	return written, nil
}

var (
	reBaked    = regexp.MustCompile(`baked\s*=\s*"([^"]*)"`)
	reComplete = regexp.MustCompile(`complete\s*=\s*(\w+)`)
	reCount    = regexp.MustCompile(`count\s*=\s*(\d+)`)
	reSVFrom   = regexp.MustCompile(`\["seasonsFrom"\]\s*=\s*(\d+)`)
	reSVSet    = regexp.MustCompile(`(?s)\["seasons"\]\s*=\s*\{(.*?)\}`)
	reSVItem   = regexp.MustCompile(`\[(\d+)\]\s*=\s*true`)
	reArchIdx  = regexp.MustCompile(`PlayerRaidsArchive\[(\d+)\]\s*=\s*(false|\{)`)
)

func tail(path string, n int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	off := fi.Size() - n
	if off < 0 {
		off = 0
	}
	buf := make([]byte, fi.Size()-off)
	_, _ = f.ReadAt(buf, off)
	return string(buf)
}

func countLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	return strings.Count(string(data), "\n[")
}

func readLocal(dir string) localMeta {
	lm := localMeta{Seasons: map[int]int{}}
	t := tail(filepath.Join(dataDir(dir), mainFile), 4096)
	if m := reBaked.FindStringSubmatch(t); m != nil {
		lm.Baked = m[1]
	}
	if m := reComplete.FindStringSubmatch(t); m != nil {
		lm.Complete = m[1] == "true"
	}
	if m := reCount.FindStringSubmatch(t); m != nil {
		lm.Count, _ = strconv.Atoi(m[1])
	}
	adir := dataDir(dir)
	entries, _ := os.ReadDir(adir)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "Data_s") || !strings.HasSuffix(name, ".lua") {
			continue
		}
		s, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "Data_s"), ".lua"))
		if err != nil {
			continue
		}
		head := tail(filepath.Join(adir, name), 1<<30)
		if m := reArchIdx.FindStringSubmatch(head); m != nil && m[2] == "false" {
			lm.Seasons[s] = -1
			continue
		}
		lm.Seasons[s] = countLines(filepath.Join(adir, name))
	}
	return lm
}

func seasonsFromGame(dir string) map[int]bool {
	wtf := filepath.Join(dir, "..", "..", "..", "WTF", "Account")
	files, _ := filepath.Glob(filepath.Join(wtf, "*", "SavedVariables", addonSVName))
	var best map[int]bool
	var bestTime time.Time
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		set := map[int]bool{}
		if m := reSVSet.FindStringSubmatch(string(data)); m != nil {
			for _, it := range reSVItem.FindAllStringSubmatch(m[1], -1) {
				if v, err := strconv.Atoi(it[1]); err == nil {
					set[v] = true
				}
			}
		} else if m := reSVFrom.FindStringSubmatch(string(data)); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil {
				for x := v; x <= 20; x++ {
					set[x] = true
				}
			}
		}
		if len(set) == 0 {
			continue
		}
		if best == nil || fi.ModTime().After(bestTime) {
			best, bestTime = set, fi.ModTime()
		}
	}
	return best
}

func loadConfig() config {
	var c config
	data, err := os.ReadFile(filepath.Join(appHome(), configFile))
	if err == nil {
		_ = json.Unmarshal(data, &c)
	}
	return c
}

func saveConfig(c config) {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(appHome(), 0o755)
	_ = os.WriteFile(filepath.Join(appHome(), configFile), data, 0o644)
}

func exeDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}
