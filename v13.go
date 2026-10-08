package main

import (
	"crypto/sha256"
	"encoding/hex"
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
)

const (
	formatV13    = 13
	manifestPath = "v13/manifest.json"
	playersFile  = "Players.lua"
	v13Global    = "PlayerRaids13"
)

var (
	reV13Season = regexp.MustCompile(`^s(\d+)\.lua$`)
	reV13Mode   = regexp.MustCompile(`^s(\d+)_([a-z0-9]+)\.lua$`)
	reV12Data   = regexp.MustCompile(`^Data(_s\d+)?\.lua$`)
)

type v13Zone struct {
	Title  string   `json:"title"`
	Bosses []string `json:"bosses"`
}

type v13Mode struct {
	ID    string `json:"id"`
	Zone  string `json:"zone"`
	Size  int    `json:"size"`
	Diff  string `json:"diff"`
	Title string `json:"title"`
	Flag  string `json:"flag"`
}

type v13File struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Season   int    `json:"season"`
	Zone     string `json:"zone"`
	Mode     string `json:"mode"`
	Rows     int    `json:"rows"`
	Raids    int    `json:"raids"`
	Complete bool   `json:"complete"`
	Bytes    int64  `json:"bytes"`
	SHA256   string `json:"sha256"`
	Zip      string `json:"zip"`
	ZipLen   int64  `json:"zipBytes"`
}

type manifest struct {
	Format  int                `json:"format"`
	Baked   string             `json:"baked"`
	Season  int                `json:"season"`
	Seasons []int              `json:"seasons"`
	Zones   map[string]v13Zone `json:"zones"`
	Modes   []v13Mode          `json:"modes"`
	Files   []v13File          `json:"files"`
	GM      *gmInfo            `json:"gm"`
}

type v13Choice struct {
	Seasons []int    `json:"seasons"`
	Modes   []string `json:"modes"`
}

type v13Sel struct {
	seasons map[int]bool
	modes   map[string]bool
}

var zoneTitleRU = map[string]string{"icc": "ЦЛК", "rs": "РС", "toc": "ИВК", "uld": "Ульдуар"}

var defaultZones = map[string]bool{"icc": true, "rs": true}

var gridCols = []struct {
	size int
	diff string
	head string
}{{10, "n", "10 об"}, {10, "h", "10 гер"}, {25, "n", "25 об"}, {25, "h", "25 гер"}}

func parseManifest(body []byte) (*manifest, error) {
	var man manifest
	if err := json.Unmarshal(body, &man); err != nil {
		return nil, fmt.Errorf("сервер прислал не то: %w", err)
	}
	if man.Format < formatV13 || man.players() == nil {
		return nil, fmt.Errorf("в списке файлов v13 нет %s", playersFile)
	}
	return &man, nil
}

func (man *manifest) players() *v13File {
	for i := range man.Files {
		if man.Files[i].Kind == "players" || man.Files[i].Path == playersFile {
			return &man.Files[i]
		}
	}
	return nil
}

func (man *manifest) core(season int) *v13File {
	for i := range man.Files {
		if man.Files[i].Kind == "season" && man.Files[i].Season == season {
			return &man.Files[i]
		}
	}
	return nil
}

func (man *manifest) modeFile(season int, mode string) *v13File {
	for i := range man.Files {
		f := &man.Files[i]
		if f.Kind == "mode" && f.Season == season && f.Mode == mode {
			return f
		}
	}
	return nil
}

