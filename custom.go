package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	customSource      = "@source"
	customZipLimit    = 64 << 20
	customUnpackLimit = 128 << 20
)

var githubPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type customProject struct {
	Repo     string                   `json:"repo"`
	Asset    string                   `json:"asset"`
	Installs map[string]customInstall `json:"installs,omitempty"`
}

type customInstall struct {
	Tag     string   `json:"tag"`
	Asset   string   `json:"asset"`
	Folders []string `json:"folders"`
}

type customAsset struct {
	Name    string
	URL     string
	Pattern string
}

type customRelease struct {
	Tag    string
	Page   string
	Assets []customAsset
}

type customBundle struct {
	Name  string
	Files map[string][]byte
}

func githubRepo(input string) (string, error) {
	s := strings.TrimSpace(input)
	if strings.HasPrefix(strings.ToLower(s), "github.com/") {
		s = "https://" + s
	}
	if !strings.Contains(s, "://") && strings.Count(s, "/") == 1 {
		s = "https://github.com/" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", errors.New(tr("вставьте ссылку на публичный проект GitHub"))
	}
	validHost := strings.EqualFold(u.Host, "github.com") || strings.EqualFold(u.Host, "www.github.com")
	validURL := u.Scheme == "https" && validHost && u.User == nil
	if !validURL || strings.ContainsAny(u.Path, "\\\x00") {
		return "", errors.New(tr("поддерживаются только HTTPS-ссылки на github.com"))
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", errors.New(tr("в ссылке нужны владелец и название проекта: github.com/владелец/проект"))
	}
	owner, repo := parts[0], strings.TrimSuffix(parts[1], ".git")
	for _, p := range []string{owner, repo} {
		if !githubPart.MatchString(p) || p == "." || p == ".." {
			return "", errors.New(tr("неверное название проекта GitHub"))
		}
	}
	for _, p := range parts[2:] {
		if p == "." || p == ".." {
			return "", errors.New(tr("в ссылке недопустимый путь"))
		}
	}
	return owner + "/" + repo, nil
}

func (p customProject) key() string { return "github/" + strings.ToLower(p.Repo) }

func (p customProject) title() string { return path.Base(p.Repo) }

func customRootKey(addons string) string {
	root, _ := filepath.Abs(realPath(addons))
	return strings.ToLower(filepath.Clean(root))
}

func (p customProject) installed(addons string) customInstall {
	return p.Installs[customRootKey(addons)]
}

func (p customProject) current(addons, tag string) bool {
	st := p.installed(addons)
	return st.Tag == tag && st.Asset == p.Asset
}

func customByKey(c config, key string) (customProject, bool) {
	for _, p := range c.Custom {
		if p.key() == key {
			return p, true
		}
	}
	return customProject{}, false
}

func customRequest(raw string, limit int64, cnt *counter) ([]byte, error) {
	if !githubDownload(raw) {
		return nil, errors.New(tr("адрес скачивания должен принадлежать GitHub"))
	}
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	timeout := 2 * time.Minute
	if limit <= 4<<20 {
		timeout = 15 * time.Second
	}
	client := &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !githubDownload(req.URL.String()) {
			return errors.New(tr("GitHub перенаправил скачивание на неподдерживаемый адрес"))
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf(tr("не удалось скачать с GitHub: %w"), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusNotFound:
			return nil, errors.New(tr("проект или релиз не найден; поддерживаются публичные проекты GitHub"))
		case http.StatusForbidden, http.StatusTooManyRequests:
			return nil, errors.New(tr("GitHub ограничил запросы — повторите позже"))
		default:
			return nil, fmt.Errorf(tr("GitHub ответил %s"), resp.Status)
		}
	}
	if resp.ContentLength > limit {
		return nil, errors.New(tr("архив или ответ GitHub слишком большой"))
	}
	var reader io.Reader = io.LimitReader(resp.Body, limit+1)
	if cnt != nil {
		cnt.total.Store(max(resp.ContentLength, 0))
		reader = &countingReader{r: reader, cnt: cnt}
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf(tr("скачивание оборвалось: %w"), err)
	}
	if int64(len(data)) > limit {
		return nil, errors.New(tr("архив или ответ GitHub слишком большой"))
	}
	return data, nil
}

func githubDownload(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	switch strings.ToLower(u.Host) {
	case "github.com", "api.github.com", "codeload.github.com", "objects.githubusercontent.com",
		"release-assets.githubusercontent.com":
		return true
	}
	return false
}

