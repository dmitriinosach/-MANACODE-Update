package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	kindNone = iota
	kindOK
	kindErr
	kindNew
)

const (
	pageMain = iota
	pageTest
)

type card struct {
	def     addonDef
	rel     *otherRelease
	arch    *rhArchive
	loading bool
	note    string
	err     error
	idx     *index
	idxErr  error
	idxBusy bool
	meta    localMeta
	pick    map[int]bool
	gm      string
	gmErr   error
	man     *manifest
	sel13   v13Sel
}

type rhTest struct {
	loading bool
	idx     *testIndex
	err     error
	accs    []testAccount
	set     int
	acc     int
	busy    string
	done    string
	fail    error
	ver     string
	rec     int
}

type app struct {
	mu      sync.Mutex
	addons  string
	cfg     config
	cards   []*card
	cur     int
	page    int
	busy    string
	job     string
	cnt     *counter
	auto    bool
	autoErr error
	game    bool
	packs   map[string]bool
	test    *rhTest
	notify  func()
	self    *selfInfo
	selfErr error
	asked   bool
	restart bool
	notice  string
	dc      *discordWatch
	dcBusy  bool
	dcNote  string
	dcKind  int
	hook    string
}

type chipView struct {
	season int
	label  string
	on     bool
	locked bool
}

type gridRow struct {
	title string
	cells []gridCell
}

type gridCell struct {
	id    string
	label string
	on    bool
	avail bool
}

type packView struct {
	code    string
	label   string
	checked bool
	enabled bool
	state   string
}

type cardView struct {
	key, title, desc string
	folder, page     string
	curse            string
	warn             string
	dot              int
	short            string
	ver, verSub      string
	verKind          int
	action           string
	actionOn         bool
	primary          bool
	busy             bool
	busyText         string
	got, total       int64
	note             string
	noteKind         int
	pri              bool
	dataTitle        string
	dataSub          string
	dataKind         int
	dataAction       string
	dataOn           bool
	dataPrimary      bool
	seasons          []chipView
	seasonsOn        bool
	seasonsSub       string
	gridHeads        []string
	grid             []gridRow
	info             []string
	rh               bool
	beta             bool
	channelOn        bool
	packs            []packView
	applyOn          bool
	testOn           bool
}

type appView struct {
	addons  string
	cards   []cardView
	cur     int
	page    int
	busy    bool
	auto    bool
	autoOn  bool
	autoSub string
	autoErr string
	hours   int
	game    bool
	version string
	selfVer string
	selfErr string
	selfOn  bool
	restart bool
	notice  string
}

type testView struct {
	sets      []string
	set       int
	setInfo   string
	setOK     bool
	accs      []string
	acc       int
	installOn bool
	restoreOn bool
	busy      bool
	got       int64
	total     int64
	status    string
	kind      int
}

func newApp(addons string, notify func()) *app {
	a := &app{cnt: &counter{}, notify: notify, dc: newDiscordWatch(), hook: loadDiscord().Webhook}
	a.setAddons(addons)
	return a
}

func (a *app) setAddons(addons string) {
	a.addons = addons
	setupHome(addons)
	a.cfg = loadConfig()
	a.cards = nil
	a.test, a.page = nil, pageMain
	for _, d := range catalog {
		a.cards = append(a.cards, &card{def: d, pick: map[int]bool{}})
	}
	if a.cur >= len(a.cards) {
		a.cur = 0
	}
	a.packs = wantedPacks(a.cfg)
	if addons == "" {
		return
	}
	if _, err := ensureTask(); err != nil {
		a.autoErr = err
	}
	a.auto = autostartOn()
	pri := a.cards[0]
	pri.meta = readLocal(priDir(addons))
	for _, s := range a.cfg.Seasons {
		pri.pick[s] = true
	}
	if len(pri.pick) == 0 {
		for s := range seasonsFromGame(priDir(addons)) {
			pri.pick[s] = true
		}
	}
}

func (a *app) setNotice(s string) {
	a.mu.Lock()
	a.notice = s
	a.mu.Unlock()
	a.changed()
}

func (a *app) changed() {
	if a.notify != nil {
		a.notify()
	}
}

func (a *app) choose(addons string) {
	a.mu.Lock()
	if a.busy != "" {
		a.mu.Unlock()
		return
	}
	autoLog("окно: указана папка AddOns %s", yesNo(addons != "", addons, "— не подошла"))
	tidyAddons(addons)
	a.setAddons(addons)
	a.mu.Unlock()
	if addons != "" {
		saveSaved(addons)
	}
	a.refresh()
}