func (man *manifest) seasonList() []int {
	seen := map[int]bool{}
	var out []int
	for _, f := range man.Files {
		if f.Kind == "season" && !seen[f.Season] {
			seen[f.Season] = true
			out = append(out, f.Season)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out
}

func (man *manifest) zoneOrder() []string {
	seen := map[string]bool{}
	var out []string
	for _, md := range man.Modes {
		if md.Zone != "" && !seen[md.Zone] {
			seen[md.Zone] = true
			out = append(out, md.Zone)
		}
	}
	return out
}

func (man *manifest) zoneTitle(z string) string {
	if t := man.Zones[z].Title; t != "" {
		return t
	}
	if t := zoneTitleRU[z]; t != "" {
		return t
	}
	return z
}

func (man *manifest) modeAt(zone string, size int, diff string) *v13Mode {
	for i := range man.Modes {
		md := &man.Modes[i]
		if md.Zone == zone && md.Size == size && md.Diff == diff {
			return md
		}
	}
	return nil
}

func (man *manifest) modeTitle(id string) string {
	for _, md := range man.Modes {
		if md.ID == id && md.Title != "" {
			return md.Title
		}
	}
	return id
}

func pickSeasons(want []int, all []int) map[int]bool {
	set := map[int]bool{}
	for _, s := range want {
		set[s] = true
	}
	sel := map[int]bool{}
	for _, s := range all {
		if set[s] {
			sel[s] = true
		}
	}
	return sel
}

func choose13(cfg config, man *manifest, fromGame map[int]bool) v13Sel {
	all := man.seasonList()
	var seasons map[int]bool
	switch {
	case cfg.V13 != nil:
		seasons = pickSeasons(cfg.V13.Seasons, all)
	case len(cfg.Seasons) > 0:
		seasons = pickSeasons(cfg.Seasons, all)
	case fromGame != nil:
		var list []int
		for s := range fromGame {
			list = append(list, s)
		}
		seasons = pickSeasons(list, all)
	}
	if len(seasons) == 0 && cfg.V13 == nil {
		seasons = pickSeasons(all, all)
	}
	if seasons == nil {
		seasons = map[int]bool{}
	}
	seasons[man.Season] = true
	modes := map[string]bool{}
	if cfg.V13 != nil {
		for _, id := range cfg.V13.Modes {
			modes[id] = true
		}
	} else {
		for _, md := range man.Modes {
			if defaultZones[md.Zone] {
				modes[md.ID] = true
			}
		}
	}
	return v13Sel{seasons: seasons, modes: modes}
}

func (s v13Sel) choice(man *manifest) *v13Choice {
	c := &v13Choice{Seasons: []int{}, Modes: []string{}}
	for _, sn := range man.seasonList() {
		if s.seasons[sn] {
			c.Seasons = append(c.Seasons, sn)
		}
	}
	for _, md := range man.Modes {
		if s.modes[md.ID] {
			c.Modes = append(c.Modes, md.ID)
		}
	}
	return c
}

func needed13(man *manifest, sel v13Sel) []v13File {
	var out []v13File
	if p := man.players(); p != nil {
		out = append(out, *p)
	}
	for _, sn := range man.seasonList() {
		if !sel.seasons[sn] {
			continue
		}
		if c := man.core(sn); c != nil {
			out = append(out, *c)
		}
		for _, md := range man.Modes {
			if sel.modes[md.ID] {
				if f := man.modeFile(sn, md.ID); f != nil {
					out = append(out, *f)
				}
			}
		}
	}
	return out
}

func isV13File(name string) bool {
	return name == playersFile || reV13Season.MatchString(name) || reV13Mode.MatchString(name)
}

func fileSHA(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func fresh13(path string, f v13File) bool {
	if f.SHA256 == "" {
		return false
	}
	return strings.EqualFold(fileSHA(path), strings.TrimSpace(f.SHA256))
}

func fetchPlain13(f v13File, cnt *counter) ([]byte, error) {
	cnt.file.Store(f.Path)
	resp, err := request(f.Zip)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	zip, err := io.ReadAll(&countingReader{r: resp.Body, cnt: cnt})
	if err != nil {
		return nil, fmt.Errorf("%s: оборвалось скачивание", f.Zip)
	}
	_, data, err := unzipFirstAES(zip, zipPass)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Zip, err)
	}
	if f.SHA256 != "" {
		sum := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(f.SHA256)) {
			return nil, fmt.Errorf("%s скачался с ошибкой (не сошлась контрольная сумма) — запустите ещё раз", f.Path)
		}
	}
	if !strings.Contains(string(data), v13Global) {
		return nil, fmt.Errorf("%s: внутри не тот файл", f.Zip)
	}
	return data, nil
}