func findCustomRelease(repo string) (*customRelease, error) {
	normal, err := githubRepo(repo)
	if err != nil {
		return nil, err
	}
	api := "https://api.github.com/repos/" + normal + "/releases/latest"
	data, err := customRequest(api, 4<<20, nil)
	if err != nil {
		return nil, fmt.Errorf(tr("%s: %w; нужен опубликованный стабильный релиз"), normal, err)
	}
	var rel struct {
		ghRelease
		Zipball string `json:"zipball_url"`
	}
	if err := json.Unmarshal(data, &rel); err != nil {
		return nil, errors.New(tr("GitHub вернул непонятный ответ о релизе"))
	}
	if rel.Tag == "" || rel.Draft || rel.Prerelease {
		return nil, errors.New(tr("нет опубликованного стабильного релиза"))
	}
	out := &customRelease{Tag: rel.Tag, Page: releasesPage(normal), Assets: []customAsset{}}
	for _, a := range rel.Assets {
		if customArchiveName(a.Name) && githubDownload(a.URL) {
			out.Assets = append(out.Assets, customAsset{
				Name: a.Name, URL: a.URL, Pattern: customAssetPattern(a.Name, rel.Tag),
			})
		}
	}
	if githubDownload(rel.Zipball) {
		out.Assets = append(out.Assets, customAsset{Name: customSource, URL: rel.Zipball})
	}
	if len(out.Assets) == 0 {
		return nil, errors.New(tr("у релиза нет поддерживаемого архива и ZIP исходников"))
	}
	return out, nil
}

func (r *customRelease) asset(name string) (customAsset, error) {
	for _, a := range r.Assets {
		if a.Name == name || a.Pattern == name {
			return a, nil
		}
	}
	return customAsset{}, errors.New(tr("выбранного архива больше нет в релизе — нажмите «Сменить архив»"))
}

func customAssetPattern(name, tag string) string {
	if tag != "" && strings.Contains(name, tag) {
		return strings.ReplaceAll(name, tag, "{tag}")
	}
	version := strings.TrimPrefix(tag, "v")
	if version != "" && strings.Contains(name, version) {
		return strings.ReplaceAll(name, version, "{version}")
	}
	return name
}

func customPath(name string) error {
	if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.ContainsRune(name, 0) {
		return fmt.Errorf(tr("небезопасный путь в архиве: %q"), name)
	}
	for _, part := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
		if err := customName(part); err != nil {
			return fmt.Errorf(tr("небезопасный путь в архиве: %q"), name)
		}
	}
	return nil
}

func customName(name string) error {
	bad := name == "" || name == "." || name == ".." || strings.TrimRight(name, " .") != name
	if bad || strings.ContainsAny(name, "<>:\"/\\|?*\x00") {
		return errors.New(tr("недопустимое имя папки или файла"))
	}
	for _, ch := range name {
		if ch < 32 {
			return errors.New(tr("управляющий символ в имени"))
		}
	}
	stem := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	reserved := stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL"
	stemRunes := []rune(stem)
	numbered := len(stemRunes) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT"))
	if reserved || (numbered && strings.ContainsRune("123456789¹²³", stemRunes[3])) {
		return errors.New(tr("служебное имя Windows"))
	}
	return nil
}