func (a *app) refresh() {
	a.mu.Lock()
	if a.addons == "" || a.busy != "" {
		a.mu.Unlock()
		return
	}
	addons, beta := a.addons, a.cfg.beta()
	var jobs []*card
	for _, c := range a.cards {
		if c.loading {
			continue
		}
		c.loading = true
		jobs = append(jobs, c)
		if c.def.kind == kindPRI {
			c.idxBusy = true
		}
	}
	askSelf := !a.asked && !selfLocked()
	a.asked = a.asked || askSelf
	a.mu.Unlock()
	a.changed()
	for _, c := range jobs {
		go a.check(c, addons, beta)
	}
	if askSelf {
		go func() {
			info, err := checkSelf()
			a.mu.Lock()
			a.self, a.selfErr, a.asked = info, err, err == nil
			a.mu.Unlock()
			a.changed()
		}()
	}
}

func (a *app) selfPage() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.self == nil {
		return ""
	}
	return a.self.page
}

func (a *app) check(c *card, addons string, beta bool) {
	ad := c.def.at(addons)
	o := checkOther(ad, c.def.kind == kindRH && beta)
	var arch *rhArchive
	if c.def.kind == kindRH {
		if r, ok := o.archive(true); ok {
			r.probe()
			arch = &r
		}
	}
	var idx *index
	var man *manifest
	var idxErr error
	if c.def.kind == kindPRI {
		if localFormat(ad.folder) >= formatV13 {
			man, idxErr = fetchManifest()
		} else {
			idx, idxErr = fetchIndex()
		}
	}
	if ad.repo != "" && o.tag == "" && !o.none {
		autoLog("сеть: GitHub не ответил про релиз %s", c.def.title)
	}
	if idxErr != nil {
		autoLog("сеть: данные %s — %v", c.def.title, idxErr)
	}
	a.mu.Lock()
	if a.addons == addons {
		c.rel, c.arch, c.loading = &o, arch, false
		if c.def.kind == kindPRI {
			c.idx, c.idxErr, c.idxBusy, c.man = idx, idxErr, false, man
			c.meta = readLocal(ad.folder)
			if man != nil {
				c.meta = readLocal13(ad.folder)
				if c.sel13.seasons == nil {
					c.sel13 = choose13(loadConfig(), man, seasonsFromGame(ad.folder))
				}
			}
			if idx != nil && len(c.pick) == 0 {
				for _, s := range seasonsOf(*idx) {
					c.pick[s] = true
				}
			}
		}
	}
	a.mu.Unlock()
	a.changed()
}

func fetchIndex() (*index, error) {
	body, err := get("index.json")
	if err != nil {
		return nil, err
	}
	return parseIndex(body)
}

func (a *app) pick(i int) {
	a.mu.Lock()
	if i >= 0 && i < len(a.cards) && a.page == pageMain {
		a.cur = i
	}
	a.mu.Unlock()
	a.changed()
}

func (a *app) step(d int) {
	a.mu.Lock()
	if a.page == pageMain {
		a.cur = (a.cur + d + len(a.cards)) % len(a.cards)
	}
	a.mu.Unlock()
	a.changed()
}

func (a *app) run(key, text string, job func() (string, error), done func(c *card, note string, err error)) bool {
	if a.busy != "" {
		return false
	}
	a.busy, a.job = key, text
	a.cnt.got.Store(0)
	a.cnt.total.Store(0)
	a.cnt.file.Store("")
	c := a.cardOf(key)
	if c != nil {
		c.note, c.err = "", nil
	}
	autoLog("окно: %s — начато", text)
	go func() {
		note, err := job()
		if err != nil {
			autoLog("окно: %s — ошибка: %v", text, err)
		} else {
			autoLog("окно: %s — %s", text, note)
		}
		a.mu.Lock()
		a.busy, a.job = "", ""
		if c != nil {
			c.note, c.err = note, err
			if done != nil {
				done(c, note, err)
			}
		}
		a.mu.Unlock()
		a.refresh()
	}()
	return true
}

func (a *app) busyKey() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.busy
}

func (a *app) cardOf(key string) *card {
	for _, c := range a.cards {
		if c.def.key == key {
			return c
		}
	}
	return nil
}

func (a *app) primaryAction() {
	a.mu.Lock()
	c := a.cards[a.cur]
	a.mu.Unlock()
	a.mainAction(c.def.key)
}