func plan13(man *manifest, dir string, sel v13Sel) (need, todo []v13File) {
	adir := dataDir(dir)
	need = needed13(man, sel)
	for _, f := range need {
		if !fresh13(filepath.Join(adir, f.Path), f) {
			todo = append(todo, f)
		}
	}
	return need, todo
}

func download13(man *manifest, dir string, sel v13Sel, cnt *counter) ([]string, error) {
	adir := dataDir(dir)
	if err := os.MkdirAll(adir, 0o755); err != nil {
		return nil, err
	}
	need, todo := plan13(man, dir, sel)
	var total int64
	for _, f := range todo {
		total += f.ZipLen
	}
	cnt.got.Store(0)
	cnt.total.Store(total)
	type staged struct{ tmp, path, name string }
	var ready []staged
	drop := func() {
		for _, s := range ready {
			_ = os.Remove(s.tmp)
		}
	}
	for _, f := range todo {
		data, err := fetchPlain13(f, cnt)
		if err != nil {
			drop()
			return nil, err
		}
		path := filepath.Join(adir, f.Path)
		if err := os.WriteFile(path+".tmp", data, 0o644); err != nil {
			drop()
			return nil, err
		}
		ready = append(ready, staged{tmp: path + ".tmp", path: path, name: f.Path})
	}
	var written []string
	for _, s := range ready {
		if err := os.Rename(s.tmp, s.path); err != nil {
			data, rerr := os.ReadFile(s.tmp)
			if rerr != nil || os.WriteFile(s.path, data, 0o644) != nil {
				drop()
				return written, fmt.Errorf("%s занят другой программой — закройте игру и запустите обновлялку снова", s.name)
			}
			_ = os.Remove(s.tmp)
		}
		written = append(written, s.name)
	}
	keep := map[string]bool{}
	for _, f := range need {
		keep[f.Path] = true
	}
	entries, _ := os.ReadDir(adir)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		stale := (isV13File(name) && !keep[name]) || reV12Data.MatchString(name) ||
			(strings.HasSuffix(name, ".lua.tmp") && isV13File(strings.TrimSuffix(name, ".tmp")))
		if stale {
			_ = os.Remove(filepath.Join(adir, name))
		}
	}
	_ = os.RemoveAll(filepath.Join(filepath.Dir(dir), archiveDir))
	return written, nil
}

func fetchManifest() (*manifest, error) {
	body, err := get(manifestPath)
	if err != nil {
		return nil, err
	}
	return parseManifest(body)
}

type manifestCheck struct {
	man     *manifest
	same    bool
	etag    string
	lastMod string
	err     error
}