func unpackCustom(data []byte) ([]customBundle, error) {
	entries, err := readCustomArchive(data)
	if err != nil {
		return nil, err
	}
	roots := map[string]string{}
	for name := range entries {
		if !strings.EqualFold(path.Ext(name), ".toc") {
			continue
		}
		folder := path.Dir(name)
		base := strings.TrimSuffix(path.Base(name), path.Ext(name))
		matches := strings.EqualFold(path.Base(folder), base)
		wrapper := folder == "." || !strings.Contains(folder, "/")
		if !matches && !wrapper {
			continue
		}
		if old, ok := roots[folder]; ok && !strings.EqualFold(old, base) {
			return nil, errors.New(tr("в корне несколько разных .toc — выберите готовый архив аддона"))
		}
		roots[folder] = base
	}
	dirs := []string{}
	for dir := range roots {
		dirs = append(dirs, dir)
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) < len(dirs[j]) })
	bundles := []customBundle{}
	used := map[string]bool{}
	selected := []string{}
	for _, dir := range dirs {
		nested := false
		for _, root := range selected {
			if root == "." || strings.HasPrefix(dir, root+"/") {
				nested = true
			}
		}
		if nested {
			continue
		}
		name := roots[dir]
		if err := customName(name); err != nil {
			return nil, err
		}
		if used[strings.ToLower(name)] {
			return nil, errors.New(tr("в архиве несколько аддонов с одинаковым именем"))
		}
		used[strings.ToLower(name)] = true
		selected = append(selected, dir)
		files := map[string][]byte{}
		for full, body := range entries {
			rel := full
			if dir != "." {
				var ok bool
				rel, ok = strings.CutPrefix(full, dir+"/")
				if !ok {
					continue
				}
			}
			if strings.HasPrefix(rel, ".") || strings.Contains(rel, "/.") {
				continue
			}
			switch strings.ToLower(path.Ext(rel)) {
			case ".toc", ".lua", ".xml", ".tga", ".blp", ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".dds",
				".ogg", ".mp3", ".wav", ".ttf", ".otf", ".txt", ".md", ".json", ".csv", ".ini":
			case "":
				base := strings.ToUpper(path.Base(rel))
				if base != "LICENSE" && base != "COPYING" && base != "NOTICE" && base != "CHANGELOG" {
					return nil, fmt.Errorf(tr("%s: разрешены только файлы аддонов WoW"), rel)
				}
			default:
				return nil, fmt.Errorf(tr("%s: разрешены только файлы аддонов WoW"), rel)
			}
			if strings.EqualFold(rel, name+".toc") {
				rel = name + ".toc"
				if !bytes.Contains(body, []byte("## Interface:")) {
					return nil, fmt.Errorf(tr("%s не похож на .toc аддона WoW"), rel)
				}
			}
			files[rel] = body
		}
		bundles = append(bundles, customBundle{Name: name, Files: files})
	}
	if len(bundles) == 0 {
		return nil, errors.New(tr("в архиве не найден аддон WoW с .toc; выберите готовый архив аддона"))
	}
	sort.Slice(bundles, func(i, j int) bool { return bundles[i].Name < bundles[j].Name })
	return bundles, nil
}

func customTarget(addons, name string) (string, error) {
	if err := customName(name); err != nil {
		return "", err
	}
	root, err := filepath.Abs(addons)
	if err != nil || !strings.EqualFold(filepath.Base(root), "AddOns") {
		return "", errors.New(tr("укажите папку Interface\\AddOns"))
	}
	if !strings.EqualFold(filepath.Base(filepath.Dir(root)), "Interface") {
		return "", errors.New(tr("укажите папку Interface\\AddOns"))
	}
	if !isDir(root) {
		return "", errors.New(tr("папка AddOns не найдена"))
	}
	target := folderIn(realPath(root), name)
	if _, err := os.Lstat(target); err == nil {
		if !strings.EqualFold(filepath.Clean(realPath(target)), filepath.Clean(target)) {
			return "", errors.New(tr("папка аддона является ссылкой или junction — установка запрещена"))
		}
		st, err := os.Lstat(target)
		if err != nil || st.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || !st.IsDir() {
			return "", errors.New(tr("вместо папки аддона находится ссылка или файл"))
		}
	}
	return target, nil
}

func installCustom(addons string, p customProject, rel *customRelease, cnt *counter) (string, error) {
	if err := gameClosed(); err != nil {
		return "", err
	}
	asset, err := rel.asset(p.Asset)
	if err != nil {
		return "", err
	}
	data, err := customRequest(asset.URL, customZipLimit, cnt)
	if err != nil {
		return "", err
	}
	bundles, err := unpackCustom(data)
	if err != nil {
		return "", err
	}
	return placeCustom(addons, p, rel.Tag, bundles)
}