func (a *app) mainAction(key string) {
	a.mu.Lock()
	defer a.changed()
	defer a.mu.Unlock()
	c := a.cardOf(key)
	if c == nil || a.addons == "" || a.busy != "" {
		return
	}
	o := c.rel
	if o == nil || c.loading {
		return
	}
	if !o.newer || o.dev {
		a.mu.Unlock()
		a.refresh()
		a.mu.Lock()
		return
	}
	ad := c.def.at(a.addons)
	cnt := a.cnt
	src := o.zip
	if src == "" {
		src = o.url
	}
	autoLog("окно: %s %s (стоит %s) в %s, источник %s", c.def.title, o.tag, yesNo(o.local != "", o.local, "нет"), ad.folder, src)
	switch c.def.kind {
	case kindPRI:
		if o.zip == "" {
			c.err = errors.New("у релиза нет архива аддона")
			return
		}
		fresh := c.meta.Baked == ""
		idx := c.idx
		sel := a.selected(c)
		a.run(key, "PlayerRaidsInfo "+tagVersion(o.tag), func() (string, error) {
			ver, err := updateAddon(ad.folder, o.zip, cnt)
			if err != nil {
				return "", err
			}
			note := "Поставлен " + ver + " — перезапустите игру полностью"
			if fresh && localFormat(ad.folder) >= formatV13 {
				man, err := fetchManifest()
				if err != nil {
					return note, err
				}
				if _, err := download13(man, ad.folder, choose13(loadConfig(), man, seasonsFromGame(ad.folder)), cnt); err != nil {
					return note, err
				}
				return note + ", данные скачаны", nil
			}
			if fresh && idx != nil && formatMismatch(ad.folder, idx) == nil {
				cnt.got.Store(0)
				cnt.total.Store(sizeOf(*idx, sel))
				if _, err := download(*idx, ad.folder, sel, cnt); err != nil {
					return note, err
				}
				note += ", данные скачаны"
			}
			return note, nil
		}, nil)
	case kindRH:
		rel, want := *o, copyPacks(a.packs)
		a.run(key, raidHelperTitle+" "+tagVersion(o.tag), func() (string, error) {
			res, err := syncRH(rel, true, want, cnt)
			if res.ver != "" {
				return res.text() + " — перезапустите игру полностью", err
			}
			return res.text(), err
		}, nil)
	default:
		zip := o.zip
		a.run(key, c.def.title+" "+tagVersion(o.tag), func() (string, error) {
			ver, err := installPlain(ad, zip, cnt)
			if err != nil {
				return "", err
			}
			return "Поставлен " + ver + " — перезапустите игру полностью", nil
		}, nil)
	}
}

func copyPacks(m map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (a *app) selected(c *card) map[int]bool {
	sel := map[int]bool{}
	if c.idx == nil {
		return sel
	}
	for _, s := range seasonsOf(*c.idx) {
		if c.pick[s] || s == c.idx.Season {
			sel[s] = true
		}
	}
	return sel
}

func sizeOf(idx index, sel map[int]bool) int64 {
	var total int64
	for _, f := range idx.Files {
		if sel[f.Season] {
			total += f.ZipLen
		}
	}
	return total
}

func (a *app) toggleMode(id string) {
	a.mu.Lock()
	c := a.cards[0]
	if a.busy == "" && c.man != nil {
		c.sel13.modes[id] = !c.sel13.modes[id]
	}
	a.mu.Unlock()
	a.changed()
}

func copySel(s v13Sel) v13Sel {
	out := v13Sel{seasons: map[int]bool{}, modes: map[string]bool{}}
	for k, v := range s.seasons {
		out.seasons[k] = v
	}
	for k, v := range s.modes {
		out.modes[k] = v
	}
	return out
}

func (a *app) dataAction13(c *card) {
	dir := priDir(a.addons)
	man, sel, cnt := c.man, copySel(c.sel13), a.cnt
	if err := formatMismatch13(dir, man); err != nil {
		c.note, c.err = "", err
		return
	}
	cfg := loadConfig()
	cfg.V13 = sel.choice(man)
	saveConfig(cfg)
	a.cfg = cfg
	a.run("pri", "данные", func() (string, error) {
		written, err := download13(man, dir, sel, cnt)
		if err != nil {
			return "", err
		}
		note := "Всё выбранное уже скачано"
		if len(written) > 0 {
			note = "Скачано: " + strings.Join(written, ", ") + " — в игре /reload"
		}
		gm, gmErr := syncGM(dir, man.GM, cnt)
		a.mu.Lock()
		c.gm, c.gmErr = gm, gmErr
		a.mu.Unlock()
		return note, nil
	}, nil)
}

func (a *app) dataAction() {
	a.mu.Lock()
	defer a.changed()
	defer a.mu.Unlock()
	c := a.cards[0]
	if a.addons == "" || a.busy != "" {
		return
	}
	if c.man != nil {
		a.dataAction13(c)
		return
	}
	if c.idx == nil {
		return
	}
	dir := priDir(a.addons)
	idx, sel, cnt := *c.idx, a.selected(c), a.cnt
	var list []int
	for s := range sel {
		list = append(list, s)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(list)))
	cfg := loadConfig()
	cfg.Seasons, cfg.SeasonsFrom = list, nil
	saveConfig(cfg)
	a.cfg = cfg
	o := c.rel
	needAddon := formatMismatch(dir, &idx) != nil
	if needAddon && (o == nil || !o.newer || o.zip == "" || o.dev) {
		c.note, c.err = "", formatMismatch(dir, &idx)
		return
	}
	text := "данные"
	if needAddon {
		text = "аддон и данные"
	}
	a.run("pri", text, func() (string, error) {
		note := ""
		if needAddon {
			ver, err := updateAddon(dir, o.zip, cnt)
			if err != nil {
				return "", err
			}
			note = "Аддон " + ver + " поставлен, "
		}
		cnt.got.Store(0)
		cnt.total.Store(sizeOf(idx, sel))
		written, err := download(idx, dir, sel, cnt)
		if err != nil {
			return "", err
		}
		note += fmt.Sprintf("скачано: %s — в игре /reload", strings.Join(written, ", "))
		if needAddon {
			note = strings.Replace(note, "— в игре /reload", "— перезапустите игру полностью", 1)
		}
		gm, gmErr := syncGM(dir, idx.GM, cnt)
		a.mu.Lock()
		c.gm, c.gmErr = gm, gmErr
		a.mu.Unlock()
		return note, nil
	}, nil)
}