func fetchManifestIf(etag, lastMod string) manifestCheck {
	resp, err := requestIf(manifestPath, etag, lastMod)
	if err != nil {
		return manifestCheck{err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return manifestCheck{same: true, etag: etag, lastMod: lastMod}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return manifestCheck{err: fmt.Errorf("оборвалось скачивание списка файлов")}
	}
	man, err := parseManifest(body)
	return manifestCheck{man: man, err: err, etag: resp.Header.Get("ETag"), lastMod: resp.Header.Get("Last-Modified")}
}

func formatMismatch13(dir string, man *manifest) error {
	want := localFormat(dir)
	if man == nil || want == 0 || man.Format == want {
		return nil
	}
	return fmt.Errorf("на сервере данные формата v%d, а аддону нужен v%d — данные не трогаю, чтобы аддон не сломался", man.Format, want)
}

func readLocal13(dir string) localMeta {
	lm := localMeta{Seasons: map[int]int{}, Modes: map[string]bool{}}
	adir := dataDir(dir)
	t := tail(filepath.Join(adir, playersFile), 4096)
	if m := reBaked.FindStringSubmatch(t); m != nil {
		lm.Baked = m[1]
	}
	if m := reCount.FindStringSubmatch(t); m != nil {
		lm.Count, _ = strconv.Atoi(m[1])
	}
	lm.Complete = true
	entries, _ := os.ReadDir(adir)
	for _, e := range entries {
		name := e.Name()
		if m := reV13Season.FindStringSubmatch(name); m != nil {
			s, _ := strconv.Atoi(m[1])
			lm.Seasons[s] = countLines(filepath.Join(adir, name))
		} else if m := reV13Mode.FindStringSubmatch(name); m != nil {
			lm.Modes[m[2]] = true
		}
	}
	return lm
}

func autoData13(dir string, cfg config, st *autoConfig, cnt *counter) int {
	baked, gmBaked, gmTag := localBaked(dir), gmLocalBaked(dir), gmKeyTag(dir)
	etag, lastMod := "", ""
	if baked != "" && baked == st.Baked && gmBaked == st.GMBaked && gmTag == st.GMKey {
		etag, lastMod = st.ETag, st.LastModified
	}
	res := fetchManifestIf(etag, lastMod)
	if res.err != nil {
		st.note(noteData, "данные: "+res.err.Error())
		return 1
	}
	if res.same {
		st.note(noteData, fmt.Sprintf("данные уже свежие (%s)", baked))
		return 0
	}
	st.ETag, st.LastModified = "", ""
	man := res.man
	if err := formatMismatch13(dir, man); err != nil {
		st.note(noteData, "данные: "+err.Error())
		return 1
	}
	code, ok := 0, true
	written, err := download13(man, dir, choose13(cfg, man, seasonsFromGame(dir)), cnt)
	switch {
	case err != nil:
		st.note(noteData, "данные: "+err.Error())
		code, ok = 1, false
	case len(written) == 0:
		st.note(noteData, fmt.Sprintf("данные уже свежие (%s)", man.Baked))
	default:
		st.note(noteData, fmt.Sprintf("данные обновлены до %s: %s", man.Baked, strings.Join(written, ", ")))
	}
	if status, err := syncGM(dir, man.GM, cnt); err != nil {
		st.note(noteGM, "ГМ: "+err.Error())
		ok = false
	} else if status != "" {
		st.note(noteGM, "ГМ: "+status)
	}
	if ok {
		st.ETag, st.LastModified = res.etag, res.lastMod
		st.Baked, st.GMBaked, st.GMKey = localBaked(dir), gmLocalBaked(dir), gmTag
	}
	return code
}

func from13(dir string, from int, cnt *counter) int {
	man, err := fetchManifest()
	if err != nil {
		fmt.Println("ОШИБКА:", err)
		return 1
	}
	if err := formatMismatch13(dir, man); err != nil {
		fmt.Println("ОШИБКА:", err)
		return 1
	}
	sel := choose13(loadConfig(), man, nil)
	sel.seasons = map[int]bool{}
	for _, s := range man.seasonList() {
		if s >= from {
			sel.seasons[s] = true
		}
	}
	sel.seasons[man.Season] = true
	written, err := download13(man, dir, sel, cnt)
	if err != nil {
		fmt.Println("ОШИБКА:", err)
		return 1
	}
	a := readLocal13(dir)
	fmt.Printf("записано: %s\nвыгрузка %s, игроков %d, сезоны %v\n", strings.Join(written, ", "), a.Baked, a.Count, a.Seasons)
	if gm, err := syncGM(dir, man.GM, cnt); err != nil {
		fmt.Println("ГМ:", err)
	} else if gm != "" {
		fmt.Println("ГМ:", gm)
	}
	return 0
}