func placeCustom(addons string, p customProject, tag string, bundles []customBundle) (string, error) {
	if len(bundles) == 0 {
		return "", errors.New(tr("в архиве нет аддонов"))
	}
	root, err := customTarget(addons, bundles[0].Name)
	if err != nil {
		return "", err
	}
	unlock, err := lockInstall(filepath.Dir(root))
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := gameClosed(); err != nil {
		return "", err
	}
	cfg := loadConfig()
	current, ok := customByKey(cfg, p.key())
	if !ok {
		return "", errors.New(tr("проект уже убран из списка"))
	}
	if current.Asset != p.Asset {
		return "", errors.New(tr("настройки архива изменились — повторите установку"))
	}
	installed := current.installed(addons)
	if len(installed.Folders) > 0 && !sameCustomFolders(installed.Folders, bundles) {
		return "", errors.New(tr("в релизе изменился набор папок — уберите проект из списка и добавьте заново"))
	}
	targets := []string{}
	for _, b := range bundles {
		target, err := customTarget(addons, b.Name)
		if err != nil {
			return "", err
		}
		if _, native := defByName(b.Name); native || strings.EqualFold(b.Name, homeSub) {
			return "", fmt.Errorf(tr("%s управляется встроенным каталогом — замена запрещена"), b.Name)
		}
		if devFolder(target) || selfInside(target) {
			return "", fmt.Errorf(tr("%s — папка разработки или программы, установка запрещена"), b.Name)
		}
		for _, other := range cfg.Custom {
			if other.key() == p.key() {
				continue
			}
			for _, folder := range other.installed(addons).Folders {
				if strings.EqualFold(folder, b.Name) {
					return "", fmt.Errorf(tr("папкой %s уже управляет %s"), b.Name, other.Repo)
				}
			}
		}
		owned := false
		for _, folder := range installed.Folders {
			owned = owned || strings.EqualFold(folder, b.Name)
		}
		if isDir(target) && !owned {
			return "", fmt.Errorf(tr("папка %s уже существует; перенесите её вручную перед первой установкой"), b.Name)
		}
		if err := validateCustomBundle(b); err != nil {
			return "", err
		}
		targets = append(targets, target)
	}
	stage, err := os.MkdirTemp(filepath.Dir(root), ".manacode-custom-")
	if err != nil {
		return "", err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stage)
		}
	}()
	for i, b := range bundles {
		fresh := filepath.Join(stage, fmt.Sprint(i))
		for rel, body := range b.Files {
			if err := writeFile(filepath.Join(fresh, filepath.FromSlash(rel)), body); err != nil {
				return "", err
			}
		}
	}
	placed, moved := 0, map[int]bool{}
	rollback := func(cause error) error {
		var restoreErr error
		for i := placed - 1; i >= 0; i-- {
			restoreErr = errors.Join(restoreErr, os.RemoveAll(targets[i]))
		}
		for i := range moved {
			err := os.Rename(filepath.Join(stage, fmt.Sprintf("old-%d", i)), targets[i])
			restoreErr = errors.Join(restoreErr, err)
		}
		if restoreErr != nil {
			cleanup = false
			return fmt.Errorf(tr("%w; не удалось вернуть часть папок: %v; копии сохранены в %s"), cause, restoreErr, stage)
		}
		return cause
	}
	for i, target := range targets {
		if _, err := customTarget(addons, bundles[i].Name); err != nil {
			return "", rollback(err)
		}
		if isDir(target) {
			if err := os.Rename(target, filepath.Join(stage, fmt.Sprintf("old-%d", i))); err != nil {
				return "", rollback(fmt.Errorf(tr("папка %s занята: %w"), bundles[i].Name, err))
			}
			moved[i] = true
		}
		if err := os.Rename(filepath.Join(stage, fmt.Sprint(i)), target); err != nil {
			return "", rollback(err)
		}
		placed++
	}
	folders := []string{}
	for _, b := range bundles {
		folders = append(folders, b.Name)
	}
	if current.Installs == nil {
		current.Installs = map[string]customInstall{}
	}
	current.Installs[customRootKey(addons)] = customInstall{Tag: tag, Asset: p.Asset, Folders: folders}
	for i := range cfg.Custom {
		if cfg.Custom[i].key() == current.key() {
			cfg.Custom[i] = current
		}
	}
	if err := writeCustomConfig(cfg); err != nil {
		return "", rollback(err)
	}
	return tag, nil
}

func validateCustomBundle(b customBundle) error {
	if _, ok := b.Files[b.Name+".toc"]; !ok {
		return errors.New(tr("в папке нет .toc аддона"))
	}
	for rel := range b.Files {
		if err := customPath(rel); err != nil {
			return err
		}
	}
	return nil
}

func sameCustomFolders(folders []string, bundles []customBundle) bool {
	if len(folders) != len(bundles) {
		return false
	}
	names := map[string]bool{}
	for _, b := range bundles {
		names[strings.ToLower(b.Name)] = true
	}
	for _, name := range folders {
		if !names[strings.ToLower(name)] {
			return false
		}
	}
	return true
}

func writeCustomConfig(cfg config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(appHome(), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(appHome(), ".custom-config-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(appHome(), configFile))
}