func (a *app) toggleSeason(s int) {
	a.mu.Lock()
	c := a.cards[0]
	switch {
	case a.busy != "":
	case c.man != nil && s != c.man.Season:
		c.sel13.seasons[s] = !c.sel13.seasons[s]
	case c.man == nil && c.idx != nil && s != c.idx.Season:
		c.pick[s] = !c.pick[s]
	}
	a.mu.Unlock()
	a.changed()
}

func (a *app) setBeta(beta bool) {
	a.mu.Lock()
	if a.addons == "" || a.busy != "" || a.cfg.beta() == beta {
		a.mu.Unlock()
		return
	}
	cfg := loadConfig()
	cfg.rh().Channel = ""
	if beta {
		cfg.rh().Channel = channelBeta
	}
	saveConfig(cfg)
	a.cfg = cfg
	if c := a.cardOf("rh"); c != nil {
		c.note, c.err, c.rel = "", nil, nil
	}
	a.mu.Unlock()
	a.refresh()
}

func (a *app) togglePack(code string) {
	a.mu.Lock()
	if a.busy == "" {
		a.packs[code] = !a.packs[code]
	}
	a.mu.Unlock()
	a.changed()
}

func (a *app) applyPacks() {
	a.mu.Lock()
	defer a.changed()
	defer a.mu.Unlock()
	c := a.cardOf("rh")
	if c == nil || c.rel == nil || a.busy != "" || a.addons == "" {
		return
	}
	cfg := loadConfig()
	list := []string{}
	for _, p := range roomPacks {
		if a.packs[p.code] {
			list = append(list, p.code)
		}
	}
	cfg.rh().Packs = &list
	saveConfig(cfg)
	a.cfg = cfg
	rel, want, cnt := *c.rel, copyPacks(a.packs), a.cnt
	a.run("rh", "3D-залы", func() (string, error) {
		res, err := syncRH(rel, false, want, cnt)
		if res.text() == "" && err == nil {
			return "Залы уже как отмечено", nil
		}
		return res.text(), err
	}, nil)
}

func (a *app) setAuto(on bool) {
	a.mu.Lock()
	if a.addons != "" {
		a.autoErr = setAutostart(on)
		a.auto = autostartOn()
	}
	a.mu.Unlock()
	a.changed()
}

func (a *app) toggleHours() {
	a.mu.Lock()
	if a.addons != "" {
		h := 24
		if a.cfg.auto().hours() == 24 {
			h = addonHours
		}
		setAddonHours(h)
		a.cfg = loadConfig()
	}
	a.mu.Unlock()
	a.changed()
}

func (a *app) setGame(on bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.game == on {
		return false
	}
	a.game = on
	return true
}

func tagLabel(o *otherRelease) string {
	t := tagVersion(o.tag)
	if o.pre {
		t += " (бета)"
	}
	return t
}

func (a *app) view() appView {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := appView{addons: a.addons, cur: a.cur, page: a.page, busy: a.busy != "", auto: a.auto, autoOn: a.addons != "", game: a.game, version: ownVersion()}
	v.hours = a.cfg.auto().hours()
	v.autoSub = "данные — каждые 15 минут, аддоны — " + hoursRU(v.hours)
	if a.autoErr != nil {
		v.autoErr = a.autoErr.Error()
	}
	if a.self != nil {
		v.selfVer = a.self.version
	}
	if a.selfErr != nil {
		v.selfErr = a.selfErr.Error()
	}
	v.selfOn, v.restart, v.notice = a.busy == "", a.restart, a.notice
	for _, c := range a.cards {
		v.cards = append(v.cards, a.cardView(c))
	}
	return v
}

func (a *app) cardView(c *card) cardView {
	d := c.def
	v := cardView{key: d.key, title: d.title, desc: d.desc, page: releasesPage(*d.repo), curse: d.curse, warn: d.warn}
	idle := a.busy == "" && a.addons != ""
	o := c.rel
	local := ""
	if a.addons != "" {
		ad := d.at(a.addons)
		local = ad.localVersion()
		if isDir(ad.folder) {
			v.folder = ad.folder
		}
	}
	if o != nil && strings.HasPrefix(o.url, "https://github.com/") {
		v.page = o.url
	}
	switch {
	case a.addons == "":
		v.ver, v.verSub, v.short = "—", "сначала укажите папку игры", "папка игры не найдена"
	case o == nil:
		v.ver = orDash(local)
		v.verSub, v.short = "спрашиваю GitHub…", "проверяю…"
	case o.dev:
		v.ver = orDash(o.local)
		v.verSub, v.short = "папка разработки — обновлялка её не трогает", "папка разработки"
	case o.none:
		v.ver, v.verSub = orDash(o.local), "релизов на GitHub пока нет"
		v.short = "скоро"
		if o.local != "" {
			v.short = o.local
		}
		v.action, v.actionOn = "Проверить", idle
	case o.tag == "" && o.local == "":
		v.ver, v.verSub, v.verKind = "не установлен", "GitHub не ответил — нажмите «Проверить»", kindErr
		v.short, v.dot = "GitHub не ответил", kindErr
		v.action, v.actionOn = "Проверить", idle
	case o.tag == "":
		v.ver, v.verSub, v.verKind = o.local, "GitHub не ответил — нажмите «Проверить»", kindErr
		v.short, v.dot = o.local+" · нет связи", kindErr
		v.action, v.actionOn = "Проверить", idle
	case o.missing:
		v.ver, v.verSub = "не установлен", "последняя версия "+tagLabel(o)
		v.short = "не установлен"
		v.action, v.actionOn, v.primary = "Установить", idle, true
	case o.newer:
		v.ver, v.verSub, v.verKind = o.local, "вышла "+tagLabel(o), kindNew
		v.short, v.dot = "вышла "+tagVersion(o.tag), kindNew
		v.action, v.actionOn, v.primary = "Обновить", idle, true
	default:
		v.ver, v.verSub, v.verKind = o.local, "последняя версия", kindOK
		v.short, v.dot = o.local, kindOK
		v.action, v.actionOn = "Проверить", idle
	}
	if a.busy == d.key {
		v.busy, v.busyText = true, a.job
		v.got, v.total = a.cnt.got.Load(), a.cnt.total.Load()
		v.short, v.dot = "скачиваю…", kindNew
	}
	switch {
	case c.err != nil:
		v.note, v.noteKind = c.err.Error(), kindErr
	case c.note != "":
		v.note, v.noteKind = c.note, kindOK
	}
	if v.noteKind == kindErr && !v.busy {
		v.dot = kindErr
	}
	switch d.kind {
	case kindPRI:
		a.priView(c, &v, idle, local)
	case kindRH:
		a.rhView(c, &v, idle, local)
	}
	return v
}

func releasesPage(repo string) string {
	if strings.HasPrefix(repo, "http") {
		return ""
	}
	return "https://github.com/" + repo + "/releases"
}

func orDash(s string) string {
	if s == "" {
		return "не установлен"
	}
	return s
}

func (a *app) priView(c *card, v *cardView, idle bool, local string) {
	if a.addons == "" || local == "" {
		return
	}
	v.pri = true
	dir := priDir(a.addons)
	if c.man != nil {
		a.priView13(c, v, idle, dir)
		return
	}
	m := c.meta
	switch {
	case c.idxBusy && c.idx == nil:
		v.dataTitle, v.dataSub = dataTitle(m), "спрашиваю GitHub, что нового…"
	case c.idxErr != nil:
		v.dataTitle, v.dataSub, v.dataKind = dataTitle(m), c.idxErr.Error(), kindErr
		v.dataAction, v.dataOn = "Скачать", false
	case c.idx == nil:
		v.dataTitle = dataTitle(m)
	case formatMismatch(dir, c.idx) != nil:
		v.dataTitle, v.dataSub, v.dataKind = dataTitle(m), "новые данные идут с новой версией аддона", kindNew
		v.dataAction, v.dataOn = "Обновить всё", idle && c.rel != nil && c.rel.newer && c.rel.zip != ""
		v.dataPrimary = !v.primary
		if !v.dataOn && c.rel != nil && !c.rel.newer {
			v.dataSub, v.dataKind = formatMismatch(dir, c.idx).Error(), kindErr
		}
	case m.Baked == "":
		v.dataTitle, v.dataSub, v.dataKind = "Данных нет", "отметьте сезоны и скачайте", kindNew
		v.dataAction, v.dataOn, v.dataPrimary = "Скачать", idle, !v.primary
	case m.Baked == c.idx.Baked:
		v.dataTitle, v.dataSub, v.dataKind = dataTitle(m), "свежие", kindOK
		v.dataAction, v.dataOn = "Скачать снова", idle
	default:
		v.dataTitle, v.dataSub, v.dataKind = dataTitle(m), "есть новее — "+bakedRU(c.idx.Baked), kindNew
		v.dataAction, v.dataOn, v.dataPrimary = "Обновить данные", idle, !v.primary
		if v.dot == kindOK {
			v.short, v.dot = "есть новые данные", kindNew
		}
	}
	if c.idx != nil {
		sel := a.selected(c)
		all := seasonsOf(*c.idx)
		for _, s := range all {
			label := fmt.Sprintf("Сезон %d", s)
			if s == c.idx.Season {
				label += " · текущий"
			}
			v.seasons = append(v.seasons, chipView{season: s, label: label, on: sel[s], locked: s == c.idx.Season})
		}
		v.seasonsOn = idle
		v.seasonsSub = fmt.Sprintf("Выбрано %d из %d · скачать %s", len(sel), len(all), mb(sizeOf(*c.idx, sel)))
	}
	if m.Count > 0 {
		v.info = append(v.info, "Игроков в данных: "+num(m.Count))
	}
	switch {
	case c.gmErr != nil:
		v.info = append(v.info, "ГМ-данные: "+c.gmErr.Error())
	case c.gm != "":
		v.info = append(v.info, "ГМ-данные: "+c.gm)
	case gmKey(dir) != "":
		v.info = append(v.info, "ГМ-ключ найден — ГМ-данные скачаются вместе с общими")
	}
}

func (a *app) priView13(c *card, v *cardView, idle bool, dir string) {
	man, m := c.man, c.meta
	switch {
	case formatMismatch13(dir, man) != nil:
		v.dataTitle, v.dataSub, v.dataKind = dataTitle(m), formatMismatch13(dir, man).Error(), kindErr
	case m.Baked == "":
		v.dataTitle, v.dataSub, v.dataKind = "Данных нет", "отметьте сезоны и рейды и скачайте", kindNew
		v.dataAction, v.dataOn, v.dataPrimary = "Скачать", idle, !v.primary
	case m.Baked == man.Baked:
		v.dataTitle, v.dataSub, v.dataKind = dataTitle(m), "свежие", kindOK
		v.dataAction, v.dataOn = "Скачать выбранное", idle
	default:
		v.dataTitle, v.dataSub, v.dataKind = dataTitle(m), "есть новее — "+bakedRU(man.Baked), kindNew
		v.dataAction, v.dataOn, v.dataPrimary = "Обновить данные", idle, !v.primary
		if v.dot == kindOK {
			v.short, v.dot = "есть новые данные", kindNew
		}
	}
	sel := c.sel13
	all := man.seasonList()
	for _, s := range all {
		label := fmt.Sprintf("Сезон %d", s)
		if s == man.Season {
			label += " · текущий"
		}
		v.seasons = append(v.seasons, chipView{season: s, label: label, on: sel.seasons[s] || s == man.Season, locked: s == man.Season})
	}
	v.seasonsOn = idle
	for _, col := range gridCols {
		v.gridHeads = append(v.gridHeads, col.head)
	}
	for _, z := range man.zoneOrder() {
		row := gridRow{title: man.zoneTitle(z)}
		for _, col := range gridCols {
			md := man.modeAt(z, col.size, col.diff)
			if md == nil {
				row.cells = append(row.cells, gridCell{})
				continue
			}
			var size int64
			for _, s := range all {
				if sel.seasons[s] || s == man.Season {
					if f := man.modeFile(s, md.ID); f != nil {
						size += f.ZipLen
					}
				}
			}
			label := mb(size)
			if size > 0 && size < 52429 {
				label = "< 0.1 МБ"
			}
			row.cells = append(row.cells, gridCell{id: md.ID, label: label, on: sel.modes[md.ID], avail: true})
		}
		v.grid = append(v.grid, row)
	}
	var zipSum, diskSum int64
	for _, f := range needed13(man, sel) {
		zipSum += f.ZipLen
		diskSum += f.Bytes
	}
	v.seasonsSub = "Выбрано: архивы " + mb(zipSum) + ", на диске и в памяти игры " + mb(diskSum) + ". Неизменившиеся файлы не качаются заново"
	if m.Count > 0 {
		v.info = append(v.info, "Игроков в данных: "+num(m.Count))
	}
	switch {
	case c.gmErr != nil:
		v.info = append(v.info, "ГМ-данные: "+c.gmErr.Error())
	case c.gm != "":
		v.info = append(v.info, "ГМ-данные: "+c.gm)
	case gmKey(dir) != "":
		v.info = append(v.info, "ГМ-ключ найден — ГМ-данные скачаются вместе с общими")
	}
}

func dataTitle(m localMeta) string {
	if m.Baked == "" {
		return "Данных нет"
	}
	return "Данные от " + bakedRU(m.Baked)
}

func (a *app) rhView(c *card, v *cardView, idle bool, local string) {
	if a.addons == "" {
		return
	}
	v.rh = true
	v.beta, v.channelOn = a.cfg.beta(), idle
	o := c.rel
	ver := ""
	if c.arch != nil {
		ver = tagVersion(c.arch.tag)
	}
	rh := c.def.at(a.addons)
	for _, p := range roomPacks {
		pv := packView{code: p.code, label: p.label, checked: a.packs[p.code], enabled: idle}
		pa := packAddon(rh.folder, p.code)
		have := pa.localVersion()
		size, known := int64(0), c.arch != nil && c.arch.packs != nil
		if known {
			size = c.arch.packs[p.code]
		}
		switch {
		case packDev(pa.folder):
			pv.state, pv.enabled = "папка разработки", false
		case o == nil:
		case known && size == 0 && have == "":
			pv.state, pv.enabled = "скоро", false
		case known && size == 0:
			pv.state = "стоит " + have + ", в новом релизе нет"
		case !known && have != "":
			pv.state = "стоит " + have
		case !known:
			pv.state = "размер неизвестен"
		case have == "":
			pv.state = mb(size)
		case have == ver:
			pv.state = mb(size) + " · стоит"
		default:
			pv.state = mb(size) + " · стоит " + have
		}
		v.packs = append(v.packs, pv)
	}
	v.applyOn = idle && o != nil && o.local != "" && !o.dev
	v.testOn = idle && local != ""
}

func (a *app) openTest() {
	a.mu.Lock()
	if a.addons == "" || a.busy != "" {
		a.mu.Unlock()
		return
	}
	rh := raidHelper(priDir(a.addons))
	t := &rhTest{loading: true, ver: rh.localVersion(), rec: rh.recFormat()}
	t.accs = listAccounts(accountsDir(priDir(a.addons)), loadConfig())
	a.test, a.page = t, pageTest
	a.mu.Unlock()
	a.changed()
	go func() {
		idx, err := fetchTestIndex()
		a.mu.Lock()
		if a.test == t {
			t.loading, t.idx, t.err = false, idx, err
		}
		a.mu.Unlock()
		a.changed()
	}()
}

func (a *app) closeTest() {
	a.mu.Lock()
	if a.test == nil || a.test.busy == "" {
		a.test, a.page = nil, pageMain
	}
	a.mu.Unlock()
	a.changed()
}

func (a *app) pickTest(set, acc int) {
	a.mu.Lock()
	if t := a.test; t != nil && t.busy == "" {
		if set >= 0 && t.idx != nil && set < len(t.idx.Sets) {
			t.set = set
		}
		if acc >= 0 && acc < len(t.accs) {
			t.acc = acc
		}
		t.done, t.fail = "", nil
	}
	a.mu.Unlock()
	a.changed()
}

func (a *app) testJob(install bool) {
	a.mu.Lock()
	t := a.test
	if t == nil || t.busy != "" || t.acc >= len(t.accs) {
		a.mu.Unlock()
		return
	}
	acc, dir, cnt := t.accs[t.acc], priDir(a.addons), a.cnt
	var job func() (string, error)
	if install {
		if t.idx == nil || t.set >= len(t.idx.Sets) {
			a.mu.Unlock()
			return
		}
		set := t.idx.Sets[t.set]
		if why := setFit(set, t.ver, t.rec); why != "" {
			t.done, t.fail = "", errors.New(why)
			a.mu.Unlock()
			a.changed()
			return
		}
		t.busy = "Ставлю «" + set.Title + "»"
		job = func() (string, error) { return installTestSet(dir, set, acc, cnt) }
	} else {
		t.busy = "Возвращаю свою запись"
		job = func() (string, error) { return returnMine(dir, acc) }
	}
	t.done, t.fail = "", nil
	cnt.got.Store(0)
	cnt.total.Store(0)
	a.mu.Unlock()
	a.changed()
	go func() {
		text, err := job()
		a.mu.Lock()
		t.busy, t.done, t.fail = "", text, err
		t.accs = listAccounts(accountsDir(dir), loadConfig())
		a.mu.Unlock()
		a.changed()
	}()
}

func (a *app) testView() testView {
	a.mu.Lock()
	defer a.mu.Unlock()
	t := a.test
	var v testView
	if t == nil {
		return v
	}
	v.busy = t.busy != ""
	v.set, v.acc = t.set, t.acc
	switch {
	case t.loading:
		v.setInfo = "Спрашиваю GitHub, какие есть записи…"
	case t.err != nil:
		v.setInfo = "Записи: " + t.err.Error()
	case len(t.idx.Sets) == 0:
		v.setInfo = "Записей пока нет"
	default:
		for _, s := range t.idx.Sets {
			v.sets = append(v.sets, s.Title)
		}
		s := t.idx.Sets[t.set]
		var info []string
		if s.Attempts > 0 {
			info = append(info, num(s.Attempts)+" попыток")
		}
		if s.Bytes > 0 {
			info = append(info, mb(s.Bytes)+" в игре")
		}
		if s.ZipLen > 0 {
			info = append(info, "скачать "+mb(s.ZipLen))
		}
		if why := setFit(s, t.ver, t.rec); why != "" {
			info = append(info, why)
		} else {
			v.setOK = true
			info = append(info, "подходит к "+raidHelperTitle+" "+t.ver)
		}
		v.setInfo = strings.Join(info, " · ")
	}
	for _, acc := range t.accs {
		state := "записи нет"
		if acc.exists {
			state = "своя " + mb(acc.size) + " · " + acc.mod.Format("02.01 15:04")
		}
		if acc.set != "" {
			state = "стоит тестовая " + acc.set
		}
		if acc.mines > 0 {
			state += fmt.Sprintf(" · копий своей: %d", acc.mines)
		}
		v.accs = append(v.accs, acc.name+"\t"+state)
	}
	idle := t.busy == "" && len(t.accs) > 0
	v.installOn = idle && v.setOK
	v.restoreOn = idle && t.acc < len(t.accs) && t.accs[t.acc].mines > 0
	v.got, v.total = a.cnt.got.Load(), a.cnt.total.Load()
	switch {
	case t.busy != "":
		phase, _ := a.cnt.file.Load().(string)
		v.status = t.busy + " · " + phase + " " + mb(v.got)
	case t.fail != nil:
		v.status, v.kind = "Не получилось: "+t.fail.Error(), kindErr
	case t.done != "":
		v.status, v.kind = t.done+" Зайдите в игру — запись прочитается при входе.", kindOK
	case len(t.accs) == 0:
		v.status, v.kind = "В WTF\\Account нет аккаунтов — зайдите в игру хоть раз и выйдите.", kindErr
	default:
		v.status = "Чужая запись рейда, чтобы открыть разбор без своей. Своя уходит в копию — «Вернуть свою». Игра должна быть закрыта."
	}
	return v
}

func gameDir(addons string) string {
	return filepath.Dir(filepath.Dir(addons))
}
